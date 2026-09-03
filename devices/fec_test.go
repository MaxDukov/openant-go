package devices

import (
	"testing"

	"github.com/maxdukov/openant-go/anttest"
	"time"
)

// collectFE hooks OnDeviceData and delivers named events to a channel.
func collectFE(f *FitnessEquipment, names ...string) (chan DeviceData, func()) {
	ch := make(chan DeviceData, 16)
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	prev := f.OnDeviceData
	f.OnDeviceData = func(page int, name string, data DeviceData) {
		if prev != nil {
			prev(page, name, data)
		}
		if want[name] {
			ch <- data
		}
	}
	return ch, func() { f.OnDeviceData = prev }
}

func expectFE(t *testing.T, ch chan DeviceData, name string) DeviceData {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for %s event", name)
		return nil
	}
}

func newFE(t *testing.T) (*FitnessEquipment, *anttest.SimDriver) {
	t.Helper()
	n, sim := newTestNode(t)
	f, err := NewFitnessEquipment(n, 0, 0)
	if err != nil {
		t.Fatalf("NewFitnessEquipment: %v", err)
	}
	return f, sim
}

// Command status replies per ANT+ FE-C Rev 5.0 Tables 8-48/8-49.
func TestFECommandStatus(t *testing.T) {
	f, sim := newFE(t)
	ch, restore := collectFE(f, "command_status")
	defer restore()

	// Basic resistance accepted: byte 7 total resistance 100*0.5 = 50 %.
	sim.EmitBroadcast(0, []byte{0x47, 0x30, 5, 0x00, 0xFF, 0xFF, 0xFF, 100})
	d := expectFE(t, ch, "command_status").(FECommandStatusData)
	if d.CommandID != ResistanceBasic || d.Sequence != 5 || d.Status != FECommandPass {
		t.Fatalf("basic header: %+v", d)
	}
	if d.TotalResistance != 50.0 {
		t.Fatalf("basic resistance: %+v", d)
	}

	// Target power pending: raw 400 -> 100 W.
	sim.EmitBroadcast(0, []byte{0x47, 0x31, 6, 0x04, 0xFF, 0xFF, 0x90, 0x01})
	d = expectFE(t, ch, "command_status").(FECommandStatusData)
	if d.Status != FECommandPending || d.TargetPower != 100 {
		t.Fatalf("target power: %+v", d)
	}

	// Wind: coefficient 0.51, speed 0 (0x7F with offset), drafting 1.00.
	sim.EmitBroadcast(0, []byte{0x47, 0x32, 7, 0x00, 0xFF, 51, 0x7F, 0x64})
	d = expectFE(t, ch, "command_status").(FECommandStatusData)
	if d.WindCoefficient != 0.51 || d.WindSpeed != 0 || d.DraftingFactor != 1.0 {
		t.Fatalf("wind: %+v", d)
	}

	// Track rejected: flat grade, rolling 0.004, status fail.
	sim.EmitBroadcast(0, []byte{0x47, 0x33, 8, 0x01, 0xFF, 0x20, 0x4E, 80})
	d = expectFE(t, ch, "command_status").(FECommandStatusData)
	if d.Status != FECommandFail || d.Grade != 0 || d.RollingResistance != 0.004 {
		t.Fatalf("track: %+v", d)
	}
}

// Metabolic page 18 per Table 8-13.
func TestFEMetabolic(t *testing.T) {
	f, sim := newFE(t)
	ch, restore := collectFE(f, "metabolic")
	defer restore()

	sim.EmitBroadcast(0, []byte{0x12, 0xFF, 0x7D, 0x00, 0x14, 0x00, 200, 0x21})
	d := expectFE(t, ch, "metabolic").(FEMetabolicData)
	if d.METs != 1.25 || d.CaloricBurnRate != 2.0 || d.Calories != 200 {
		t.Fatalf("metabolic: %+v", d)
	}
	if d.Capabilities != 1 || d.State != FEStateReady {
		t.Fatalf("metabolic bits: %+v", d)
	}

	// Invalid METs/burn rate.
	sim.EmitBroadcast(0, []byte{0x12, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0x10})
	d = expectFE(t, ch, "metabolic").(FEMetabolicData)
	if d.METs != -1 || d.CaloricBurnRate != -1 {
		t.Fatalf("invalid metabolic: %+v", d)
	}
}

// Treadmill page 19 per Table 8-15.
func TestFETreadmill(t *testing.T) {
	f, sim := newFE(t)
	ch, restore := collectFE(f, "treadmill")
	defer restore()

	sim.EmitBroadcast(0, []byte{0x13, 0xFF, 0xFF, 0xFF, 90, 12, 34, 0x12})
	d := expectFE(t, ch, "treadmill").(FETreadmillData)
	if d.Cadence != 90 || d.NegativeVerticalDistance != 1.2 || d.PositiveVerticalDistance != 3.4 {
		t.Fatalf("treadmill: %+v", d)
	}
	if d.Capabilities != 2 || d.State != FEStateAsleep {
		t.Fatalf("treadmill bits: %+v", d)
	}

	sim.EmitBroadcast(0, []byte{0x13, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0x00})
	d = expectFE(t, ch, "treadmill").(FETreadmillData)
	if d.Cadence != -1 {
		t.Fatalf("invalid cadence: %+v", d)
	}
}

// FE capabilities page 54 per Tables 8-45/8-46.
func TestFECapabilities(t *testing.T) {
	f, sim := newFE(t)
	ch, restore := collectFE(f, "fe_capabilities")
	defer restore()

	if f.Capabilities.MaxResistance != -1 {
		t.Fatal("capabilities must start invalid")
	}
	sim.EmitBroadcast(0, []byte{0x36, 0xFF, 0xFF, 0xFF, 0xFF, 0xDC, 0x05, 0x07})
	d := expectFE(t, ch, "fe_capabilities").(FECapabilitiesData)
	if !d.BasicMode || !d.TargetPowerMode || !d.SimulationMode {
		t.Fatalf("capabilities bits: %+v", d)
	}
	if d.MaxResistance != 1500 {
		t.Fatalf("max resistance: %+v", d)
	}

	// Max resistance invalid, only target power supported.
	sim.EmitBroadcast(0, []byte{0x36, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x02})
	d = expectFE(t, ch, "fe_capabilities").(FECapabilitiesData)
	if d.MaxResistance != -1 || !d.TargetPowerMode || d.BasicMode || d.SimulationMode {
		t.Fatalf("invalid max resistance: %+v", d)
	}
}

// Trainer status carried by page 25 (0x19), Tables 8-25/8-27/8-28.
func TestFETrainerStatus(t *testing.T) {
	f, sim := newFE(t)
	ch, restore := collectFE(f, "trainer_status")
	defer restore()

	sim.EmitBroadcast(0, []byte{0x19, 3, 88, 100, 0, 150, 0x56, 0x21})
	d := expectFE(t, ch, "trainer_status").(FETrainerStatusData)
	if d.EventCount != 3 || d.Cadence != 88 || d.InstantaneousPower != 150+0x06<<8 {
		t.Fatalf("trainer status: %+v", d)
	}
	if !d.PowerCalibrationRequired || d.ResistanceCalibrationRequired || !d.UserConfigRequired {
		t.Fatalf("calibration bits: %+v", d)
	}
	if d.TargetPowerLimit != 1 || d.State != FEStateReady {
		t.Fatalf("flags/state: %+v", d)
	}
}

// Trainer torque page 26 per Table 8-29: bytes 3-4 wheel period,
// bytes 5-6 accumulated torque (the layout python openant shifts by one).
func TestFETrainerTorque(t *testing.T) {
	f, sim := newFE(t)
	ch, restore := collectFE(f, "standard_torque")
	defer restore()

	sim.EmitBroadcast(0, []byte{0x1A, 5, 3, 0x00, 0x10, 0x40, 0x01, 0x20})
	sim.EmitBroadcast(0, []byte{0x1A, 6, 4, 0x20, 0x10, 0x60, 0x01, 0x20})
	expectFE(t, ch, "standard_torque")
	// Second event: read the data from the event itself (a copy taken
	// under the dispatcher's happens-before), not the shared profile
	// fields which the dispatcher may still be writing for a newer page.
	d := expectFE(t, ch, "standard_torque").(PowerData)
	// deltaTorque = 32 -> 32/(32*1) = 1.00 Nm; deltaPeriod = 32 ->
	// 2*pi*1/(32/2048) = 402.12 rad/s -> 402 W.
	if d.Torque != 1.0 || d.AngularVelocity != 402.12 || d.AveragePower != 402 {
		t.Fatalf("torque math: %+v", d)
	}
}

// Heart rate master TX schedule per ANT+ HRM spec Rev 2.1: main page 4
// with beats, background pages 1-3 every 65th message, toggle bit every
// 4th message.
func TestHeartRateMasterTX(t *testing.T) {
	n, _ := newTestNode(t)
	h, err := NewHeartRateMaster(n, 0)
	if err != nil {
		t.Fatalf("NewHeartRateMaster: %v", err)
	}
	// Close the channel to stop the real-time EVENT_TX fallback so the
	// beat clock is only driven by the direct txPage calls below.
	if err := h.Channel().Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if h.DeviceID == 0 || h.DeviceID > 65535 {
		t.Fatalf("random device id out of range: %d", h.DeviceID)
	}
	if h.TransType != 1 {
		t.Fatalf("transmission type = %d, want 1 (LSN)", h.TransType)
	}
	if err := h.SetHeartRate(256); err == nil {
		t.Fatal("256 bpm must be rejected")
	}
	h.Data.HeartRate = 0 // stop beating for the deterministic part

	// Before any beat: default data page 0 with invalid heart rate.
	t0 := time.Now()
	h.txStart, h.lastBeat, h.prevBeat = t0, t0, t0
	h.Data.HeartRate = 0
	p := h.txPage(0)
	if p[0] != 0x00 {
		t.Fatalf("page = %02X, want 0x00", p[0])
	}
	if p[7] != 0 {
		t.Fatal("invalid heart rate must be 0x00")
	}

	// 240 bpm = a beat every 250 ms.
	h.Data.HeartRate = 240
	h.lastBeat = time.Now().Add(-300 * time.Millisecond)
	h.prevBeat = h.lastBeat
	p = h.txPage(5)
	if p[0]&0x7F != 0x04 {
		t.Fatalf("page = %02X, want 0x04 after the first beat", p[0]&0x7F)
	}
	if p[6] != 1 {
		t.Fatalf("beat count = %d, want 1", p[6])
	}
	if p[7] != 240 {
		t.Fatalf("heart rate = %d, want 240", p[7])
	}
	prev := int(p[2]) + int(p[3])<<8
	beat := int(p[4]) + int(p[5])<<8
	if beat <= prev || beat == 0 {
		t.Fatalf("beat time %d must be after previous %d", beat, prev)
	}

	// Background schedule: every 65th message, rotating pages 1-3.
	h.Data.OperatingTime = 7200 // 2 s counts -> 3600
	h.bgCount = 0
	if got := h.txPage(64)[0] & 0x7F; got != 0x01 {
		t.Fatalf("message 64 page = %02X, want 0x01", got)
	}
	if got := h.txPage(129)[0] & 0x7F; got != 0x02 {
		t.Fatalf("second background page = %02X, want 0x02", got)
	}
	if got := h.txPage(194)[0] & 0x7F; got != 0x03 {
		t.Fatalf("third background page = %02X, want 0x03", got)
	}
	bgp := h.txPage(259) // next background slot, rotation back to page 1
	op := int(bgp[1]) + int(bgp[2])<<8 + int(bgp[3])<<16
	if bgp[0]&0x7F != 0x01 || op != 3600 {
		t.Fatalf("background page = %02X, operating time = %d*2 s, want 0x01/3600", bgp[0]&0x7F, op)
	}

	// Toggle bit flips every 4th message.
	tg0, tg3 := pageToggle(0), pageToggle(3)
	tg4, tg7 := pageToggle(4), pageToggle(7)
	if tg0 != 0 || tg3 != 0 || tg4 == 0 || tg7 == 0 {
		t.Fatalf("toggle: %02X %02X %02X %02X", tg0, tg3, tg4, tg7)
	}
}

// The master answers display page requests (common page 70) for
// capabilities (6) and battery status (7).
func TestHeartRateMasterAck(t *testing.T) {
	n, _ := newTestNode(t)
	h, err := NewHeartRateMaster(n, 12345)
	if err != nil {
		t.Fatalf("NewHeartRateMaster: %v", err)
	}
	h.Channel().Close() // stop the EVENT_TX fallback ticker
	h.Data.FeaturesSupported = 0x07
	h.Data.FeaturesEnabled = 0x01
	p := h.ackPage([]byte{0x46, 0x06, 0, 0, 0, 0, 0, 0})
	if p == nil || p[0]&0x7F != 0x06 || p[2] != 0x07 || p[3] != 0x01 {
		t.Fatalf("capabilities ack: % X", p)
	}

	h.Data.BatteryPercentage = 85
	h.Common.LastBattery.VoltageFractional = 0.5
	h.Common.LastBattery.VoltageCoarse = 3
	h.Common.LastBattery.Status = BatteryStatusGood
	p = h.ackPage([]byte{0x46, 0x07, 0, 0, 0, 0, 0, 0})
	if p == nil || p[0]&0x7F != 0x07 || p[1] != 85 || p[2] != 128 {
		t.Fatalf("battery ack: % X", p)
	}
	desc := int(p[3])
	if desc&0x0F != 3 || (desc>>4)&0x07 != int(BatteryStatusGood) {
		t.Fatalf("battery descriptor %02X", desc)
	}
	if p := h.ackPage([]byte{0x46, 0x42, 0, 0, 0, 0, 0, 0}); p != nil {
		t.Fatalf("unknown page must be nil, got % X", p)
	}
}

// Display side: product info page 3 and swim interval summary page 5.
func TestHeartRateRXPages0305(t *testing.T) {
	n, sim := newTestNode(t)
	h, err := NewHeartRate(n, 0, 0)
	if err != nil {
		t.Fatalf("NewHeartRate: %v", err)
	}
	ch := make(chan DeviceData, 4)
	h.OnDeviceData = func(page int, name string, data DeviceData) {
		ch <- data
	}

	sim.EmitBroadcast(0, []byte{0x03, 42, 7, 200, 0xFF, 0xFF, 0xFF, 0xFF})
	d := (<-ch).(HeartRateData)
	if d.HwVersion != 42 || d.SwVersion != 7 || d.ModelNumber != 200 {
		t.Fatalf("product info: %+v", d)
	}

	sim.EmitBroadcast(0, []byte{0x05, 120, 150, 110, 0xFF, 0xFF, 0xFF, 0xFF})
	d = (<-ch).(HeartRateData)
	if d.IntervalAverageHR != 120 || d.IntervalMaximumHR != 150 || d.SessionAverageHR != 110 {
		t.Fatalf("swim summary: %+v", d)
	}

	sim.EmitBroadcast(0, []byte{0x05, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF})
	d = (<-ch).(HeartRateData)
	if d.IntervalAverageHR != -1 || d.SessionAverageHR != -1 {
		t.Fatalf("invalid swim summary: %+v", d)
	}
}
