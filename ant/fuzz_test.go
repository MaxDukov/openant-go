package ant

import "testing"

// FuzzParseFrame ensures no panics on arbitrary input (code review PR #1,
// P0-7). The input comes from the USB stick / serial line and a
// compromised device controls it fully.
func FuzzParseFrame(f *testing.F) {
	f.Add([]byte{0xA4, 0x01, 0x4A, 0x00, 0xEF})                   // reset system
	f.Add([]byte{0xA4, 0x03, 0x40, 0x00, 0x46, 0x00, 0xA1})       // channel event response
	f.Add([]byte{0xA4, 0x00, 0x4D, 0xE9})                         // empty payload
	f.Add([]byte{0x53, 0x00, 0x00, 0x00})                         // bad sync
	f.Add([]byte{0xA4, 0x09, 0x4E, 0x00, 1, 2, 3, 4, 5, 6, 7, 8}) // short broadcast
	f.Add([]byte{0xFF, 0xFF, 0xFF})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		msg, n, err := ParseFrame(data)
		if err == nil {
			if msg == nil || n <= 0 || n > len(data) {
				t.Fatalf("inconsistent success: msg=%v n=%d len=%d", msg, n, len(data))
			}
			if len(msg.Data) != int(data[1]) {
				t.Fatalf("payload length mismatch: got %d, frame says %d", len(msg.Data), data[1])
			}
		}
		// Resync variant must not panic either.
		ParseFrames(data)
	})
}
