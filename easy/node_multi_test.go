package easy

import (
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
