package devices

import (
	"testing"
	"time"
)

// testPayload exercises every InfluxFields conversion rule.
type testPayload struct {
	Plain   int `influx:"plain"`
	Skipped int `influx:"-"`
	NoTag   string
	Ptr     *float64      `influx:"ptr"`
	PtrNil  *int          `influx:"ptr_nil"`
	Arr     [3]int        `influx:"arr"`
	ArrPtr  [2]*int       `influx:"arr_ptr"`
	Nested  CommonData    `influx:"nested"`
	When    time.Time     `influx:"when"`
	Enum    BatteryStatus `influx:"enum"`
	Byte    byte          `influx:"b"`
	Uint    uint16        `influx:"u"`
}

func (testPayload) DataName() string { return "TestPayload" }

func fieldMap(fields []InfluxField) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		m[f.Key] = f.Value
	}
	return m
}

func TestInfluxFieldsConversions(t *testing.T) {
	p2 := 2.5
	zero := 0
	tp := testPayload{
		Plain:   7,
		Skipped: 99,
		NoTag:   "hello",
		Ptr:     &p2,
		Arr:     [3]int{1, 2, 3},
		ArrPtr:  [2]*int{nil, &zero},
		Nested:  CommonData{ManufacturerID: 42, SoftwareVer: "1.0"},
		When:    time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC),
		Enum:    BatteryStatusGood,
		Byte:    3,
		Uint:    65535,
	}
	got := fieldMap(InfluxFields(tp))

	want := map[string]any{
		"plain":                  int64(7),
		"NoTag":                  "hello",
		"ptr":                    2.5,
		"arr_0":                  int64(1),
		"arr_1":                  int64(2),
		"arr_2":                  int64(3),
		"arr_ptr_1":              int64(0),
		"nested_manufacturer_id": int64(42),
		"nested_SoftwareVer":     "1.0",
		"when":                   "2026-09-04T12:00:00Z",
		"enum":                   int64(2),
		"b":                      int64(3),
		"u":                      int64(65535),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %s = %#v, want %#v", k, got[k], v)
		}
	}
	for _, absent := range []string{"skipped", "ptr_nil", "arr_ptr_0"} {
		if _, ok := got[absent]; ok {
			t.Errorf("field %s should be skipped, got %#v", absent, got[absent])
		}
	}
}

func TestInfluxFieldsBikeSpeedData(t *testing.T) {
	speed := 31.4
	d := BikeSpeedData{
		CumulativeOperatingTime:   120,
		BikeSpeedEventTime:        [2]float64{1.5, 2.25},
		CumulativeSpeedRevolution: [2]int{10, 14},
		CalculatedSpeed:           &speed,
	}
	fields := InfluxFields(d)
	if fields[0].Key != "cumulative_operating_time" || fields[0].Value != int64(120) {
		t.Errorf("first field = %+v", fields[0])
	}
	m := fieldMap(fields)
	if m["bike_speed_event_time_0"] != 1.5 || m["bike_speed_event_time_1"] != 2.25 {
		t.Errorf("event time expansion: %v", m)
	}
	if m["cumulative_speed_revolution_1"] != int64(14) {
		t.Errorf("revolution expansion: %v", m)
	}
	if m["calculated_speed"] != 31.4 {
		t.Errorf("pointer deref: %v", m["calculated_speed"])
	}
	if _, ok := m["calculated_distance"]; ok {
		t.Error("nil calculated_distance should be skipped")
	}
}

func TestInfluxMeasurement(t *testing.T) {
	if got := InfluxMeasurement(HeartRateData{}); got != "HeartRateData" {
		t.Errorf("measurement = %q", got)
	}
	if got := InfluxMeasurement(BikeSpeedData{}); got != "BikeSpeedData" {
		t.Errorf("measurement = %q", got)
	}
}
