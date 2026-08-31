package devices

import (
	"fmt"

	"github.com/maxdukov/openant-go/easy"
)

// LightType is the bicycle light type.
type LightType byte

// Light types.
const (
	LightHeadlight          LightType = 0
	LightReservedType       LightType = 1
	LightTaillight          LightType = 2
	LightSignalConfigurable LightType = 3
	LightSignalLeft         LightType = 4
	LightSignalRight        LightType = 5
	LightOther              LightType = 7
)

// BatteryWarning is the light battery status.
type BatteryWarning byte

// Light battery warning values.
const (
	LightBattReserved BatteryWarning = 0
	LightBattNew      BatteryWarning = 1
	LightBattGood     BatteryWarning = 2
	LightBattOk       BatteryWarning = 3
	LightBattLow      BatteryWarning = 4
	LightBattCritical BatteryWarning = 5
	LightBattCharging BatteryWarning = 6
	LightBattInvalid  BatteryWarning = 7
)

// LightBeam is the beam position.
type LightBeam byte

// Beam values.
const (
	BeamLow  LightBeam = 0
	BeamHigh LightBeam = 1
)

// LightMode is the light operating mode (raw 6-bit value).
type LightMode byte

// Light modes.
const (
	LightModeOff                       LightMode = 0
	LightModeSteady81To100             LightMode = 1
	LightModeSteady61To80              LightMode = 2
	LightModeSteady41To60              LightMode = 3
	LightModeSteady21To40              LightMode = 4
	LightModeSteady0To20               LightMode = 5
	LightModeSlowFlash                 LightMode = 6
	LightModeFastFlash                 LightMode = 7
	LightModeRandomFlash               LightMode = 8
	LightModeAuto                      LightMode = 9
	LightModeTurnSignalLeftSelfCancel  LightMode = 10
	LightModeTurnSignalLeftOngoing     LightMode = 11
	LightModeTurnSignalRightSelfCancel LightMode = 12
	LightModeTurnSignalRightOngoing    LightMode = 13
	LightModeHazard                    LightMode = 14
	LightModeReserved                  LightMode = 15 // 15-47
	LightModeCustom                    LightMode = 48 // 48-63
)

// LightModeFromByte maps raw values (15-47 reserved, 48-63 custom).
func LightModeFromByte(b byte) LightMode {
	if b >= 15 && b <= 47 {
		return LightModeReserved
	}
	if b >= 48 && b <= 63 {
		return LightModeCustom
	}
	return LightMode(b)
}

// Intensity sentinel values.
const (
	LightIntensityBrakeOverride byte = 0xFD
	LightIntensityAuto          byte = 0xFE
	LightIntensityInvalid       byte = 0xFF
	BeamFocusInvalid            byte = 0xFF
	SubLightIntensityAuto       byte = 0x7E
	SubLightIntensityInvalid    byte = 0x7F
)

// SubLightData is the state of one sub-light (data page 3).
type SubLightData struct {
	SubLightIndex  int       `influx:"sub_light_index"`
	LightType      LightType `influx:"light_type"`
	Beam           LightBeam `influx:"beam"`
	Mode           LightMode `influx:"mode"`
	ModeRaw        int       `influx:"mode_raw"`
	Intensity      int       `influx:"intensity"`
	BatteryWarning bool      `influx:"battery_warning"`
}

// DataName implements DeviceData.
func (SubLightData) DataName() string { return "SubLightData" }

// BicycleLightsData is the light state from data page 1.
type BicycleLightsData struct {
	LightIndex          int            `influx:"light_index"`
	BikeRadarSupport    bool           `influx:"bike_radar_support"`
	LightType           LightType      `influx:"light_type"`
	BatteryStatus       BatteryWarning `influx:"battery_status"`
	NumberOfSubLights   int            `influx:"number_of_sub_lights"`
	LastCommandSequence int            `influx:"last_command_sequence"`
	BeamFocus           int            `influx:"beam_focus"` // %
	Beam                LightBeam      `influx:"beam"`
	Mode                LightMode      `influx:"mode"`
	ModeRaw             int            `influx:"mode_raw"`
	Intensity           int            `influx:"intensity"` // %
}

// DataName implements DeviceData.
func (BicycleLightsData) DataName() string { return "BicycleLightsData" }

// BicycleLightsCapabilities is the capabilities from data page 2.
type BicycleLightsCapabilities struct {
	LightIndex                     int  `influx:"light_index"`
	AutoIntensitySupported         bool `influx:"auto_intensity_supported"`
	HighLowBeamSupported           bool `influx:"high_low_beam_supported"`
	NumberOfSupportedModes         int  `influx:"number_of_supported_modes"`
	BatteryCapacityMah             int  `influx:"battery_capacity_mah"`
	SupportedSecondaryLights       int  `influx:"supported_secondary_lights"`
	BeamFocusControlCapable        bool `influx:"beam_focus_control_capable"`
	BeamIntensityControlCapable    bool `influx:"beam_intensity_control_capable"`
	SynchronousBrakeLightSupported bool `influx:"synchronous_brake_light_supported"`
	SupportedStandardModes         int  `influx:"supported_standard_modes"`
	SupportedLightTypes            int  `influx:"supported_light_types"`
}

// DataName implements DeviceData.
func (BicycleLightsCapabilities) DataName() string { return "BicycleLightsCapabilities" }

// BicycleLights is the ANT+ bicycle lights profile (device type 35).
type BicycleLights struct {
	baseDevice
	Data         BicycleLightsData
	Capabilities BicycleLightsCapabilities
	SubLights    map[int]SubLightData

	commandSequence byte
}

// NewBicycleLights creates the profile.
func NewBicycleLights(node *easy.Node, deviceID int, transType int) (*BicycleLights, error) {
	b := &BicycleLights{SubLights: map[int]SubLightData{}}
	b.node = node
	b.log = defaultLogger()
	b.DeviceType = int(DeviceTypeBicycleLights)
	b.DeviceID = deviceID
	b.Period = 4084 // ~8.02 Hz
	b.RFFreq = 57
	b.Name = "bicycle_lights"
	b.TransType = transType
	b.Data = BicycleLightsData{
		LastCommandSequence: 0xFF,
		BeamFocus:           int(BeamFocusInvalid),
		Intensity:           int(LightIntensityInvalid),
	}
	b.onProfileData = b.onData
	if err := b.openChannel(true, 0); err != nil {
		return nil, err
	}
	return b, nil
}

func decodeSubLight(index int, typeByte, stateByte, intensityByte byte) SubLightData {
	return SubLightData{
		SubLightIndex:  index,
		LightType:      LightType((typeByte >> 3) & 0x07),
		Beam:           LightBeam((stateByte >> 1) & 0x01),
		Mode:           LightModeFromByte((stateByte >> 2) & 0x3F),
		ModeRaw:        int((stateByte >> 2) & 0x3F),
		Intensity:      int(intensityByte & 0x7F),
		BatteryWarning: intensityByte&0x80 != 0,
	}
}

func (b *BicycleLights) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01: // Light States 1
		d := &b.Data
		d.LightIndex = int(data[1] & 0x3F)
		d.BikeRadarSupport = (data[2]>>1)&0x01 != 0
		d.LightType = LightType((data[2] >> 2) & 0x07)
		d.BatteryStatus = BatteryWarning((data[2] >> 5) & 0x07)
		d.NumberOfSubLights = int(data[3] & 0x07)
		d.LastCommandSequence = int(data[4])
		d.BeamFocus = int(data[5])
		d.Beam = LightBeam((data[6] >> 1) & 0x01)
		d.ModeRaw = int((data[6] >> 2) & 0x3F)
		d.Mode = LightModeFromByte(byte(d.ModeRaw))
		d.Intensity = int(data[7])
		b.fireDeviceData(int(data[0]), "light_states", *d)

	case 0x02: // Light Capabilities
		c := &b.Capabilities
		c.LightIndex = int(data[1] & 0x3F)
		c.AutoIntensitySupported = data[2]&0x01 != 0
		c.HighLowBeamSupported = (data[2]>>1)&0x01 != 0
		c.NumberOfSupportedModes = int((data[2] >> 2) & 0x3F)
		c.BatteryCapacityMah = 0
		if data[3] != 0xFF {
			c.BatteryCapacityMah = int(data[3]) * 200
		}
		c.SupportedSecondaryLights = int(data[4] & 0x0F)
		// Bit 6 inverted: 0 = capable.
		c.BeamFocusControlCapable = ((data[4] >> 6) & 0x01) == 0
		c.BeamIntensityControlCapable = (data[4]>>7)&0x01 != 0
		c.SupportedStandardModes = int(data[5]) | int(data[6]&0x7F)<<8
		c.SynchronousBrakeLightSupported = (data[6]>>7)&0x01 != 0
		c.SupportedLightTypes = int(data[7])
		b.fireDeviceData(int(data[0]), "light_capabilities", *c)

	case 0x03: // Sub-light State (two per page)
		subAIndex := int(data[2] & 0x07)
		subA := decodeSubLight(subAIndex, data[2], data[3], data[4])
		b.SubLights[subAIndex] = subA
		b.fireDeviceData(int(data[0]), "sub_light_state", subA)

		if data[5] != 0x00 || data[6] != 0x00 || data[7] != 0x00 {
			subBIndex := subAIndex + 1
			subB := decodeSubLight(subBIndex, data[5], data[6], data[7])
			b.SubLights[subBIndex] = subB
			b.fireDeviceData(int(data[0]), "sub_light_state", subB)
		}
	}
}

// SetLightOption configures a Light Settings command (page 34).
type SetLightOption struct {
	LightIndex          int
	SubLightIndex       int
	AddressAllSubLights bool
	LightType           LightType
	Mode                *LightMode
	Beam                LightBeam
	Intensity           *int // 0-100 %
	ControllerID        int
}

// SetLight sends the Light Settings command page 0x22.
func (b *BicycleLights) SetLight(o SetLightOption) {
	page := make([]byte, 8)
	page[0] = 0x22
	page[1] = byte(o.LightIndex) & 0x3F

	var addrByte byte
	if o.AddressAllSubLights {
		addrByte |= 1 << 3
	}
	page[2] = byte(o.SubLightIndex&0x07) | addrByte | (byte(o.LightType)&0x0F)<<4

	b.commandSequence = (b.commandSequence + 1) & 0xFF
	page[3] = b.commandSequence
	page[4] = byte(o.ControllerID) & 0xFF

	// Byte 5 beam adjustment specifier: bit 4 set = do not adjust focus,
	// bit 5 set = adjust intensity (value in byte 7).
	adjust := byte(0b00010000)
	beamAdjustment := byte(0xFF)
	if o.Intensity != nil {
		adjust |= 0b00100000
		v := *o.Intensity
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		beamAdjustment = byte(v)
	}
	page[5] = adjust
	if o.Mode != nil {
		page[6] = (byte(o.Beam) & 0x01) << 1
		page[6] |= (byte(*o.Mode) & 0x3F) << 2
	}
	page[7] = beamAdjustment
	b.SendAcknowledgedData(page)
}

// Disconnect sends the Disconnect command page 0x20; lightIndex 0
// disconnects all lights.
func (b *BicycleLights) Disconnect(lightIndex int, controllerID int) {
	page := make([]byte, 8)
	page[0] = 0x20
	page[1] = byte(lightIndex) & 0x3F
	page[2] = byte(controllerID) & 0xFF
	b.SendAcknowledgedData(page)
}

// RequestDP requests a data page from the light. Byte 1 carries the light
// index in this profile (spec 7.22.3); descriptor 1 defaults to 63 for the
// custom mode description page.
func (b *BicycleLights) RequestDP(page int, noTimes int, lightIndex int, descriptor1 int, descriptor2 int) {
	if descriptor1 == 0 {
		descriptor1 = 63
	}
	if descriptor2 == 0 {
		descriptor2 = 0xFF
	}
	data := []byte{
		0x46,
		byte(lightIndex) & 0x3F,
		0xFF,
		byte(descriptor1) & 0xFF,
		byte(descriptor2) & 0xFF,
		byte(noTimes) & 0x7F,
		byte(page) & 0xFF,
		0x01,
	}
	b.log.Info("requesting data page", "device", b.String(),
		"page", fmt.Sprintf("%#04x", page), "light_index", lightIndex, "no_times", noTimes)
	b.SendAcknowledgedData(data)
}

func init() {
	registerProfile(DeviceTypeBicycleLights, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewBicycleLights(node, deviceID, transType)
	})
}
