package ant

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// reconDriver is a minimal blocking driver with fault injection for
// reconnect tests.
type reconDriver struct {
	mu     sync.Mutex
	closed bool
	fail   bool
	rxBuf  []byte
	wait   chan struct{}
	writes [][]byte
}

func newReconDriver() *reconDriver {
	return &reconDriver{wait: make(chan struct{}, 1)}
}

var errRecon = errors.New("recon: simulated pipe error")

func (d *reconDriver) signal() {
	select {
	case d.wait <- struct{}{}:
	default:
	}
}

func (d *reconDriver) Open() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errors.New("recon: permanently closed")
	}
	return nil
}

func (d *reconDriver) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.signal()
	return nil
}

func (d *reconDriver) FailReads(fail bool) {
	d.mu.Lock()
	d.fail = fail
	d.mu.Unlock()
	d.signal()
}

func (d *reconDriver) Queue(b []byte) {
	d.mu.Lock()
	d.rxBuf = append(d.rxBuf, b...)
	d.mu.Unlock()
	d.signal()
}

func (d *reconDriver) Writes() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]byte, len(d.writes))
	copy(out, d.writes)
	return out
}

func (d *reconDriver) Read(p []byte) (int, error) {
	for {
		d.mu.Lock()
		if d.fail && !d.closed {
			d.mu.Unlock()
			return 0, errRecon
		}
		if len(d.rxBuf) > 0 {
			n := copy(p, d.rxBuf)
			d.rxBuf = d.rxBuf[n:]
			d.mu.Unlock()
			return n, nil
		}
		if d.closed {
			d.mu.Unlock()
			return 0, ErrDriverClosed
		}
		d.mu.Unlock()
		<-d.wait
	}
}

func (d *reconDriver) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, ErrDriverClosed
	}
	f := append([]byte(nil), p...)
	d.writes = append(d.writes, f)
	return len(p), nil
}

func broadcastFrame(ch byte, data []byte) []byte {
	return NewMessage(IDBroadcastData, append([]byte{ch}, data...)).Encode()
}

// TestCoreReconnect verifies that a fatal read error triggers a re-open,
// the hook runs, and data from the new generation reaches the handler.
func TestCoreReconnect(t *testing.T) {
	oldBase, oldMax := reconnectBaseDelay, reconnectMaxDelay
	reconnectBaseDelay, reconnectMaxDelay = 5*time.Millisecond, 20*time.Millisecond
	defer func() { reconnectBaseDelay, reconnectMaxDelay = oldBase, oldMax }()

	gen1 := newReconDriver()
	var gens []*reconDriver
	var mu sync.Mutex
	factoryCalls := 0
	hookCalled := make(chan int, 4)

	core, err := NewCore(gen1,
		WithDriverFactory(func() (Driver, error) {
			mu.Lock()
			defer mu.Unlock()
			factoryCalls++
			g := newReconDriver()
			gens = append(gens, g)
			return g, nil
		}),
		WithReconnectHook(func(attempt int, lastErr error) error {
			if !errors.Is(lastErr, errRecon) {
				t.Errorf("hook lastErr = %v, want %v", lastErr, errRecon)
			}
			hookCalled <- attempt
			return nil
		}),
		WithEventHandler(func(Event) {}),
	)
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}
	defer core.Stop()

	// Kill the first generation; the reader must reconnect to gen2.
	gen1.FailReads(true)

	select {
	case attempt := <-hookCalled:
		if attempt != 1 {
			t.Errorf("attempt = %d, want 1", attempt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconnect hook not called")
	}

	// Feed a broadcast frame through the new generation and expect the
	// channel event.
	events := make(chan Event, 8)
	core.handler = func(ev Event) {
		if ev.Kind == KindChannel {
			events <- ev
		}
	}
	mu.Lock()
	gen2 := gens[len(gens)-1]
	mu.Unlock()
	gen2.Queue(broadcastFrame(0, []byte{1, 2, 3, 4, 5, 6, 7, 8}))

	select {
	case ev := <-events:
		if ev.Code != EventRxBroadcast {
			t.Errorf("code = %v, want %v", ev.Code, EventRxBroadcast)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no data event from reconnected driver")
	}

	mu.Lock()
	calls := factoryCalls
	mu.Unlock()
	if calls != 1 {
		t.Errorf("factory calls = %d, want 1", calls)
	}
	if gen2.Writes() == nil {
		t.Error("no writes recorded on new driver")
	}
	// The reset issued after re-open must have reached the new driver.
	if len(gen2.Writes()) == 0 || gen2.Writes()[0][2] != byte(IDResetSystem) {
		t.Errorf("first write on new driver = %v, want reset system", gen2.Writes())
	}
}

// TestCoreReconnectHookRetry verifies that a failing hook makes Core retry
// the whole re-open procedure with a fresh driver.
func TestCoreReconnectHookRetry(t *testing.T) {
	oldBase, oldMax := reconnectBaseDelay, reconnectMaxDelay
	reconnectBaseDelay, reconnectMaxDelay = 5*time.Millisecond, 20*time.Millisecond
	defer func() { reconnectBaseDelay, reconnectMaxDelay = oldBase, oldMax }()

	gen1 := newReconDriver()
	var mu sync.Mutex
	factoryCalls := 0
	hookCalls := 0
	hookDone := make(chan struct{}, 4)

	core, err := NewCore(gen1,
		WithDriverFactory(func() (Driver, error) {
			mu.Lock()
			defer mu.Unlock()
			factoryCalls++
			return newReconDriver(), nil
		}),
		WithReconnectHook(func(attempt int, lastErr error) error {
			mu.Lock()
			hookCalls++
			calls := hookCalls
			mu.Unlock()
			hookDone <- struct{}{}
			if calls < 2 {
				return errors.New("restore failed (simulated)")
			}
			return nil
		}),
		WithEventHandler(func(Event) {}),
	)
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}
	defer core.Stop()

	gen1.FailReads(true)
	// First hook call fails, second must happen with a fresh driver.
	<-hookDone
	<-hookDone

	mu.Lock()
	calls, opens := hookCalls, factoryCalls
	mu.Unlock()
	if calls != 2 || opens != 2 {
		t.Errorf("hook calls = %d, factory calls = %d, want 2/2", calls, opens)
	}
}

// TestCoreReconnectGiveUpOnStop verifies that Stop interrupts an ongoing
// reconnect retry loop and that Stop returns promptly.
func TestCoreReconnectGiveUpOnStop(t *testing.T) {
	oldBase, oldMax := reconnectBaseDelay, reconnectMaxDelay
	reconnectBaseDelay, reconnectMaxDelay = 20*time.Millisecond, 50*time.Millisecond
	defer func() { reconnectBaseDelay, reconnectMaxDelay = oldBase, oldMax }()

	gen1 := newReconDriver()
	core, err := NewCore(gen1,
		WithDriverFactory(func() (Driver, error) {
			return nil, errors.New("no device (simulated)")
		}),
		WithEventHandler(func(Event) {}),
	)
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}

	gen1.FailReads(true)
	time.Sleep(50 * time.Millisecond) // enter the retry loop

	done := make(chan struct{})
	go func() {
		core.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop blocked during reconnect retry loop")
	}
}

func TestSticksFrom(t *testing.T) {
	factories := []DriverFactory{
		{
			Name: "usb2",
			List: func() []StickInfo {
				return []StickInfo{
					{Serial: "b", Bus: 1, Address: 4},
					{Serial: "", Bus: 1, Address: 3},
				}
			},
		},
		{
			Name: "usb3",
			List: func() []StickInfo {
				return []StickInfo{{Serial: "a", Bus: 2, Address: 1}}
			},
		},
		{Name: "serial"}, // no List hook
	}
	got := sticksFrom(factories)
	want := []StickInfo{
		{Serial: "", Product: "usb2", Bus: 1, Address: 3},
		{Serial: "b", Product: "usb2", Bus: 1, Address: 4},
		{Serial: "a", Product: "usb3", Bus: 2, Address: 1},
	}
	if !slices.EqualFunc(got, want, func(a, b StickInfo) bool {
		return a.Serial == b.Serial && a.Product == b.Product && a.Bus == b.Bus && a.Address == b.Address
	}) {
		t.Fatalf("sticksFrom = %v, want %v", got, want)
	}
	if s := want[0].String(); s != "usb2 serial=<unreadable> bus=1 addr=3" {
		t.Fatalf("String() = %q", s)
	}
}
