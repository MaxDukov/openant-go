package devices

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/maxdukov/openant-go/easy"
)

// deviceProfiles maps profile types to constructors (openant
// device_profiles dict + utilities.auto_create_device).
var deviceProfiles = map[DeviceType]func(node *easy.Node, deviceID int, transType int) (any, error){}

func registerProfile(t DeviceType, ctor func(node *easy.Node, deviceID int, transType int) (any, error)) {
	deviceProfiles[t] = ctor
}

// AutoCreateDevice instantiates the device profile for the given type,
// matching openant.devices.utilities.auto_create_device.
func AutoCreateDevice(node *easy.Node, deviceID int, deviceType DeviceType, transType int) (any, error) {
	ctor, ok := deviceProfiles[deviceType]
	if !ok {
		return nil, fmt.Errorf("devices: unknown device profile %v", deviceType)
	}
	return ctor(node, deviceID, transType)
}

// JSONDevice is one entry of a scan config file.
type JSONDevice struct {
	Device           string `json:"device"`
	ID               int    `json:"id"`
	Type             int    `json:"type"`
	TransmissionType int    `json:"transmission_type"`
	Serial           int    `json:"serial"`
}

// ReadJSONDevices reads a devices.json produced by the scanner; returns nil
// when the file does not exist (openant read_json returns False).
func ReadJSONDevices(path string) ([]JSONDevice, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var devices []JSONDevice
	if err := json.Unmarshal(b, &devices); err != nil {
		return nil, fmt.Errorf("devices: parse %s: %w", path, err)
	}
	return devices, nil
}
