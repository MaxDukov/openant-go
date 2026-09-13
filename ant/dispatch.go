package ant

import "errors"

// eventsBuffer is the pipeline depth between the reader and the dispatcher.
const eventsBuffer = 64

// maxBurstBytes caps a reassembled burst transfer (code review PR #1,
// P2-16); real ANT-FS transfers stay well below this.
const maxBurstBytes = 1 << 20 // 1 MiB

// defaultAdvBurstMax is the assumed advanced burst packet size (payload
// bytes per EXTENDED_BURST_DATA packet) when 0 was configured (stick
// default) or when receiving packets without a local configuration.
const defaultAdvBurstMax = 24

// consume parses as many complete frames as available and returns the
// remaining unconsumed bytes. It performs resynchronisation on bad sync
// bytes or checksum errors instead of panicking (openant asserts instead).
func (c *Core) consume(buf []byte) []byte {
	for len(buf) > 0 {
		msg, n, err := ParseFrame(buf)
		if err != nil {
			switch {
			case errors.Is(err, ErrShortFrame):
				return buf
			case errors.Is(err, ErrBadSync):
				c.log.Debug("resync: bad sync byte")
				c.mBadFrames.Add(1)
				buf = buf[1:]
				continue
			default:
				c.log.Debug("resync: dropping bad frame", "error", err, "bytes", n)
				c.mBadFrames.Add(1)
				buf = buf[n:]
				continue
			}
		}
		buf = buf[n:]
		c.handleMessage(msg)
	}
	return buf
}

func (c *Core) handleMessage(m *Message) {
	// Only fire callbacks for new data; resent data merely marks a new
	// channel timeslot (openant semantics).
	newData := !(m.ID == IDBroadcastData && equalBytes(m.Data, c.lastData))
	if newData {
		c.dispatch(m)
	} else {
		c.log.Debug("no new data this period")
	}

	// Send queued messages in the timeslot indicated by any broadcast
	// message (including duplicates).
	if m.ID == IDBroadcastData {
		c.drainTimeslot()
	}

	c.lastData = cloneBytes(m.Data)
}

func (c *Core) dispatch(m *Message) {
	switch m.ID {
	case IDBroadcastData:
		if len(m.Data) < 1 {
			return
		}
		c.emit(Event{Kind: KindChannel, Channel: m.Data[0], Code: EventRxBroadcast, Data: cloneBytes(m.Data[1:])})

	case IDAcknowledgedData:
		if len(m.Data) < 1 {
			return
		}
		c.emit(Event{Kind: KindChannel, Channel: m.Data[0], Code: EventRxAcknowledged, Data: cloneBytes(m.Data[1:])})

	case IDBurstTransferData:
		if len(m.Data) < 1 {
			return
		}
		seq := m.Data[0] >> 5
		channel := m.Data[0] & 0x1F
		if seq == 0 {
			// Start of a new burst transfer.
			c.burst = c.burst[:0]
			c.advActive = false
		}
		c.burst = append(c.burst, m.Data[1:]...)
		if len(c.burst) > maxBurstBytes {
			// A misbehaving peer could stream burst packets without the
			// last-sequence flag forever; cap the buffer (code review
			// PR #1, P2-16).
			c.log.Warn("burst exceeds limit, dropping transfer", "bytes", len(c.burst), "limit", maxBurstBytes)
			c.mBurstDropped.Add(1)
			c.burst = c.burst[:0]
			return
		}
		if seq&0b100 != 0 {
			// Last packet of the burst.
			c.emit(Event{Kind: KindChannel, Channel: channel, Code: EventRxBurstPacket, Data: cloneBytes(c.burst)})
			c.burst = c.burst[:0]
		}

	case IDExtendedBurstData: // advanced burst (Config Advanced Burst 0x61)
		if len(m.Data) < 2 {
			return
		}
		flags := m.Data[1]
		payload := m.Data[2:]
		if flags&0x80 != 0 {
			c.log.Debug("advanced burst packet carries extended data (not decoded)")
		}
		seq := flags & 0x7F
		maxPkt := int(c.advBurstMax.Load())
		if maxPkt == 0 {
			maxPkt = defaultAdvBurstMax
		}
		if !c.advActive {
			if seq != 0 {
				c.log.Debug("advanced burst packet without start, dropping", "seq", seq)
				return
			}
			c.advActive, c.advLastSeq = true, 0
			c.burst = c.burst[:0]
		} else if want := (c.advLastSeq + 1) & 0x7F; seq == want {
			// Continue the transfer (this includes the 127 -> 0 wrap).
			c.advLastSeq = seq
		} else if seq == 0 {
			// Sequence restarts at 0 mid-transfer: the peer began a new
			// burst without completing the previous one; start over.
			c.burst = c.burst[:0]
			c.advLastSeq = 0
		} else {
			c.log.Warn("advanced burst sequence mismatch, dropping transfer", "seq", seq, "want", want)
			c.mBurstDropped.Add(1)
			c.advActive = false
			c.burst = c.burst[:0]
			return
		}
		c.burst = append(c.burst, payload...)
		if len(c.burst) > maxBurstBytes {
			c.log.Warn("advanced burst exceeds limit, dropping transfer", "bytes", len(c.burst), "limit", maxBurstBytes)
			c.mBurstDropped.Add(1)
			c.burst = c.burst[:0]
			c.advActive = false
			return
		}
		if len(payload) < maxPkt {
			// A packet shorter than the configured maximum terminates the
			// transfer (senders emit an empty packet when the payload is an
			// exact multiple of the packet size).
			channel := m.Data[0]
			c.emit(Event{Kind: KindChannel, Channel: channel, Code: EventRxBurstPacket, Data: cloneBytes(c.burst)})
			c.burst = c.burst[:0]
			c.advActive = false
		}

	case IDChannelEvent: // 0x40
		if len(m.Data) < 3 {
			return
		}
		if m.Data[1] == 0x01 {
			// Asynchronous channel event; data begins with the event code.
			c.emit(Event{Kind: KindChannel, Channel: m.Data[0], Code: Code(m.Data[2]), Data: cloneBytes(m.Data[2:])})
		} else {
			// Response to a configuration command.
			c.emit(Event{Kind: KindResponse, Channel: m.Data[0], Code: Code(m.Data[1]), Data: cloneBytes(m.Data[2:])})
		}

	case IDChannelStatus, IDSetChannelID: // 0x52, 0x51
		if len(m.Data) < 1 {
			return
		}
		c.emit(Event{Kind: KindResponse, Channel: m.Data[0], Code: Code(m.ID), Data: cloneBytes(m.Data[1:])})

	case IDUnassignChannel, IDCloseChannel, IDEnableExtendedMessages,
		IDAntVersion, IDCapabilities, IDSerialNumber, IDSerialNumberNew,
		IDStartupMessage, IDSerialError:
		c.emit(Event{Kind: KindResponse, Code: Code(m.ID), Data: cloneBytes(m.Data)})
		if (m.ID == IDSerialNumber || m.ID == IDSerialNumberNew) && len(m.Data) <= 8 {
			c.detectMu.Lock()
			ch := c.detectCh
			c.detectMu.Unlock()
			if ch != nil {
				select {
				case ch <- m.Data:
				default:
				}
			}
		}

	default:
		c.log.Debug("unhandled message", "id", m.ID.String(), "data", m.Data)
	}
}

func (c *Core) emit(ev Event) {
	select {
	case c.events <- ev:
	case <-c.stopCh:
	}
}

func (c *Core) dispatcher() {
	defer c.wg.Done()
	for {
		select {
		case <-c.stopCh:
			return
		case ev := <-c.events:
			if c.handler != nil {
				c.handler(ev)
			}
		}
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
