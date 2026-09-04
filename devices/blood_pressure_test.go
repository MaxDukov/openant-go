package devices

import (
	"testing"
	"time"
)

// TestBloodPressureMeasurement verifies the Measurement Data Page 1
// decoder, including the invalid value markers.
func TestBloodPressureMeasurement(t *testing.T) {
	n, sim := newTestNode(t)
	bp, err := NewBloodPressure(n, 0, 0)
	if err != nil {
		t.Fatalf("NewBloodPressure: %v", err)
	}
	if bp.DeviceType != int(DeviceTypeBloodPressure) || bp.Period != 8192 || bp.RFFreq != 57 {
		t.Fatalf("channel parameters: type=%d period=%d rf=%d", bp.DeviceType, bp.Period, bp.RFFreq)
	}

	got := make(chan BloodPressureData, 8)
	bp.OnDeviceData = func(page int, name string, d DeviceData) {
		if bd, ok := d.(BloodPressureData); ok {
			got <- bd
		}
	}

	// Page 1: 120/80 mmHg, MAP 93, HR 62, no flags.
	sim.EmitBroadcast(0, []byte{0x01, 0x78, 0x00, 0x50, 0x00, 93, 62, 0x00})
	select {
	case d := <-got:
		if d.Systolic != 120 || d.Diastolic != 80 || d.MAP != 93 || d.HeartRate != 62 || d.Flags != 0 {
			t.Fatalf("measurement page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for measurement page")
	}

	// All-invalid measurement: 0xFFFF/0xFFFF/0xFF/0.
	sim.EmitBroadcast(0, []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00})
	select {
	case d := <-got:
		if d.Systolic != BloodPressureInvalid || d.Diastolic != BloodPressureInvalid ||
			d.MAP != BloodPressureInvalid || d.HeartRate != BloodPressureHRInval {
			t.Fatalf("invalid measurement page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invalid measurement page")
	}

	// A short broadcast must not panic (P0-2 guard).
	sim.EmitBroadcast(0, []byte{0x01, 0x78})
}
