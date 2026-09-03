package easy

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/anttest"
)

// TestTwoNodesParallel addresses openant issues #67/#91: several nodes
// (dongles) run side by side, each with its own driver, reader goroutine
// and channel state, and data of one stick never reaches the other.
func TestTwoNodesParallel(t *testing.T) {
	sim1 := anttest.NewSimDriver()
	n1, err := NewWithDriver(sim1)
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	t.Cleanup(n1.Stop)
	sim2 := anttest.NewSimDriver()
	n2, err := NewWithDriver(sim2)
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	t.Cleanup(n2.Stop)

	ch1, err := n1.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel 1: %v", err)
	}
	ch2, err := n2.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel 2: %v", err)
	}
	ch1.OnBroadcastData = func(data []byte) {
		if data[0] != 0x11 {
			t.Errorf("node 1 got foreign data % X", data)
		}
	}
	ch2.OnBroadcastData = func(data []byte) {
		if data[0] != 0x22 {
			t.Errorf("node 2 got foreign data % X", data)
		}
	}
	if err := ch1.Open(); err != nil {
		t.Fatalf("open 1: %v", err)
	}
	if err := ch2.Open(); err != nil {
		t.Fatalf("open 2: %v", err)
	}

	sim1.EmitBroadcast(0, []byte{0x11, 1, 2, 3, 4, 5, 6, 7})
	sim2.EmitBroadcast(0, []byte{0x22, 1, 2, 3, 4, 5, 6, 7})
	time.Sleep(200 * time.Millisecond) // let the dispatcher settle
}

// TestTxTickerFallback: firmwares that never report EVENT_TX for master
// channels (e.g. ANTUSB2 BLJ06.01.01) still need the profile's pages to
// go out, so Open starts a ticker at the channel period.
func TestTxTickerFallback(t *testing.T) {
	n, _ := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalTransmit, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	var cnt atomic.Int32
	ch.OnBroadcastTxData = func([]byte) { cnt.Add(1) }
	if err := ch.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ch.Close()
	// Default period 8192/32768 = 250 ms; wait for at least two ticks.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && cnt.Load() < 2 {
		time.Sleep(50 * time.Millisecond)
	}
	if cnt.Load() < 2 {
		t.Fatalf("tx ticker fired %d times in 3 s, want >= 2", cnt.Load())
	}
}
