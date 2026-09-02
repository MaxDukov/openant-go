package devices

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/maxdukov/openant-go/easy"
)

// ResistanceMode is the FE resistance mode.
type ResistanceMode byte

// Resistance modes.
const (
	ResistanceBasic       ResistanceMode = 0x30
	ResistanceTargetPower ResistanceMode = 0x31
	ResistanceWind        ResistanceMode = 0x32
	ResistanceTrack       ResistanceMode = 0x33
	ResistanceUnknown     ResistanceMode = 0xFF
)

// FECommandStatus is the FE command status reply code.
type FECommandStatus byte

// Command status values (5-254 map to Unknown like openant).
const (
	FECommandPass         FECommandStatus = 0
	FECommandFail         FECommandStatus = 1
	FECommandNotSupported FECommandStatus = 2
	FECommandRejected     FECommandStatus = 3
	FECommandPending      FECommandStatus = 4
	FECommandUnknown      FECommandStatus = 0xFE
	FECommandUnitialised  FECommandStatus = 0xFF
)

// FECommandStatusFromByte maps 5-254 to Unknown.
func FECommandStatusFromByte(b byte) FECommandStatus {
	if b >= 5 && b <= 254 {
		return FECommandUnknown
	}
	return FECommandStatus(b)
}

// FitnessEquipmentState is the FE state.
type FitnessEquipmentState byte

// FE states.
const (
	FEStateUnknown  FitnessEquipmentState = 0
	FEStateAsleep   FitnessEquipmentState = 1
	FEStateReady    FitnessEquipmentState = 2
	FEStateInUse    FitnessEquipmentState = 3
	FEStateFinished FitnessEquipmentState = 4
)

// FitnessEquipmentType is the FE type.
type FitnessEquipmentType byte

// FE types.
const (
	FETypeTreadmill   FitnessEquipmentType = 19
	FETypeEliptical   FitnessEquipmentType = 20
	FETypeRower       FitnessEquipmentType = 22
	FETypeClimber     FitnessEquipmentType = 23
	FETypeNordicSkier FitnessEquipmentType = 24
	FETypeTrainer     FitnessEquipmentType = 25
	FETypeReserved    FitnessEquipmentType = 0
)

// FitnessEquipmentData is the ANT+ fitness equipment data.
type FitnessEquipmentData struct {
	ResistanceMode   ResistanceMode        `influx:"resistance_mode"`
	Resistance       float64               `influx:"resistance"`
	State            FitnessEquipmentState `influx:"state"`
	Type             FitnessEquipmentType  `influx:"type"`
	Capabilities     int                   `influx:"capabilities"`
	Speed            float64               `influx:"speed"`
	Incline          float64               `influx:"incline"`
	TargetResistance float64               `influx:"target_resistance"`
}

// DataName implements DeviceData.
func (FitnessEquipmentData) DataName() string { return "FitnessEquipmentData" }

// FETrainerStatusData is the trainer status information carried by the
// specific trainer data page 25 (0x19), Tables 8-27/8-28 of the ANT+ FE-C
// spec Rev 5.0.
type FETrainerStatusData struct {
	EventCount                    int                   `influx:"event_count"`
	Cadence                       int                   `influx:"cadence"`
	InstantaneousPower            int                   `influx:"instantaneous_power"`
	AveragePower                  int                   `influx:"average_power"`
	PowerCalibrationRequired      bool                  `influx:"power_calibration_required"`
	ResistanceCalibrationRequired bool                  `influx:"resistance_calibration_required"`
	UserConfigRequired            bool                  `influx:"user_config_required"`
	TargetPowerLimit              int                   `influx:"target_power_limit"`
	State                         FitnessEquipmentState `influx:"state"`
}

// DataName implements DeviceData.
func (FETrainerStatusData) DataName() string { return "FETrainerStatusData" }

// FECommandStatusData is the decoded common page 71 (0x47) command
// status reply, Tables 8-48/8-49 of the ANT+ FE-C spec Rev 5.0. The
// response-data fields are filled in according to the last received
// command ID; the ones unrelated to it keep zero values.
type FECommandStatusData struct {
	CommandID         ResistanceMode  `influx:"command_id"`
	Sequence          int             `influx:"sequence"`
	Status            FECommandStatus `influx:"status"`
	TotalResistance   float64         `influx:"total_resistance"`   // basic resistance mode: 0-100 %
	TargetPower       int             `influx:"target_power"`       // target power mode: W
	WindCoefficient   float64         `influx:"wind_coefficient"`   // wind mode: kg/m
	WindSpeed         int             `influx:"wind_speed"`         // wind mode: km/h
	DraftingFactor    float64         `influx:"drafting_factor"`    // wind mode: 0-1.00
	Grade             float64         `influx:"grade"`              // track mode: -200..200 %
	RollingResistance float64         `influx:"rolling_resistance"` // track mode: 0-0.0127
}

// DataName implements DeviceData.
func (FECommandStatusData) DataName() string { return "FECommandStatusData" }

// FEMetabolicData is the general FE metabolic data page 18 (0x12),
// Table 8-13 of the ANT+ FE-C spec Rev 5.0.
type FEMetabolicData struct {
	METs            float64               `influx:"mets"`              // instantaneous metabolic equivalents, 0-100.00; -1 invalid
	CaloricBurnRate float64               `influx:"caloric_burn_rate"` // 0.1 kCal/hr resolution; -1 invalid
	Calories        int                   `influx:"calories"`          // accumulated kcal, rolls over at 256
	Capabilities    int                   `influx:"capabilities"`
	State           FitnessEquipmentState `influx:"state"`
}

// DataName implements DeviceData.
func (FEMetabolicData) DataName() string { return "FEMetabolicData" }

// FETreadmillData is the specific treadmill data page 19 (0x13),
// Table 8-15 of the ANT+ FE-C spec Rev 5.0.
type FETreadmillData struct {
	Cadence                  int                   `influx:"cadence"`                    // strides/min; -1 invalid
	NegativeVerticalDistance float64               `influx:"negative_vertical_distance"` // accumulated metres, rolls over at 25.6
	PositiveVerticalDistance float64               `influx:"positive_vertical_distance"` // accumulated metres, rolls over at 25.6
	Capabilities             int                   `influx:"capabilities"`
	State                    FitnessEquipmentState `influx:"state"`
}

// DataName implements DeviceData.
func (FETreadmillData) DataName() string { return "FETreadmillData" }

// FECapabilitiesData is the FE capabilities page 54 (0x36), Tables
// 8-45/8-46 of the ANT+ FE-C spec Rev 5.0. It is requested with
// RequestDP(54, 1) (see RequestCapabilities).
type FECapabilitiesData struct {
	BasicMode       bool `influx:"basic_mode"`
	TargetPowerMode bool `influx:"target_power_mode"`
	SimulationMode  bool `influx:"simulation_mode"`
	MaxResistance   int  `influx:"max_resistance"` // Newtons; -1 invalid
}

// DataName implements DeviceData.
func (FECapabilitiesData) DataName() string { return "FECapabilitiesData" }

// WorkoutInterval is one (power, duration) step.
type WorkoutInterval struct {
	Power  int
	Period float64 // seconds
}

// Workout is a series of power intervals for a trainer.
type Workout struct {
	Intervals []WorkoutInterval
	Cycles    int
	Loop      bool
}

// WorkoutFromArrays builds a workout from power/period slices.
func WorkoutFromArrays(powers []int, periods []float64) (*Workout, error) {
	if len(powers) != len(periods) {
		return nil, errors.New("power levels and periods must be equal in length")
	}
	w := &Workout{Cycles: 1}
	for i := range powers {
		w.Intervals = append(w.Intervals, WorkoutInterval{Power: powers[i], Period: periods[i]})
	}
	return w, nil
}

// WorkoutFromRamp builds a ramp workout (optionally triangular with a peak).
func WorkoutFromRamp(start, stop, step int, period float64, peak int) (*Workout, error) {
	if start > stop {
		return nil, errors.New("start power must be less than stop power")
	}
	if step == 0 || period == 0 {
		return nil, errors.New("step or period cannot be zero")
	}
	if peak != 0 && (peak < stop || peak < start) {
		return nil, errors.New("peak value if used must be greater than start and stop value")
	}
	w := &Workout{Cycles: 1}
	if peak != 0 {
		for p := start; p < peak; p += step {
			w.Intervals = append(w.Intervals, WorkoutInterval{p, period})
		}
		for p := peak; p > stop; p -= step {
			w.Intervals = append(w.Intervals, WorkoutInterval{p, period})
		}
	} else {
		for p := start; p < stop; p += step {
			w.Intervals = append(w.Intervals, WorkoutInterval{p, period})
		}
	}
	return w, nil
}

// FitnessEquipment is the ANT+ fitness equipment profile (device type 17).
type FitnessEquipment struct {
	baseDevice
	Power PowerData
	Data  FitnessEquipmentData

	CommandStatus FECommandStatus
	LastCommand   FECommandStatusData
	TrainerStatus FETrainerStatusData
	Metabolic     FEMetabolicData
	Treadmill     FETreadmillData
	Capabilities  FECapabilitiesData

	powerUpdateEventCount  [2]int
	accumulatedPower       [2]int
	torqueUpdateEventCount [2]int
	wheelTicks             [2]int
	accumulatedTorque      [2]int
	wheelPeriod            [2]int

	workoutCh     chan *Workout
	stopWorkout   context.Context
	cancelWorkout context.CancelFunc
	workoutWG     chan struct{} // closed when the worker goroutine exits
}

// NewFitnessEquipment creates the profile.
func NewFitnessEquipment(node *easy.Node, deviceID int, transType int) (*FitnessEquipment, error) {
	f := &FitnessEquipment{}
	f.node = node
	f.log = defaultLogger()
	f.DeviceType = int(DeviceTypeFitnessEquipment)
	f.DeviceID = deviceID
	f.Period = 8192
	f.RFFreq = 57
	f.Name = "fitness_equipment"
	f.TransType = transType
	f.CommandStatus = FECommandUnknown
	f.Power = PowerData{LeftPower: -1, RightPower: -1, Cadence: 255}
	f.Data = FitnessEquipmentData{Resistance: 255.0, Speed: 65535.0, Incline: 32767.0, TargetResistance: 255.0}
	f.Metabolic = FEMetabolicData{METs: -1, CaloricBurnRate: -1}
	f.Treadmill = FETreadmillData{Cadence: -1}
	f.Capabilities = FECapabilitiesData{MaxResistance: -1}
	f.onProfileData = f.onData
	if err := f.openChannel(true, 0); err != nil {
		return nil, err
	}
	return f, nil
}

// StartWorkouts queues workouts to run sequentially in a background
// goroutine (openant uses a thread + queue; Go uses a goroutine + channel).
// The worker stops when the device channel is closed or ctx is cancelled.
func (f *FitnessEquipment) StartWorkouts(ctx context.Context, workouts ...*Workout) {
	f.workoutOnce(ctx)
	for _, w := range workouts {
		select {
		case f.workoutCh <- w:
		case <-f.stopWorkout.Done():
			return
		}
	}
}

func (f *FitnessEquipment) workoutOnce(ctx context.Context) {
	if f.workoutCh != nil {
		return
	}
	f.stopWorkout, f.cancelWorkout = context.WithCancel(ctx)
	f.workoutCh = make(chan *Workout)
	f.workoutWG = make(chan struct{})
	go func() {
		defer close(f.workoutWG)
		for {
			select {
			case <-f.stopWorkout.Done():
				return
			case w := <-f.workoutCh:
				if w.Loop {
					for {
						if f.runWorkout(w) {
							return
						}
					}
				} else {
					if f.runWorkout(w) {
						return
					}
				}
			}
		}
	}()
}

// runWorkout runs one workout pass; it reports whether the worker should stop.
func (f *FitnessEquipment) runWorkout(w *Workout) bool {
	for x := 0; x < w.Cycles; x++ {
		for i, iv := range w.Intervals {
			select {
			case <-f.stopWorkout.Done():
				return true
			default:
			}
			f.log.Info("workout interval",
				"loop", w.Loop, "cycle", fmt.Sprintf("%d/%d", x+1, w.Cycles),
				"interval", fmt.Sprintf("%d/%d", i+1, len(w.Intervals)),
				"power", iv.Power, "seconds", iv.Period)
			f.SetTargetPower(iv.Power)
			select {
			case <-time.After(time.Duration(iv.Period * float64(time.Second))):
			case <-f.stopWorkout.Done():
				return true
			}
		}
	}
	return false
}

func (f *FitnessEquipment) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x19: // standard power (specific trainer data, Table 8-25)
		f.powerUpdateEventCount[0] = f.powerUpdateEventCount[1]
		f.powerUpdateEventCount[1] = int(data[1])
		f.accumulatedPower[0] = f.accumulatedPower[1]
		f.accumulatedPower[1] = int(data[3]) + int(data[4])<<8

		f.Power.Cadence = int(data[2])
		f.Power.InstantaneousPower = int(data[5]) + int(data[6]&0x0F)<<8

		// Trainer status (byte 6 bits 4-7) and flags (byte 7 bits 0-3)
		// per Tables 8-27/8-28, plus the FE state (byte 7 bits 4-7).
		f.TrainerStatus = FETrainerStatusData{
			EventCount:                    int(data[1]),
			Cadence:                       int(data[2]),
			InstantaneousPower:            f.Power.InstantaneousPower,
			PowerCalibrationRequired:      data[6]&0x10 != 0,
			ResistanceCalibrationRequired: data[6]&0x20 != 0,
			UserConfigRequired:            data[6]&0x40 != 0,
			TargetPowerLimit:              int(data[7] & 0x03),
			State:                         FitnessEquipmentState((data[7] & 0x70) >> 4),
		}
		f.Data.State = f.TrainerStatus.State
		if f.TrainerStatus.UserConfigRequired {
			f.log.Warn("trainer requests user configuration", "device", f.String())
		}

		delta := (f.powerUpdateEventCount[1] + 256 - f.powerUpdateEventCount[0]) % 256
		if delta != 0 {
			f.Power.AveragePower = ((f.accumulatedPower[1] + 65536 - f.accumulatedPower[0]) % 65536) / delta
			f.TrainerStatus.AveragePower = f.Power.AveragePower
			f.log.Info("standard power update", "device", f.String(),
				"power", f.Power.InstantaneousPower, "average", f.Power.AveragePower,
				"cadence", f.Power.Cadence)
			f.fireDeviceData(int(data[0]), "standard_power", f.Power)
		}
		f.fireDeviceData(int(data[0]), "trainer_status", f.TrainerStatus)

	case 0x1A: // standard torque (wheel)
		// Layout per ANT+ FE-C Rev 5.0 Table 8-29: byte 1 event count,
		// byte 2 wheel ticks, bytes 3-4 wheel period, bytes 5-6
		// accumulated torque, byte 7 capabilities/FE state. Note: python
		// openant reads the period from bytes 4-5 and the torque from
		// bytes 6-7, which does not match the spec; we follow the spec.
		f.torqueUpdateEventCount[0] = f.torqueUpdateEventCount[1]
		f.torqueUpdateEventCount[1] = int(data[1])
		f.wheelTicks[0] = f.wheelTicks[1]
		f.wheelTicks[1] = int(data[2])
		f.wheelPeriod[0] = f.wheelPeriod[1]
		f.wheelPeriod[1] = int(data[3]) + int(data[4])<<8
		f.accumulatedTorque[0] = f.accumulatedTorque[1]
		f.accumulatedTorque[1] = int(data[5]) + int(data[6])<<8

		delta := (f.torqueUpdateEventCount[1] + 256 - f.torqueUpdateEventCount[0]) % 256
		deltaTorque := (f.accumulatedTorque[1] + 65536 - f.accumulatedTorque[0]) % 65536
		// Modular delta over uint16; see power_meter.go for the operator
		// precedence note (code review PR #1, P1-11).
		deltaWheelPeriod := (f.wheelPeriod[1] + 65536 - f.wheelPeriod[0]) % 65536

		f.Data.State = FitnessEquipmentState((data[7] & 0x70) >> 4)

		if delta != 0 {
			f.Power.Torque = math.Round(float64(deltaTorque)/(32*float64(delta))*100) / 100
			if deltaWheelPeriod != 0 {
				f.Power.AngularVelocity = math.Round((2*math.Pi*float64(delta))/(float64(deltaWheelPeriod)/2048)*100) / 100
			} else {
				f.Power.AngularVelocity = 0
			}
			f.Power.AveragePower = int(f.Power.Torque * f.Power.AngularVelocity)
			f.fireDeviceData(int(data[0]), "standard_torque", f.Power)
		}

	case 0x10: // general FE data
		f.Data.Type = FitnessEquipmentType(data[1])
		f.Data.Capabilities = int(data[2] & 0x0F)
		f.Data.Speed = math.Round(float64(int(data[4])+int(data[5])<<8)/1000*1000) / 1000
		f.Data.State = FitnessEquipmentState((data[7] & 0x70) >> 4)
		f.log.Info("general FE", "device", f.String(), "type", int(f.Data.Type), "state", int(f.Data.State))
		f.fireDeviceData(int(data[0]), "general_fe", f.Data)

	case 0x11: // general settings
		f.Data.Type = FitnessEquipmentType(data[1])
		f.Data.Resistance = math.Round(float64(data[6])/2*10) / 10
		incline := int(data[4]) + int(data[5])<<8
		if incline != 0x7FFF {
			f.Data.Incline = math.Round(float64(incline)/100*100) / 100 // 0.01 %
		}
		f.fireDeviceData(int(data[0]), "general_settings", f.Data)

	case 0x47: // common page 71: command status reply (Tables 8-48/8-49)
		f.Data.ResistanceMode = ResistanceMode(data[1])
		f.CommandStatus = FECommandStatusFromByte(data[3])
		f.LastCommand = FECommandStatusData{
			CommandID: ResistanceMode(data[1]),
			Sequence:  int(data[2]),
			Status:    f.CommandStatus,
		}
		switch f.Data.ResistanceMode {
		case ResistanceBasic:
			f.Data.Resistance = math.Round(float64(data[7])/2*10) / 10
			f.LastCommand.TotalResistance = f.Data.Resistance
		case ResistanceTargetPower:
			raw := int(data[6]) + int(data[7])<<8
			f.Data.Resistance = math.Round(float64(raw)/4*100) / 100
			f.LastCommand.TargetPower = raw / 4
		case ResistanceWind:
			f.LastCommand.WindCoefficient = float64(data[5]) / 100
			f.LastCommand.WindSpeed = int(data[6]) - 127
			f.LastCommand.DraftingFactor = float64(data[7]) / 100
		case ResistanceTrack:
			f.LastCommand.Grade = (float64(int(data[5])+int(data[6])<<8) - 20000) / 100
			f.LastCommand.RollingResistance = float64(data[7]) * 5e-5
		}
		if f.CommandStatus != FECommandPass && f.CommandStatus != FECommandUnitialised && f.CommandStatus != FECommandPending {
			f.log.Warn("last command went wrong", "status", int(f.CommandStatus))
		}
		f.log.Info("command page", "device", f.String(),
			"status", int(f.CommandStatus), "mode", int(f.Data.ResistanceMode),
			"resistance", f.Data.Resistance)
		f.fireDeviceData(int(data[0]), "command_status", f.LastCommand)

	case 0x12: // general FE metabolic data (Table 8-13)
		mets := int(data[2]) + int(data[3])<<8
		burn := int(data[4]) + int(data[5])<<8
		f.Metabolic = FEMetabolicData{
			METs:            -1,
			CaloricBurnRate: -1,
			Calories:        int(data[6]),
			Capabilities:    int(data[7] & 0x0F),
			State:           FitnessEquipmentState((data[7] & 0x70) >> 4),
		}
		if mets != 0xFFFF {
			f.Metabolic.METs = float64(mets) / 100
		}
		if burn != 0xFFFF {
			f.Metabolic.CaloricBurnRate = float64(burn) / 10
		}
		f.fireDeviceData(int(data[0]), "metabolic", f.Metabolic)

	case 0x13: // specific treadmill data (Table 8-15)
		f.Treadmill = FETreadmillData{
			Cadence:                  -1,
			NegativeVerticalDistance: float64(data[5]) / 10,
			PositiveVerticalDistance: float64(data[6]) / 10,
			Capabilities:             int(data[7] & 0x0F),
			State:                    FitnessEquipmentState((data[7] & 0x70) >> 4),
		}
		if data[4] != 0xFF {
			f.Treadmill.Cadence = int(data[4])
		}
		f.fireDeviceData(int(data[0]), "treadmill", f.Treadmill)

	case 0x36: // FE capabilities (Tables 8-45/8-46)
		maxRes := int(data[5]) + int(data[6])<<8
		f.Capabilities = FECapabilitiesData{
			BasicMode:       data[7]&0x01 != 0,
			TargetPowerMode: data[7]&0x02 != 0,
			SimulationMode:  data[7]&0x04 != 0,
			MaxResistance:   -1,
		}
		if maxRes != 0xFFFF {
			f.Capabilities.MaxResistance = maxRes
		}
		f.fireDeviceData(int(data[0]), "fe_capabilities", f.Capabilities)
	}
}

// SetTargetPower sends the target power page 0x31 (0.25 W units).
func (f *FitnessEquipment) SetTargetPower(power int) error {
	if power > 4000 {
		return errors.New("target power cannot exceed 4000 W")
	}
	f.Data.TargetResistance = float64(power)
	data := []byte{0x31, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00}
	p := power * 4
	data[6] = byte(p)
	data[7] = byte(p >> 8)
	f.SendAcknowledgedData(data)
	f.RequestDP(71, 1)
	return nil
}

// SetBasicResistance sends the basic resistance page 0x30 (0.5 % units).
func (f *FitnessEquipment) SetBasicResistance(resistance float64) error {
	if resistance > 100.0 {
		return errors.New("target resistance cannot exceed 100%")
	}
	f.Data.TargetResistance = resistance
	data := []byte{0x30, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
	data[7] = byte(int(resistance*2)) & 0xFF
	f.SendAcknowledgedData(data)
	f.RequestDP(71, 1)
	return nil
}

// encodeWindResistancePage builds the wind resistance page 0x32
// (display → trainer) per ANT+ FE-C Rev 5.0 Table 8-38: bytes 1-4 are
// reserved 0xFF, byte 5 is the wind resistance coefficient in 0.01 kg/m
// units (invalid 0xFF), byte 6 the wind speed with a -127 km/h offset
// (0x00 = -127 km/h, 0x7F = 0, 0xFE = +127) and byte 7 the drafting
// scale factor in 0.01 units (0.00-1.00, invalid 0xFF; 1.00 = no
// drafting effects).
func encodeWindResistancePage(coefficient float64, windSpeed int, draftingFactor float64) ([]byte, error) {
	raw := int(math.Round(coefficient * 100))
	if coefficient < 0 || raw > 254 {
		return nil, fmt.Errorf("wind resistance coefficient %v out of range 0..2.54 kg/m", coefficient)
	}
	if windSpeed < -127 || windSpeed > 127 {
		return nil, fmt.Errorf("wind speed %d out of range -127..127 km/h", windSpeed)
	}
	draft := int(math.Round(draftingFactor * 100))
	if draftingFactor < 0 || draft > 100 {
		return nil, fmt.Errorf("drafting factor %v out of range 0..1.00", draftingFactor)
	}
	data := []byte{0x32, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	data[5] = byte(raw)
	data[6] = byte(windSpeed + 127)
	data[7] = byte(draft)
	return data, nil
}

// SetWindResistance sends the wind resistance page 0x32: the wind
// resistance coefficient (0..2.54 kg/m; the trainer assumes the default
// 0.51 kg/m when the field is invalid), the wind speed (-127..127 km/h,
// positive is a headwind) and the drafting scale factor (0.00-1.00,
// where 1.00 = no drafting effects and 0.00 = air resistance removed).
// Addresses openant issue #126.
func (f *FitnessEquipment) SetWindResistance(coefficient float64, windSpeed int, draftingFactor float64) error {
	data, err := encodeWindResistancePage(coefficient, windSpeed, draftingFactor)
	if err != nil {
		return err
	}
	f.SendAcknowledgedData(data)
	f.RequestDP(71, 1)
	return nil
}

// encodeTrackResistancePage builds the track resistance page 0x33
// (display → trainer) per ANT+ FE-C Rev 5.0 Table 8-42: bytes 1-4 are
// reserved 0xFF, bytes 5-6 the grade as an unsigned 16-bit value in
// 0.01 % units with a -200 % offset (0x4E20 = 0 %, 0x9C40 = +200 %,
// invalid 0xFFFF) and byte 7 the rolling resistance coefficient in
// 5x10^-5 units (0..0.0127, invalid 0xFF = use default).
func encodeTrackResistancePage(grade, rollingResistanceCoefficient float64) ([]byte, error) {
	raw := int(math.Round(grade*100)) + 20000 // offset -200 %
	if grade < -200 || grade > 200 || raw < 0 || raw > 0x9C40 {
		return nil, fmt.Errorf("grade %v out of range -200..200 %%", grade)
	}
	rolling := int(math.Round(rollingResistanceCoefficient * 20000))
	if rollingResistanceCoefficient < 0 || rolling > 254 {
		return nil, fmt.Errorf("rolling resistance coefficient %v out of range 0..0.0127", rollingResistanceCoefficient)
	}
	data := []byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	data[5] = byte(raw)
	data[6] = byte(raw >> 8)
	data[7] = byte(rolling)
	return data, nil
}

// SetTrackResistance sends the track resistance page 0x33: the grade
// (-200..200 %, positive is uphill) and the rolling resistance
// coefficient (0..0.0127). Addresses openant issue #126.
func (f *FitnessEquipment) SetTrackResistance(grade, rollingResistanceCoefficient float64) error {
	data, err := encodeTrackResistancePage(grade, rollingResistanceCoefficient)
	if err != nil {
		return err
	}
	f.SendAcknowledgedData(data)
	f.RequestDP(71, 1)
	return nil
}

// encodeUserConfigPage builds the user configuration page 0x37
// (display → trainer) per ANT+ FE-C Rev 5.0 Table 8-47: user weight in
// 0.01 kg units (u16, invalid 0xFFFF), bicycle weight in 0.05 kg units
// (12 bits: low nibble in byte 4 bits 4-7, high byte in byte 5, invalid
// 0xFFF), wheel diameter offset in millimetres (byte 4 bits 0-3) on top
// of the wheel diameter in 0.01 m units (byte 6, invalid 0xFF) and the
// gear ratio in 0.03 units (byte 7, invalid 0x00).
func encodeUserConfigPage(userKg, bicycleKg, wheelDiameterM, gearRatio float64) ([]byte, error) {
	user := int(math.Round(userKg * 100))
	if userKg < 0 || user > 65534 {
		return nil, fmt.Errorf("user weight %v out of range 0..655.34 kg", userKg)
	}
	bicycle := int(math.Round(bicycleKg / 0.05))
	if bicycleKg < 0 || bicycle > 1000 {
		return nil, fmt.Errorf("bicycle weight %v out of range 0..50 kg", bicycleKg)
	}
	wheelMm := int(math.Round(wheelDiameterM * 1000))
	base, offset := wheelMm/10, wheelMm%10
	if wheelDiameterM < 0 || base > 254 {
		return nil, fmt.Errorf("wheel diameter %v out of range 0..2.549 m", wheelDiameterM)
	}
	gear := int(math.Round(gearRatio / 0.03))
	if gearRatio < 0 || gear > 255 {
		return nil, fmt.Errorf("gear ratio %v out of range 0..7.65", gearRatio)
	}
	data := []byte{0x37, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	data[1] = byte(user)
	data[2] = byte(user >> 8)
	data[4] = byte(offset) | byte(bicycle&0x0F)<<4
	data[5] = byte(bicycle >> 4)
	data[6] = byte(base)
	data[7] = byte(gear)
	return data, nil
}

// SetUserConfig sends the user configuration page 0x37 as an
// acknowledged message (ANT+ FE-C Rev 5.0 section 8.10.2): the user
// weight (0..655.34 kg), bicycle weight (0..50 kg), bicycle wheel
// diameter (0..2.549 m, millimetre resolution) and gear ratio (front :
// rear, 0..7.65; 0 marks the field invalid). Trainers use this data for
// accurate simulation and target power modes and request it via the
// user-configuration-required trainer status bit.
func (f *FitnessEquipment) SetUserConfig(userKg, bicycleKg, wheelDiameterM, gearRatio float64) error {
	data, err := encodeUserConfigPage(userKg, bicycleKg, wheelDiameterM, gearRatio)
	if err != nil {
		return err
	}
	f.SendAcknowledgedData(data)
	return nil
}

// RequestCapabilities asks the trainer for the FE capabilities page 54
// (0x36); the reply arrives as a broadcast and populates the
// Capabilities field (and the "fe_capabilities" device data event).
func (f *FitnessEquipment) RequestCapabilities() {
	f.RequestDP(54, 1)
}

// CloseChannel stops the workout worker before closing the channel.
func (f *FitnessEquipment) CloseChannel() {
	if f.cancelWorkout != nil {
		f.cancelWorkout()
		if f.workoutWG != nil {
			<-f.workoutWG
		}
	}
	f.baseDevice.CloseChannel()
}

func init() {
	registerProfile(DeviceTypeFitnessEquipment, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewFitnessEquipment(node, deviceID, transType)
	})
}
