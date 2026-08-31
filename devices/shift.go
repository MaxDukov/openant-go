package devices

import (
	"fmt"

	"github.com/maxdukov/openant-go/easy"
)

// ShiftingSystemID identifies shifting subsystems.
type ShiftingSystemID byte

// Shifting subsystem identifiers.
const (
	ShiftSystem                 ShiftingSystemID = 0
	ShiftFrontDerailleur        ShiftingSystemID = 1
	ShiftRearDerailleur         ShiftingSystemID = 2
	ShiftLeftShifter            ShiftingSystemID = 3
	ShiftRightShifter           ShiftingSystemID = 4
	ShiftShifter                ShiftingSystemID = 5
	ShiftLeftExtensionShifter   ShiftingSystemID = 6
	ShiftRightExtensionShifter  ShiftingSystemID = 7
	ShiftExtensionShifter1      ShiftingSystemID = 8
	ShiftLeftExtensionShifter2  ShiftingSystemID = 9
	ShiftRightExtensionShifter2 ShiftingSystemID = 10
	ShiftExtensionShifter2      ShiftingSystemID = 11
	ShiftUnknown                ShiftingSystemID = 15
)

func (s ShiftingSystemID) String() string {
	names := map[ShiftingSystemID]string{
		ShiftSystem: "System", ShiftFrontDerailleur: "FrontDerailleur",
		ShiftRearDerailleur: "RearDerailleur", ShiftLeftShifter: "LeftShifter",
		ShiftRightShifter: "RightShifter", ShiftShifter: "Shifter",
		ShiftLeftExtensionShifter:   "LeftExtensionShifter",
		ShiftRightExtensionShifter:  "RightExtensionShifter",
		ShiftExtensionShifter1:      "ExtensionShifter1",
		ShiftLeftExtensionShifter2:  "LeftExtensionShifter2",
		ShiftRightExtensionShifter2: "RightExtensionShifter2",
		ShiftExtensionShifter2:      "ExtensionShifter2",
	}
	if n, ok := names[s]; ok {
		return n
	}
	return "Unknown"
}

// FunctionSetEventType is a function set press type.
type FunctionSetEventType byte

// Function set event types.
const (
	FunctionSetSingle  FunctionSetEventType = 0
	FunctionSetDouble  FunctionSetEventType = 1
	FunctionSetLong    FunctionSetEventType = 2
	FunctionSetSystem  FunctionSetEventType = 3
	FunctionSetUnknown FunctionSetEventType = 4
)

// FunctionSetEvent is one shifting function set event.
type FunctionSetEvent struct {
	FunctionSetID        int
	FunctionSetEventType FunctionSetEventType
}

// FunctionSetConfiguration holds press enabling flags of a function set.
type FunctionSetConfiguration struct {
	IsShortPressEnabled  bool
	IsDoublePressEnabled bool
	IsLongPressEnabled   bool
}

// ShiftData is the ANT+ shifting data.
type ShiftData struct {
	GearRear             int `influx:"gear_rear"`
	GearFront            int `influx:"gear_front"`
	TotalRear            int `influx:"total_rear"`
	TotalFront           int `influx:"total_front"`
	InvalidInboardRear   int `influx:"invalid_inboard_rear"`
	InvalidOutboardRear  int `influx:"invalid_outboard_rear"`
	InvalidInboardFront  int `influx:"invalid_inboard_front"`
	InvalidOutboardFront int `influx:"invalid_outboard_front"`
	ShiftFailureRear     int `influx:"shift_failure_rear"`
	ShiftFailureFront    int `influx:"shift_failure_front"`
	FunctionSets         [8]FunctionSetConfiguration
	EventCount           int `influx:"event_count"`
	Events               [6]FunctionSetEvent
	MaxTrimRear          int `influx:"max_trim_rear"`
	MaxTrimFront         int `influx:"max_trim_front"`
	CurrentTrimRear      int `influx:"current_trim_rear"`
	CurrentTrimFront     int `influx:"current_trim_front"`
}

// DataName implements DeviceData.
func (ShiftData) DataName() string { return "ShiftData" }

// Shifting is the ANT+ electronic shifting profile (device type 34).
type Shifting struct {
	baseDevice
	Data ShiftData

	eventCount [3][2]int // pages 0x01, 0x02, 0x04
}

// NewShifting creates the profile.
func NewShifting(node *easy.Node, deviceID int, transType int) (*Shifting, error) {
	s := &Shifting{}
	s.node = node
	s.log = defaultLogger()
	s.DeviceType = int(DeviceTypeShifting)
	s.DeviceID = deviceID
	s.Period = 8192
	s.RFFreq = 57
	s.Name = "shifting"
	s.TransType = transType
	s.Data = ShiftData{GearRear: 31, GearFront: 7}
	s.onProfileData = s.onData
	s.onBatteryLog = s.onBatteryData
	if err := s.openChannel(true, 0); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Shifting) onBatteryData(b BatteryData) {
	if b.BatteryID != 0xFF {
		s.log.Info("shifting battery update", "system", ShiftingSystemID(b.BatteryID).String(), "data", fmt.Sprintf("%+v", b))
	} else {
		s.log.Info("battery info", "device", s.String(),
			"id", s.Common.LastBatteryID,
			"fractional_v", s.Common.LastBattery.VoltageFractional,
			"coarse_v", s.Common.LastBattery.VoltageCoarse,
			"status", s.Common.LastBattery.Status.String())
	}
	if s.OnBattery != nil {
		s.OnBattery(b)
	}
}

func functionSetEvent(b byte) FunctionSetEvent {
	return FunctionSetEvent{
		FunctionSetID: int(b & 0x0F),
		// Event type occupies bits 4-5 (openant used mask 0x03, which
		// always yields 0 after the shift; code review PR #1, P1-12).
		FunctionSetEventType: FunctionSetEventType((b & 0x30) >> 4),
	}
}

func (s *Shifting) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01: // shift system status
		s.eventCount[0][0] = s.eventCount[0][1]
		s.eventCount[0][1] = int(data[1])

		s.Data.GearRear = int(data[3] & 0x1F)
		s.Data.GearFront = int(data[3]&0xE0) >> 5
		s.Data.TotalRear = int(data[4] & 0x1F)
		s.Data.TotalFront = int(data[4]&0xE0) >> 5
		s.Data.InvalidInboardRear = int(data[5] & 0x0F)
		s.Data.InvalidOutboardRear = int(data[5]&0xF0) >> 4
		s.Data.InvalidInboardFront = int(data[6] & 0x0F)
		s.Data.InvalidOutboardFront = int(data[6]&0xF0) >> 4
		s.Data.ShiftFailureRear = int(data[7] & 0x0F)
		s.Data.ShiftFailureFront = int(data[7]&0xF0) >> 4

		if delta := (s.eventCount[0][1] + 256 - s.eventCount[0][0]) % 256; delta != 0 {
			s.log.Info("shifting status update", "device", s.String())
			s.fireDeviceData(int(data[0]), "shift_system_status", s.Data)
		}

	case 0x02: // shift function state
		s.eventCount[1][0] = s.eventCount[1][1]
		s.eventCount[1][1] = int(data[1])
		s.Data.EventCount = int(data[1])
		for i := 0; i < 5; i++ {
			s.Data.Events[i] = functionSetEvent(data[2+i])
		}
		if delta := (s.eventCount[1][1] + 256 - s.eventCount[1][0]) % 256; delta != 0 {
			s.log.Info("shifting status update", "device", s.String())
			s.fireDeviceData(int(data[0]), "shift_system_status", s.Data)
		}

	case 0x03: // function set config
		// openant reads data[8] (out of bounds) for the 8th set; Go safely
		// decodes the seven available bytes.
		for i := 0; i < 7 && i+1 < len(data); i++ {
			b := data[i+1]
			s.Data.FunctionSets[i] = FunctionSetConfiguration{
				IsShortPressEnabled:  b&0x02 != 0,
				IsDoublePressEnabled: b&0x04 != 0,
				IsLongPressEnabled:   b&0x08 != 0,
			}
		}

	case 0x04: // shift trim state
		s.eventCount[2][0] = s.eventCount[2][1]
		s.eventCount[2][1] = int(data[1])

		s.Data = ShiftData{}
		s.Data.MaxTrimRear = int(data[2])
		s.Data.MaxTrimFront = int(data[3])
		s.Data.CurrentTrimRear = int(data[4])
		s.Data.CurrentTrimFront = int(data[5])

		if delta := (s.eventCount[2][1] + 16 - s.eventCount[2][0]) % 16; delta != 0 {
			s.log.Info("shifting status update", "device", s.String())
			s.fireDeviceData(int(data[0]), "shift_system_status", s.Data)
		}
	}
}

func init() {
	registerProfile(DeviceTypeShifting, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewShifting(node, deviceID, transType)
	})
}
