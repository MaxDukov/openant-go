package ant

import (
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

func errShortPayload(what string, want, got int) error {
	return fmt.Errorf("ant: %s payload too short: want %d bytes, got %d", what, want, got)
}
