package devices

import (
	"github.com/maxdukov/openant-go/easy"
)

// BikeSpeedData is the ANT+ bike speed data (openant BikeSpeedData).
type BikeSpeedData struct {
	CumulativeOperatingTime   int        `influx:"cumulative_operating_time"`
	BikeSpeedEventTime        [2]float64 `influx:"bike_speed_event_time"` // [prev, curr] seconds
	CumulativeSpeedRevolution [2]int     `influx:"cumulative_speed_revolution"`
	ManufacturerIDLSB         int        `influx:"manufacturer_id_lsb"`
	SerialNumber              int        `influx:"serial_number"`
	CalculatedSpeed           *float64   `influx:"calculated_speed"`    // km/h
	CalculatedDistance        *float64   `influx:"calculated_distance"` // m
}

// DataName implements DeviceData.
func (BikeSpeedData) DataName() string { return "BikeSpeedData" }

// CalculateSpeed returns speed in km/h given the wheel circumference in
// meters, or nil when there is no time delta.
func (b *BikeSpeedData) CalculateSpeed(wheelCircumferenceM float64) *float64 {
	deltaRev := b.CumulativeSpeedRevolution[1] - b.CumulativeSpeedRevolution[0]
	deltaTime := b.BikeSpeedEventTime[1] - b.BikeSpeedEventTime[0]
	if deltaTime > 0 {
		v := wheelCircumferenceM * float64(deltaRev) / deltaTime * 3.6
		b.CalculatedSpeed = &v
		return &v
	}
	return nil
}

// CalculateDistance returns the distance in meters from the total
// revolutions and wheel circumference.
func (b *BikeSpeedData) CalculateDistance(wheelCircumferenceM float64) float64 {
	v := wheelCircumferenceM * float64(b.CumulativeSpeedRevolution[1])
	b.CalculatedDistance = &v
	return v
}

// BikeCadenceData is the ANT+ bike cadence data (openant BikeCadenceData).
type BikeCadenceData struct {
	CumulativeOperatingTime     int        `influx:"cumulative_operating_time"`
	BikeCadenceEventTime        [2]float64 `influx:"bike_cadence_event_time"` // [prev, curr] seconds
	CumulativeCadenceRevolution [2]int     `influx:"cumulative_cadence_revolution"`
	ManufacturerIDLSB           int        `influx:"manufacturer_id_lsb"`
	SerialNumber                int        `influx:"serial_number"`
	CalculatedCadence           *float64   `influx:"calculated_cadence"` // rpm
}

// DataName implements DeviceData.
func (BikeCadenceData) DataName() string { return "BikeCadenceData" }

// Cadence returns the cached calculated cadence (nil when unknown).
func (b *BikeCadenceData) Cadence() *float64 { return b.CalculatedCadence }

// CalculateCadence calculates rpm from deltas; nil when no time delta.
func (b *BikeCadenceData) CalculateCadence() *float64 {
	deltaRev := b.CumulativeCadenceRevolution[1] - b.CumulativeCadenceRevolution[0]
	deltaTime := b.BikeCadenceEventTime[1] - b.BikeCadenceEventTime[0]
	if deltaTime > 0 {
		v := 60 * float64(deltaRev) / deltaTime
		b.CalculatedCadence = &v
		return &v
	}
	return nil
}

// updateSpeedData parses the 4 byte speed block (event time u16/1024 s,
// revolutions u16).
func updateSpeedData(d *BikeSpeedData, data []byte, wheelCircumferenceM *float64) {
	if len(data) != 4 {
		return
	}
	eventTime := float64(int(data[0])+int(data[1])<<8) / 1024
	d.BikeSpeedEventTime[0] = d.BikeSpeedEventTime[1]
	d.BikeSpeedEventTime[1] = eventTime
	revs := int(data[2]) + int(data[3])<<8
	d.CumulativeSpeedRevolution[0] = d.CumulativeSpeedRevolution[1]
	d.CumulativeSpeedRevolution[1] = revs
	if wheelCircumferenceM != nil {
		d.CalculateSpeed(*wheelCircumferenceM)
		d.CalculateDistance(*wheelCircumferenceM)
	}
}

// updateCadenceData parses the 4 byte cadence block.
func updateCadenceData(d *BikeCadenceData, data []byte) {
	if len(data) != 4 {
		return
	}
	eventTime := float64(int(data[0])+int(data[1])<<8) / 1024
	d.BikeCadenceEventTime[0] = d.BikeCadenceEventTime[1]
	d.BikeCadenceEventTime[1] = eventTime
	revs := int(data[2]) + int(data[3])<<8
	d.CumulativeCadenceRevolution[0] = d.CumulativeCadenceRevolution[1]
	d.CumulativeCadenceRevolution[1] = revs
	d.CalculateCadence()
}

// onSpeedCadencePages decodes the shared page layout used by BikeSpeed and
// BikeCadence profiles.
func onSpeedCadencePages(d *baseDevice, speed *BikeSpeedData, cadence *BikeCadenceData, data []byte, pageName string, wheel *float64) {
	if len(data) < 8 {
		return
	}
	page := data[0]
	if page&0x0F > 7 {
		return
	}
	dp := page & 0x0F
	switch dp {
	case 0x00:
		if speed != nil {
			updateSpeedData(speed, data[4:8], wheel)
		}
		if cadence != nil {
			updateCadenceData(cadence, data[4:8])
		}
	case 0x01: // cumulative operating time
		opTime := int(data[1]) + int(data[2])<<8 + int(data[3])<<16
		if speed != nil {
			speed.CumulativeOperatingTime = opTime * 2
			updateSpeedData(speed, data[4:8], wheel)
		}
		if cadence != nil {
			cadence.CumulativeOperatingTime = opTime * 2
			updateCadenceData(cadence, data[4:8])
		}
	case 0x02: // manufacturer id
		if speed != nil {
			speed.ManufacturerIDLSB = int(data[1])
			speed.SerialNumber = int(data[2]) + int(data[3])<<8
			updateSpeedData(speed, data[4:8], wheel)
		}
		if cadence != nil {
			cadence.ManufacturerIDLSB = int(data[1])
			cadence.SerialNumber = int(data[2]) + int(data[3])<<8
			updateCadenceData(cadence, data[4:8])
		}
	case 0x03: // product info
		if speed != nil {
			updateSpeedData(speed, data[4:8], wheel)
		}
		if cadence != nil {
			updateCadenceData(cadence, data[4:8])
		}
	case 0x04: // battery voltage
		d.Common.LastBattery.VoltageFractional = float64(data[2]) / 256
		d.Common.LastBattery.VoltageCoarse = int(data[3] & 0x0F)
		d.Common.LastBattery.Status = BatteryStatus((data[3] & 0x70) >> 4)
		if speed != nil {
			updateSpeedData(speed, data[4:8], wheel)
		}
		if cadence != nil {
			updateCadenceData(cadence, data[4:8])
		}
		d.onBattery(d.Common.LastBattery)
	case 0x05: // motion and speed
		if speed != nil {
			updateSpeedData(speed, data[4:8], wheel)
		}
		if cadence != nil {
			updateCadenceData(cadence, data[4:8])
		}
	}
}

// BikeSpeed is the ANT+ bicycle speed profile (device type 123).
type BikeSpeed struct {
	baseDevice
	Data BikeSpeedData
}

// NewBikeSpeed creates the profile.
func NewBikeSpeed(node *easy.Node, deviceID int, transType int) (*BikeSpeed, error) {
	b := &BikeSpeed{}
	b.node = node
	b.log = defaultLogger()
	b.DeviceType = int(DeviceTypeBikeSpeed)
	b.DeviceID = deviceID
	b.Period = 8118
	b.RFFreq = 57
	b.Name = "bike_speed"
	b.TransType = transType
	b.onProfileData = b.onData
	if err := b.openChannel(true, 0); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *BikeSpeed) onData(data []byte) {
	onSpeedCadencePages(&b.baseDevice, &b.Data, nil, data, "bike_speed", nil)
	b.fireDeviceData(int(data[0]), "bike_speed", b.Data)
}

// BikeCadence is the ANT+ bicycle cadence profile (device type 122).
type BikeCadence struct {
	baseDevice
	Data BikeCadenceData
}

// NewBikeCadence creates the profile.
func NewBikeCadence(node *easy.Node, deviceID int, transType int) (*BikeCadence, error) {
	b := &BikeCadence{}
	b.node = node
	b.log = defaultLogger()
	b.DeviceType = int(DeviceTypeBikeCadence)
	b.DeviceID = deviceID
	b.Period = 8102
	b.RFFreq = 57
	b.Name = "bike_cadence"
	b.TransType = transType
	b.onProfileData = b.onData
	if err := b.openChannel(true, 0); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *BikeCadence) onData(data []byte) {
	onSpeedCadencePages(&b.baseDevice, nil, &b.Data, data, "bike_cadence", nil)
	b.fireDeviceData(int(data[0]), "bike_cadence", b.Data)
}

// BikeSpeedCadence is the combined ANT+ speed and cadence profile (device
// type 121) which broadcasts a single main data page.
type BikeSpeedCadence struct {
	baseDevice
	SpeedData   BikeSpeedData
	CadenceData BikeCadenceData

	wheelCircumferenceM *float64
}

// NewBikeSpeedCadence creates the combined profile. wheelCircumferenceM
// may be nil to skip speed/distance calculations.
func NewBikeSpeedCadence(node *easy.Node, deviceID int, transType int) (*BikeSpeedCadence, error) {
	b := &BikeSpeedCadence{}
	b.node = node
	b.log = defaultLogger()
	b.DeviceType = int(DeviceTypeBikeSpeedCadence)
	b.DeviceID = deviceID
	b.Period = 8086
	b.RFFreq = 57
	b.Name = "bike_speed_cadence"
	b.TransType = transType
	b.onProfileData = b.onData
	if err := b.openChannel(true, 0); err != nil {
		return nil, err
	}
	return b, nil
}

// SetWheelCircumference sets the wheel circumference used for speed and
// distance calculations (meters).
func (b *BikeSpeedCadence) SetWheelCircumference(m float64) { b.wheelCircumferenceM = &m }

func (b *BikeSpeedCadence) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	page := data[0]
	if page&0x0F <= 5 {
		updateCadenceData(&b.CadenceData, data[0:4])
		updateSpeedData(&b.SpeedData, data[4:8], b.wheelCircumferenceM)
		b.fireDeviceData(int(page), "bike_cadence", b.CadenceData)
		b.fireDeviceData(int(page), "bike_speed", b.SpeedData)
	}
}

func init() {
	registerProfile(DeviceTypeBikeSpeed, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewBikeSpeed(node, deviceID, transType)
	})
	registerProfile(DeviceTypeBikeCadence, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewBikeCadence(node, deviceID, transType)
	})
	registerProfile(DeviceTypeBikeSpeedCadence, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewBikeSpeedCadence(node, deviceID, transType)
	})
}
