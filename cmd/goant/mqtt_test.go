package main

import (
	"strings"
	"testing"

	"github.com/maxdukov/openant-go/devices"
)

func TestDeviceTopicListSet(t *testing.T) {
	var l deviceTopicList
	if err := l.Set("HeartRate:123:sensors/hr"); err != nil {
		t.Fatal(err)
	}
	if err := l.Set("121:5:combo"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"nocolon", "HeartRate:abc:x", "HeartRate:123"} {
		if err := l.Set(bad); err == nil {
			t.Errorf("Set(%q) should fail", bad)
		}
	}
	if got := l.baseFor(devices.DeviceTypeHeartRate, 123); got != "sensors/hr" {
		t.Errorf("baseFor by name = %q", got)
	}
	if got := l.baseFor(devices.DeviceTypeBikeSpeedCadence, 5); got != "combo" {
		t.Errorf("baseFor by number = %q", got)
	}
	if got := l.baseFor(devices.DeviceTypeHeartRate, 999); got != "" {
		t.Errorf("baseFor miss = %q", got)
	}
	if !strings.Contains(l.String(), "HeartRate:123:sensors/hr") {
		t.Errorf("String() = %q", l.String())
	}
}

func TestDataPayload(t *testing.T) {
	beat := 1024.0
	d := devices.HeartRateData{HeartRate: 90, BeatTime: beat}
	p := dataPayload(d)
	if p["_type"] != "HeartRateData" {
		t.Errorf("_type = %v", p["_type"])
	}
	if p["heart_rate"] != int64(90) {
		t.Errorf("heart_rate = %v", p["heart_rate"])
	}
	if p["beat_time"] != beat {
		t.Errorf("beat_time = %v", p["beat_time"])
	}
}

func TestEscapeTopic(t *testing.T) {
	if got := escapeTopic("a/b c"); got != "a-b c" {
		t.Errorf("escapeTopic = %q", got)
	}
}
