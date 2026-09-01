package devices_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maxdukov/openant-go/devices"
)

// This example exports decoded device data as a tag/field map, the format
// used by line-protocol writers such as InfluxDB.
func ExampleToInfluxJSON() {
	data := devices.HeartRateData{
		PageSpecific:      0x0E,
		BeatCount:         176,
		HeartRate:         72,
		BatteryPercentage: 90,
	}
	res := devices.ToInfluxJSON(data, map[string]string{"device": "HRM-123"})
	fields := res["fields"].(map[string]any)
	fmt.Println(res["measurement"], fields["heart_rate"], fields["battery_percentage"])
	// Output: HeartRateData 72 90
}

// This example loads a devices.json previously written by Scanner.Save.
func ExampleReadJSONDevices() {
	path := filepath.Join(os.TempDir(), "example-devices.json")
	content := `{"devices":[{"device":"HRM","id":12345,"type":120,"transmission_type":1,"serial":67890}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		fmt.Println("error:", err)
		return
	}
	defer os.Remove(path)

	devs, err := devices.ReadJSONDevices(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, d := range devs {
		fmt.Printf("%s: id=%d type=%d\n", d.Device, d.ID, d.Type)
	}
	// Output: HRM: id=12345 type=120
}
