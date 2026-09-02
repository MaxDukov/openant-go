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
		drafting    int
		want        []byte
		wantErr     bool
	}{
		{"typical", 0.51, -10, 1, []byte{0x32, 51, 0xF6, 0x01, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"zero coefficient", 0, 0, 0, []byte{0x32, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"full drafting", 1.86, 127, 3, []byte{0x32, 186, 127, 0x03, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"reserved drafting passes through", 0.5, 5, 8, []byte{0x32, 50, 5, 0x08, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"coefficient too large", 2.55, 0, 1, nil, true},
		{"negative coefficient", -0.01, 0, 1, nil, true},
		{"wind speed too fast", 0.5, 128, 1, nil, true},
		{"wind speed too slow", 0.5, -128, 1, nil, true},
		{"drafting too large", 0.5, 0, 16, nil, true},
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
		{"flat road", 0, 0.004, []byte{0x33, 0, 0, 4, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"uphill 5%", 5.0, 0, []byte{0x33, 0xF4, 0x01, 0, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"downhill 40%", -40.0, 0.015, []byte{0x33, 0x60, 0xF0, 15, 0xFF, 0xFF, 0xFF, 0xFF}, false},
		{"grade too steep", 40.01, 0, nil, true},
		{"grade too low", -40.01, 0, nil, true},
		{"negative rolling", 0, -0.001, nil, true},
		{"rolling too large", 0, 0.016, nil, true},
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
