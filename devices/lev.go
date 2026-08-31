package devices

import (
	"fmt"

	"github.com/maxdukov/openant-go/easy"
)

// LevErrorMessage is the LEV error code.
type LevErrorMessage byte

// LEV error values (5-15 Unknown, 16-255 manufacturer specific).
const (
	LevNoError              LevErrorMessage = 0
	LevBatteryError         LevErrorMessage = 1
	LevDriveTrainError      LevErrorMessage = 2
	LevBatteryEOL           LevErrorMessage = 3
	LevOverheating          LevErrorMessage = 4
	LevUnknown              LevErrorMessage = 5
	LevManufacturerSpecific LevErrorMessage = 16
)

// LevErrorMessageFromByte maps unknown values like openant's _missing_.
func LevErrorMessageFromByte(b byte) LevErrorMessage {
	if b < 16 {
		return LevUnknown
	}
	return LevManufacturerSpecific
}

// TemperatureState is the LEV temperature state code.
type TemperatureState byte

// LEV temperature states.
const (
	TempUnknown  TemperatureState = 0
	TempCold     TemperatureState = 1
	TempColdWarm TemperatureState = 2
	TempWarm     TemperatureState = 3
	TempWarmHot  TemperatureState = 4
	TempHot      TemperatureState = 5
)

// TemperatureAlert is the LEV overheat alert.
type TemperatureAlert byte

// LEV temperature alerts.
const (
	NoAlert          TemperatureAlert = 0
	OverheatingAlert TemperatureAlert = 1
)

// GearState codes.
type GearState byte

// Gear state values.
const (
	GearAutomatic GearState = 0
	GearManual    GearState = 1
	GearUnknown   GearState = 2
)

// LevData is the ANT+ light electric vehicle data.
type LevData struct {
	MotorTemperature            TemperatureState `influx:"motor_temperature"`
	MotorAlert                  TemperatureAlert `influx:"motor_alert"`
	BatteryTemperature          TemperatureState `influx:"battery_temperature"`
	BatteryAlert                TemperatureAlert `influx:"battery_alert"`
	ErrorMessage                byte             `influx:"error_message"`
	Speed                       float64          `influx:"speed"` // km/h
	CurrentAssistLevel          int              `influx:"current_assist_level"`
	CurrentRegenerativeLevel    int              `influx:"current_regenerative_level"`
	ManualThrottle              bool             `influx:"manual_throttle"`
	Lights                      bool             `influx:"lights"`
	LightHighBeam               bool             `influx:"light_high_beam"`
	TurnSignalLeft              bool             `influx:"turn_signal_left"`
	TurnSignalRight             bool             `influx:"turn_signal_right"`
	GearExist                   bool             `influx:"gear_exist"`
	GearManual                  bool             `influx:"gear_manual"`
	GearRear                    int              `influx:"gear_rear"`
	GearFront                   int              `influx:"gear_front"`
	Odometer                    float64          `influx:"odometer"` // km
	RemainingRange              int              `influx:"remaining_range"`
	FuelConsumption             float64          `influx:"fuel_consumption"` // Wh/km
	Assist                      int              `influx:"assist"`
	BatterySOC                  int              `influx:"battery_soc"`
	BatteryCycles               int              `influx:"battery_cycles"`
	BatteryVoltage              float64          `influx:"battery_voltage"` // V
	BatteryDistanceCharge       float64          `influx:"battery_distance_charge"`
	WheelCircumference          int              `influx:"wheel_circumference"` // mm
	SupportedAssistLevels       int              `influx:"supported_assist_levels"`
	SupportedRegenerativeLevels int              `influx:"supported_regenerative_levels"`
}

// DataName implements DeviceData.
func (LevData) DataName() string { return "LevData" }

// LevDisplayCommand is the TX display command of the LEV profile.
type LevDisplayCommand struct {
	GearRear        int
	GearFront       int
	Lights          bool
	LightHighBeam   bool
	TurnSignalLeft  bool
	TurnSignalRight bool
}

// ToInt packs the command into one byte.
func (dc LevDisplayCommand) ToInt() int {
	i := dc.GearRear << 6
	i |= dc.GearFront << 4
	if dc.Lights {
		i |= 1 << 3
	}
	if dc.LightHighBeam {
		i |= 1 << 2
	}
	if dc.TurnSignalLeft {
		i |= 1 << 1
	}
	if dc.TurnSignalRight {
		i |= 1
	}
	return i
}

// ToBytes packs the command as 2 little-endian bytes.
func (dc LevDisplayCommand) ToBytes() []byte {
	i := dc.ToInt()
	return []byte{byte(i), byte(i >> 8)}
}

// Lev is the ANT+ light electric vehicle profile (device type 20).
type Lev struct {
	baseDevice
	Data LevData
}

// NewLev creates the profile.
func NewLev(node *easy.Node, deviceID int, transType int) (*Lev, error) {
	l := &Lev{}
	l.node = node
	l.log = defaultLogger()
	l.DeviceType = int(DeviceTypeLev)
	l.DeviceID = deviceID
	l.Period = 8192
	l.RFFreq = 57
	l.Name = "lev"
	l.TransType = transType
	l.onProfileData = l.onData
	if err := l.openChannel(true, 0); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Lev) updateSystemState(b byte) {
	l.Data.ManualThrottle = b&0x10 == 0x10
	l.Data.Lights = b&0x08 == 0x08
	l.Data.LightHighBeam = b&0x04 == 0x04
	l.Data.TurnSignalLeft = b&0x02 == 0x02
	l.Data.TurnSignalRight = b&0x01 == 0x01
}

func (l *Lev) updateTravelMode(b byte) {
	l.Data.CurrentAssistLevel = int(b>>3) & 0x07
	l.Data.CurrentRegenerativeLevel = int(b) & 0x07
}

func (l *Lev) updateGearState(b byte) {
	l.Data.GearExist = b&0x80 == 0x80
	l.Data.GearManual = b&0x40 == 0x40
	l.Data.GearRear = int(b>>2) & 0x07
	l.Data.GearFront = int(b) & 0x03
}

func levSpeed(data []byte) float64 {
	return float64(int(data[6])+int(data[7]&0x0F)<<8) * 0.1
}

func (l *Lev) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01: // main page
		l.Data.MotorTemperature = TemperatureState((data[1] >> 4) & 0x07)
		l.Data.MotorAlert = TemperatureAlert((data[1] >> 4) & 0x08)
		l.Data.BatteryTemperature = TemperatureState(data[1] & 0x07)
		l.Data.BatteryAlert = TemperatureAlert(data[1] & 0x08)
		l.updateTravelMode(data[2])
		l.updateSystemState(data[3])
		l.updateGearState(data[4])
		l.Data.ErrorMessage = byte(LevErrorMessageFromByte(data[5]))
		l.Data.Speed = levSpeed(data)
		l.fireDeviceData(int(data[0]), "speed_system", l.Data)
	case 0x02: // speed and distance
		l.Data.Odometer = float64(int(data[1])+int(data[2])<<8+int(data[3])<<16) * 0.01
		l.Data.RemainingRange = int(data[4]) + int(data[5]&0x0F)<<8
		l.Data.Speed = levSpeed(data)
		l.fireDeviceData(int(data[0]), "speed_distance", l.Data)
	case 0x22: // alternative speed and distance
		l.Data.Odometer = float64(int(data[1])+int(data[2])<<8+int(data[3])<<16) * 0.01
		l.Data.FuelConsumption = float64(int(data[4])+int(data[5]&0x0F)<<8) * 0.1
		l.Data.Speed = levSpeed(data)
		l.fireDeviceData(int(data[0]), "alt_speed_distance", l.Data)
	case 0x03: // system and speed 2
		l.Data.BatterySOC = int(data[1]) & 0x7F
		l.updateTravelMode(data[2])
		l.updateSystemState(data[3])
		l.updateGearState(data[4])
		l.Data.Assist = int(data[5])
		l.Data.Speed = levSpeed(data)
		l.fireDeviceData(int(data[0]), "system_speed_2", l.Data)
	case 0x04: // battery information
		l.Data.BatteryCycles = int(data[2]) + int(data[3]&0x0F)<<8
		l.Data.FuelConsumption = float64(int(data[4])+int(data[3]&0xF0)<<4) * 0.1
		l.Data.BatteryVoltage = float64(data[5]) / 4
		l.Data.BatteryDistanceCharge = float64(int(data[6]) + int(data[7])<<8)
		l.fireDeviceData(int(data[0]), "battery", l.Data)
	case 0x05: // capabilities
		l.Data.SupportedAssistLevels = int(data[2]>>3) & 0x07
		l.Data.SupportedRegenerativeLevels = int(data[2]) & 0x07
		l.Data.WheelCircumference = int(data[3]) + int(data[4]&0x0F)<<8
		l.fireDeviceData(int(data[0]), "capabilities", l.Data)
	}
	l.log.Debug("lev page", "page", fmt.Sprintf("%#x", data[0]))
}

// SetData sends the display command page 0x10.
func (l *Lev) SetData(displayCommand LevDisplayCommand, assistLevel, regenerativeLevel, wheelCircumference int, manufacturerID int) {
	page := make([]byte, 8)
	page[0] = 0x10
	page[1] = byte(wheelCircumference & 0xFF)
	page[2] = byte(wheelCircumference & 0x0F)
	if assistLevel != 0xFF || regenerativeLevel != 0xFF {
		if assistLevel != 0xFF {
			page[3] |= byte((assistLevel & 0x07) << 3)
		}
		if regenerativeLevel != 0xFF {
			page[3] |= byte(regenerativeLevel & 0x07)
		}
	} else {
		page[3] = 0xFF
	}
	copy(page[4:6], displayCommand.ToBytes())
	page[6] = byte(manufacturerID)
	page[7] = byte(manufacturerID >> 8)
	l.SendAcknowledgedData(page)
}

func init() {
	registerProfile(DeviceTypeLev, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewLev(node, deviceID, transType)
	})
}
