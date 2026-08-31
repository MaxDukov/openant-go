package devices

import (
	"log/slog"
	"testing"
)

// newFuzzBaseDevice builds a baseDevice suitable for fuzzing the data
// dispatcher without a live channel: `attached` is preset so the extended
// attach path (which reconfigures a real channel) is skipped.
func newFuzzBaseDevice() *baseDevice {
	d := &baseDevice{
		log:      slog.Default(),
		attached: true,
	}
	return d
}

// FuzzBaseDeviceOnData covers the common page dispatcher with arbitrary
// RF payloads (review P0-2).
func FuzzBaseDeviceOnData(f *testing.F) {
	f.Add([]byte{80, 0xFF, 0xFF, 1, 0x59, 0x01, 0x20, 0x00})
	f.Add([]byte{81, 0xFF, 0xFF, 0x01, 0x55, 0x00, 0x00, 0x00})
	f.Add([]byte{82, 0xFF, 0x00, 0x10, 0x00, 0x00, 0x80, 0x22})
	f.Add([]byte{83, 0xFF, 30, 12, 23, 0x1F, 5, 24})
	f.Add([]byte{80, 1, 2})                                               // short common page
	f.Add([]byte{0, 1, 2, 3})                                             // short page
	f.Add([]byte{1, 0xFF, 0xFF, 0x80, 0x2A, 0x00, 120, 0, 0, 0, 0, 0, 0}) // extended
	f.Add([]byte{1, 0xFF, 0xFF, 0x80, 0x2A, 0x00, 120, 0, 0})             // 9 bytes: guard case
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		newFuzzBaseDevice().onData(data)
	})
}

// FuzzScannerScanData covers the scanner handler with arbitrary extended
// payloads (review P0-3).
func FuzzScannerScanData(f *testing.F) {
	f.Add([]byte{80, 0xFF, 0xFF, 1, 0x59, 0x01, 0x20, 0x00, 0x80, 0x2A, 0x00, 120, 0})
	f.Add([]byte{81, 0xFF, 0xFF, 0x01, 0x55, 0x00, 0xAB, 0xCD, 0x80, 0x2A, 0x00, 120, 0})
	f.Add([]byte{80, 1, 2, 3, 4, 5, 6, 7, 8, 9}) // 10 bytes: guard case
	f.Add([]byte{1})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		s := &Scanner{
			baseDevice: baseDevice{log: slog.Default()},
			found:      map[DeviceTuple]struct{}{},
			common:     map[string]CommonData{},
		}
		s.onDataFull = s.scanData
		s.onDataFull(data)
	})
}
