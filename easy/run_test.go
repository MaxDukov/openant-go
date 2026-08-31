package easy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/ant"
)

// TestRunRecoversCallbackPanic ensures a panicking user callback does not
// kill the dispatch loop (code review PR #1, P0-6).
func TestRunRecoversCallbackPanic(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	var after atomic.Int32
	ch.OnBroadcastData = func(data []byte) {
		if data[0] == 0x01 {
			panic("user callback panic")
		}
		after.Add(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := n.Start(ctx)
	t.Cleanup(func() { cancel(); <-done })

	sim.EmitBroadcast(0, []byte{0x01, 1, 2, 3, 4, 5, 6, 7}) // panics
	sim.EmitBroadcast(0, []byte{0x02, 1, 2, 3, 4, 5, 6, 7}) // must still arrive

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && after.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if after.Load() == 0 {
		t.Fatal("dispatch loop died after callback panic")
	}
}

// TestShortPayloadsDoNotPanic drives handlers with truncated payloads
// mirroring the review's P0 findings: malformed burst messages with short
// payloads (a compromised stick could emit them).
func TestShortPayloadsDoNotPanic(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	got := make(chan int, 16)
	ch.OnBurstData = func(data []byte) { got <- len(data) }
	ctx, cancel := context.WithCancel(context.Background())
	done := n.Start(ctx)
	t.Cleanup(func() { cancel(); <-done })

	// Raw burst messages with 1..7 data bytes (last-packet flag set):
	// the reassembled burst is shorter than a page.
	for l := 1; l < 8; l++ {
		payload := make([]byte, l+1) // seq byte + l data bytes
		payload[0] = 0 | 4<<5        // channel 0, seq 4 (last packet)
		payload[1] = 0x43            // beacon mark
		sim.QueueMessage(ant.NewMessage(ant.IDBurstTransferData, payload))
	}

	timeout := time.After(2 * time.Second)
	count := 0
	for count < 7 {
		select {
		case l := <-got:
			if l < 1 || l > 7 {
				t.Fatalf("unexpected burst length %d", l)
			}
			count++
		case <-timeout:
			t.Fatalf("only %d/7 bursts dispatched", count)
		}
	}
}
