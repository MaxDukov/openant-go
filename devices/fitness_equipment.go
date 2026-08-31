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
	case 0x19: // standard power
		f.powerUpdateEventCount[0] = f.powerUpdateEventCount[1]
		f.powerUpdateEventCount[1] = int(data[1])
		f.accumulatedPower[0] = f.accumulatedPower[1]
		f.accumulatedPower[1] = int(data[3]) + int(data[4])<<8

		f.Power.Cadence = int(data[2])
		f.Power.InstantaneousPower = int(data[5]) + int(data[6]&0x0F)<<8

		delta := (f.powerUpdateEventCount[1] + 256 - f.powerUpdateEventCount[0]) % 256
		if delta != 0 {
			f.Power.AveragePower = ((f.accumulatedPower[1] + 65536 - f.accumulatedPower[0]) % 65536) / delta
			f.log.Info("standard power update", "device", f.String(),
				"power", f.Power.InstantaneousPower, "average", f.Power.AveragePower,
				"cadence", f.Power.Cadence)
			f.fireDeviceData(int(data[0]), "standard_power", f.Power)
		}

	case 0x1A: // standard torque (wheel)
		f.torqueUpdateEventCount[0] = f.torqueUpdateEventCount[1]
		f.torqueUpdateEventCount[1] = int(data[1])
		f.wheelTicks[0] = f.wheelTicks[1]
		f.wheelTicks[1] = int(data[2])
		f.wheelPeriod[0] = f.wheelPeriod[1]
		f.wheelPeriod[1] = int(data[4]) + int(data[5])<<8
		f.accumulatedTorque[0] = f.accumulatedTorque[1]
		f.accumulatedTorque[1] = int(data[6]) + int(data[7])<<8

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

	case 0x47: // command status reply
		f.Data.ResistanceMode = ResistanceMode(data[1])
		switch f.Data.ResistanceMode {
		case ResistanceBasic:
			f.Data.Resistance = math.Round(float64(data[7])/2*10) / 10
		case ResistanceTargetPower:
			f.Data.Resistance = math.Round(float64(int(data[6])+int(data[7])<<8)/4*100) / 100
		}
		f.CommandStatus = FECommandStatusFromByte(data[3])
		if f.CommandStatus != FECommandPass && f.CommandStatus != FECommandUnitialised && f.CommandStatus != FECommandPending {
			f.log.Warn("last command went wrong", "status", int(f.CommandStatus))
		}
		f.log.Info("command page", "device", f.String(),
			"status", int(f.CommandStatus), "mode", int(f.Data.ResistanceMode),
			"resistance", f.Data.Resistance)
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
