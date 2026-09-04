package ant

import (
	"testing"
)

// The benchmarks cover the receive hot path: single frame parsing, the
// stream parser (including resynchronisation on garbage) and the Core
// consume loop that feeds the dispatcher.

// benchFrames builds a well-formed byte stream of alternating broadcast
// frames, the steady-state shape of an ANT link.
func benchFrames(n int) []byte {
	var buf []byte
	for i := 0; i < n; i++ {
		payload := []byte{0, byte(i), 2, 3, byte(i) ^ 0xFF, 5, 6, 7, 8}
		buf = append(buf, broadcastFrame(0, payload)...)
	}
	return buf
}

func BenchmarkParseFrame(b *testing.B) {
	frame := broadcastFrame(0, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	b.SetBytes(int64(len(frame)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := ParseFrame(frame); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseFramesStream(b *testing.B) {
	// 64 broadcast frames plus 2 garbage bytes: measures the happy path
	// together with a small resync penalty.
	stream := benchFrames(64)
	stream = append(stream, 0x00, 0xFF)
	b.SetBytes(int64(len(stream)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgs, consumed := ParseFrames(stream)
		if len(msgs) != 64 || consumed != len(stream) {
			b.Fatalf("msgs=%d consumed=%d", len(msgs), consumed)
		}
	}
}

func BenchmarkCoreConsume(b *testing.B) {
	core, err := NewCore(newReconDriver(), WithEventHandler(func(Event) {}))
	if err != nil {
		b.Fatal(err)
	}
	defer core.Stop()
	stream := benchFrames(128)
	b.SetBytes(int64(len(stream)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rest := core.consume(stream); len(rest) != 0 {
			b.Fatalf("leftover %d bytes", len(rest))
		}
	}
}

func BenchmarkCoreConsumeBurst(b *testing.B) {
	core, err := NewCore(newReconDriver(), WithEventHandler(func(Event) {}))
	if err != nil {
		b.Fatal(err)
	}
	defer core.Stop()
	// A burst transfer split into 24 packets, reassembled per dispatch.
	var stream []byte
	for i := 0; i < 24; i++ {
		seq := byte(i % 32)
		if i == 23 {
			seq |= 0x80 // last packet flag
		}
		data := append([]byte{seq}, []byte{byte(i), 0xAA, 0xBB, 0xCC}...)
		stream = append(stream, NewMessage(IDBurstTransferData, data).Encode()...)
	}
	b.SetBytes(int64(len(stream)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rest := core.consume(stream); len(rest) != 0 {
			b.Fatalf("leftover %d bytes", len(rest))
		}
	}
}
