package devices

import (
	"log/slog"

	"github.com/maxdukov/openant-go/easy"
)

// HeartRateData is the ANT+ heart rate data page (openant HeartRateData).
type HeartRateData struct {
	PageSpecific          int     `influx:"page_specific"`
	BeatTime              float64 `influx:"beat_time"`
	BeatCount             int     `influx:"beat_count"`
	HeartRate             int     `influx:"heart_rate"` // bpm
	OperatingTime         int     `influx:"operating_time"`
	ManufacturerIDLSB     int     `influx:"manufacturer_id_lsb"`
	SerialNumber          int     `influx:"serial_number"`
	PreviousHeartBeatTime float64 `influx:"previous_heart_beat_time"`
	BatteryPercentage     int     `influx:"battery_percentage"`
	FeaturesSupported     int     `influx:"features_supported"`
	FeaturesEnabled       int     `influx:"features_enabled"`
}

// DataName implements DeviceData.
func (HeartRateData) DataName() string { return "HeartRateData" }

// HeartRate is the ANT+ heart rate monitor profile (device type 120).
type HeartRate struct {
	baseDevice
	Data HeartRateData
}

// NewHeartRate creates the profile on the given node.
func NewHeartRate(node *easy.Node, deviceID int, transType int) (*HeartRate, error) {
	h := &HeartRate{}
	h.node = node
	h.log = slog.Default()
	h.DeviceType = int(DeviceTypeHeartRate)
	h.DeviceID = deviceID
	h.Period = 8070
	h.RFFreq = 57
	h.Name = "heart_rate"
	h.TransType = transType
	h.Data = HeartRateData{
		PageSpecific: 0xFFFFFF, OperatingTime: 0xFFFFFF,
		ManufacturerIDLSB: 0xFF, SerialNumber: 0xFFFF,
		BeatTime: -1, PreviousHeartBeatTime: -1, BatteryPercentage: 0xFF,
	}
	h.onProfileData = h.onData
	if err := h.openChannel(true, 0); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *HeartRate) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	page := data[0]
	// MSB is the page toggle; pages 0-7 carry the common HR fields.
	if page&0x0F > 7 {
		return
	}
	dp := page & 0x0F

	h.Data.PageSpecific = int(data[1]) + int(data[2])<<8 + int(data[3])<<16
	h.Data.BeatTime = float64(int(data[4])+int(data[5])<<8) / 1024
	h.Data.BeatCount = int(data[6])
	h.Data.HeartRate = int(data[7])

	switch dp {
	case 0x01:
		h.Data.OperatingTime = h.Data.PageSpecific * 2
	case 0x02:
		h.Data.ManufacturerIDLSB = int(data[1])
		h.Data.SerialNumber = int(data[2]) + int(data[3])<<8
	case 0x04:
		h.Data.PreviousHeartBeatTime = float64(int(data[2])+int(data[3])<<8) / 1024
	case 0x06:
		h.Data.FeaturesSupported = int(data[2])
		h.Data.FeaturesEnabled = int(data[3])
	case 0x07:
		h.Data.BatteryPercentage = int(data[1])
		h.Common.LastBattery.VoltageFractional = float64(data[2]) / 256
		h.Common.LastBattery.VoltageCoarse = int(data[3] & 0x0F)
		h.Common.LastBattery.Status = BatteryStatus((data[3] & 0x70) >> 4)
		h.onBattery(h.Common.LastBattery)
	}

	h.fireDeviceData(int(page), "heart_rate", h.Data)
}

func init() {
	registerProfile(DeviceTypeHeartRate, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewHeartRate(node, deviceID, transType)
	})
}
