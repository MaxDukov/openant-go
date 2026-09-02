package devices

import (
	"bytes"
	"testing"
)

func TestEncodeWindResistancePage(t *testing.T) {
	tests := []struct {
		name        string
		coefficient float64
		windSpeed   int
		drafting    float64
		want        []byte
		wantErr     bool
	}{
		// Byte layouts per ANT+ FE-C Rev 5.0 Tables 8-38/8-41: byte 5
		// coefficient (0.01 kg/m), byte 6 wind speed with -127 km/h
		// offset (0x7F = 0 km/h), byte 7 drafting factor (0.01 units,
		// 0x64 = 1.00 no drafting).
		{"road default 0.51 no drafting", 0.51, 0, 1.0, []byte{0x32, 0xFF, 0xFF, 0xFF, 0xFF, 51, 0x7F, 0x64}, false},
		{"tailwind -10 km/h", 0.4, -10, 0.5, []byte{0x32, 0xFF, 0xFF, 0xFF, 0xFF, 40, 0x75, 0x32}, false},
		{"headwind +127", 2.54, 127, 0, []byte{0x32, 0xFF, 0xFF, 0xFF, 0xFF, 254, 0xFE, 0x00}, false},
		{"coefficient too large", 2.55, 0, 1, nil, true},
		{"negative coefficient", -0.01, 0, 1, nil, true},
		{"wind speed too fast", 0.5, 128, 1, nil, true},
		{"wind speed too slow", 0.5, -128, 1, nil, true},
		{"drafting too large", 0.5, 0, 1.01, nil, true},
		{"negative drafting", 0.5, 0, -0.1, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeWindResistancePage(tt.coefficient, tt.windSpeed, tt.drafting)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("got %x, want %x", got, tt.want)
			}
		})
	}
}

func TestEncodeTrackResistancePage(t *testing.T) {
	tests := []struct {
		name    string
		grade   float64
		rolling float64
		want    []byte
		wantErr bool
	}{
		// Byte layouts per ANT+ FE-C Rev 5.0 Table 8-42/8-43: bytes 5-6
		// grade in 0.01 % with a -200 % offset (0x4E20 = 0 %), byte 7
		// rolling resistance in 5x10^-5 units.
		{"flat road", 0, 0.004, []byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0x20, 0x4E, 80}, false},
		{"uphill 5%", 5.0, 0, []byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0x14, 0x50, 0}, false},
		{"downhill 10%", -10.0, 0.0127, []byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0x38, 0x4A, 254}, false},
		{"uphill 200%", 200.0, 0, []byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0x40, 0x9C, 0}, false},
		{"grade too steep", 200.01, 0, nil, true},
		{"grade too low", -200.01, 0, nil, true},
		{"negative rolling", 0, -0.001, nil, true},
		{"rolling too large", 0, 0.0128, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeTrackResistancePage(tt.grade, tt.rolling)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("got %x, want %x", got, tt.want)
			}
		})
	}
}

func TestEncodeUserConfigPage(t *testing.T) {
	tests := []struct {
		name      string
		userKg    float64
		bicycleKg float64
		wheelM    float64
		gear      float64
		want      []byte
		wantErr   bool
	}{
		// Byte layout per ANT+ FE-C Rev 5.0 Table 8-47.
		{"75 kg rider, 10 kg bike, 2096 mm wheel, no gear", 75, 10, 2.096, 0,
			[]byte{0x37, 0x4C, 0x1D, 0xFF, 0x86, 0x0C, 209, 0}, false},
		{"gear ratio passes through", 0, 0, 0, 7.65,
			[]byte{0x37, 0, 0, 0xFF, 0, 0, 0, 255}, false},
		{"wheel offset nibble", 0, 0, 0.705, 0,
			[]byte{0x37, 0, 0, 0xFF, 0x05, 0, 70, 0}, false},
		{"user weight too heavy", 655.35, 0, 0, 0, nil, true},
		{"bicycle weight too heavy", 0, 50.1, 0, 0, nil, true},
		{"wheel too large", 0, 0, 2.6, 0, nil, true},
		{"gear too large", 0, 0, 0, 7.7, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeUserConfigPage(tt.userKg, tt.bicycleKg, tt.wheelM, tt.gear)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("got %x, want %x", got, tt.want)
			}
		})
	}
}

func TestManufacturerName(t *testing.T) {
	tests := []struct {
		id   int
		want string
	}{
		{1, "Garmin"},
		{15, "Dynastream"},
		{32, "Wahoo Fitness"},
		{89, "Tacx"},
		{107, "Magene"},
		{144, "Zwift"},
		{255, "Development"},
		{0, "Unknown (0)"},
		{0xFFFF, "Unknown (65535)"},
	}
	for _, tt := range tests {
		if got := ManufacturerName(tt.id); got != tt.want {
			t.Errorf("ManufacturerName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
