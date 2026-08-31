package devices

import (
	"fmt"

	"github.com/maxdukov/openant-go/easy"
)

// ValveState is the dropper seatpost valve state.
type ValveState byte

// Valve states.
const (
	ValveLocked       ValveState = 0
	ValveUnlocked     ValveState = 1
	ValveUnknownState ValveState = 2
)

// DelayIndicator tells whether the unlock delay is configurable.
type DelayIndicator byte

// Delay indicator values.
const (
	DelayConfigurable    DelayIndicator = 0
	DelayNotConfigurable DelayIndicator = 1
	DelayUnknown         DelayIndicator = 2
)

// DropperSeatpostData is the ANT+ dropper seatpost data.
type DropperSeatpostData struct {
	ConfiguredUnlockDelay float64        `influx:"configured_unlock_delay"` // s
	DelayIndicator        DelayIndicator `influx:"delay_indicator"`
	ValveState            ValveState     `influx:"valve_state"`
	LockSetting           ValveState     `influx:"lock_setting"`
	SlaveSerial           int            `influx:"slave_serial"`
	CommandSequence       int            `influx:"command_sequence"`
}

// DataName implements DeviceData.
func (DropperSeatpostData) DataName() string { return "DropperSeatpostData" }

// DropperSeatpost is the ANT+ dropper seatpost profile (device type 115).
type DropperSeatpost struct {
	baseDevice
	Data DropperSeatpostData

	eventCount [2]int
}

// NewDropperSeatpost creates the profile.
func NewDropperSeatpost(node *easy.Node, deviceID int, transType int) (*DropperSeatpost, error) {
	d := &DropperSeatpost{}
	d.node = node
	d.log = defaultLogger()
	d.DeviceType = int(DeviceTypeDropperSeatpost)
	d.DeviceID = deviceID
	d.Period = 8192
	d.RFFreq = 57
	d.Name = "dropper_seatpost"
	d.TransType = transType
	d.Data = DropperSeatpostData{
		ConfiguredUnlockDelay: 0x7F,
		DelayIndicator:        DelayUnknown,
		ValveState:            ValveUnknownState,
		LockSetting:           ValveUnknownState,
		SlaveSerial:           0xFFFF,
	}
	d.onProfileData = d.onData
	d.onBatteryLog = d.onBatteryData
	if err := d.openChannel(true, 0); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DropperSeatpost) onBatteryData(b BatteryData) {
	if b.BatteryID != 0xFF {
		d.log.Info("dropper seatpost battery update", "system", ShiftingSystemID(b.BatteryID).String(), "data", fmt.Sprintf("%+v", b))
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

func (d *DropperSeatpost) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	switch data[0] {
	case 0x01: // main page
		d.eventCount[0] = d.eventCount[1]
		d.eventCount[1] = int(data[4]) + int(data[5])<<8

		delay := int(data[6] & 0x7F)
		if delay == 0x7F {
			d.Data.ConfiguredUnlockDelay = 0x7F
		} else {
			d.Data.ConfiguredUnlockDelay = float64(delay) * 0.01
		}
		d.Data.DelayIndicator = DelayIndicator((data[6] & 0x80) >> 7)
		d.Data.ValveState = ValveState((data[7] & 0x80) >> 7)

		if delta := (d.eventCount[1] + 256 - d.eventCount[0]) % 256; delta != 0 {
			d.log.Info("seat post state change", "device", d.String())
			d.fireDeviceData(int(data[0]), "dropper_seatpost_status", d.Data)
		}
	case 0x20: // settings page
		d.Data.SlaveSerial = int(data[1]) + int(data[2])<<8
		d.Data.CommandSequence = int(data[3])
		d.Data.LockSetting = ValveState(data[4] & 0x01)
	}
}

// SetData sends the settings page 0x20.
func (d *DropperSeatpost) SetData(storeUnlockDelay bool) {
	page := make([]byte, 8)
	page[0] = 0x20
	d.Data.CommandSequence++
	page[3] = byte(d.Data.CommandSequence)
	page[4] = byte(d.Data.LockSetting) & 0x01
	delay := int(d.Data.ConfiguredUnlockDelay)
	if delay != 0x7F {
		page[7] = byte(int(d.Data.ConfiguredUnlockDelay*100)) & 0x7F
	} else {
		page[7] = 0x7F
	}
	if storeUnlockDelay {
		page[7] |= 1 << 7
	}
	d.SendAcknowledgedData(page)
}

// SetValve commands the valve into the given state.
func (d *DropperSeatpost) SetValve(state ValveState) {
	d.Data.LockSetting = state
	d.SetData(false)
}

func init() {
	registerProfile(DeviceTypeDropperSeatpost, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewDropperSeatpost(node, deviceID, transType)
	})
}
