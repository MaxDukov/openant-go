package devices

import (
	"github.com/maxdukov/openant-go/easy"
)

// PressureSensorPosition is the TPMS sensor position.
type PressureSensorPosition byte

// Sensor positions.
const (
	PositionUnknown PressureSensorPosition = 0
	PositionFront   PressureSensorPosition = 1
	PositionRear    PressureSensorPosition = 2
)

// PressureSensorAlarm is the TPMS alarm state.
type PressureSensorAlarm byte

// Alarm states.
const (
	AlarmAllWell      PressureSensorAlarm = 0
	AlarmHighPressure PressureSensorAlarm = 1
	AlarmLowPressure  PressureSensorAlarm = 2
	AlarmUnknown      PressureSensorAlarm = 0xFF
)

// TirePressureData is the ANT+ tire pressure monitor data.
type TirePressureData struct {
	Position           PressureSensorPosition `influx:"position"`
	AlarmState         PressureSensorAlarm    `influx:"alarm_state"`
	Capabilities       int                    `influx:"capabilities"`
	Pressure           int                    `influx:"pressure"`            // mbar
	BarometricPressure int                    `influx:"barometric_pressure"` // mbar
	LowPressureAlarm   int                    `influx:"low_pressure_alarm"`  // mbar
	HighPressureAlarm  int                    `influx:"high_pressure_alarm"` // mbar
}

// DataName implements DeviceData.
func (TirePressureData) DataName() string { return "TirePressureData" }

// TirePressureMonitor is the ANT+ tire pressure profile (device type 48).
type TirePressureMonitor struct {
	baseDevice
	Data TirePressureData
}

// NewTirePressureMonitor creates the profile.
func NewTirePressureMonitor(node *easy.Node, deviceID int, transType int) (*TirePressureMonitor, error) {
	t := &TirePressureMonitor{}
	t.node = node
	t.log = defaultLogger()
	t.DeviceType = int(DeviceTypeTirePressureMonitor)
	t.DeviceID = deviceID
	t.Period = 8192 // 4 Hz when pumping, 1 Hz in normal use
	t.RFFreq = 57
	t.Name = "tire_pressure_monitor"
	t.TransType = transType
	t.Data = TirePressureData{
		Position:           PositionUnknown,
		AlarmState:         AlarmUnknown,
		Pressure:           0x8000,
		BarometricPressure: 0x8000,
		LowPressureAlarm:   0x8000,
		HighPressureAlarm:  0x8000,
	}
	t.onProfileData = t.onData
	if err := t.openChannel(true, 0); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *TirePressureMonitor) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01: // main page
		t.Data.Position = PressureSensorPosition(data[1] & 0x0F)
		t.Data.AlarmState = PressureSensorAlarm((data[1] & 0xF0) >> 4)
		t.Data.Capabilities = int(data[2])
		t.Data.Pressure = int(data[6]) + int(data[7])<<8
		t.log.Info("tire pressure main update", "device", t.String(),
			"pressure", t.Data.Pressure)
		t.fireDeviceData(int(data[0]), "tire_pressure", t.Data)
	case 0x10: // get/set parameters
		t.Data.Position = PressureSensorPosition(data[1] & 0x0F)
		t.Data.AlarmState = PressureSensorAlarm((data[1] & 0xF0) >> 4)
		t.Data.BarometricPressure = int(data[2]) + int(data[3])<<8
		t.Data.LowPressureAlarm = int(data[4]) + int(data[5])<<8
		t.Data.HighPressureAlarm = int(data[6]) + int(data[7])<<8
		t.fireDeviceData(int(data[0]), "get_set", t.Data)
	}
}

// SetData sends the get/set parameters page 0x10.
func (t *TirePressureMonitor) SetData(setPosition, setBarometric, setHighPressure, setLowPressure bool) {
	page := make([]byte, 8)
	page[0] = 0x10
	var flags byte
	if setPosition {
		flags |= 1
	}
	if setBarometric {
		flags |= 1 << 1
	}
	if setHighPressure {
		flags |= 1 << 2
	}
	if setLowPressure {
		flags |= 1 << 3
	}
	page[1] = byte(t.Data.Position) | flags<<4
	page[2] = byte(t.Data.BarometricPressure)
	page[3] = byte(t.Data.BarometricPressure >> 8)
	page[4] = byte(t.Data.LowPressureAlarm)
	page[5] = byte(t.Data.LowPressureAlarm >> 8)
	page[6] = byte(t.Data.HighPressureAlarm)
	page[7] = byte(t.Data.HighPressureAlarm >> 8)
	t.SendAcknowledgedData(page)
}

func init() {
	registerProfile(DeviceTypeTirePressureMonitor, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewTirePressureMonitor(node, deviceID, transType)
	})
}
