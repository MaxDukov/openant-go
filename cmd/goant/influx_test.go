package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/devices"
)

func TestBuildLine(t *testing.T) {
	ts := time.Unix(1700000000, 123456789)
	fields := []devices.InfluxField{
		{Key: "heart_rate", Value: int64(93)},
		{Key: "speed", Value: 2.5},
		{Key: "name", Value: `he said "hi"`},
		{Key: "ok", Value: true},
	}
	got := buildLine("HeartRateData", "host=mypc", "device=HeartRate,id=123", fields, ts)
	want := `HeartRateData,host=mypc,device=HeartRate,id=123 heart_rate=93i,speed=2.5,name="he said \"hi\"",ok=true 1700000000123456789`
	if got != want {
		t.Errorf("buildLine:\n got %s\nwant %s", got, want)
	}
}

func TestBuildLineEscaping(t *testing.T) {
	// buildLine escapes measurements, tag pairs and field keys it
	// renders itself; pre-rendered tag strings pass through verbatim.
	ts := time.Unix(1, 0)
	got := buildLine("My Data, v2", "", "device=Foo Bar=id", []devices.InfluxField{{Key: "a b", Value: int64(1)}}, ts)
	want := `My\ Data\,\ v2,device=Foo Bar=id a\ b=1i 1000000000`
	if got != want {
		t.Errorf("escaping:\n got %s\nwant %s", got, want)
	}
	got = buildLine("M", "", specFields(deviceSpec{name: "Foo Bar", id: 3}), []devices.InfluxField{{Key: "a b", Value: int64(1)}}, ts)
	want = `M,device=Foo\ Bar,id=3 a\ b=1i 1000000000`
	if got != want {
		t.Errorf("spec tags:\n got %s\nwant %s", got, want)
	}
}

func TestBuildLineEmpty(t *testing.T) {
	if got := buildLine("M", "", "", nil, time.Unix(1, 0)); got != "" {
		t.Errorf("empty fields should yield empty line, got %q", got)
	}
}

func TestFormatFieldValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{int64(42), "42i", true},
		{2.5, "2.5", true},
		{float64(7), "7i", true}, // integral floats stay integer typed
		{"s", `"s"`, true},
		{true, "true", true},
		{[]int{1}, "", false},
	}
	for _, c := range cases {
		got, ok := formatFieldValue(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("formatFieldValue(%#v) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNewInfluxWriterURL(t *testing.T) {
	a := &influxArgs{}
	a.db = "ant"
	w, err := newInfluxWriter(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w.writeURL, "/write?db=ant&precision=ns") {
		t.Errorf("v1 URL = %s", w.writeURL)
	}

	a2 := &influxArgs{}
	a2.token = "tok"
	a2.org = "default"
	a2.bucket = "bkt"
	w2, err := newInfluxWriter(a2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w2.writeURL, "/api/v2/write?org=default&bucket=bkt&precision=ns") {
		t.Errorf("v2 URL = %s", w2.writeURL)
	}
	if w2.token != "tok" {
		t.Errorf("token not stored")
	}
}

func TestNewInfluxWriterValidation(t *testing.T) {
	if _, err := newInfluxWriter(&influxArgs{}); err == nil {
		t.Error("expected error without -db")
	}
}

func TestSpecFields(t *testing.T) {
	got := specFields(deviceSpec{name: "Bike Speed", id: 5, dtype: devices.DeviceTypeBikeSpeed})
	if got != `device=Bike\ Speed,id=5` {
		t.Errorf("specFields = %q", got)
	}
}

func TestInfluxWriterPost(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	w, err := newInfluxWriter(&influxArgs{url: srv.URL, db: "ant", interval: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.write(deviceSpec{name: "HeartRate", id: 123, dtype: devices.DeviceTypeHeartRate}, 0, "Background", devices.HeartRateData{HeartRate: 88})
	if !strings.HasPrefix(gotPath, "/write?db=ant&precision=ns") {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, ",uuid=") || !strings.Contains(gotBody, ",device=HeartRate,id=123 ") {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.Contains(gotBody, "heart_rate=88i") {
		t.Errorf("body = %q", gotBody)
	}
}

func TestInfluxWriterPostV2Auth(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	w, err := newInfluxWriter(&influxArgs{url: srv.URL, token: "secret", org: "me", bucket: "bkt", interval: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.write(deviceSpec{name: "HeartRate", id: 1, dtype: devices.DeviceTypeHeartRate}, 0, "Background", devices.HeartRateData{HeartRate: 50})
	if !strings.HasPrefix(gotPath, "/api/v2/write?org=me&bucket=bkt&precision=ns") {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Token secret" {
		t.Errorf("auth = %q", gotAuth)
	}
}
