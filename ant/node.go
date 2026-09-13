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
