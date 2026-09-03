package easy

import (
	"slices"
	"testing"

	"github.com/maxdukov/openant-go/ant"
)

// TestProximitySearchAndIDList exercises the ANT proximity search and
// channel search list commands (openant gap analysis: msg 0x60, 0x59/0x5A)
// and verifies the exact wire payloads.
func TestProximitySearchAndIDList(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}

	if err := ch.SetProximitySearch(3); err != nil {
		t.Fatalf("SetProximitySearch: %v", err)
	}
	if err := ch.SetProximitySearch(0); err != nil {
		t.Fatalf("SetProximitySearch(disable): %v", err)
	}
	if err := ch.EnableChannelIDList(2); err != nil {
		t.Fatalf("EnableChannelIDList: %v", err)
	}
	if err := ch.AddChannelID(0x1234, 0x78); err != nil {
		t.Fatalf("AddChannelID: %v", err)
	}
	if err := ch.AddChannelID(0, 0x78); err == nil {
		t.Fatal("AddChannelID accepted device number 0")
	}

	want := map[ant.MessageID][]byte{
		ant.IDSetProximitySearch: {ch.ID, 0},
		ant.IDChannelIDList:      {ch.ID, 2},
		ant.IDAddChannelID:       {ch.ID, 0x34, 0x12, 0x78},
	}
	got := map[ant.MessageID][]byte{}
	for _, w := range sim.MockDriver.Writes() {
		msgs, _ := ant.ParseFrames(w)
		for _, m := range msgs {
			if _, ok := want[m.ID]; ok {
				got[m.ID] = m.Data
			}
		}
	}
	for id, payload := range want {
		if !slices.Equal(got[id], payload) {
			t.Errorf("%v data = % X, want % X", id, got[id], payload)
		}
	}
}
