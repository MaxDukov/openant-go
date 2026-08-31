package devices

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/maxdukov/openant-go/easy"
)

// DeviceTuple identifies a found device: (id, type, transmission type).
type DeviceTuple struct {
	ID    int
	Type  int
	Trans int
}

// String renders the tuple like openant's "(id, type, trans)".
func (t DeviceTuple) String() string { return fmt.Sprintf("(%d, %d, %d)", t.ID, t.Type, t.Trans) }

// Scanner discovers ANT+ devices in range using a wildcard channel with
// extended messages. Unlike profiles it does not attach to a single device
// but tracks every device seen (openant.devices.scanner.Scanner).
type Scanner struct {
	baseDevice

	mu     sync.Mutex
	found  map[DeviceTuple]struct{}
	common map[string]CommonData

	// OnScanFound is called for each newly discovered device.
	OnScanFound func(t DeviceTuple)
	// OnScanUpdate is called when a device updates its common pages and
	// the data actually changed.
	OnScanUpdate func(t DeviceTuple, common CommonData)
}

// NewScanner creates the scanner. A deviceID of 0 and deviceType 0 (or
// DeviceTypeUnknown) scans for everything.
func NewScanner(node *easy.Node, deviceID int, deviceType DeviceType, transType int) (*Scanner, error) {
	s := &Scanner{
		found:  map[DeviceTuple]struct{}{},
		common: map[string]CommonData{},
	}
	s.node = node
	s.log = slog.Default()
	s.DeviceID = deviceID
	s.DeviceType = int(deviceType)
	s.Period = 8070
	s.RFFreq = 57
	s.TransType = transType
	s.Name = "scanner"
	s.onDataFull = s.scanData
	if err := s.openChannel(true, 0); err != nil {
		return nil, err
	}
	return s, nil
}

// scanData implements the scanning data handler.
func (s *Scanner) scanData(data []byte) {
	if len(data) <= 8 {
		return
	}
	deviceID := int(data[9]) + int(data[10])<<8
	deviceType := int(data[11])
	transType := int(data[12])
	tuple := DeviceTuple{ID: deviceID, Type: deviceType, Trans: transType}
	key := fmt.Sprintf("%d:%d", deviceID, deviceType)

	s.mu.Lock()
	if _, seen := s.found[tuple]; !seen {
		s.common[key] = CommonData{}
		s.found[tuple] = struct{}{}
		cb := s.OnScanFound
		s.mu.Unlock()
		s.log.Info("found new device", "tuple", tuple.String())
		if cb != nil {
			cb(tuple)
		}
		s.mu.Lock()
	}

	common := s.common[key]
	updated := common
	switch data[0] {
	case 80: // manufacturer info
		updated.HardwareRev = int(data[3])
		updated.ManufacturerID = int(data[4]) + int(data[5])<<8
		updated.ModelNo = int(data[6]) + int(data[7])<<8
	case 81: // product info
		swRev := data[2]
		swMain := data[3]
		if swRev == 0xFF {
			updated.SoftwareVer = fmt.Sprintf("%v", float64(swMain)/10)
		} else {
			updated.SoftwareVer = fmt.Sprintf("%v", float64(swMain*100+swRev)/1000)
		}
		updated.SerialNo = uint32(data[4]) | uint32(data[5])<<8 | uint32(data[6])<<16 | uint32(data[7])<<24
	default:
		s.mu.Unlock()
		return
	}

	changed := updated != common
	s.common[key] = updated
	cb := s.OnScanUpdate
	s.mu.Unlock()

	// Only fire the callback when the data actually changed.
	if changed && cb != nil {
		cb(tuple, updated)
	}
}

// FoundDevices returns the devices discovered so far.
func (s *Scanner) FoundDevices() []DeviceTuple {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DeviceTuple, 0, len(s.found))
	for t := range s.found {
		out = append(out, t)
	}
	return out
}

// Common returns the common data of a scanned device.
func (s *Scanner) Common(t DeviceTuple) CommonData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.common[fmt.Sprintf("%d:%d", t.ID, t.Type)]
}

// Save writes the found devices to a JSON file, merging with existing
// content (openant Scanner.save).
func (s *Scanner) Save(path string) error {
	devices := []JSONDevice{}
	if b, err := os.ReadFile(path); err == nil {
		var data struct {
			Devices []JSONDevice `json:"devices"`
		}
		if json.Unmarshal(b, &data) == nil {
			devices = data.Devices
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	existing := map[int]bool{}
	for _, d := range devices {
		existing[d.ID] = true
	}

	s.mu.Lock()
	for t := range s.found {
		if existing[t.ID] {
			continue
		}
		c := s.common[fmt.Sprintf("%d:%d", t.ID, t.Type)]
		devices = append(devices, JSONDevice{
			Device:           DeviceType(t.Type).String(),
			ID:               t.ID,
			Type:             t.Type,
			TransmissionType: t.Trans,
			Serial:           int(c.SerialNo),
		})
	}
	s.mu.Unlock()

	out := map[string]any{"devices": devices}
	b, err := json.MarshalIndent(out, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
