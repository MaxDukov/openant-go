package devices

import (
	"log/slog"

	"github.com/maxdukov/openant-go/easy"
)

// StrideSpeedDistanceData is the ANT+ stride based speed and distance
// monitor main data page (openant issue #125).
type StrideSpeedDistanceData struct {
	// UpdateLatency is the time since the last change to speed or
	// distance, in seconds (integer + 1/200 s fractional).
	UpdateLatency float64 `influx:"update_latency"`
	// Distance in metres (1 m integer + 1/16 m fractional, rollover 256).
	Distance float64 `influx:"distance"`
	// Speed in metres per second (1 m/s integer + 1/256 m/s fractional).
	Speed float64 `influx:"speed"`
	// StrideCount is the accumulated stride count (rollover 256).
	StrideCount int `influx:"stride_count"`
}

// DataName implements DeviceData.
func (StrideSpeedDistanceData) DataName() string { return "StrideSpeedDistanceData" }

// StrideSpeedDistance is the ANT+ stride based speed and distance monitor
// profile (device type 124, e.g. a foot pod).
type StrideSpeedDistance struct {
	baseDevice
	Data StrideSpeedDistanceData
}

// NewStrideSpeedDistance creates the profile on the given node.
func NewStrideSpeedDistance(node *easy.Node, deviceID int, transType int) (*StrideSpeedDistance, error) {
	s := &StrideSpeedDistance{}
	s.node = node
	s.log = slog.Default()
	s.DeviceType = int(DeviceTypeStrideSpeed)
	s.DeviceID = deviceID
	s.Period = 8134
	s.RFFreq = 57
	s.Name = "stride_speed_distance"
	s.TransType = transType
	s.onProfileData = s.onData
	if err := s.openChannel(true, 0); err != nil {
		return nil, err
	}
	return s, nil
}

// onData decodes SDM data page 0x01 (main data page):
//
//	byte 1: update latency fractional (1/200 s units)
//	byte 2: update latency integral (s)
//	byte 3: distance integral (m)
//	byte 4: bits 7-4 distance fractional (1/16 m), bits 3-0 speed integral (m/s)
//	byte 5: speed fractional (1/256 m/s)
//	byte 6: stride count
//	byte 7: reserved
//
// This mirrors the page layout exercised by openant's broadcast_send
// example (the only SSDP reference in the Python library).
func (s *StrideSpeedDistance) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	if data[0]&0x7F != 0x01 {
		return
	}
	if data[1] != 0xFF || data[2] != 0xFF {
		s.Data.UpdateLatency = float64(data[2]) + float64(data[1])/200
	}
	if data[3] != 0xFF || data[4]>>4 != 0x0F {
		s.Data.Distance = float64(data[3]) + float64(data[4]>>4)/16
	}
	if data[4]&0x0F != 0x0F || data[5] != 0xFF {
		s.Data.Speed = float64(data[4]&0x0F) + float64(data[5])/256
	}
	if data[6] != 0xFF {
		s.Data.StrideCount = int(data[6])
	}
	s.log.Info("stride speed distance", "device", s.String(),
		"speed", s.Data.Speed, "distance", s.Data.Distance,
		"strides", s.Data.StrideCount, "latency", s.Data.UpdateLatency)
	s.fireDeviceData(int(data[0]&0x7F), "stride_speed_distance", s.Data)
}

func init() {
	registerProfile(DeviceTypeStrideSpeed, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewStrideSpeedDistance(node, deviceID, transType)
	})
}
