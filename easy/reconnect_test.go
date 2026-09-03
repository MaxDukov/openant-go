package easy

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/anttest"
)

// TestNodeReconnectRestoresConfiguration verifies the full reconnect path:
// a fatal driver error re-opens the stick and replays network keys and
// channel configuration; data flows to the same channel callback again.
func TestNodeReconnectRestoresConfiguration(t *testing.T) {
	sim1 := anttest.NewSimDriver()

	var mu sync.Mutex
	var sim2 *anttest.SimDriver
	reconnected := make(chan struct{}, 1)

	n, err := NewWithDriver(sim1,
		WithReopen(func() (ant.Driver, error) {
			mu.Lock()
			defer mu.Unlock()
			sim2 = anttest.NewSimDriver()
			return sim2, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	defer n.Stop()

	n.OnReconnect = func(attempt int, lastErr error) {
		reconnected <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	key := []byte{0xA8, 0xA4, 0x23, 0xB9, 0xF5, 0x5E, 0x63, 0xC1}
	if err := n.SetNetworkKey(1, key); err != nil {
		t.Fatalf("SetNetworkKey: %v", err)
	}

	gotData := make(chan []byte, 8)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 1, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	ch.OnBroadcastData = func(data []byte) { gotData <- data }
	if err := ch.SetID(123, 0x78, 0x00); err != nil {
		t.Fatalf("SetID: %v", err)
	}
	if err := ch.SetPeriod(8070); err != nil {
		t.Fatalf("SetPeriod: %v", err)
	}
	if err := ch.SetRFFrequency(57); err != nil {
		t.Fatalf("SetRFFrequency: %v", err)
	}
	if err := ch.SetProximitySearch(3); err != nil {
		t.Fatalf("SetProximitySearch: %v", err)
	}
	if err := ch.EnableChannelIDList(2); err != nil {
		t.Fatalf("EnableChannelIDList: %v", err)
	}
	if err := ch.AddChannelID(0x1234, 0x78); err != nil {
		t.Fatalf("AddChannelID: %v", err)
	}
	if err := ch.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Simulate the stick dying (unplug / USB pipe error).
	sim1.FailReads(errors.New("usb: read: LIBUSB_ERROR_PIPE"))

	select {
	case <-reconnected:
	case <-time.After(10 * time.Second):
		t.Fatal("OnReconnect not called")
	}

	mu.Lock()
	newSim := sim2
	mu.Unlock()
	if newSim == nil {
		t.Fatal("reopen factory not used")
	}

	// The fresh stick must have received the full configuration replay,
	// including the network key and search list.
	var haveAssign, haveID, havePeriod, haveRF, haveOpen, haveKey, haveProx bool
	var listSize byte
	var listAdds [][]byte
	for _, w := range newSim.Writes() {
		msgs, _ := ant.ParseFrames(w)
		for _, m := range msgs {
			switch m.ID {
			case ant.IDAssignChannel:
				haveAssign = true
			case ant.IDSetChannelID:
				haveID = true
			case ant.IDChannelPeriod:
				havePeriod = true
			case ant.IDChannelRFFrequency:
				haveRF = true
			case ant.IDOpenChannel:
				haveOpen = true
			case ant.IDSetProximitySearch:
				if len(m.Data) >= 2 && m.Data[1] == 3 {
					haveProx = true
				}
			case ant.IDChannelIDList:
				if len(m.Data) >= 2 {
					listSize = m.Data[1]
				}
			case ant.IDAddChannelID:
				listAdds = append(listAdds, append([]byte(nil), m.Data...))
			case ant.IDSetNetworkKey:
				if len(m.Data) >= 2 && m.Data[0] == 1 {
					haveKey = true
				}
			}
		}
	}
	if listSize != 2 {
		t.Errorf("channel id list size = %d, want 2", listSize)
	}
	if len(listAdds) != 1 || !slices.Equal(listAdds[0], []byte{0, 0x34, 0x12, 0x78}) {
		t.Errorf("channel id list adds = %v, want [[0 34 12 78]]", listAdds)
	}
	for name, ok := range map[string]bool{
		"assign channel":   haveAssign,
		"set channel id":   haveID,
		"channel period":   havePeriod,
		"rf frequency":     haveRF,
		"open channel":     haveOpen,
		"network key":      haveKey,
		"proximity search": haveProx,
	} {
		if !ok {
			t.Errorf("configuration replay missing %s", name)
		}
	}

	// Data from the new generation must reach the same callback.
	newSim.EmitBroadcast(0, []byte{0x0E, 1, 2, 3, 4, 5, 6, 7})
	select {
	case data := <-gotData:
		if len(data) != 8 {
			t.Errorf("data len = %d, want 8", len(data))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no broadcast routed after reconnect")
	}
}
