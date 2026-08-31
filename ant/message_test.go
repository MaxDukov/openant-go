package ant

import (
	"bytes"
	"errors"
	"testing"
)

func TestMessageEncodeParseRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		id   MessageID
		data []byte
	}{
		{"reset", IDResetSystem, []byte{0x00}},
		{"network key", IDSetNetworkKey, append([]byte{0x00}, make([]byte, 8)...)},
		{"broadcast", IDBroadcastData, []byte{0x00, 1, 2, 3, 4, 5, 6, 7, 8}},
		{"empty payload", IDOpenChannel, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMessage(tt.id, tt.data)
			frame := m.Encode()
			if frame[0] != SyncByte {
				t.Fatalf("sync byte = %#x", frame[0])
			}
			if frame[1] != byte(len(tt.data)) {
				t.Fatalf("length = %d, want %d", frame[1], len(tt.data))
			}
			got, n, err := ParseFrame(frame)
			if err != nil {
				t.Fatalf("ParseFrame: %v", err)
			}
			if n != len(frame) {
				t.Fatalf("consumed = %d, want %d", n, len(frame))
			}
			if got.ID != tt.id {
				t.Fatalf("id = %#x, want %#x", got.ID, tt.id)
			}
			if !bytes.Equal(got.Data, tt.data) {
				t.Fatalf("data = % X, want % X", got.Data, tt.data)
			}
		})
	}
}

func TestParseFrameKnownGood(t *testing.T) {
	// RESET_SYSTEM message as used by openant tests: [0xa4, 0x01, 0x4a, 0x00, 0xef].
	frame := []byte{0xA4, 0x01, 0x4A, 0x00, 0xEF}
	m, n, err := ParseFrame(frame)
	if err != nil {
		t.Fatalf("ParseFrame: %v", err)
	}
	if n != len(frame) {
		t.Fatalf("consumed = %d", n)
	}
	if m.ID != IDResetSystem || !bytes.Equal(m.Data, []byte{0x00}) {
		t.Fatalf("unexpected message %+v", m)
	}

	// Channel event response: channel 0, response to 0x46 (SET_NETWORK_KEY),
	// code 0 (RESPONSE_NO_ERROR). XOR of frame bytes = 0xA1.
	frame2 := []byte{0xA4, 0x03, 0x40, 0x00, 0x46, 0x00, 0xA1}
	m2, _, err := ParseFrame(frame2)
	if err != nil {
		t.Fatalf("ParseFrame: %v", err)
	}
	if m2.ID != IDChannelEvent || !bytes.Equal(m2.Data, []byte{0x00, 0x46, 0x00}) {
		t.Fatalf("unexpected message %+v", m2)
	}
}

func TestParseFrameShort(t *testing.T) {
	frame := []byte{0xA4, 0x09, 0x4E}
	if _, _, err := ParseFrame(frame); !errors.Is(err, ErrShortFrame) {
		t.Fatalf("err = %v, want ErrShortFrame", err)
	}
}

func TestParseFrameBadSync(t *testing.T) {
	frame := []byte{0x53, 0x00, 0x00, 0x00}
	_, n, err := ParseFrame(frame)
	if !errors.Is(err, ErrBadSync) {
		t.Fatalf("err = %v, want ErrBadSync", err)
	}
	if n != 1 {
		t.Fatalf("skip = %d, want 1", n)
	}
}

func TestParseFrameBadChecksum(t *testing.T) {
	frame := []byte{0xA4, 0x01, 0x4A, 0x00, 0xFF}
	_, n, err := ParseFrame(frame)
	if !errors.Is(err, ErrBadChecksum) {
		t.Fatalf("err = %v, want ErrBadChecksum", err)
	}
	if n != 5 {
		t.Fatalf("skip = %d, want 5", n)
	}
}

func TestParseFramesResync(t *testing.T) {
	good := NewMessage(IDBroadcastData, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8}).Encode()
	buf := append([]byte{0x00, 0x12}, good...) // leading garbage
	buf = append(buf, 0xA4, 0x03)              // trailing partial frame
	msgs, consumed := ParseFrames(buf)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d, want 1", len(msgs))
	}
	if msgs[0].ID != IDBroadcastData {
		t.Fatalf("id = %v", msgs[0].ID)
	}
	if consumed != len(buf)-2 {
		t.Fatalf("consumed = %d, want %d", consumed, len(buf)-2)
	}
}

func TestChecksum(t *testing.T) {
	// XOR over [0xA4, 0x01, 0x4A, 0x00]
	if got := Checksum(0xA4, 0x01, 0x4A, []byte{0x00}); got != 0xEF {
		t.Fatalf("checksum = %#x, want 0xEF", got)
	}
}

func TestCodeString(t *testing.T) {
	if got := ResponseNoError.String(); got != "RESPONSE_NO_ERROR" {
		t.Fatalf("got %q", got)
	}
	if got := EventTransferTxCompleted.String(); got != "EVENT_TRANSFER_TX_COMPLETED" {
		t.Fatalf("got %q", got)
	}
	if got := Code(4444).String(); got != "UNKNOWN_4444" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCapabilities(t *testing.T) {
	// capabilities payload with 7 bytes
	data := []byte{8, 3, 0x00, 0x00, 0x00, 0x00, 0x00}
	c, err := ParseCapabilities(data)
	if err != nil {
		t.Fatalf("ParseCapabilities: %v", err)
	}
	if c.MaxChannels != 8 || c.MaxNetworks != 3 {
		t.Fatalf("channels/networks = %d/%d", c.MaxChannels, c.MaxNetworks)
	}
	if _, err := ParseCapabilities([]byte{1, 2}); err == nil {
		t.Fatal("expected error for short payload")
	}
}

func TestAntVersionAndSerial(t *testing.T) {
	if got := AntVersion([]byte{'3', '.', '5', 0}); got != "3.5" {
		t.Fatalf("version = %q", got)
	}
	if got := SerialNumber([]byte{0x01, 0x02, 0x03, 0x04}); got != 0x04030201 {
		t.Fatalf("serial = %#x", got)
	}
}
