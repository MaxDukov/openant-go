package easy

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/anttest"
)

// recordingHandler is a tiny in-test slog.Handler that captures every
// record so tests can assert exact retry log wording, level and attrs.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// snapshot returns a copy of the captured records.
func (h *recordingHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record(nil), h.records...)
}

// find returns the first record with the given message.
func (h *recordingHandler) find(t *testing.T, msg string) slog.Record {
	t.Helper()
	for _, r := range h.snapshot() {
		if r.Message == msg {
			return r
		}
	}
	t.Fatalf("no log record with message %q; got %v", msg, h.messages())
	return slog.Record{}
}

// messages lists the captured record messages (for failure output).
func (h *recordingHandler) messages() []string {
	recs := h.snapshot()
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Message)
	}
	return out
}

// newRetryTestNode builds a node over the sim driver wired to the given
// logging handler (pattern: easy/node_test.go newTestNode).
func newRetryTestNode(t *testing.T, h *recordingHandler) (*Node, *anttest.SimDriver) {
	t.Helper()
	sim := anttest.NewSimDriver()
	n, err := NewWithDriver(sim, WithNodeLogger(slog.New(h)))
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	t.Cleanup(n.Stop)
	return n, sim
}

// newTxChannel opens a bidirectional transmit channel on the node.
func newTxChannel(t *testing.T, n *Node) *Channel {
	t.Helper()
	ch, err := n.NewChannel(ChannelBidirectionalTransmit, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	if err := ch.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ch
}

// awaitSend runs send in a goroutine and requires it to finish within a
// generous deadline (the retry loop re-arms, so a bug could hang forever).
func awaitSend(t *testing.T, send func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- send() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("send did not return within deadline")
		return nil
	}
}

// requireChannelAttr asserts the record carries a "channel" attr equal to
// the channel id, as the retry warnings do.
func requireChannelAttr(t *testing.T, r slog.Record, id byte) {
	t.Helper()
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "channel" {
			found = true
			// slog stores a byte argument as KindUint64.
			if a.Value.Kind() != slog.KindUint64 || a.Value.Uint64() != uint64(id) {
				t.Fatalf("channel attr = %v (%s), want %d", a.Value, a.Value.Kind(), id)
			}
			return false
		}
		return true
	})
	if !found {
		t.Error("record has no \"channel\" attr")
	}
}

func TestSendAcknowledgedDataRetriesOnTransferFailed(t *testing.T) {
	h := &recordingHandler{}
	n, sim := newRetryTestNode(t, h)
	ch := newTxChannel(t, n)

	// First attempt fails, the retried attempt completes (the retry loop
	// re-arms the wait, so fail-then-success sequencing works).
	go func() {
		time.Sleep(30 * time.Millisecond)
		sim.EmitAckEvent(0, ant.EventTransferTxFailed)
		time.Sleep(30 * time.Millisecond)
		sim.EmitAckEvent(0, ant.EventTransferTxCompleted)
	}()

	if err := awaitSend(t, func() error { return ch.SendAcknowledgedData([]byte{1, 2, 3, 4, 5, 6, 7, 8}) }); err != nil {
		t.Fatalf("SendAcknowledgedData: %v", err)
	}

	rec := h.find(t, "acknowledged data transfer failed, retrying")
	if rec.Level != slog.LevelWarn {
		t.Fatalf("level = %v, want Warn", rec.Level)
	}
	requireChannelAttr(t, rec, ch.ID)
}

func TestSendBurstTransferRetriesOnTransferFailed(t *testing.T) {
	h := &recordingHandler{}
	n, sim := newRetryTestNode(t, h)
	ch := newTxChannel(t, n)

	// Burst attempt: TxStart+TxFailed fails the transfer after the start
	// stage; TxStart+TxCompleted completes the retried attempt.
	go func() {
		time.Sleep(30 * time.Millisecond)
		sim.EmitAckEvent(0, ant.EventTransferTxStart)
		time.Sleep(30 * time.Millisecond)
		sim.EmitAckEvent(0, ant.EventTransferTxFailed)
		time.Sleep(30 * time.Millisecond)
		sim.EmitAckEvent(0, ant.EventTransferTxStart)
		time.Sleep(30 * time.Millisecond)
		sim.EmitAckEvent(0, ant.EventTransferTxCompleted)
	}()

	if err := awaitSend(t, func() error { return ch.SendBurstTransfer(make([]byte, 8)) }); err != nil {
		t.Fatalf("SendBurstTransfer: %v", err)
	}

	rec := h.find(t, "burst transfer failed, retrying")
	if rec.Level != slog.LevelWarn {
		t.Fatalf("level = %v, want Warn", rec.Level)
	}
	requireChannelAttr(t, rec, ch.ID)
}

func TestSendAcknowledgedDataReturnsOtherErrorsImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("wait-timeout path is slow without the shortened event interval")
	}
	h := &recordingHandler{}
	n, _ := newRetryTestNode(t, h)
	// Shorten the wait to keep the ~10 s openant timeout out of the suite.
	n.events.interval = 20 * time.Millisecond
	ch := newTxChannel(t, n)

	// No sim events at all: the wait times out with ErrWaitTimeout, which
	// is not ErrTransferFailed and must return immediately (no retry).
	start := time.Now()
	err := ch.SendAcknowledgedData([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("SendAcknowledgedData = nil, want ErrWaitTimeout")
	}
	if err != ErrWaitTimeout {
		t.Fatalf("err = %v, want ErrWaitTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("returned after %v, retry loop did not stop immediately", elapsed)
	}
	for _, msg := range h.messages() {
		if len(msg) > 8 && msg[len(msg)-8:] == "retrying" {
			t.Fatalf("unexpected retry log %q", msg)
		}
	}
}
