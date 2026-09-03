package devices

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

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
	HwVersion             int     `influx:"hw_version"`
	SwVersion             int     `influx:"sw_version"`
	ModelNumber           int     `influx:"model_number"`
	IntervalAverageHR     int     `influx:"interval_average_hr"`
	IntervalMaximumHR     int     `influx:"interval_maximum_hr"`
	SessionAverageHR      int     `influx:"session_average_hr"`
}

// DataName implements DeviceData.
func (HeartRateData) DataName() string { return "HeartRateData" }

// HeartRate is the ANT+ heart rate monitor profile (device type 120). The
// same type doubles as the display-side decoder (NewHeartRate) and, via
// NewHeartRateMaster, as a sensor emulator transmitting pages 0-4 per the
// ANT+ Heart Rate Monitor spec Rev 2.1.
type HeartRate struct {
	baseDevice
	Data HeartRateData

	// Master state (beat simulation).
	txStart   time.Time
	lastBeat  time.Time
	prevBeat  time.Time
	beatCount uint8
	bgCount   int // background page rotation
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
		HwVersion: -1, SwVersion: -1, ModelNumber: -1,
		IntervalAverageHR: -1, IntervalMaximumHR: -1, SessionAverageHR: -1,
	}
	h.onProfileData = h.onData
	if err := h.openChannel(true, 0); err != nil {
		return nil, err
	}
	return h, nil
}

// NewHeartRateMaster creates a simulated ANT+ heart rate sensor
// (spec Rev 2.1 section 5.2: master channel, period 8070, RF 57,
// device type 120, transmission type LSN 1). A device number of 0 is
// replaced with a random one because the spec forbids 0x0000.
//
// Set the simulated values through the Data fields (HeartRate,
// OperatingTime, ManufacturerIDLSB, SerialNumber and the product info
// fields), set the battery via Common.LastBattery, then call
// SetHeartRate to start beating. Every profile page is transmitted
// according to the spec's background page schedule (pages 1-3 every
// 65th message); pages 6 (capabilities) and 7 (battery status) are
// only sent in reply to a page request from a display.
func NewHeartRateMaster(node *easy.Node, deviceID int) (*HeartRate, error) {
	if deviceID == 0 {
		deviceID = rand.IntN(65535) + 1
	}
	h := &HeartRate{}
	h.node = node
	h.log = slog.Default()
	h.master = true
	h.DeviceType = int(DeviceTypeHeartRate)
	h.DeviceID = deviceID
	h.Period = 8070
	h.RFFreq = 57
	h.Name = "heart_rate_master"
	h.TransType = 1
	h.Data = HeartRateData{
		HeartRate: 0, ManufacturerIDLSB: 255, // development ID
		BatteryPercentage: 0xFF,
	}
	now := time.Now()
	h.txStart, h.lastBeat, h.prevBeat = now, now, now
	h.onProfileTXFull = h.txPage
	h.onProfileAck = h.ackPage
	if err := h.openChannel(true, 0); err != nil {
		return nil, err
	}
	return h, nil
}

// SetHeartRate sets the simulated instantaneous heart rate in bpm
// (0-255); 0 marks the computed heart rate invalid and stops beating
// (the beat count then no longer changes).
func (h *HeartRate) SetHeartRate(bpm int) error {
	if bpm < 0 || bpm > 255 {
		return fmt.Errorf("heart rate %d out of range 0..255 bpm", bpm)
	}
	h.Data.HeartRate = bpm
	return nil
}

// beat advances the simulated beat clock to now.
func (h *HeartRate) beat(now time.Time) {
	bpm := h.Data.HeartRate
	if bpm <= 0 {
		return
	}
	period := time.Minute / time.Duration(bpm)
	for now.Sub(h.lastBeat) >= period {
		h.prevBeat = h.lastBeat
		h.lastBeat = h.lastBeat.Add(period)
		h.beatCount++
	}
}

// beatTimeRaw renders a beat moment in 1/1024 s units relative to the
// session start, as u16 with the spec's 63.999 s rollover.
func beatTimeRaw(t, start time.Time) uint16 {
	n := int((t.Sub(start)) / (time.Second / 1024))
	return uint16(n & 0xFFFF)
}

// pageToggle implements the spec's page change toggle bit 7, flipped
// every 4th message (section 6.4.1).
func pageToggle(count int) byte {
	return byte(((count >> 2) & 1) << 7)
}

// txPage builds the page scheduled at message count (spec Rev 2.1
// section 6.2.2: a background page 1-3 every 65th message, otherwise
// the main page 0/4; common pages 80/81 are not part of the HRM
// profile's transmitter data).
func (h *HeartRate) txPage(count int) []byte {
	now := time.Now()
	h.beat(now)
	t := pageToggle(count)
	bt := beatTimeRaw(h.lastBeat, h.txStart)
	hr := byte(h.Data.HeartRate)

	// Background pages 1-3 every 65th message.
	if count > 0 && count%65 == 64 {
		h.bgCount++
		switch h.bgCount % 3 {
		case 1: // cumulative operating time (Table 6-5), 2 s LSB
			op := h.Data.OperatingTime / 2
			return []byte{0x01 | t, byte(op), byte(op >> 8), byte(op >> 16), 0xFF, 0xFF, 0xFF, 0xFF}
		case 2: // manufacturer info (Table 6-6): upper 16 bits of the serial
			return []byte{0x02 | t, byte(h.Data.ManufacturerIDLSB), byte(h.Data.SerialNumber), byte(h.Data.SerialNumber >> 8), 0xFF, 0xFF, 0xFF, 0xFF}
		default: // product info (Table 6-7)
			return []byte{0x03 | t, byte(h.Data.HwVersion), byte(h.Data.SwVersion), byte(h.Data.ModelNumber), 0xFF, 0xFF, 0xFF, 0xFF}
		}
	}

	// Main page: previous heart beat (Table 6-8) once there is a
	// previous beat, default page (Table 6-3/6-4) before that.
	if !h.prevBeat.Equal(h.txStart) {
		pt := beatTimeRaw(h.prevBeat, h.txStart)
		return []byte{0x04 | t, 0xFF, byte(pt), byte(pt >> 8), byte(bt), byte(bt >> 8), h.beatCount, hr}
	}
	return []byte{0x00 | t, 0xFF, 0xFF, 0xFF, byte(bt), byte(bt >> 8), h.beatCount, hr}
}

// ackPage answers a display's request data page (common page 70) for
// the optional background pages 6 (capabilities, Table 6-10) and 7
// (battery status, Table 6-11).
func (h *HeartRate) ackPage(data []byte) []byte {
	if len(data) < 2 || data[0] != 0x46 {
		return nil
	}
	t := pageToggle(0)
	switch data[1] {
	case 0x06:
		return []byte{0x06 | t, 0xFF, byte(h.Data.FeaturesSupported), byte(h.Data.FeaturesEnabled), 0xFF, 0xFF, 0xFF, 0xFF}
	case 0x07:
		frac := byte(h.Common.LastBattery.VoltageFractional * 256)
		desc := byte(h.Common.LastBattery.VoltageCoarse&0x0F) | byte(h.Common.LastBattery.Status&0x07)<<4
		return []byte{0x07 | t, byte(h.Data.BatteryPercentage), frac, desc, 0xFF, 0xFF, 0xFF, 0xFF}
	}
	return nil
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
	case 0x03:
		h.Data.HwVersion = int(data[1])
		h.Data.SwVersion = int(data[2])
		h.Data.ModelNumber = int(data[3])
	case 0x04:
		h.Data.PreviousHeartBeatTime = float64(int(data[2])+int(data[3])<<8) / 1024
	case 0x05:
		// Swim interval summary (Table 6-9); 0x00 is invalid.
		set := func(b byte) int {
			if b == 0 {
				return -1
			}
			return int(b)
		}
		h.Data.IntervalAverageHR = set(data[1])
		h.Data.IntervalMaximumHR = set(data[2])
		h.Data.SessionAverageHR = set(data[3])
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
