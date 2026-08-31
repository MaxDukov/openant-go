package easy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/anttest"
)

func newTestNode(t *testing.T) (*Node, *anttest.SimDriver) {
	t.Helper()
	sim := anttest.NewSimDriver()
	n, err := NewWithDriver(sim)
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	t.Cleanup(n.Stop)
	return n, sim
}

func TestNodeCapabilitiesAndMeta(t *testing.T) {
	n, _ := newTestNode(t)
	if err := n.RequestMessage(ant.IDAntVersion); err != nil {
		t.Fatalf("RequestMessage: %v", err)
	}
	if err := n.WaitForSpecial(ant.IDCapabilities); err != nil {
		t.Fatalf("WaitForSpecial(capabilities): %v", err)
	}
	if n.AntVersion() != "1.0.0" {
		t.Fatalf("AntVersion = %q", n.AntVersion())
	}
	if n.SerialNumber() != 0x44332211 {
		t.Fatalf("SerialNumber = %#x", n.SerialNumber())
	}
	// Wait for the capabilities event to be processed (it arrives via the
	// async handler even without an explicit wait).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n.Capabilities() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n.Capabilities() == nil || n.Capabilities().MaxChannels != 8 {
		t.Fatalf("capabilities not parsed: %+v", n.Capabilities())
	}
	if n.MaxNetworks() != 2 {
		t.Fatalf("MaxNetworks = %d, want 2", n.MaxNetworks())
	}
}

func TestNodeNewChannelAndNetworkKey(t *testing.T) {
	n, _ := newTestNode(t)
	if err := n.SetNetworkKey(0x00, []byte{0xB9, 0xA5, 0x21, 0xFB, 0xBD, 0x72, 0xC3, 0x45}); err != nil {
		t.Fatalf("SetNetworkKey: %v", err)
	}
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	if ch.ID != 0 {
		t.Fatalf("channel id = %d", ch.ID)
	}
	if err := ch.SetID(0, 120, 0); err != nil {
		t.Fatalf("SetID: %v", err)
	}
	if err := ch.SetPeriod(8070); err != nil {
		t.Fatalf("SetPeriod: %v", err)
	}
	if err := ch.SetRFFrequency(57); err != nil {
		t.Fatalf("SetRFFrequency: %v", err)
	}
	if err := ch.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
}

func TestNodeBroadcastDispatch(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	var mu sync.Mutex
	got := [][]byte{}
	ch.OnBroadcastData = func(data []byte) {
		mu.Lock()
		got = append(got, append([]byte(nil), data...))
		mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := n.Start(ctx)
	t.Cleanup(func() { cancel(); <-done })

	sim.EmitBroadcast(0, []byte{0x00, 1, 2, 3, 4, 5, 6, 7})
	sim.EmitBroadcast(0, []byte{0x01, 1, 2, 3, 4, 5, 6, 7})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		l := len(got)
		mu.Unlock()
		if l == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("broadcasts received = %d, want 2", len(got))
	}
	if got[0][0] != 0x00 || got[1][0] != 0x01 {
		t.Fatalf("unexpected pages %v", got)
	}
}

func TestNodeBurstDispatch(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	got := make(chan []byte, 1)
	ch.OnBurstData = func(data []byte) { got <- append([]byte(nil), data...) }
	ctx, cancel := context.WithCancel(context.Background())
	done := n.Start(ctx)
	t.Cleanup(func() { cancel(); <-done })

	payload := make([]byte, 20) // 3 burst packets
	for i := range payload {
		payload[i] = byte(i)
	}
	sim.EmitBurst(0, payload)

	select {
	case data := <-got:
		if len(data) != 24 { // padded to multiple of 8
			t.Fatalf("burst length = %d, want 24", len(data))
		}
		for i := 0; i < 20; i++ {
			if data[i] != byte(i) {
				t.Fatalf("burst data[%d] = %d", i, data[i])
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for burst data")
	}
}

func TestChannelSendAcknowledgedRetry(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}

	// When acknowledged data is queued it is transmitted in the channel
	// timeslot; the sim never emits broadcast data to trigger the timeslot,
	// so we emit one ourselves and then the completion event.
	go func() {
		time.Sleep(100 * time.Millisecond)
		sim.EmitBroadcast(0, []byte{9, 1, 2, 3, 4, 5, 6, 7}) // triggers timeslot drain
		sim.EmitAckEvent(0, ant.EventTransferTxCompleted)
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.SendAcknowledgedData([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SendAcknowledgedData: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWaitForEventTimeout(t *testing.T) {
	n, _ := newTestNode(t)
	// Temporarily shorten the wait to keep the test fast.
	n.events.interval = 10 * time.Millisecond

	start := time.Now()
	if _, err := n.WaitForEvent(ant.EventTransferTxCompleted); err != ErrWaitTimeout {
		t.Fatalf("err = %v, want ErrWaitTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestWaitForResponseError(t *testing.T) {
	n, sim := newTestNode(t)
	// Queue a response reporting CHANNEL_IN_WRONG_STATE for ASSIGN_CHANNEL.
	sim.QueueMessage(ant.NewMessage(ant.IDChannelEvent, []byte{0x05, byte(ant.IDAssignChannel), byte(ant.ChannelInWrongState)}))
	err := n.WaitForResponse(ant.IDAssignChannel)
	if err == nil {
		t.Fatal("expected error")
	}
	re, ok := err.(*ResponseError)
	if !ok {
		t.Fatalf("err type = %T", err)
	}
	if re.Code != ant.ChannelInWrongState {
		t.Fatalf("code = %v", re.Code)
	}
}
