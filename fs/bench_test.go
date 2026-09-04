package fs

import (
	"testing"
)

// Benchmarks for the ANT-FS decoders (beacon per channel period,
// command dispatcher on burst payloads, directory on file download).

func BenchmarkParseBeacon(b *testing.B) {
	beacon := []byte{0x43, 0x38, 0x02, 0x02, 0x11, 0x22, 0x33, 0x44}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseBeacon(beacon); err != nil {
			b.Fatal(err)
		}
	}
}

var commands = [][]byte{
	{0x44, 0x04, 0x01, 0x00, 0x15, 0xCD, 0x5B, 0x07},                                                                                                       // link
	{0x44, 0x04, 0x02, 0x05, 0xB1, 0x68, 0xDE, 0x3A, 'h', 'e', 'l', 'l', 'o', 0, 0, 0},                                                                     // disconnect
	{0x44, 0x09, 0x5F, 0x00, 0x00, 0xBA, 0x00, 0x00, 0x00, 0x00, 0x9E, 0xC2, 0x00, 0x00, 0x00, 0x00},                                                       // auth
	{0x44, 0x89, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0xAB, 0xCD}, // upload
}

func BenchmarkParseCommand(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseCommand(commands[i%len(commands)]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseDirectory(b *testing.B) {
	header := make([]byte, 16)
	header[0] = 0x81
	dir := make([]byte, 0, 16+16*8)
	dir = append(dir, header...)
	for i := 0; i < 8; i++ {
		entry := make([]byte, 16)
		entry[2] = 0x80
		entry[3] = 4
		dir = append(dir, entry...)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseDirectory(dir); err != nil {
			b.Fatal(err)
		}
	}
}
