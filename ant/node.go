package ant

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// EventKind distinguishes protocol responses from asynchronous channel
// events (including the virtual data events).
type EventKind int

const (
	// KindResponse is a response to a configuration command, or an
	// unsolicited response (startup, capabilities, ...). Event.Code holds
	// the message id being responded to.
	KindResponse EventKind = iota
	// KindChannel is a channel event; Event.Code holds the event code
	// (1..17 physical codes, 1000/2000/3000 virtual data codes).
	KindChannel
)

// Event is a classified message emitted by the Core engine.
type Event struct {
	Kind    EventKind
	Channel byte
	Code    Code
	Data    []byte
}

// resetWait is the delay after a system reset for the stick to reboot,
// mirroring openant Ant._RESET_WAIT.
const resetWait = 1 * time.Second

// readBufferSize is the size of USB read chunks (openant uses 4096).
const readBufferSize = 4096

// eventsBuffer is the pipeline depth between the reader and the dispatcher.
const eventsBuffer = 64

// maxBurstBytes caps a reassembled burst transfer (code review PR #1,
// P2-16); real ANT-FS transfers stay well below this.
const maxBurstBytes = 1 << 20 // 1 MiB

// Core is the low-level ANT engine: it reads frames from a Driver,
// reassembles burst transfers, classifies messages into events, and
// schedules acknowledged/burst transmission in the channel timeslot. It is
// the Go equivalent of openant.base.ant.Ant.
type Core struct {
	driver Driver
	log    *slog.Logger

	handler func(Event)

	events chan Event

	writeMu sync.Mutex

	txMu    sync.Mutex
	txQueue []*Message

	// Reader-goroutine local state (no locking required).
	burst    []byte
	lastData []byte

	running atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// Option configures a Core.
type Option func(*Core)

// WithLogger sets a custom structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(c *Core) { c.log = l }
}

// WithEventHandler installs the callback invoked (from the dispatcher
// goroutine) for every classified event. It replaces openant's
// response_function/channel_event_function hooks.
func WithEventHandler(fn func(Event)) Option {
	return func(c *Core) { c.handler = fn }
}

// NewCore creates the engine around an opened driver and starts its reader
// and dispatcher goroutines. As in openant, a system reset is issued on
// start (with a 1 second wait), so the stick is in a known state.
func NewCore(d Driver, opts ...Option) (*Core, error) {
	if d == nil {
		return nil, fmt.Errorf("ant: nil driver")
	}
	c := &Core{
		driver: d,
		log:    slog.Default(),
		events: make(chan Event, eventsBuffer),
		stopCh: make(chan struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	if err := d.Open(); err != nil {
		return nil, fmt.Errorf("ant: open driver: %w", err)
	}
	c.running.Store(true)
	c.wg.Add(2)
	go c.reader()
	go c.dispatcher()
	c.log.Info("ant core started")

	// Reset the system and wait for the stick to reboot (openant does the
	// same in Ant.__init__). The startup message will arrive as a response
	// event while we sleep.
	c.ResetSystem()
	time.Sleep(resetWait)
	return c, nil
}

// Stop terminates the reader and dispatcher goroutines and closes the
// driver. Stop is idempotent.
func (c *Core) Stop() {
	if !c.running.CompareAndSwap(true, false) {
		return
	}
	close(c.stopCh)
	// Closing the driver unblocks a pending Read.
	if err := c.driver.Close(); err != nil {
		c.log.Warn("driver close", "error", err)
	}
	c.wg.Wait()
	c.log.Info("ant core stopped")
}

func (c *Core) reader() {
	defer c.wg.Done()
	buf := make([]byte, 0, readBufferSize*2)
	chunk := make([]byte, readBufferSize)
	var errDelay time.Duration // backoff on consecutive driver errors
	for c.running.Load() {
		n, err := c.driver.Read(chunk)
		if err != nil {
			if !c.running.Load() {
				return
			}
			if err == ErrTimeout {
				continue // timeout is the normal poll tick
			}
			// Persistent driver failures (stick unplugged, USB errors)
			// must not busy-spin the loop (code review PR #1, P1-9):
			// back off exponentially up to one second.
			c.log.Debug("driver read", "error", err, "backoff", errDelay)
			if errDelay < time.Second {
				errDelay = errDelay*2 + 10*time.Millisecond
			}
			time.Sleep(errDelay)
			continue
		}
		errDelay = 0
		buf = append(buf, chunk[:n]...)
		buf = c.consume(buf)
	}
}

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
				buf = buf[1:]
				continue
			default:
				c.log.Debug("resync: dropping bad frame", "error", err, "bytes", n)
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
		}
		c.burst = append(c.burst, m.Data[1:]...)
		if len(c.burst) > maxBurstBytes {
			// A misbehaving peer could stream burst packets without the
			// last-sequence flag forever; cap the buffer (code review
			// PR #1, P2-16).
			c.log.Warn("burst exceeds limit, dropping transfer", "bytes", len(c.burst), "limit", maxBurstBytes)
			c.burst = c.burst[:0]
			return
		}
		if seq&0b100 != 0 {
			// Last packet of the burst.
			c.emit(Event{Kind: KindChannel, Channel: channel, Code: EventRxBurstPacket, Data: cloneBytes(c.burst)})
			c.burst = c.burst[:0]
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
		IDAntVersion, IDCapabilities, IDSerialNumber, IDStartupMessage, IDSerialError:
		c.emit(Event{Kind: KindResponse, Code: Code(m.ID), Data: cloneBytes(m.Data)})

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

// drainTimeslot sends queued messages in the channel timeslot triggered by
// a received broadcast message. Non-burst messages are sent one per
// timeslot; burst transfers continue until the last packet flag, matching
// openant exactly.
func (c *Core) drainTimeslot() {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	for len(c.txQueue) > 0 {
		m := c.txQueue[0]
		c.txQueue = c.txQueue[1:]
		c.write(m)
		last := m.ID != IDBurstTransferData || (len(m.Data) > 0 && m.Data[0]&0x80 != 0)
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
	if _, err := c.driver.Write(frame); err != nil {
		c.log.Warn("driver write", "id", m.ID.String(), "error", err)
		return err
	}
	return nil
}

// ---- Configuration and control commands (fire and forget; the easy layer
// waits for the responses). ----

// ResetSystem resets the ANT node.
func (c *Core) ResetSystem() { _ = c.Write(NewMessage(IDResetSystem, []byte{0x00})) }

// AssignChannel assigns a channel with the given type and network.
func (c *Core) AssignChannel(ch, channelType, network byte, extAssign *byte) {
	data := []byte{ch, channelType, network}
	if extAssign != nil {
		data = append(data, *extAssign)
	}
	_ = c.Write(NewMessage(IDAssignChannel, data))
}

// UnassignChannel unassigns a channel.
func (c *Core) UnassignChannel(ch byte) {
	_ = c.Write(NewMessage(IDUnassignChannel, []byte{ch}))
}

// OpenChannel opens a previously configured channel.
func (c *Core) OpenChannel(ch byte) {
	_ = c.Write(NewMessage(IDOpenChannel, []byte{ch}))
}

// CloseChannel closes a channel.
func (c *Core) CloseChannel(ch byte) {
	_ = c.Write(NewMessage(IDCloseChannel, []byte{ch}))
}

// RequestMessage requests a specific message from the node.
func (c *Core) RequestMessage(ch byte, msgID MessageID) {
	_ = c.Write(NewMessage(IDRequestMessage, []byte{ch, byte(msgID)}))
}

// OpenRxScanMode enables continuous RX scan mode.
func (c *Core) OpenRxScanMode(ch byte) {
	_ = c.Write(NewMessage(IDOpenRxScanMode, []byte{ch, 0x01}))
}

// SetChannelID configures the channel id (device number, type and
// transmission type; 0 wildcards as a slave).
func (c *Core) SetChannelID(ch byte, deviceNum uint16, deviceType, transmissionType byte) {
	data := []byte{ch, byte(deviceNum), byte(deviceNum >> 8), deviceType, transmissionType}
	_ = c.Write(NewMessage(IDSetChannelID, data))
}

// SetChannelPeriod sets the channel messaging period in 1/32768 s units.
func (c *Core) SetChannelPeriod(ch byte, period uint16) {
	_ = c.Write(NewMessage(IDChannelPeriod, []byte{ch, byte(period), byte(period >> 8)}))
}

// SetChannelSearchTimeout sets the search timeout in 2.5 s units (255 = infinite).
func (c *Core) SetChannelSearchTimeout(ch, timeout byte) {
	_ = c.Write(NewMessage(IDChannelSearchTimeout, []byte{ch, timeout}))
}

// SetChannelRFFrequency sets the RF frequency offset from 2400 MHz.
func (c *Core) SetChannelRFFrequency(ch, freq byte) {
	_ = c.Write(NewMessage(IDChannelRFFrequency, []byte{ch, freq}))
}

// SetNetworkKey sets the key for a network number (key is 8 or 16 bytes).
func (c *Core) SetNetworkKey(network byte, key []byte) error {
	if len(key) != 8 && len(key) != 16 {
		return fmt.Errorf("ant: network key must be 8 or 16 bytes, got %d", len(key))
	}
	data := append([]byte{network}, key...)
	return c.Write(NewMessage(IDSetNetworkKey, data))
}

// SetTransmitPower sets the global transmit power (0..4).
func (c *Core) SetTransmitPower(power byte) {
	_ = c.Write(NewMessage(IDSetTransmitPower, []byte{0x00, power}))
}

// SetSearchWaveform configures the search waveform (default [0x53, 0x00]).
func (c *Core) SetSearchWaveform(ch byte, waveform []byte) {
	data := append([]byte{ch}, waveform...)
	_ = c.Write(NewMessage(IDSetSearchWaveform, data))
}

// EnableExtendedMessages enables/disables extended (16 byte) receive messages.
func (c *Core) EnableExtendedMessages(ch byte, enable bool) {
	e := byte(0)
	if enable {
		e = 1
	}
	_ = c.Write(NewMessage(IDEnableExtendedMessages, []byte{ch, e}))
}

// EnableLED enables/disables the stick LED.
func (c *Core) EnableLED(enable bool) {
	e := byte(0)
	if enable {
		e = 1
	}
	_ = c.Write(NewMessage(IDEnableLED, []byte{0x00, e}))
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

func errShortPayload(what string, want, got int) error {
	return fmt.Errorf("ant: %s payload too short: want %d bytes, got %d", what, want, got)
}
