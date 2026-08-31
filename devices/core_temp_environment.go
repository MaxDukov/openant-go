package devices

import (
	"github.com/maxdukov/openant-go/easy"
)

// Invalid-value sentinels for the core temperature profile.
const (
	HeatStrainIndexInvalid = float64(0xFF)
	CoreTempInvalid        = float64(0x8000)
	SkinTempInvalid        = float64(0x800)
	ReservedInvalid        = float64(0x800)
)

// CoreTempDataQuality classifies sensor confidence.
type CoreTempDataQuality byte

// Core temperature data quality values.
const (
	QualityPoor      CoreTempDataQuality = 0
	QualityFair      CoreTempDataQuality = 1
	QualityGood      CoreTempDataQuality = 2
	QualityExcellent CoreTempDataQuality = 3
	QualityUnused    CoreTempDataQuality = 0xFF
)

// CoreTemperatureData is the ANT+ core temperature data.
type CoreTemperatureData struct {
	Quality         CoreTempDataQuality `influx:"quality"`
	SkinTemp        float64             `influx:"skin_temp"` // C
	CoreTemp        float64             `influx:"core_temp"` // C
	HeatStrainIndex float64             `influx:"heat_strain_index"`
	Reserved        float64             `influx:"reserved"`
}

// DataName implements DeviceData.
func (CoreTemperatureData) DataName() string { return "CoreTemperatureData" }

// CoreTemperature is the ANT+ core temperature sensor profile (device
// type 127).
type CoreTemperature struct {
	baseDevice
	Data CoreTemperatureData
}

// NewCoreTemperature creates the profile.
func NewCoreTemperature(node *easy.Node, deviceID int, transType int) (*CoreTemperature, error) {
	c := &CoreTemperature{}
	c.node = node
	c.log = defaultLogger()
	c.DeviceType = int(DeviceTypeCoreTemp)
	c.DeviceID = deviceID
	c.Period = 16384 // 2 Hz
	c.RFFreq = 57
	c.Name = "core_temp"
	c.TransType = transType
	c.Data = CoreTemperatureData{
		Quality:         QualityUnused,
		SkinTemp:        SkinTempInvalid,
		CoreTemp:        CoreTempInvalid,
		HeatStrainIndex: HeatStrainIndexInvalid,
		Reserved:        ReservedInvalid,
	}
	c.onProfileData = c.onData
	if err := c.openChannel(true, 0); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *CoreTemperature) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x00: // general info
		c.Data.Quality = CoreTempDataQuality(data[2])
	case 0x01: // main page
		hsi := float64(data[1])
		skin := float64(int(data[3]) | int(data[4]&0xF0)<<4)
		core := float64(int(data[6]) + int(data[7])<<8)
		reserved := float64(int(data[5])<<4 | int(data[4]&0x0F))

		if skin == SkinTempInvalid {
			c.Data.SkinTemp = SkinTempInvalid
		} else {
			c.Data.SkinTemp = skin * 0.05
		}
		if core == CoreTempInvalid {
			c.Data.CoreTemp = CoreTempInvalid
		} else {
			c.Data.CoreTemp = core * 0.01
		}
		if hsi == HeatStrainIndexInvalid {
			c.Data.HeatStrainIndex = HeatStrainIndexInvalid
		} else {
			c.Data.HeatStrainIndex = hsi * 0.1
		}
		if reserved == ReservedInvalid {
			c.Data.Reserved = ReservedInvalid
		} else {
			c.Data.Reserved = reserved
		}
	}
	c.fireDeviceData(int(data[0]), "core_temp", c.Data)
}

func init() {
	registerProfile(DeviceTypeCoreTemp, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewCoreTemperature(node, deviceID, transType)
	})
}

// EnvironmentData is the ANT+ environment data.
type EnvironmentData struct {
	Temperature       float64 `influx:"temperature"`         // C
	Min24hTemperature float64 `influx:"min_24h_temperature"` // C
	Max24hTemperature float64 `influx:"max_24h_temperature"` // C
}

// DataName implements DeviceData.
func (EnvironmentData) DataName() string { return "EnvironmentData" }

// Environment is the ANT+ environment profile (device type 25).
type Environment struct {
	baseDevice
	Data EnvironmentData
}

// NewEnvironment creates the profile.
func NewEnvironment(node *easy.Node, deviceID int, transType int) (*Environment, error) {
	e := &Environment{}
	e.node = node
	e.log = defaultLogger()
	e.DeviceType = int(DeviceTypeEnvironment)
	e.DeviceID = deviceID
	e.Period = 8070
	e.RFFreq = 57
	e.Name = "environment"
	e.TransType = transType
	e.Data = EnvironmentData{Temperature: -1, Min24hTemperature: -1, Max24hTemperature: -1}
	e.onProfileData = e.onData
	if err := e.openChannel(true, 0); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Environment) onData(data []byte) {
	if len(data) < 8 || data[0] != 1 {
		return
	}
	// Data page 1: 24 h min/max nibbles packed around byte 4.
	lowMSN := (data[4] & 0xF0) >> 4
	highLSN := (data[4] & 0x0F) << 4

	e.Data.Temperature = float64(int(data[6])+int(data[7])<<8) * 0.01
	e.Data.Min24hTemperature = float64(int(data[3])+int(lowMSN)<<8) * 0.1
	// max_24h: big-endian [data[5], high_lsn] >> 4 per openant.
	maxRaw := int(data[5])<<8 | int(highLSN)
	e.Data.Max24hTemperature = float64(maxRaw>>4) * 0.1

	e.fireDeviceData(int(data[0]), "environment", e.Data)
}

func init() {
	registerProfile(DeviceTypeEnvironment, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewEnvironment(node, deviceID, transType)
	})
}
