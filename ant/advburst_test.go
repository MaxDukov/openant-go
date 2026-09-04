package ant

import (
	"bytes"
	"testing"
	"time"
)

// runAdvBurstCore starts a Core on a reconDriver with advanced burst
// configured for 9 byte packets and returns the driver and an event
// channel holding every EventRxBurstPacket.
func runAdvBurstCore(t *testing.T) (*reconDriver, chan Event, *Core) {
	t.Helper()
	d := newReconDriver()
	evCh := make(chan Event, 8)
	core, err := NewCore(d, WithEventHandler(func(ev Event) {
		if ev.Code == EventRxBurstPacket {
			evCh <- ev
		}
	}))
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}
	t.Cleanup(core.Stop)
	if err := core.SetAdvancedBurst(true, 9); err != nil {
		t.Fatalf("SetAdvancedBurst: %v", err)
	}
	return d, evCh, core
}

func advPacket(ch, seq byte, payload []byte) []byte {
	return NewMessage(IDExtendedBurstData, append([]byte{ch, seq}, payload...)).Encode()
}

// TestCoreAdvancedBurstReassembly reassembles EXTENDED_BURST_DATA packets
// into a single burst event: short packet terminates, empty packet
// terminates an exact multiple, sequence wrap at 127 continues, and a
// broken sequence drops the transfer.
func TestCoreAdvancedBurstReassembly(t *testing.T) {
	d, evCh, _ := runAdvBurstCore(t)

	// Two full 9-byte packets plus a 2-byte short terminating packet.
	data := bytes.Repeat([]byte{0xA5}, 20)
	d.Queue(advPacket(3, 0, data[0:9]))
	d.Queue(advPacket(3, 1, data[9:18]))
	d.Queue(advPacket(3, 2, data[18:20]))
	select {
	case ev := <-evCh:
		if ev.Channel != 3 {
			t.Errorf("channel = %d, want 3", ev.Channel)
		}
		if !bytes.Equal(ev.Data, data) {
			t.Errorf("reassembled = % X, want % X", ev.Data, data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no burst event for short-terminated transfer")
	}

	// An exact multiple is terminated by an empty packet.
	data = bytes.Repeat([]byte{0x5A}, 18)
	d.Queue(advPacket(1, 0, data[0:9]))
	d.Queue(advPacket(1, 1, data[9:18]))
	d.Queue(advPacket(1, 2, nil))
	select {
	case ev := <-evCh:
		if !bytes.Equal(ev.Data, data) {
			t.Errorf("reassembled = % X, want % X", ev.Data, data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no burst event for empty-terminated transfer")
	}

	// The sequence number wraps: 128 full-size packets run seq 0..127 and
	// the empty packet at seq 0 terminates the transfer.
	for seq := 0; seq <= 127; seq++ {
		d.Queue(advPacket(2, byte(seq), bytes.Repeat([]byte{byte(seq)}, 9)))
	}
	d.Queue(advPacket(2, 0, nil))
	select {
	case ev := <-evCh:
		if len(ev.Data) != 128*9 {
			t.Fatalf("reassembled len = %d, want %d", len(ev.Data), 128*9)
		}
		for seq := 0; seq <= 127; seq++ {
			block := ev.Data[seq*9 : (seq+1)*9]
			if !bytes.Equal(block, bytes.Repeat([]byte{byte(seq)}, 9)) {
				t.Errorf("block %d = % X", seq, block)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no burst event after sequence wrap")
	}

	// A mid-packet without a start packet is dropped silently.
	d.Queue(advPacket(4, 5, []byte{1}))
	select {
	case ev := <-evCh:
		t.Fatalf("unexpected event for mid packet: % X", ev.Data)
	case <-time.After(200 * time.Millisecond):
	}

	// A sequence gap aborts the transfer (two events must not arrive).
	d.Queue(advPacket(4, 0, bytes.Repeat([]byte{0}, 9)))
	d.Queue(advPacket(4, 3, nil)) // gap: want 1
	select {
	case ev := <-evCh:
		t.Fatalf("unexpected event for broken sequence: % X", ev.Data)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestCoreAdvancedBurstSplitTX verifies the send side: SendAdvancedBurst
// splits data into configured-size packets with rolling sequence numbers
// and appends the terminating empty packet on exact multiples.
func TestCoreAdvancedBurstSplitTX(t *testing.T) {
	d, _, core := runAdvBurstCore(t)

	data := bytes.Repeat([]byte{0x11}, 20)
	if err := core.SendAdvancedBurst(1, data); err != nil {
		t.Fatalf("SendAdvancedBurst: %v", err)
	}
	if err := core.SendAdvancedBurst(1, nil); err == nil {
		t.Error("SendAdvancedBurst accepted empty payload")
	}
	core.drainTimeslot()

	got := lastAdvBurstPackets(t, d, 3)
	want := [][]byte{
		append([]byte{1, 0}, data[0:9]...),
		append([]byte{1, 1}, data[9:18]...),
		append([]byte{1, 2}, data[18:20]...),
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("packet %d = % X, want % X", i, got[i], want[i])
		}
	}

	// An exact multiple gets the empty terminating packet.
	if err := core.SendAdvancedBurst(1, bytes.Repeat([]byte{0x22}, 18)); err != nil {
		t.Fatalf("SendAdvancedBurst: %v", err)
	}
	core.drainTimeslot()

	got = lastAdvBurstPackets(t, d, 3)
	if len(got[2]) != 2 || got[2][0] != 1 || got[2][1] != 2 {
		t.Errorf("terminating packet = % X, want [01 02]", got[2])
	}
}

// lastAdvBurstPackets drains the queue and returns the last n advanced
// burst payloads written to the driver.
func lastAdvBurstPackets(t *testing.T, d *reconDriver, n int) [][]byte {
	t.Helper()
	var got [][]byte
	for _, w := range d.Writes() {
		frames, _ := ParseFrames(w)
		for _, m := range frames {
			if m.ID == IDExtendedBurstData {
				got = append(got, m.Data)
			}
		}
	}
	if len(got) < n {
		t.Fatalf("only %d advanced burst packets written, want >= %d", len(got), n)
	}
	return got[len(got)-n:]
}
