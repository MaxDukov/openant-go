package devices

import (
	"testing"
	"time"
)

func TestStrideSpeedDistancePage(t *testing.T) {
	n, sim := newTestNode(t)
	s, err := NewStrideSpeedDistance(n, 0, 0)
	if err != nil {
		t.Fatalf("NewStrideSpeedDistance: %v", err)
	}
	got := make(chan StrideSpeedDistanceData, 4)
	s.OnDeviceData = func(page int, name string, d DeviceData) {
		if dd, ok := d.(StrideSpeedDistanceData); ok {
			got <- dd
		}
	}

	// speed 3.5 m/s (int 3, frac 128/256), latency 1.5 s (1 + 100/200),
	// distance 12 + 4/16 m, 42 strides.
	sim.EmitBroadcast(0, []byte{0x01, 100, 1, 12, 0x43, 128, 42, 0xFF})
	select {
	case d := <-got:
		if d.Speed != 3.5 || d.UpdateLatency != 1.5 || d.Distance != 12.25 || d.StrideCount != 42 {
			t.Fatalf("data = %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SDM data")
	}

	// Invalid speed bytes (0x?F + 0xFF): speed must be retained.
	sim.EmitBroadcast(0, []byte{0x01, 100, 1, 12, 0xFF, 0xFF, 43, 0xFF})
	select {
	case d := <-got:
		if d.Speed != 3.5 {
			t.Fatalf("invalid speed overwrote data: %+v", d)
		}
		if d.StrideCount != 43 {
			t.Fatalf("stride count not updated: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SDM data")
	}
}

func TestWeightScalePages(t *testing.T) {
	n, sim := newTestNode(t)
	w, err := NewWeightScale(n, 0, 0)
	if err != nil {
		t.Fatalf("NewWeightScale: %v", err)
	}
	got := make(chan WeightScaleData, 8)
	w.OnDeviceData = func(page int, name string, d DeviceData) {
		if wd, ok := d.(WeightScaleData); ok {
			got <- wd
		}
	}

	// Page 1: profile 0x0123, weight 75.32 kg.
	sim.EmitBroadcast(0, []byte{0x01, 0x23, 0x01, 0xFF, 0xFF, 0xFF, 0x6C, 0x1D})
	select {
	case d := <-got:
		if d.UserProfile != 0x123 || d.Weight != 75.32 {
			t.Fatalf("weight page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for weight page")
	}

	// Page 2: hydration 55.5 %, body fat 18.25 %.
	sim.EmitBroadcast(0, []byte{0x02, 0x23, 0x01, 0xFF, 0xAE, 0x15, 0x21, 0x07})
	select {
	case d := <-got:
		if d.Hydration != 55.5 || d.BodyFat != 18.25 {
			t.Fatalf("composition page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for composition page")
	}

	// Page 3: active MET 2000 kcal, basal MET 1600.25 kcal.
	sim.EmitBroadcast(0, []byte{0x03, 0x23, 0x01, 0xFF, 0x40, 0x1F, 0x01, 0x19})
	select {
	case d := <-got:
		if d.ActiveMetabolic != 2000 || d.BasalMetabolic != 1600.25 {
			t.Fatalf("metabolic page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for metabolic page")
	}

	// Page 4: muscle mass 32.5 kg, bone mass 3.1 kg.
	sim.EmitBroadcast(0, []byte{0x04, 0x23, 0x01, 0xFF, 0xFF, 0xB2, 0x0C, 31})
	select {
	case d := <-got:
		if d.MuscleMass != 32.5 || d.BoneMass != 3.1 {
			t.Fatalf("mass page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for mass page")
	}

	// Page 0x3A: male, 42 years, 180 cm.
	sim.EmitBroadcast(0, []byte{0x3A, 0x23, 0x01, 0xFF, 0xFF, 0xAA, 180, 0xFF})
	select {
	case d := <-got:
		if d.Gender != "M" || d.Age != 42 || d.Height != 180 {
			t.Fatalf("user profile page: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for user profile page")
	}

	// Invalid weight (0xFFFF) must not overwrite the previous value.
	sim.EmitBroadcast(0, []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	select {
	case d := <-got:
		if d.Weight != 75.32 || d.UserProfile != 0x123 {
			t.Fatalf("invalid page overwrote data: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invalid page")
	}
}
