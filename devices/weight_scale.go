package devices

import (
	"log/slog"
	"math"

	"github.com/maxdukov/openant-go/easy"
)

// WeightScaleData is the ANT+ weight scale data (openant issue #92).
// Fields not yet received keep their invalid defaults (-1).
type WeightScaleData struct {
	UserProfile     int     `influx:"user_profile"`     // profile ID, 0xFFFF invalid
	Weight          float64 `influx:"weight"`           // kg
	Hydration       float64 `influx:"hydration"`        // %
	BodyFat         float64 `influx:"body_fat"`         // %
	ActiveMetabolic float64 `influx:"active_metabolic"` // kcal/day
	BasalMetabolic  float64 `influx:"basal_metabolic"`  // kcal/day
	MuscleMass      float64 `influx:"muscle_mass"`      // kg
	BoneMass        float64 `influx:"bone_mass"`        // kg
	Gender          string  `influx:"gender"`           // "M"/"F" from the user profile page
	Age             int     `influx:"age"`              // years
	Height          int     `influx:"height"`           // cm
}

// DataName implements DeviceData.
func (WeightScaleData) DataName() string { return "WeightScaleData" }

// WeightScale is the ANT+ weight scale profile (device type 119).
type WeightScale struct {
	baseDevice
	Data WeightScaleData
}

// NewWeightScale creates the profile on the given node.
func NewWeightScale(node *easy.Node, deviceID int, transType int) (*WeightScale, error) {
	w := &WeightScale{}
	w.node = node
	w.log = slog.Default()
	w.DeviceType = int(DeviceTypeWeightScale)
	w.DeviceID = deviceID
	w.Period = 8192
	w.RFFreq = 57
	w.Name = "weight_scale"
	w.TransType = transType
	w.Data = WeightScaleData{UserProfile: 0xFFFF, Weight: -1, Hydration: -1,
		BodyFat: -1, ActiveMetabolic: -1, BasalMetabolic: -1,
		MuscleMass: -1, BoneMass: -1, Age: -1, Height: -1}
	w.onProfileData = w.onData
	if err := w.openChannel(true, 0); err != nil {
		return nil, err
	}
	return w, nil
}

func uint16LE(data []byte, i int) int { return int(data[i]) + int(data[i+1])<<8 }

// validU16 reports whether the little-endian field at data[i:i+2] carries
// a measurement (invalid is 0xFFFF, 0xFFFE is out of range).
func validU16(data []byte, i int) bool {
	v := uint16LE(data, i)
	return v != 0xFFFF && v != 0xFFFE
}

// onData decodes the weight scale TX data pages:
//
//	0x01 body weight:         u16 profile, byte3 status, u16 weight (0.01 kg)
//	0x02 body composition %:  u16 profile, u16 hydration (0.01 %), u16 body fat (0.01 %)
//	0x03 metabolic:           u16 profile, u16 active MET (0.25 kcal), u16 basal MET (0.25 kcal)
//	0x04 body composition kg: u16 profile, u16 muscle mass (0.01 kg), byte7 bone mass (0.1 kg)
//	0x3A user profile:        u16 profile, byte5 gender|age, byte6 height (cm)
func (w *WeightScale) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01:
		if p := uint16LE(data, 1); p != 0xFFFF {
			w.Data.UserProfile = p
		}
		if validU16(data, 6) {
			w.Data.Weight = math.Round(float64(uint16LE(data, 6))/100*100) / 100
		}
	case 0x02:
		if p := uint16LE(data, 1); p != 0xFFFF {
			w.Data.UserProfile = p
		}
		if validU16(data, 4) {
			w.Data.Hydration = float64(uint16LE(data, 4)) / 100
		}
		if validU16(data, 6) {
			w.Data.BodyFat = float64(uint16LE(data, 6)) / 100
		}
	case 0x03:
		if p := uint16LE(data, 1); p != 0xFFFF {
			w.Data.UserProfile = p
		}
		if validU16(data, 4) {
			w.Data.ActiveMetabolic = float64(uint16LE(data, 4)) / 4
		}
		if validU16(data, 6) {
			w.Data.BasalMetabolic = float64(uint16LE(data, 6)) / 4
		}
	case 0x04:
		if p := uint16LE(data, 1); p != 0xFFFF {
			w.Data.UserProfile = p
		}
		if validU16(data, 5) {
			w.Data.MuscleMass = float64(uint16LE(data, 5)) / 100
		}
		if data[7] != 0xFF && data[7] != 0xFE {
			w.Data.BoneMass = float64(data[7]) / 10
		}
	case 0x3A:
		if p := uint16LE(data, 1); p != 0xFFFF {
			w.Data.UserProfile = p
		}
		if g := data[5]; g != 0 {
			if g&0x80 != 0 {
				w.Data.Gender = "M"
			} else {
				w.Data.Gender = "F"
			}
			w.Data.Age = int(g & 0x7F)
		}
		if h := data[6]; h != 0xFF && h != 0 {
			w.Data.Height = int(h)
		}
	default:
		return
	}
	w.log.Info("weight scale", "device", w.String(), "weight", w.Data.Weight)
	w.fireDeviceData(int(data[0]), "weight_scale", w.Data)
}

func init() {
	registerProfile(DeviceTypeWeightScale, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewWeightScale(node, deviceID, transType)
	})
}
