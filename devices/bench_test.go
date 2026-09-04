package devices

import (
	"io"
	"log/slog"
	"testing"
)

// The benchmarks measure the profile page decoders (the hot path called
// once per RF event per device) and the InfluxFields serialisation used
// by the influx/mqtt CLI targets. Profiles are constructed without a
// node: the decoders under test are pure functions of the payload.

// benchLogger discards the info-level decode logs so benchmark results
// measure parsing, not terminal output.
func benchLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func benchHeartRate() *HeartRate {
	return &HeartRate{baseDevice: baseDevice{log: benchLogger(), attached: true}}
}

func benchBikeSpeed() *BikeSpeed {
	return &BikeSpeed{baseDevice: baseDevice{log: benchLogger(), attached: true}}
}

func benchBikeCadence() *BikeCadence {
	return &BikeCadence{baseDevice: baseDevice{log: benchLogger(), attached: true}}
}

func benchBaseDevice() *baseDevice {
	return &baseDevice{log: benchLogger(), attached: true}
}

// hrPages rotates the main HRM pages the way a real transmitter does
// (0..4 plus battery page 7), so lastData-style dedup in upper layers
// would still let every event through.
var hrPages = [][]byte{
	{0x00, 0, 0, 0, 0x00, 0x04, 60, 150},     // main
	{0x01, 100, 3, 0, 0, 0, 60, 150},         // cumulative operating time
	{0x02, 19, 0x2A, 0, 0, 0, 60, 150},       // manufacturer / serial
	{0x03, 3, 1, 24, 0, 0, 60, 150},          // product info
	{0x04, 0, 0, 0x80, 0x02, 0, 60, 150},     // previous beat
	{0x07, 50, 0x80, 0x22, 0x02, 0, 60, 150}, // battery
}

func BenchmarkHeartRateOnData(b *testing.B) {
	hr := benchHeartRate()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hr.onData(hrPages[i%len(hrPages)])
	}
}

var bscPages = [][]byte{
	{0x01, 0xFF, 0xFF, 10, 0, 0x80, 0x02, 0x40, 0x1F, 0x4E, 0x2A, 0x1B, 0x35, 0, 0}, // speed main
	{0x02, 0xFF, 0xFF, 5, 0, 0x80, 0x02, 0x40, 0x1F, 0x4E, 0x2A, 0x1B, 0x35, 0, 0},  // cadence main
	{0x03, 0x80, 0x1F, 0x4E, 0x2A, 0x1B, 0x35, 0x34},                                // product/common
}

func BenchmarkBikeSpeedOnData(b *testing.B) {
	bs := benchBikeSpeed()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bs.onData(bscPages[i%len(bscPages)])
	}
}

func BenchmarkBikeCadenceOnData(b *testing.B) {
	bc := benchBikeCadence()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bc.onData(bscPages[i%len(bscPages)])
	}
}

var commonPages = [][]byte{
	{80, 0xFF, 0xFF, 1, 0x59, 0x01, 0x20, 0x00},           // manufacturer
	{81, 0xFF, 0xFF, 0x01, 0x55, 0x00, 0x00, 0x00},        // product
	{82, 0xFF, 30, 12, 23, 0x1F, 5, 24},                   // battery
	{83, 0xFF, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},        // time & date
	{82, 0xFF, 30, 12, 23, 0x1F, 5, 24, 0x01, 0, 0, 0, 0}, // battery + profile page (9 bytes)
}

func BenchmarkBaseDeviceCommonPages(b *testing.B) {
	bd := benchBaseDevice()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bd.onData(commonPages[i%len(commonPages)])
	}
}

func BenchmarkInfluxFieldsHeartRate(b *testing.B) {
	d := HeartRateData{HeartRate: 120, BeatCount: 1234, BeatTime: 512.5}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = InfluxFields(d)
	}
}

func BenchmarkInfluxFieldsBikeSpeed(b *testing.B) {
	speed := 31.4
	d := BikeSpeedData{
		CumulativeOperatingTime:   120,
		BikeSpeedEventTime:        [2]float64{1.5, 2.25},
		CumulativeSpeedRevolution: [2]int{10, 14},
		CalculatedSpeed:           &speed,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = InfluxFields(d)
	}
}
