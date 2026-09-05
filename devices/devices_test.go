package devices

import (
	"context"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/anttest"
	"github.com/maxdukov/openant-go/easy"
)

func newTestNode(t *testing.T) (*easy.Node, *anttest.SimDriver) {
	t.Helper()
	sim := anttest.NewSimDriver()
	n, err := easy.NewWithDriver(sim)
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	t.Cleanup(n.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	done := n.Start(ctx)
	t.Cleanup(func() { cancel(); <-done; n.Stop() })
	return n, sim
}

func TestHeartRatePages(t *testing.T) {
	n, sim := newTestNode(t)
	hr, err := NewHeartRate(n, 0, 0)
	if err != nil {
		t.Fatalf("NewHeartRate: %v", err)
	}
	got := make(chan HeartRateData, 4)
	batt := make(chan BatteryData, 1)
	hr.OnDeviceData = func(page int, name string, d DeviceData) {
		if hd, ok := d.(HeartRateData); ok && name == "heart_rate" {
			got <- hd
		}
	}
	hr.OnBattery = func(b BatteryData) { batt <- b }

	// Main page 0: beat time 1024/1024=1s, beat count 60, HR 150.
	sim.EmitBroadcast(0, []byte{0x00, 0, 0, 0, 0x00, 0x04, 60, 150})
	select {
	case d := <-got:
		if d.HeartRate != 150 || d.BeatCount != 60 || d.BeatTime != 1.0 {
			t.Fatalf("data = %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for heart rate data")
	}

	// Battery page 7: 50%, 3.5 V fractional (byte 2), coarse 2 + status
	// Good (byte 3).
	sim.EmitBroadcast(0, []byte{0x07, 50, 0x80, 0x22, 0, 0, 60, 150})
	select {
	case b := <-batt:
		if b.VoltageFractional != 0.5 || b.VoltageCoarse != 2 {
			t.Fatalf("battery = %+v", b)
		}
		if b.Status != BatteryStatusGood {
			t.Fatalf("status = %v", b.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for battery data")
	}
}

func TestPowerMeterStandardPower(t *testing.T) {
	n, sim := newTestNode(t)
	pm, err := NewPowerMeter(n, 0, 0)
	if err != nil {
		t.Fatalf("NewPowerMeter: %v", err)
	}
	got := make(chan PowerData, 2)
	pm.OnDeviceData = func(page int, name string, d DeviceData) {
		if pd, ok := d.(PowerData); ok {
			got <- pd
		}
	}
	// Page 0x10: event count 1, accumulated power 400, cadence 90,
	// instantaneous power 250.
	sim.EmitBroadcast(0, []byte{0x10, 1, 0xFF, 90, 0x90, 0x01, 0xFA, 0x00})
	select {
	case d := <-got:
		if d.InstantaneousPower != 250 || d.Cadence != 90 {
			t.Fatalf("data = %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	// Second page with new event count: avg = (600-400)/1 = 200 W.
	sim.EmitBroadcast(0, []byte{0x10, 2, 0xFF, 90, 0x58, 0x02, 0xFA, 0x00})
	select {
	case d := <-got:
		if d.AveragePower != 200 {
			t.Fatalf("average = %d, want 200", d.AveragePower)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestBicycleLightsPage1(t *testing.T) {
	n, sim := newTestNode(t)
	bl, err := NewBicycleLights(n, 0, 0)
	if err != nil {
		t.Fatalf("NewBicycleLights: %v", err)
	}
	got := make(chan interface{}, 4)
	bl.OnDeviceData = func(page int, name string, d DeviceData) {
		got <- d
	}
	// Page 1: light index 3, radar support set, type Taillight(2),
	// battery Low(4), 2 sub lights, beam high, mode SlowFlash(6),
	// intensity 80.
	sim.EmitBroadcast(0, []byte{
		0x01,
		0x03,
		0x00 | 1<<1 | 2<<2 | 4<<5, // radar | type | battery
		0x02,
		0x12,
		0x64,
		0x00 | 1<<1 | 6<<2, // beam high | mode 6
		80,
	})
	select {
	case d := <-got:
		ld, ok := d.(BicycleLightsData)
		if !ok {
			t.Fatalf("type = %T", d)
		}
		if ld.LightIndex != 3 || !ld.BikeRadarSupport {
			t.Fatalf("data = %+v", ld)
		}
		if ld.LightType != LightTaillight || ld.BatteryStatus != LightBattLow {
			t.Fatalf("data = %+v", ld)
		}
		if ld.NumberOfSubLights != 2 {
			t.Fatalf("sub lights = %d", ld.NumberOfSubLights)
		}
		if ld.Beam != BeamHigh || ld.Mode != LightModeSlowFlash || ld.Intensity != 80 {
			t.Fatalf("data = %+v", ld)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestBikeSpeedCadenceCalc(t *testing.T) {
	n, sim := newTestNode(t)
	bsc, err := NewBikeSpeedCadence(n, 0, 0)
	if err != nil {
		t.Fatalf("NewBikeSpeedCadence: %v", err)
	}
	bsc.SetWheelCircumference(2.3)
	got := make(chan BikeSpeedData, 2)
	bsc.OnDeviceData = func(page int, name string, d DeviceData) {
		if name == "bike_speed" {
			got <- d.(BikeSpeedData)
		}
	}
	// Main page: cadence block and speed block baseline at zero.
	sim.EmitBroadcast(0, []byte{0x00, 0, 0, 0, 0, 0, 0, 0})
	// 5 revolutions in 1 second: speed = 2.3*5/1*3.6 = 41.4 km/h.
	sim.EmitBroadcast(0, []byte{0x00, 0x00, 0x04, 0x05, 0x00, 0x04, 0x05, 0x00})
	// First callback is the zero baseline (speed nil); wait for the second.
	if d := <-got; d.CalculatedSpeed != nil {
		t.Fatalf("baseline speed should be nil: %+v", d.CalculatedSpeed)
	}
	select {
	case d := <-got:
		if d.CalculatedSpeed == nil || *d.CalculatedSpeed < 41.39 || *d.CalculatedSpeed > 41.41 {
			t.Fatalf("speed = %+v", d.CalculatedSpeed)
		}
		if d.CalculatedDistance == nil || *d.CalculatedDistance != 11.5 {
			t.Fatalf("distance = %+v", d.CalculatedDistance)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestFitnessEquipmentGeneralFE(t *testing.T) {
	n, sim := newTestNode(t)
	fe, err := NewFitnessEquipment(n, 0, 0)
	if err != nil {
		t.Fatalf("NewFitnessEquipment: %v", err)
	}
	got := make(chan FitnessEquipmentData, 2)
	fe.OnDeviceData = func(page int, name string, d DeviceData) {
		if name == "general_fe" {
			got <- d.(FitnessEquipmentData)
		}
	}
	// Page 0x10: type Trainer(25), speed 0x0966=2406 -> 2.406 km/h,
	// state InUse(3).
	sim.EmitBroadcast(0, []byte{0x10, 25, 0x0F, 0xFF, 0x66, 0x09, 0xFF, 3 << 4})
	select {
	case d := <-got:
		if d.Type != FETypeTrainer || d.State != FEStateInUse {
			t.Fatalf("data = %+v", d)
		}
		if d.Speed < 2.405 || d.Speed > 2.407 {
			t.Fatalf("speed = %v", d.Speed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if err := fe.SetTargetPower(5000); err == nil {
		t.Fatal("expected error for power > 4000")
	}
}

func TestScannerDiscovery(t *testing.T) {
	n, sim := newTestNode(t)
	sc, err := NewScanner(n, 0, DeviceTypeUnknown, 0)
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	found := make(chan DeviceTuple, 1)
	sc.OnScanFound = func(t DeviceTuple) { found <- t }
	updates := make(chan CommonData, 2)
	sc.OnScanUpdate = func(t DeviceTuple, c CommonData) { updates <- c }

	// Extended broadcast: 8 page bytes + flag byte + id LE16 + type + trans.
	page := []byte{80, 0xFF, 0xFF, 1, 0x59, 0x01, 0x20, 0x00}
	ext := []byte{0x80, 0x2A, 0x00, 120, 0} // device 42, type 120, trans 0
	sim.EmitBroadcast(0, append(page, ext...))

	select {
	case tuple := <-found:
		if tuple.ID != 42 || tuple.Type != 120 {
			t.Fatalf("tuple = %+v", tuple)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for discovery")
	}
	select {
	case c := <-updates:
		if c.HardwareRev != 1 || c.ManufacturerID != 345 || c.ModelNo != 32 {
			t.Fatalf("common = %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for update")
	}
}

func TestDeviceTypeNames(t *testing.T) {
	if DeviceType(11).String() != "PowerMeter" {
		t.Fatal("PowerMeter name")
	}
	if DeviceType(251).String() != "Unknown" {
		t.Fatal("unknown fallback")
	}
	if DeviceTypeByName("HeartRate") != DeviceTypeHeartRate {
		t.Fatal("by name lookup")
	}
}

func TestToInfluxJSON(t *testing.T) {
	p := PowerData{InstantaneousPower: 100, AveragePower: 90, LeftPower: -1, RightPower: -1, Torque: 2.5, AngularVelocity: 3, Cadence: 90}
	m := ToInfluxJSON(p, map[string]string{"device": "x"})
	if m["measurement"] != "PowerData" {
		t.Fatalf("measurement = %v", m["measurement"])
	}
	fields := m["fields"].(map[string]any)
	if fields["instantaneous_power"] != int64(100) {
		t.Fatalf("fields = %#v", fields)
	}
	if _, ok := m["time"]; !ok {
		t.Fatal("missing time")
	}
}

func TestCommonDataLocal(t *testing.T) {
	utc := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := CommonData{TimeDate: &utc}
	local := c.Local()
	if local.TimeDate == nil || !local.TimeDate.Equal(utc) {
		t.Fatalf("Local() = %+v", local.TimeDate)
	}
	if local.TimeDate.Location() != time.Local {
		t.Errorf("Local() location = %v, want time.Local", local.TimeDate.Location())
	}
	// The original stays UTC.
	if c.TimeDate.Location() != time.UTC {
		t.Errorf("receiver mutated: %v", c.TimeDate.Location())
	}
	// nil-safe
	if (CommonData{}).Local().TimeDate != nil {
		t.Error("nil TimeDate must stay nil")
	}
}

func TestWorkouts(t *testing.T) {
	w, err := WorkoutFromArrays([]int{100, 200, 300}, []float64{5, 5.5, 10})
	if err != nil || len(w.Intervals) != 3 {
		t.Fatalf("w = %+v err = %v", w, err)
	}
	if _, err := WorkoutFromArrays([]int{1}, []float64{1, 2}); err == nil {
		t.Fatal("expected length error")
	}
	r, err := WorkoutFromRamp(100, 200, 50, 10, 0)
	if err != nil || len(r.Intervals) != 2 {
		t.Fatalf("ramp = %+v err = %v", r, err)
	}
	tri, err := WorkoutFromRamp(100, 100, 50, 10, 200)
	if err != nil || len(tri.Intervals) != 4 { // up: 100,150; down: 200,150
		t.Fatalf("triangle = %+v (%d) err = %v", tri, len(tri.Intervals), err)
	}
}

func TestTryDateTime(t *testing.T) {
	if td := tryDateTime(2024, 2, 30, 10, 0, 0); td != nil {
		t.Fatalf("30 Feb accepted: %v", td)
	}
	if td := tryDateTime(2024, 13, 1, 0, 0, 0); td != nil {
		t.Fatalf("month 13 accepted: %v", td)
	}
	td := tryDateTime(2024, 5, 1, 12, 30, 45)
	if td == nil || td.Year() != 2024 || td.Minute() != 30 {
		t.Fatalf("td = %v", td)
	}
}

func TestCommonPagePayloads(t *testing.T) {
	c := CommonData{HardwareRev: 2, ManufacturerID: 0x0059, ModelNo: 0x0020, SerialNo: 0xAABBCCDD, SoftwareVer: "1.2"}
	mfr := c.ManufacturerPagePayload()
	want := []byte{0x50, 0xFF, 0xFF, 2, 0x59, 0x00, 0x20, 0x00}
	for i := range want {
		if mfr[i] != want[i] {
			t.Fatalf("mfr = % X", mfr)
		}
	}
	prod := c.ProductInfoPagePayload()
	if prod[0] != 0x51 || prod[3] != 10 || prod[4] != 0xDD || prod[7] != 0xAA {
		t.Fatalf("prod = % X", prod)
	}
}
