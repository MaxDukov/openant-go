package ant

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ReopenFunc creates a fresh driver instance, used to re-open the device
// after a fatal driver error (USB stick unplugged, LIBUSB pipe/IO errors,
// serial port disappearing). The returned driver must not be opened yet;
// Core opens it as part of the reconnect procedure.
type ReopenFunc func() (Driver, error)

// ReconnectHook is invoked from the reader goroutine after a successful
// reconnect, before regular operation resumes. Returning an error signals
// that post-reconnect reconfiguration failed and makes Core retry the
// whole re-open procedure.
type ReconnectHook func(attempt int, lastErr error) error

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

// Reconnect timing: first retry after reconnectBaseDelay, doubling up to
// reconnectMaxDelay, indefinitely until Stop (long-running service
// semantics; openant issue #51/#122). Tests override the base delay.
var (
	reconnectBaseDelay = 500 * time.Millisecond
	reconnectMaxDelay  = 5 * time.Second
)

// driverRef boxes a Driver so the active instance can be swapped atomically
// during a reconnect (the concrete type may change between generations).
type driverRef struct {
	d Driver
}

// maxReconnectAttempts caps reconnect cycles; 0 means retry forever.
var maxReconnectAttempts = 0

// maxBurstBytes caps a reassembled burst transfer (code review PR #1,
// P2-16); real ANT-FS transfers stay well below this.
const maxBurstBytes = 1 << 20 // 1 MiB

// defaultAdvBurstMax is the assumed advanced burst packet size (payload
// bytes per EXTENDED_BURST_DATA packet) when 0 was configured (stick
// default) or when receiving packets without a local configuration.
const defaultAdvBurstMax = 24

// Metrics is a snapshot of the drop/error counters instrumented by Core
// (openant issues #6/#111 "missed readings"): they tell apart a noisy USB
// link (bad frames, read errors) from application-level data loss (dropped
// burst transfers, failed writes, stick reconnects).
type Metrics struct {
	// BadFrames counts bytes/frames dropped during stream resynchronisation
	// (bad sync byte or checksum): a symptom of USB noise or a wedged host
	// controller.
	BadFrames uint64
	// BurstDropped counts burst transfers discarded because they exceeded
	// maxBurstBytes (misbehaving peer or a stalled reader).
	BurstDropped uint64
	// ReadErrors counts driver read failures (excluding timeouts); with a
	// driver factory configured every one of them triggers a reconnect.
	ReadErrors uint64
	// WriteErrors counts failed writes to the driver.
	WriteErrors uint64
	// Reconnects counts completed re-open cycles.
	Reconnects uint64
}

// Core is the low-level ANT engine: it reads frames from a Driver,
// reassembles burst transfers, classifies messages into events, and
// schedules acknowledged/burst transmission in the channel timeslot. It is
// the Go equivalent of openant.base.ant.Ant.
type Core struct {
	driver atomic.Pointer[driverRef]
	log    *slog.Logger

	handler func(Event)

	// reopen, when set, enables automatic reconnect on fatal driver
	// errors; hook runs after the new driver is opened and reset.
	reopen ReopenFunc
	hook   ReconnectHook

	// reconnecting guards a single reconnectLoop; gen increments on
	// every successful driver swap so the reader can drop stale state.
	reconnecting atomic.Bool
	gen          atomic.Uint32

	// Drop/error counters (see Metrics). Updated from the reader and
	// reconnect goroutines, read from anywhere.
	mBadFrames    atomic.Uint64
	mBurstDropped atomic.Uint64
	mReadErrors   atomic.Uint64
	mWriteErrors  atomic.Uint64
	mReconnects   atomic.Uint64

	events chan Event

	writeMu sync.Mutex

	txMu    sync.Mutex
	txQueue []*Message

	// protoLegacy selects the Rev 5.1 (nRF24AP2/ANTUSB2-era) spellings of
	// the messages whose ids changed in later protocol revisions
	// (proximity search, LIB config, search sharing, advanced burst).
	// It is set once at start-up (see DetectProtocol) and read from the
	// send paths.
	protoLegacy atomic.Bool

	// detectCh, when non-nil, receives every raw SERIAL_NUMBER-class
	// response for DetectProtocol. Guarded by detectMu.
	detectMu sync.Mutex
	detectCh chan []byte

	// Advanced burst packet size configured with SetAdvancedBurst
	// (0 = not configured, sender/receiver uses defaultAdvBurstMax).
	advBurstMax atomic.Uint32

	// Reader-goroutine local state (no locking required).
	burst    []byte
	lastData []byte

	// Advanced burst reassembly state (reader goroutine only).
	advActive  bool
	advLastSeq byte

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

// WithDriverFactory enables automatic device re-connect (openant issues
// #51/#122): on fatal driver errors Core closes the dead driver, re-creates
// it via fn, opens it, resets the stick and invokes the WithReconnectHook
// callback before resuming. Without this option driver errors only back
// off, as before.
func WithDriverFactory(fn ReopenFunc) Option {
	return func(c *Core) { c.reopen = fn }
}

// WithReconnectHook sets the callback invoked after a successful
// re-connect (new driver opened, system reset issued). Use it to restore
// channel configuration: the stick loses all state on power cycle. A
// returned error makes Core retry the re-open procedure.
func WithReconnectHook(fn ReconnectHook) Option {
	return func(c *Core) { c.hook = fn }
}

// NewCore creates the engine around an opened driver and starts its reader
// and dispatcher goroutines. As in openant, a system reset is issued on
// start (with a 1 second wait), so the stick is in a known state.
func NewCore(d Driver, opts ...Option) (*Core, error) {
	if d == nil {
		return nil, fmt.Errorf("ant: nil driver")
	}
	c := &Core{
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
	c.driver.Store(&driverRef{d: d})
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
	if d := c.currentDriver(); d != nil {
		if err := d.Close(); err != nil {
			c.log.Warn("driver close", "error", err)
		}
	}
	c.wg.Wait()
	c.log.Info("ant core stopped")
}

// currentDriver returns the active driver (nil only if Core is being torn
// down between generations).
func (c *Core) currentDriver() Driver {
	if r := c.driver.Load(); r != nil {
		return r.d
	}
	return nil
}

// Driver returns the active driver, e.g. to configure driver-specific
// behaviour (see SetDriverReadTimeout).
func (c *Core) Driver() Driver { return c.currentDriver() }

func (c *Core) reader() {
	defer c.wg.Done()
	buf := make([]byte, 0, readBufferSize*2)
	chunk := make([]byte, readBufferSize)
	myGen := c.gen.Load()
	var errDelay time.Duration // backoff on consecutive driver errors
	for c.running.Load() {
		d := c.currentDriver()
		if d == nil {
			// Torn down between generations; wait for a swap or stop.
			select {
			case <-c.stopCh:
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		n, err := d.Read(chunk)
		if err != nil {
			if !c.running.Load() {
				return
			}
			if err == ErrTimeout {
				continue // timeout is the normal poll tick
			}
			c.mReadErrors.Add(1)
			// Fatal driver failure (stick unplugged, USB pipe/IO error,
			// serial port gone): with a driver factory configured, hand
			// over to the reconnect supervisor. It swaps the driver and
			// restores the stick configuration while this goroutine
			// keeps serving the new driver so the hook's response waits
			// can complete.
			if c.reopen != nil {
				if c.reconnecting.CompareAndSwap(false, true) {
					c.wg.Add(1)
					go c.reconnectLoop(err)
				}
				select {
				case <-c.stopCh:
					return
				case <-time.After(10 * time.Millisecond):
				}
				continue
			}
			// Persistent driver failures must not busy-spin the loop
			// (code review PR #1, P1-9): back off exponentially up to
			// one second.
			c.log.Debug("driver read", "error", err, "backoff", errDelay)
			if errDelay < time.Second {
				errDelay = errDelay*2 + 10*time.Millisecond
			}
			time.Sleep(errDelay)
			continue
		}
		errDelay = 0
		// A new generation means a fresh stick: drop any state left
		// over from the dead one, including a partially read frame.
		if g := c.gen.Load(); g != myGen {
			buf = buf[:0]
			c.burst = c.burst[:0]
			c.lastData = nil
			c.advActive = false
			myGen = g
		}
		buf = append(buf, chunk[:n]...)
		buf = c.consume(buf)
	}
}

// reconnectLoop closes the dead driver and re-opens a fresh one, retrying
// with exponential backoff until it succeeds or Core is stopped. It runs
// in its own goroutine so the reader can continue serving the new driver
// while the hook restores the configuration. The driver pointer is swapped
// BEFORE the hook: response waits inside the hook require an active reader.
func (c *Core) reconnectLoop(cause error) {
	defer c.wg.Done()
	defer c.reconnecting.Store(false)

	c.log.Warn("driver failure, reconnecting", "error", cause)
	if d := c.currentDriver(); d != nil {
		_ = d.Close() // best effort; the device may already be gone
	}

	delay := reconnectBaseDelay
	for attempt := 1; maxReconnectAttempts == 0 || attempt <= maxReconnectAttempts; attempt++ {
		select {
		case <-c.stopCh:
			return
		case <-time.After(delay):
		}
		if !c.running.Load() {
			return
		}

		nd, err := c.reopen()
		if err != nil {
			c.log.Debug("re-open driver", "attempt", attempt, "error", err)
		} else if err := nd.Open(); err != nil {
			c.log.Debug("open driver", "attempt", attempt, "error", err)
		} else {
			c.driver.Store(&driverRef{d: nd})
			c.gen.Add(1) // reader drops stale state on the next frame
			c.mReconnects.Add(1)
			c.ResetSystem()
			time.Sleep(resetWait)
			if c.hook != nil {
				if herr := c.hook(attempt, cause); herr != nil {
					c.log.Warn("reconnect hook failed, retrying", "attempt", attempt, "error", herr)
					_ = nd.Close()
					delay = c.nextDelay(delay)
					continue
				}
			}
			c.log.Info("driver reconnected", "attempt", attempt)
			return
		}
		delay = c.nextDelay(delay)
	}
}

func (c *Core) nextDelay(delay time.Duration) time.Duration {
	if delay *= 2; delay > reconnectMaxDelay {
		return reconnectMaxDelay
	}
	return delay
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

// Metrics returns a snapshot of the drop/error counters (openant issues
// #6/#111).
func (c *Core) Metrics() Metrics {
	return Metrics{
		BadFrames:    c.mBadFrames.Load(),
		BurstDropped: c.mBurstDropped.Load(),
		ReadErrors:   c.mReadErrors.Load(),
		WriteErrors:  c.mWriteErrors.Load(),
		Reconnects:   c.mReconnects.Load(),
	}
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

// SetProximitySearch limits the search radius of a searching slave channel:
// 0 disables the proximity search (normal search), 1..255 restricts it to
// the given number of signal bins (~dB of RX attenuation). Useful to pick
// the closest sensor among several identical ones.
//
// The message id depends on the protocol revision (0x60 modern,
// 0x71 Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetProximitySearch(ch, threshold byte) {
	id := IDSetProximitySearch
	if c.protoLegacy.Load() {
		id = IDSetProximitySearchLegacy
	}
	_ = c.Write(NewMessage(id, []byte{ch, threshold}))
}

// SetChannelIDList switches the channel to list-based search matching: the
// stick then only connects to the device IDs added with AddChannelID
// (up to size entries), instead of matching the single channel id set with
// SetChannelID.
func (c *Core) SetChannelIDList(ch, size byte) {
	_ = c.Write(NewMessage(IDChannelIDList, []byte{ch, size}))
}

// AddChannelID adds one entry to the channel search list (see
// SetChannelIDList). deviceNum 0 is not allowed here; use deviceType 0 as
// a type wildcard.
func (c *Core) AddChannelID(ch byte, deviceNum uint16, deviceType byte) {
	_ = c.Write(NewMessage(IDAddChannelID, []byte{ch, byte(deviceNum), byte(deviceNum >> 8), deviceType}))
}

// SetSearchSharing makes several channels share one search: the shared
// search runs every cyclesPerSearch channel periods on each channel in
// turn, saving bandwidth and battery when many slave channels search
// simultaneously. 0 disables search sharing.
//
// The message id depends on the protocol revision (0x53 modern,
// 0x81 Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetSearchSharing(ch, cyclesPerSearch byte) {
	id := IDChannelSearchSharing
	if c.protoLegacy.Load() {
		id = IDChannelSearchSharingLegacy
	}
	_ = c.Write(NewMessage(id, []byte{ch, cyclesPerSearch}))
}

// LIBConfig flag bits for SetLIBConfig: they select what is appended to
// extended RX data messages (identical in both protocol revisions).
const (
	LIBConfigRxTimestamp byte = 0x20
	LIBConfigRSSI        byte = 0x40
	LIBConfigChannelID   byte = 0x80
)

// SetLIBConfig sets the library configuration: the flag bits
// (LIBConfigRxTimestamp, LIBConfigRSSI, LIBConfigChannelID) select which
// extended data (timestamp, RSSI, channel ID) is appended to received
// data messages.
//
// The message id depends on the protocol revision (0x71 modern,
// 0x6E Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetLIBConfig(ch, config byte) {
	id := IDLIBConfig
	if c.protoLegacy.Load() {
		id = IDLIBConfigLegacy
	}
	_ = c.Write(NewMessage(id, []byte{ch, config}))
}

// SetProtocolLegacy forces the Rev 5.1 message spellings (nRF24AP2 /
// ANTUSB2-era devices: proximity search 0x71, LIB config 0x6E, search
// sharing 0x81, advanced burst config 0x78). Modern firmware uses 0x60 /
// 0x71 / 0x53 / 0x61 respectively. DetectProtocol chooses automatically.
func (c *Core) SetProtocolLegacy(legacy bool) {
	c.protoLegacy.Store(legacy)
}

// ProtocolLegacy reports whether Rev 5.1 message spellings are in use.
func (c *Core) ProtocolLegacy() bool { return c.protoLegacy.Load() }

// DetectProtocol auto-detects the protocol revision of the stick: legacy
// (Rev 5.1) firmware answers a serial number request (0x61) with the
// 4-byte serial number, while modern firmware either answers with the
// 3-byte advanced burst configuration or does not implement that request
// at all (in which case a 0x3F serial request is tried). It returns true
// when the stick uses the Rev 5.1 spellings. Call it once right after
// NewCore, before any channels are configured; easy.New does this
// automatically.
func (c *Core) DetectProtocol(timeout time.Duration) bool {
	ch := make(chan []byte, 4)
	c.detectMu.Lock()
	c.detectCh = ch
	c.detectMu.Unlock()
	defer func() {
		c.detectMu.Lock()
		c.detectCh = nil
		c.detectMu.Unlock()
	}()

	_ = c.Write(NewMessage(IDRequestMessage, []byte{0x00, byte(IDSerialNumber)}))
	select {
	case data := <-ch:
		// 4 bytes = serial number (Rev 5.1); 3 bytes = the modern
		// firmware reporting its advanced burst configuration.
		c.protoLegacy.Store(len(data) == 4)
		return c.protoLegacy.Load()
	case <-time.After(timeout):
		// No answer at 0x61: try the modern serial number request.
		_ = c.Write(NewMessage(IDRequestMessage, []byte{0x00, byte(IDSerialNumberNew)}))
		select {
		case <-ch:
			c.protoLegacy.Store(false)
			return false
		case <-time.After(timeout):
			// Undetectable; keep the modern default.
			return c.protoLegacy.Load()
		}
	}
}

// SetAdvancedBurst enables or disables advanced burst transfers on the
// node with maxPacketSize payload bytes per packet. maxPacketSize is
// rounded to what the device supports: modern firmware accepts any size
// up to 24 bytes, Rev 5.1 devices support 8, 16 or 24 bytes only (0 = the
// 24 byte maximum in both cases). When enabled, data can be sent with
// SendAdvancedBurst and received transfers are reassembled into regular
// burst events.
//
// The configuration message id depends on the protocol revision (0x61
// modern, 0x78 Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetAdvancedBurst(enabled bool, maxPacketSize uint16) error {
	if maxPacketSize > 24 {
		return fmt.Errorf("ant: advanced burst packet size %d exceeds the 24 byte maximum", maxPacketSize)
	}
	e := byte(0)
	if enabled {
		e = 1
	}
	var err error
	if c.protoLegacy.Load() {
		// Rev 5.1: [filler][enable][max packet enum][required features
		// (3 bytes)][optional features (3 bytes)].
		if maxPacketSize == 0 {
			maxPacketSize = 24
		}
		enum := byte(1)
		switch {
		case maxPacketSize > 16:
			enum = 3
		case maxPacketSize > 8:
			enum = 2
		}
		err = c.Write(NewMessage(IDConfigAdvancedBurstLegacy, []byte{
			0x00, e, enum, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}))
	} else {
		if maxPacketSize == 0 {
			maxPacketSize = 24
		}
		err = c.Write(NewMessage(IDConfigAdvancedBurst, []byte{0x00, e, byte(maxPacketSize), byte(maxPacketSize >> 8)}))
	}
	if err != nil {
		return err
	}
	if !enabled {
		c.advBurstMax.Store(0)
		return nil
	}
	c.advBurstMax.Store(uint32(maxPacketSize))
	return nil
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
