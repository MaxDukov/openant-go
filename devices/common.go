// Package devices implements ANT+ device profiles on top of the easy
// layer. It is a Go port of the openant.devices Python module.
package devices

import (
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxdukov/openant-go/easy"
)

// ANTPLUS_NETWORK_KEY is the public ANT+ network key.
var ANTPLUS_NETWORK_KEY = []byte{0xB9, 0xA5, 0x21, 0xFB, 0xBD, 0x72, 0xC3, 0x45}

// DeviceType is an ANT+ device profile identifier.
type DeviceType int

// ANT+ device profile identifiers.
const (
	DeviceTypeUnknown             DeviceType = 255
	DeviceTypePowerMeter          DeviceType = 11
	DeviceTypeControlsDevice      DeviceType = 16
	DeviceTypeFitnessEquipment    DeviceType = 17
	DeviceTypeBloodPressure       DeviceType = 18
	DeviceTypeGeocache            DeviceType = 19
	DeviceTypeLev                 DeviceType = 20
	DeviceTypeEnvironment         DeviceType = 25
	DeviceTypeRadar               DeviceType = 40
	DeviceTypeShifting            DeviceType = 34
	DeviceTypeBicycleLights       DeviceType = 35
	DeviceTypeTirePressureMonitor DeviceType = 48
	DeviceTypeWeightScale         DeviceType = 119
	DeviceTypeHeartRate           DeviceType = 120
	DeviceTypeBikeSpeedCadence    DeviceType = 121
	DeviceTypeBikeCadence         DeviceType = 122
	DeviceTypeBikeSpeed           DeviceType = 123
	DeviceTypeStrideSpeed         DeviceType = 124
	DeviceTypeDropperSeatpost     DeviceType = 115
	DeviceTypeCoreTemp            DeviceType = 127
)

// String returns the profile name; unknown values map to "Unknown" like
// openant's Enum._missing_ fallback.
func (t DeviceType) String() string {
	switch t {
	case DeviceTypePowerMeter:
		return "PowerMeter"
	case DeviceTypeControlsDevice:
		return "ControlsDevice"
	case DeviceTypeFitnessEquipment:
		return "FitnessEquipment"
	case DeviceTypeBloodPressure:
		return "BloodPressure"
	case DeviceTypeGeocache:
		return "Geocache"
	case DeviceTypeLev:
		return "Lev"
	case DeviceTypeEnvironment:
		return "Environment"
	case DeviceTypeRadar:
		return "Radar"
	case DeviceTypeShifting:
		return "Shifting"
	case DeviceTypeBicycleLights:
		return "BicycleLights"
	case DeviceTypeTirePressureMonitor:
		return "TirePressureMonitor"
	case DeviceTypeWeightScale:
		return "WeightScale"
	case DeviceTypeHeartRate:
		return "HeartRate"
	case DeviceTypeBikeSpeedCadence:
		return "BikeSpeedCadence"
	case DeviceTypeBikeCadence:
		return "BikeCadence"
	case DeviceTypeBikeSpeed:
		return "BikeSpeed"
	case DeviceTypeStrideSpeed:
		return "StrideSpeed"
	case DeviceTypeDropperSeatpost:
		return "DropperSeatpost"
	case DeviceTypeCoreTemp:
		return "CoreTemp"
	}
	return "Unknown"
}

// DeviceTypeByName parses a profile name (as used by the CLI), returning
// DeviceTypeUnknown when unknown.
func DeviceTypeByName(name string) DeviceType {
	for _, t := range []DeviceType{
		DeviceTypePowerMeter, DeviceTypeControlsDevice, DeviceTypeFitnessEquipment,
		DeviceTypeBloodPressure, DeviceTypeGeocache, DeviceTypeLev,
		DeviceTypeEnvironment, DeviceTypeRadar, DeviceTypeShifting,
		DeviceTypeBicycleLights, DeviceTypeTirePressureMonitor,
		DeviceTypeWeightScale, DeviceTypeHeartRate, DeviceTypeBikeSpeedCadence,
		DeviceTypeBikeCadence, DeviceTypeBikeSpeed, DeviceTypeStrideSpeed,
		DeviceTypeDropperSeatpost, DeviceTypeCoreTemp,
	} {
		if t.String() == name {
			return t
		}
	}
	return DeviceTypeUnknown
}

// BatteryStatus is the ANT+ battery status code.
type BatteryStatus byte

// Battery status values.
const (
	BatteryStatusUnknown  BatteryStatus = 0
	BatteryStatusNew      BatteryStatus = 1
	BatteryStatusGood     BatteryStatus = 2
	BatteryStatusOk       BatteryStatus = 3
	BatteryStatusLow      BatteryStatus = 4
	BatteryStatusCritical BatteryStatus = 5
	BatteryStatusCharging BatteryStatus = 6
	BatteryStatusInvalid  BatteryStatus = 7
)

func (s BatteryStatus) String() string {
	switch s {
	case BatteryStatusNew:
		return "New"
	case BatteryStatusGood:
		return "Good"
	case BatteryStatusOk:
		return "Ok"
	case BatteryStatusLow:
		return "Low"
	case BatteryStatusCritical:
		return "Critical"
	case BatteryStatusCharging:
		return "Charging"
	case BatteryStatusInvalid:
		return "Invalid"
	}
	return "Unknown"
}

// DeviceData is implemented by every device data page structure.
type DeviceData interface {
	// DataName is the measurement name (the Python dataclass name).
	DataName() string
}

// ToInfluxJSON converts a DeviceData page to the InfluxDB point dict used
// by openant's to_influx_json (numeric fields only, enums as raw values).
func ToInfluxJSON(d DeviceData, tags map[string]string) map[string]any {
	fields := map[string]any{}
	v := reflect.ValueOf(d)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("influx"); tag != "" {
			name = tag
		}
		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fields[name] = fv.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fields[name] = fv.Uint()
		case reflect.Float32, reflect.Float64:
			fields[name] = fv.Float()
		case reflect.Bool:
			fields[name] = fv.Bool()
		}
	}
	return map[string]any{
		"measurement": d.DataName(),
		"tags":        tags,
		"time":        time.Now().UTC().UnixNano(),
		"fields":      fields,
	}
}

// BatteryData describes one battery (a device can have several).
type BatteryData struct {
	BatteryID         int     `influx:"battery_id"`
	VoltageFractional float64 `influx:"voltage_fractional"` // V
	VoltageCoarse     int     `influx:"voltage_coarse"`     // V
	Status            BatteryStatus
	OperatingTime     int `influx:"operating_time"` // seconds
}

// DataName implements DeviceData.
func (BatteryData) DataName() string { return "BatteryData" }

// CommonData holds the ANT+ common pages (80-83).
type CommonData struct {
	ManufacturerID int    `influx:"manufacturer_id"`
	SerialNo       uint32 `influx:"serial_no"`
	SoftwareVer    string
	HardwareRev    int `influx:"hardware_rev"`
	ModelNo        int `influx:"model_no"`
	BatteryNumber  int `influx:"battery_number"`
	LastBatteryID  int `influx:"last_battery_id"`
	LastBattery    BatteryData
	TimeDate       *time.Time
}

// DataName implements DeviceData.
func (CommonData) DataName() string { return "CommonData" }

// ManufacturerPagePayload builds page 80 for master transmission.
func (c *CommonData) ManufacturerPagePayload() []byte {
	p := []byte{0x50, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00}
	p[3] = byte(c.HardwareRev)
	p[4] = byte(c.ManufacturerID & 0xFF)
	p[5] = byte((c.ManufacturerID >> 8) & 0xFF)
	p[6] = byte(c.ModelNo & 0xFF)
	p[7] = byte((c.ModelNo >> 8) & 0xFF)
	return p
}

// ProductInfoPagePayload builds page 81 for master transmission.
func (c *CommonData) ProductInfoPagePayload() []byte {
	p := []byte{0x51, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00}
	ver, err := strconv.ParseFloat(c.SoftwareVer, 64)
	if err == nil {
		p[3] = byte(int(ver) * 10)
	}
	p[4] = byte(c.SerialNo)
	p[5] = byte(c.SerialNo >> 8)
	p[6] = byte(c.SerialNo >> 16)
	p[7] = byte(c.SerialNo >> 24)
	return p
}

// defaultLogger returns the package default logger.
func defaultLogger() *slog.Logger { return slog.Default() }

// baseDevice carries the state and common page handling shared by all
// profiles. Profiles embed it and register their page hooks.
type baseDevice struct {
	node    *easy.Node
	channel *easy.Channel
	log     *slog.Logger

	DeviceID   int
	DeviceType int
	Period     int
	RFFreq     int
	TransType  int
	Name       string
	master     bool

	found     atomic.Bool
	attached  bool
	pageCount int // for interleaving master pages

	foundMu sync.Mutex
	foundCh chan struct{} // closed when found flips to true

	Common    CommonData
	Batteries [15]BatteryData

	// Virtual hooks (replace Python method overriding).
	onProfileData   func(data []byte)        // profile page parsing
	onProfileTX     func() []byte            // master: profile TX page
	onProfileTXFull func(count int) []byte   // master: full page scheduler override
	onProfileAck    func(data []byte) []byte // master: profile ACK reply
	onBatteryLog    func(b BatteryData)      // battery handling override
	onDataFull      func(data []byte)        // full data handler override (Scanner)

	// User callbacks.
	OnDeviceData func(page int, pageName string, data DeviceData)
	OnUpdate     func(data []byte)
	OnFound      func()
	OnBattery    func(data BatteryData)
}

func (d *baseDevice) String() string {
	return fmt.Sprintf("%s_%05d", d.Name, d.DeviceID)
}

// Channel returns the device channel.
func (d *baseDevice) Channel() *easy.Channel { return d.channel }

// Node returns the node the device runs on.
func (d *baseDevice) Node() *easy.Node { return d.node }

// Found reports whether the device has been found at least once.
func (d *baseDevice) Found() bool { return d.found.Load() }

// foundSignal returns the channel that is closed when the device is
// found. Safe for concurrent use.
func (d *baseDevice) foundSignal() <-chan struct{} {
	d.foundMu.Lock()
	defer d.foundMu.Unlock()
	if d.foundCh == nil {
		d.foundCh = make(chan struct{})
	}
	return d.foundCh
}

// WaitFound blocks until the device transmits its first data packet
// (meaning it is present and attached) or the timeout elapses, whichever
// comes first. It is nil-safe with respect to the internal channel and
// addresses openant issue #35 (connect to a specific device): create the
// profile with the known device ID and wait for it to appear.
func (d *baseDevice) WaitFound(timeout time.Duration) error {
	ch := d.foundSignal()
	if d.Found() {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("device %s not found within %s", d.String(), timeout)
	}
}

// openChannel configures and opens the device channel (openant
// AntPlusDevice.open_channel).
func (d *baseDevice) openChannel(extended bool, channelType byte) error {
	if channelType == 0 {
		if !d.master {
			channelType = easy.ChannelBidirectionalReceive
		} else {
			channelType = easy.ChannelBidirectionalTransmit
		}
	}
	extAssign := byte(0x01)
	ch, err := d.node.NewChannel(channelType, 0x00, &extAssign)
	if err != nil {
		return err
	}
	d.channel = ch
	if !d.master {
		ch.OnBroadcastData = d.onData
		ch.OnBurstData = d.onData
		ch.OnAcknowledgedData = d.onData
		if err := ch.SetSearchTimeout(0xFF); err != nil {
			return err
		}
	} else {
		ch.OnBroadcastTxData = d.onTXData
		ch.OnAcknowledgedData = d.onAckData
	}
	if err := ch.SetID(d.DeviceID, byte(d.DeviceType), byte(d.TransType)); err != nil {
		return err
	}
	if extended {
		if err := ch.EnableExtendedMessages(true); err != nil {
			return err
		}
	}
	if err := ch.SetPeriod(d.Period); err != nil {
		return err
	}
	if err := ch.SetRFFrequency(byte(d.RFFreq)); err != nil {
		return err
	}
	d.log.Debug("opening device channel", "device", d.String(), "type", fmt.Sprintf("%02x", channelType))
	return ch.Open()
}

// CloseChannel closes and removes the device channel.
func (d *baseDevice) CloseChannel() {
	d.node.RemoveChannel(d.channel)
}

// RequestDP requests a data page using the request page (0x46).
func (d *baseDevice) RequestDP(page int, noTimes int) {
	data := []byte{0x46, 0xFF, 0xFF, 0xFF, 0xFF, byte(noTimes) & 0x7F, byte(page & 0xFF), 0x01}
	d.SendAcknowledgedData(data)
}

// SendAcknowledgedData sends acknowledged data, logging (not propagating)
// failures like openant's wrapper.
func (d *baseDevice) SendAcknowledgedData(data []byte) {
	if err := d.channel.SendAcknowledgedData(data); err != nil {
		if len(data) > 0 && data[0] == 0x46 {
			d.log.Warn("failed to get acknowledgement of TX request page", "page", fmt.Sprintf("%#x", data[6]), "error", err)
		} else if len(data) > 0 {
			d.log.Warn("failed to get acknowledgement of TX page", "page", fmt.Sprintf("%#x", data[0]), "error", err)
		}
	}
}

// onData is the main RX dispatcher (openant _on_data).
func (d *baseDevice) onData(data []byte) {
	if len(data) == 0 {
		return
	}
	if d.onDataFull != nil {
		d.onDataFull(data)
		return
	}

	// Extended (> 8 bytes) messages carry the device number and id beyond
	// the page: 8 page bytes + flag byte + 2 id + type + trans = 13 bytes
	// minimum (code review PR #1, P0-2).
	if len(data) >= 13 && !d.attached {
		deviceID := int(data[9]) + int(data[10])<<8
		deviceType := int(data[11])
		transType := int(data[12])

		if d.DeviceID == 0 {
			// Attach to the first device found.
			d.DeviceID = deviceID
			d.TransType = transType
			if err := d.channel.Close(); err != nil {
				d.log.Warn("close channel for reattach", "error", err)
			}
			if err := d.channel.SetID(d.DeviceID, byte(d.DeviceType), byte(d.TransType)); err != nil {
				d.log.Warn("set channel id for reattach", "error", err)
			}
			if err := d.channel.Open(); err != nil {
				d.log.Warn("reopen channel for reattach", "error", err)
			}
		} else if d.DeviceID != deviceID {
			// openant raises RuntimeError here; in Go we log and drop the
			// page to avoid killing the run loop.
			d.log.Error("device id mismatch", "got", deviceID, "want", d.DeviceID)
			return
		}

		d.log.Info("device attached", "device", d.String(),
			"type", deviceType, "trans", transType)
		d.attached = true
	}

	if !d.found.Load() {
		d.found.Store(true)
		d.foundSignal() // ensure a waiter channel exists before closing
		if d.OnFound != nil {
			d.OnFound()
		}
		close(d.foundCh)
	}

	// Common pages read bytes up to data[7]; require a full page
	// (code review PR #1, P0-2). Short payloads still reach the profile
	// hooks below, which perform their own length checks.
	if len(data) >= 8 {
		switch data[0] {
		case 80: // manufacturer info
			d.Common.HardwareRev = int(data[3])
			d.Common.ManufacturerID = int(data[4]) + int(data[5])<<8
			d.Common.ModelNo = int(data[6]) + int(data[7])<<8
			d.log.Info("manufacturer info", "device", d.String(),
				"hw_rev", d.Common.HardwareRev, "id", d.Common.ManufacturerID,
				"model", d.Common.ModelNo)
		case 81: // product info
			swRev := data[2]
			swMain := data[3]
			if swRev == 0xFF {
				d.Common.SoftwareVer = fmt.Sprintf("%v", float64(swMain)/10)
			} else {
				d.Common.SoftwareVer = fmt.Sprintf("%v", float64(swMain*100+swRev)/1000)
			}
			d.Common.SerialNo = uint32(data[4]) | uint32(data[5])<<8 | uint32(data[6])<<16 | uint32(data[7])<<24
			d.log.Info("product info", "device", d.String(),
				"software", d.Common.SoftwareVer, "serial", d.Common.SerialNo)
		case 82: // battery status
			d.Common.LastBattery.VoltageFractional = float64(data[6]) / 256
			d.Common.LastBattery.VoltageCoarse = int(data[7] & 0x0F)
			d.Common.LastBattery.Status = BatteryStatus((data[7] & 0x70) >> 4)
			d.Common.LastBattery.BatteryID = int((data[2] & 0xF0) >> 4)

			opTime := int(data[3]) + int(data[4])<<8
			if data[7]&0x80 == 0x80 {
				d.Common.LastBattery.OperatingTime = opTime * 2
			} else {
				d.Common.LastBattery.OperatingTime = opTime * 16
			}

			if data[2] != 0xFF {
				// Multi-battery system: store per battery id.
				d.Common.BatteryNumber = int(data[2] & 0x0F)
				d.Common.LastBatteryID = int((data[2] & 0xF0) >> 4)
				if d.Common.LastBatteryID < len(d.Batteries) {
					d.Batteries[d.Common.LastBatteryID] = d.Common.LastBattery
				}
			} else {
				d.Common.BatteryNumber = 1
				d.Common.LastBatteryID = 0xFF
			}
			d.onBattery(d.Common.LastBattery)
		case 83: // date and time
			second := int(data[2])
			minute := int(data[3])
			hour := int(data[4])
			day := int(data[5] & 0x1F)
			month := int(data[6])
			year := int(data[7]) + 2000
			if td := tryDateTime(year, month, day, hour, minute, second); td != nil {
				d.Common.TimeDate = td
			} else {
				d.log.Warn("invalid date and time", "device", d.DeviceID, "raw", strings.TrimSpace(fmt.Sprintf("% X", data)))
			}
		}
	}

	// Profile pages.
	if d.onProfileData != nil {
		d.onProfileData(data)
	}
	// User update hook.
	if d.OnUpdate != nil {
		d.OnUpdate(data)
	}
}

func (d *baseDevice) onBattery(b BatteryData) {
	if d.onBatteryLog != nil {
		d.onBatteryLog(b)
	} else {
		d.log.Info("battery info", "device", d.String(),
			"id", d.Common.LastBatteryID,
			"fractional_v", d.Common.LastBattery.VoltageFractional,
			"coarse_v", d.Common.LastBattery.VoltageCoarse,
			"status", d.Common.LastBattery.Status.String())
	}
	if d.OnBattery != nil {
		d.OnBattery(b)
	}
}

// onTXData sends pages on the EVENT_TX interval (master mode). A profile
// with onProfileTXFull takes over the whole page schedule (including the
// common pages), e.g. the HRM sends its own background pages instead of
// common 80/81.
func (d *baseDevice) onTXData(_ []byte) {
	var payload []byte
	switch {
	case d.onProfileTXFull != nil:
		payload = d.onProfileTXFull(d.pageCount)
	case d.pageCount == 0:
		payload = d.Common.ManufacturerPagePayload()
	case d.pageCount == 65:
		payload = d.Common.ProductInfoPagePayload()
	default:
		if d.onProfileTX != nil {
			payload = d.onProfileTX()
		}
	}
	if payload != nil {
		d.log.Debug("sending EVENT_TX page", "count", d.pageCount, "payload", fmt.Sprintf("% X", payload))
		if err := d.channel.SendBroadcastData(payload); err != nil {
			d.log.Warn("broadcast send", "error", err)
		}
	}
	if d.pageCount == 129 {
		d.pageCount = 0
	} else {
		d.pageCount++
	}
}

// onAckData replies to common page requests (master mode).
func (d *baseDevice) onAckData(data []byte) {
	if len(data) == 0 {
		return
	}
	var payload []byte
	switch data[0] {
	case 80:
		payload = d.Common.ManufacturerPagePayload()
	case 81:
		payload = d.Common.ProductInfoPagePayload()
	case 82:
		payload = nil // TODO: battery status page payload
	default:
		if d.onProfileAck != nil {
			payload = d.onProfileAck(data)
		}
	}
	if payload != nil {
		if err := d.channel.SendBroadcastData(payload); err != nil {
			d.log.Warn("broadcast send", "error", err)
		}
	}
}

// fireDeviceData invokes the user's OnDeviceData callback.
func (d *baseDevice) fireDeviceData(page int, pageName string, data DeviceData) {
	if d.OnDeviceData != nil {
		d.OnDeviceData(page, pageName, data)
	}
}

// SetOnDeviceData installs the device data callback.
func (d *baseDevice) SetOnDeviceData(fn func(page int, pageName string, data DeviceData)) {
	d.OnDeviceData = fn
}

// SetOnFound installs the found callback.
func (d *baseDevice) SetOnFound(fn func()) { d.OnFound = fn }

// SetOnBattery installs the battery callback.
func (d *baseDevice) SetOnBattery(fn func(data BatteryData)) { d.OnBattery = fn }

// SetOnUpdate installs the raw update callback.
func (d *baseDevice) SetOnUpdate(fn func(data []byte)) { d.OnUpdate = fn }

// tryDateTime validates and builds a UTC timestamp, returning nil when the
// device supplied an invalid date (openant issue #109).
func tryDateTime(year, month, day, hour, minute, second int) *time.Time {
	if month < 1 || month > 12 || day < 1 || day > 31 ||
		hour > 23 || minute > 59 || second > 60 {
		return nil
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	// time.Date normalises; reject if the normalised values differ.
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return nil
	}
	return &t
}
