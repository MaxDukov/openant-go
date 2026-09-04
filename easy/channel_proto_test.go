package easy

import (
	"slices"
	"testing"

	"github.com/maxdukov/openant-go/ant"
)

// TestProximitySearchAndIDList exercises the ANT proximity search (both
// protocol revisions: msg 0x60 modern, 0x71 Rev 5.1) and channel search
// list commands (msg 0x59/0x5A) and verifies the exact wire payloads.
func TestProximitySearchAndIDList(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}

	for _, tc := range []struct {
		name   string
		legacy bool
		id     ant.MessageID
	}{
		{"modern", false, ant.IDSetProximitySearch},
		{"rev 5.1", true, ant.IDSetProximitySearchLegacy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n.Core.SetProtocolLegacy(tc.legacy)
			if err := ch.SetProximitySearch(3); err != nil {
				t.Fatalf("SetProximitySearch: %v", err)
			}
			if err := ch.SetProximitySearch(0); err != nil {
				t.Fatalf("SetProximitySearch(disable): %v", err)
			}
			want := map[ant.MessageID][]byte{
				tc.id: {ch.ID, 0},
			}
			got := map[ant.MessageID][]byte{}
			for _, w := range sim.MockDriver.Writes() {
				msgs, _ := ant.ParseFrames(w)
				for _, m := range msgs {
					if m.ID == tc.id {
						got[m.ID] = m.Data
					}
				}
			}
			for id, payload := range want {
				if !slices.Equal(got[id], payload) {
					t.Errorf("%v data = % X, want % X", id, got[id], payload)
				}
			}
		})
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
		ant.IDChannelIDList: {ch.ID, 2},
		ant.IDAddChannelID:  {ch.ID, 0x34, 0x12, 0x78},
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

// TestSearchSharingLibConfigAdvancedBurst exercises the proximity search,
// channel search sharing, LIB config and advanced burst config commands
// in both protocol revisions (modern and Rev 5.1) and verifies the exact
// wire payloads.
func TestSearchSharingLibConfigAdvancedBurst(t *testing.T) {
	n, sim := newTestNode(t)
	ch, err := n.NewChannel(ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}

	libFlags := ant.LIBConfigRSSI | ant.LIBConfigChannelID
	type step struct {
		id   ant.MessageID
		data []byte
	}
	for _, tc := range []struct {
		name   string
		legacy bool
		sharing,
		lib,
		advOn,
		advOff step
	}{
		{
			name:    "modern",
			legacy:  false,
			sharing: step{ant.IDChannelSearchSharing, []byte{ch.ID, 4}},
			lib:     step{ant.IDLIBConfig, []byte{ch.ID, libFlags}},
			advOn:   step{ant.IDConfigAdvancedBurst, []byte{0x00, 1, 9, 0}},
			advOff:  step{ant.IDConfigAdvancedBurst, []byte{0x00, 0, 24, 0}},
		},
		{
			name:    "rev 5.1",
			legacy:  true,
			sharing: step{ant.IDChannelSearchSharingLegacy, []byte{ch.ID, 4}},
			lib:     step{ant.IDLIBConfigLegacy, []byte{ch.ID, libFlags}},
			advOn:   step{ant.IDConfigAdvancedBurstLegacy, []byte{0x00, 1, 2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
			advOff:  step{ant.IDConfigAdvancedBurstLegacy, []byte{0x00, 0, 3, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n.Core.SetProtocolLegacy(tc.legacy)
			if !tc.legacy {
				// The node was started in the detected (legacy) mode, so
				// its serial number response shares the modern 0x61
				// advanced burst config id; drop buffered events to keep
				// the wait unambiguous.
				n.responses.reset()
			}

			if err := ch.SetSearchSharing(4); err != nil {
				t.Fatalf("SetSearchSharing: %v", err)
			}
			if err := ch.SetLIBConfig(libFlags); err != nil {
				t.Fatalf("SetLIBConfig: %v", err)
			}
			if err := ch.EnableAdvancedBurst(9); err != nil {
				t.Fatalf("EnableAdvancedBurst: %v", err)
			}
			if err := ch.EnableAdvancedBurst(300); err == nil {
				t.Fatal("EnableAdvancedBurst accepted packet size above the 24 byte maximum")
			}
			if err := ch.DisableAdvancedBurst(); err != nil {
				t.Fatalf("DisableAdvancedBurst: %v", err)
			}

			for _, want := range []step{tc.sharing, tc.lib, tc.advOn, tc.advOff} {
				found := false
				for _, w := range sim.MockDriver.Writes() {
					msgs, _ := ant.ParseFrames(w)
					for _, m := range msgs {
						if m.ID == want.id && slices.Equal(m.Data, want.data) {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("%v with data % X not found in driver writes", want.id, want.data)
				}
			}
		})
	}
}
