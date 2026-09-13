package ant

import "fmt"

// drainTimeslot sends queued messages in the channel timeslot triggered by
// a received broadcast message. Non-burst messages are sent one per
// timeslot; burst transfers (classic and advanced) continue until the last
// packet, matching openant exactly.
func (c *Core) drainTimeslot() {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	for len(c.txQueue) > 0 {
		m := c.txQueue[0]
		c.txQueue = c.txQueue[1:]
		c.write(m)
		var last bool
		switch m.ID {
		case IDBurstTransferData:
			last = len(m.Data) > 0 && m.Data[0]&0x80 != 0
		case IDExtendedBurstData:
			// A packet shorter than the maximum terminates an advanced
			// burst (the send side appends an empty packet when needed).
			maxPkt := int(c.advBurstMax.Load())
			if maxPkt == 0 {
				maxPkt = defaultAdvBurstMax
			}
			last = len(m.Data)-2 < maxPkt
		default:
			last = true
		}
		if last {
			break
		}
	}
}

// Write sends a message immediately. Oversized payloads (> 255 bytes)
// are rejected instead of being silently truncated by the length byte.
func (c *Core) Write(m *Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeLocked(m)
}

// WriteTimeslot queues a message for transmission in the next channel
// timeslot (used for acknowledged and burst data). Oversized payloads are
// rejected.
func (c *Core) WriteTimeslot(m *Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	c.txMu.Lock()
	c.txQueue = append(c.txQueue, m)
	c.txMu.Unlock()
	return nil
}

func (c *Core) write(m *Message) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.writeLocked(m)
}

func (c *Core) writeLocked(m *Message) error {
	frame := m.Encode()
	d := c.currentDriver()
	if d == nil {
		return ErrDriverClosed
	}
	if _, err := d.Write(frame); err != nil {
		c.mWriteErrors.Add(1)
		c.log.Warn("driver write", "id", m.ID.String(), "error", err)
		return err
	}
	return nil
}

// ---- Data transmission ----

// SendBroadcastData immediately sends 8 bytes of broadcast data.
func (c *Core) SendBroadcastData(ch byte, data []byte) error {
	if len(data) != 8 {
		return fmt.Errorf("ant: broadcast data must be 8 bytes, got %d", len(data))
	}
	return c.Write(NewMessage(IDBroadcastData, append([]byte{ch}, data...)))
}

// SendAcknowledgedData queues 8 bytes of acknowledged data for the next
// timeslot.
func (c *Core) SendAcknowledgedData(ch byte, data []byte) error {
	if len(data) != 8 {
		return fmt.Errorf("ant: acknowledged data must be 8 bytes, got %d", len(data))
	}
	_ = c.WriteTimeslot(NewMessage(IDAcknowledgedData, append([]byte{ch}, data...)))
	return nil
}

// SendBurstTransferPacket queues a single burst packet; chSeq packs the
// channel number and sequence bits.
func (c *Core) SendBurstTransferPacket(chSeq byte, data []byte) {
	_ = c.WriteTimeslot(NewMessage(IDBurstTransferData, append([]byte{chSeq}, data...)))
}

// SendBurstTransfer splits data (multiple of 8 bytes) into burst packets
// with ANT sequence numbers and queues them for timeslot transmission.
func (c *Core) SendBurstTransfer(ch byte, data []byte) error {
	if len(data)%8 != 0 {
		return fmt.Errorf("ant: burst data must be a multiple of 8 bytes, got %d", len(data))
	}
	packets := len(data) / 8
	for i := 0; i < packets; i++ {
		var seq byte
		if i == 0 {
			seq = 0
		} else {
			seq = byte((i-1)%3) + 1
		}
		if i == packets-1 {
			seq |= 0b100 // last packet flag
		}
		c.SendBurstTransferPacket(ch|seq<<5, data[i*8:(i+1)*8])
	}
	return nil
}

// SendAdvancedBurst queues data as EXTENDED_BURST_DATA packets for
// timeslot transmission (advanced burst must be enabled with
// SetAdvancedBurst first; otherwise the stick default packet size is
// assumed). A terminating short packet is appended when the payload is an
// exact multiple of the packet size, per the ANT specification.
func (c *Core) SendAdvancedBurst(ch byte, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("ant: advanced burst payload is empty")
	}
	maxPkt := int(c.advBurstMax.Load())
	if maxPkt == 0 {
		maxPkt = defaultAdvBurstMax
	}
	seq := byte(0)
	for off := 0; off < len(data); off += maxPkt {
		end := min(off+maxPkt, len(data))
		c.WriteTimeslot(NewMessage(IDExtendedBurstData, append([]byte{ch, seq}, data[off:end]...)))
		seq = (seq + 1) & 0x7F
	}
	if len(data)%maxPkt == 0 {
		c.WriteTimeslot(NewMessage(IDExtendedBurstData, []byte{ch, seq}))
	}
	return nil
}
