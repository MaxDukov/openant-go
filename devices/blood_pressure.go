package devices

import (
	"github.com/maxdukov/openant-go/easy"
)

// Blood pressure measurement values (mmHg) use these invalid markers per
// the ANT+ Blood Pressure Monitor profile data page.
const (
	BloodPressureInvalid = -1 // 0xFFFF systolic/diastolic, 0xFF MAP
	BloodPressureHRInval = -1 // 0 heart rate
)

// BloodPressureData is the ANT+ blood pressure measurement data
// (Measurement Data Page 1).
//
// The ANT+ Blood Pressure Monitor device profile specification is no
// longer distributed openly (the ANT+ Alliance membership and document
// downloads ended in 2025); the layout decoded here follows that profile
// and has not been verified against an official PDF. Treat the decoded
// fields as informational until verified with a real blood pressure
// monitor.
type BloodPressureData struct {
	Systolic  int  `influx:"systolic"`   // mmHg, BloodPressureInvalid when invalid
	Diastolic int  `influx:"diastolic"`  // mmHg, BloodPressureInvalid when invalid
	MAP       int  `influx:"map"`        // mean arterial pressure, mmHg
	HeartRate int  `influx:"heart_rate"` // bpm, BloodPressureHRInval when invalid
	Flags     byte `influx:"flags"`      // raw measurement flags byte
}

// DataName implements DeviceData.
func (BloodPressureData) DataName() string { return "BloodPressureData" }

// BloodPressure is the ANT+ blood pressure monitor display profile
// (device type 18). Blood pressure monitors usually deliver their stored
// measurements over an ANT-FS file transfer; this profile only listens to
// the broadcast measurement page.
type BloodPressure struct {
	baseDevice
	Data BloodPressureData
}

// NewBloodPressure creates the profile. deviceID 0 matches the first
// found monitor.
func NewBloodPressure(node *easy.Node, deviceID int, transType int) (*BloodPressure, error) {
	b := &BloodPressure{}
	b.node = node
	b.log = defaultLogger()
	b.DeviceType = int(DeviceTypeBloodPressure)
	b.DeviceID = deviceID
	b.Period = 8192 // 4 Hz
	b.RFFreq = 57
	b.Name = "blood_pressure"
	b.TransType = transType
	b.Data = BloodPressureData{
		Systolic:  BloodPressureInvalid,
		Diastolic: BloodPressureInvalid,
		MAP:       BloodPressureInvalid,
		HeartRate: BloodPressureHRInval,
	}
	b.onProfileData = b.onData
	if err := b.openChannel(true, 0); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *BloodPressure) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01: // Measurement Data Page 1
		b.Data.Systolic = int(uint16(data[1]) | uint16(data[2])<<8)
		if b.Data.Systolic == 0xFFFF {
			b.Data.Systolic = BloodPressureInvalid
		}
		b.Data.Diastolic = int(uint16(data[3]) | uint16(data[4])<<8)
		if b.Data.Diastolic == 0xFFFF {
			b.Data.Diastolic = BloodPressureInvalid
		}
		b.Data.MAP = int(data[5])
		if b.Data.MAP == 0xFF {
			b.Data.MAP = BloodPressureInvalid
		}
		b.Data.HeartRate = int(data[6])
		if b.Data.HeartRate == 0 {
			b.Data.HeartRate = BloodPressureHRInval
		}
		b.Data.Flags = data[7]
		b.log.Info("blood pressure update", "device", b.String(),
			"systolic", b.Data.Systolic, "diastolic", b.Data.Diastolic,
			"map", b.Data.MAP, "heart_rate", b.Data.HeartRate)
		b.fireDeviceData(int(data[0]), "measurement", b.Data)
	}
}

func init() {
	registerProfile(DeviceTypeBloodPressure, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewBloodPressure(node, deviceID, transType)
	})
}
