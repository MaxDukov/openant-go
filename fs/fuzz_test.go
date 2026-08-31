package fs

import (
	"log/slog"
	"testing"
)

// FuzzParseBeacon covers the 8 byte ANT-FS beacon from the RF peer.
func FuzzParseBeacon(f *testing.F) {
	f.Add([]byte{0x43, 0x00, 0x00, 0x00, 0x5E, 0x00, 0x00, 0x00})
	f.Add([]byte{0x43, 0x38, 0x02, 0x02, 0x11, 0x22, 0x33, 0x44})
	f.Add([]byte{0x43, 0x00})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if b, err := ParseBeacon(data); err == nil {
			_ = b.Serial() + uint32(b.ChannelPeriod()) // no panic expected
			_ = b.DataAvailable()
		}
	})
}

// FuzzParseCommand covers the ANT-FS command dispatcher (burst payloads).
func FuzzParseCommand(f *testing.F) {
	f.Add([]byte{0x44, 0x04, 0x01, 0x00, 0x15, 0xCD, 0x5B, 0x07})
	f.Add([]byte{0x44, 0x04, 0x02, 0x05, 0xB1, 0x68, 0xDE, 0x3A, 'h', 'e', 'l', 'l', 'o', 0, 0, 0})
	f.Add([]byte{0x44, 0x09, 0x5F, 0x00, 0x00, 0xBA, 0x00, 0x00, 0x00, 0x00, 0x9E, 0xC2, 0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0x44, 0x89, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0xAB, 0xCD})
	f.Add([]byte{0x44, 0x05})
	f.Add([]byte{0x44, 0xFF})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if cmd, err := ParseCommand(data); err == nil && cmd != nil {
			_ = cmd.Bytes()
		}
	})
}

// FuzzParsePipeCommand covers the command pipe dispatcher.
func FuzzParsePipeCommand(f *testing.F) {
	f.Add([]byte{0x01, 0x00, 0x00, 0x01, 0x03, 0x00, 0x00, 0x00})
	f.Add([]byte{0x02, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{0x02, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x80, 0x04, 0x7B, 0x00, 0x67, 0x00, 0x00, 0x00})
	f.Add([]byte{0x03, 0x00, 0x00, 0x01, 42, 0, 0, 0, 42, 0, 0, 0, 0x01, 0, 0, 0})
	f.Add([]byte{0x04, 0x00, 0x00, 0x01, 0x02, 0, 0, 0, 0x80, 0x04, 0, 0, 0, 0, 0xFF, 0xFF})
	f.Add([]byte{0x44})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if cmd, err := ParsePipeCommand(data); err == nil && cmd != nil {
			_ = cmd.PipeBytes()
		}
	})
}

// FuzzParseDirectory covers directory parsing of downloaded files.
func FuzzParseDirectory(f *testing.F) {
	header := make([]byte, 16)
	header[0] = 0x81
	entry := make([]byte, 16)
	entry[2] = 0x80
	entry[3] = 4
	f.Add(append(append([]byte{}, header...), entry...))
	f.Add(header)
	f.Add([]byte{0x81})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if d, err := ParseDirectory(data); err == nil {
			for _, file := range d.Files {
				_ = file.Time()
				_ = file.FlagString()
			}
		}
	})
}

// FuzzApplicationOnData covers the session data classifier with arbitrary
// burst payloads (review P0-1).
func FuzzApplicationOnData(f *testing.F) {
	f.Add([]byte{0x43, 0x00, 0x00, 0x00, 0x5E, 0x00, 0x00, 0x00})
	f.Add([]byte{0x44, 0x04, 0x01, 0x00, 0x15, 0xCD, 0x5B, 0x07})
	f.Add([]byte{0x43, 0x00, 0x01, 0x00, 0x01, 0, 0, 0, 0x44, 0x02, 0x13, 0x04, 0x39, 0x05, 0x00, 0x00})
	f.Add([]byte{0x43})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a := &Application{
			log:      slog.Default(),
			beacons:  make(chan Beacon, 1),
			commands: make(chan Command, 1),
		}
		a.onData(data) // must not panic; queues may drop
	})
}
