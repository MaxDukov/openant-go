package ant

import (
	"testing"
	"time"
)

// TestCoreMetrics verifies the drop/error counters instrumented for
// openant issues #6/#111: stream resynchronisation, read failures and
// failed writes are all visible in the Metrics snapshot.
func TestCoreMetrics(t *testing.T) {
	d := newReconDriver()
	core, err := NewCore(d, WithEventHandler(func(Event) {}))
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}
	defer core.Stop()

	// A bad sync byte and a corrupted checksum both count as bad frames.
	d.Queue([]byte{0x00})
	frame := broadcastFrame(0, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	frame[len(frame)-1] ^= 0xFF
	d.Queue(frame)
	time.Sleep(200 * time.Millisecond)
	if m := core.Metrics(); m.BadFrames < 2 {
		t.Errorf("BadFrames = %d, want >= 2", m.BadFrames)
	}

	// Read failures are counted (no factory configured: plain backoff).
	d.FailReads(true)
	time.Sleep(100 * time.Millisecond)
	if m := core.Metrics(); m.ReadErrors == 0 {
		t.Error("ReadErrors = 0 after read failures")
	}

	// A write on a closed driver counts as a write error.
	d.Close()
	_ = core.Write(NewMessage(IDResetSystem, []byte{0x00}))
	time.Sleep(100 * time.Millisecond)
	if m := core.Metrics(); m.WriteErrors == 0 {
		t.Error("WriteErrors = 0 after failed write")
	}

	// The defaults must stay zero on a healthy link.
	if m := core.Metrics(); m.BurstDropped != 0 || m.Reconnects != 0 {
		t.Errorf("unexpected counters: %+v", m)
	}
}

// TestSetDriverReadTimeout verifies the optional-interface dispatch for
// openant issue #42.
func TestSetDriverReadTimeout(t *testing.T) {
	if SetDriverReadTimeout(newReconDriver(), 100*time.Millisecond) {
		t.Error("reconDriver must not claim read timeout support")
	}
	u := &usbDriver{}
	if !SetDriverReadTimeout(u, 250*time.Millisecond) {
		t.Fatal("usbDriver must implement SetReadTimeout")
	}
	u.mu.Lock()
	got := u.readTimeout
	u.mu.Unlock()
	if got != 250*time.Millisecond {
		t.Errorf("readTimeout = %v, want 250ms", got)
	}
}
