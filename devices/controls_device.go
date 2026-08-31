package devices

import (
	"github.com/maxdukov/openant-go/easy"
)

// ControlCapability is a controls device capability bit.
type ControlCapability int

// Control capabilities.
const (
	CapAutoControl    ControlCapability = 0
	CapKeypadControl  ControlCapability = 3
	CapGenericControl ControlCapability = 4
	CapVideoControl   ControlCapability = 5
	CapBurstCommand   ControlCapability = 6
)

// ControlCapabilitiesFromByte decodes the capability byte.
func ControlCapabilitiesFromByte(b byte) map[ControlCapability]bool {
	out := map[ControlCapability]bool{}
	for i := 0; i < 7; i++ {
		if b>>i&0x01 != 0 {
			out[ControlCapability(i)] = true
		}
	}
	return out
}

// ControlCapabilitiesToByte encodes capabilities.
func ControlCapabilitiesToByte(caps map[ControlCapability]bool) byte {
	var b byte
	for c, on := range caps {
		if on {
			b |= 1 << c
		}
	}
	return b
}

// ControlCommand is a generic controls command value.
type ControlCommand int

// Control commands. Reserved covers 5-31 and 37-32767, Custom 32768-65534.
const (
	CmdMenuUp     ControlCommand = 0
	CmdMenuDown   ControlCommand = 1
	CmdMenuSelect ControlCommand = 2
	CmdMenuBack   ControlCommand = 3
	CmdHome       ControlCommand = 4
	CmdStart      ControlCommand = 32
	CmdStop       ControlCommand = 33
	CmdReset      ControlCommand = 34
	CmdLength     ControlCommand = 35
	CmdLap        ControlCommand = 36
	CmdReserved   ControlCommand = 0x7FFF
	CmdCustom     ControlCommand = 0xFFFE
	CmdNoCommand  ControlCommand = 0xFFFF
)

// ControlCommandFromInt maps raw values into the command categories.
func ControlCommandFromInt(i int) ControlCommand {
	switch i {
	case int(CmdMenuUp), int(CmdMenuDown), int(CmdMenuSelect), int(CmdMenuBack),
		int(CmdHome), int(CmdStart), int(CmdStop), int(CmdReset), int(CmdLength), int(CmdLap):
		return ControlCommand(i)
	}
	if (i >= 5 && i <= 31) || (i >= 37 && i <= 32767) {
		return CmdReserved
	}
	if i >= 32768 && i <= 65534 {
		return CmdCustom
	}
	return CmdNoCommand
}

func (c ControlCommand) String() string {
	switch c {
	case CmdMenuUp:
		return "MenuUp"
	case CmdMenuDown:
		return "MenuDown"
	case CmdMenuSelect:
		return "MenuSelect"
	case CmdMenuBack:
		return "MenuBack"
	case CmdHome:
		return "Home"
	case CmdStart:
		return "Start"
	case CmdStop:
		return "Stop"
	case CmdReset:
		return "Reset"
	case CmdLength:
		return "Length"
	case CmdLap:
		return "Lap"
	case CmdReserved:
		return "Reserved"
	case CmdCustom:
		return "Custom"
	}
	return "NoCommand"
}

// ControlsCommandStatus is the controls command status code.
type ControlsCommandStatus byte

// Controls command status values.
const (
	ControlsStatusPass          ControlsCommandStatus = 0
	ControlsStatusFail          ControlsCommandStatus = 3
	ControlsStatusNotSupported  ControlsCommandStatus = 4
	ControlsStatusRejected      ControlsCommandStatus = 5
	ControlsStatusPending       ControlsCommandStatus = 6
	ControlsStatusReserved      ControlsCommandStatus = 254
	ControlsStatusUninitialized ControlsCommandStatus = 255
)

// ControlsDeviceData is the ANT+ controls device data.
type ControlsDeviceData struct {
	SlaveSerial             int                   `influx:"slave_serial"`
	SlaveManufacturerID     int                   `influx:"slave_manufacturer_id"`
	Capabilities            byte                  `influx:"capabilities"`
	CurrentNotifications    int                   `influx:"current_notifications"`
	CommandSequence         int                   `influx:"command_sequence"`
	CommandStatus           ControlsCommandStatus `influx:"command_status"`
	LastReceivedCommandPage int                   `influx:"last_received_command_page"`
	LastControlCommand      ControlCommand        `influx:"last_control_command"`
	ResponseData            [4]byte
}

// DataName implements DeviceData.
func (ControlsDeviceData) DataName() string { return "ControlsDeviceData" }

// ControlsDevice is the ANT+ controls device profile (device type 16),
// usable as slave (remote) or master (controllable device).
type ControlsDevice struct {
	baseDevice
	Data ControlsDeviceData

	// OnControlCommand is called (master mode) when a generic command
	// arrives.
	OnControlCommand func(command ControlCommand, raw int)
}

// NewControlsDevice creates the profile; master selects the controllable
// device mode.
func NewControlsDevice(node *easy.Node, deviceID int, transType int, master bool) (*ControlsDevice, error) {
	c := &ControlsDevice{}
	c.node = node
	c.log = defaultLogger()
	c.DeviceType = int(DeviceTypeControlsDevice)
	c.DeviceID = deviceID
	c.Period = 8192
	c.RFFreq = 57
	c.Name = "controls_device"
	c.TransType = transType
	c.master = master
	c.Data = ControlsDeviceData{
		SlaveSerial:             0xFFFF,
		SlaveManufacturerID:     0xFFFF,
		CommandStatus:           ControlsStatusUninitialized,
		LastReceivedCommandPage: 0xFF,
		LastControlCommand:      CmdNoCommand,
		ResponseData:            [4]byte{0xFF, 0xFF, 0xFF, 0xFF},
	}
	c.onProfileTX = c.onTXData
	c.onProfileAck = c.onAckData
	if err := c.openChannel(true, 0); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *ControlsDevice) onTXData() []byte {
	// Control Device Availability page 0x02; common pages come from the base.
	return []byte{
		0x02,
		byte(c.Data.CurrentNotifications),
		0x00, 0x00, 0x00, 0x00, 0x00,
		ControlCapabilitiesToByte(ControlCapabilitiesFromByte(c.Data.Capabilities)),
	}
}

func (c *ControlsDevice) onAckData(data []byte) []byte {
	if len(data) < 8 {
		return nil
	}
	switch data[0] {
	case 0x49: // generic command
		c.Data.CommandSequence = int(data[5])
		raw := int(data[6]) + int(data[7])<<8
		command := ControlCommandFromInt(raw)
		c.log.Debug("received control command", "command", command.String())
		if c.OnControlCommand != nil {
			c.OnControlCommand(command, raw)
		}
		// Track status for the command status page.
		if c.Data.CommandStatus == ControlsStatusUninitialized {
			c.Data.CommandStatus = ControlsStatusPass
		}
		c.Data.LastReceivedCommandPage = int(data[0])
		c.Data.LastControlCommand = command
		c.Data.ResponseData = [4]byte{byte(raw), byte(raw >> 8), 0xFF, 0xFF}
	case 0x10: // audio/video
		c.log.Debug("received audio/video request without action")
	case 0x11: // character
		c.log.Debug("received character request without action")
	case 0x47: // command status
		payload := make([]byte, 8)
		payload[0] = 0x47
		payload[1] = byte(c.Data.LastReceivedCommandPage)
		payload[2] = byte(c.Data.CommandSequence)
		payload[3] = byte(c.Data.CommandStatus)
		copy(payload[4:8], c.Data.ResponseData[:])
		return payload
	}
	return nil
}

// SendControlCommandRaw sends a generic command page 0x49 with a raw value.
func (c *ControlsDevice) SendControlCommandRaw(command int) {
	page := make([]byte, 8)
	page[0] = 0x49
	c.Data.CommandSequence++
	page[1] = byte(c.Data.SlaveSerial)
	page[2] = byte(c.Data.SlaveSerial >> 8)
	page[3] = byte(c.Data.SlaveManufacturerID)
	page[4] = byte(c.Data.SlaveManufacturerID >> 8)
	page[5] = byte(c.Data.CommandSequence)
	page[6] = byte(command)
	page[7] = byte(command >> 8)
	c.SendAcknowledgedData(page)
}

// SendControlCommand sends a generic control command.
func (c *ControlsDevice) SendControlCommand(command ControlCommand) {
	c.SendControlCommandRaw(int(command))
}

// GenericRemoteControl is a slave controls device with generic control.
type GenericRemoteControl struct {
	ControlsDevice
}

// NewGenericRemoteControl creates the remote control preset.
func NewGenericRemoteControl(node *easy.Node, deviceID int, transType int) (*GenericRemoteControl, error) {
	g := &GenericRemoteControl{}
	g.node = node
	g.log = defaultLogger()
	g.DeviceType = int(DeviceTypeControlsDevice)
	g.DeviceID = deviceID
	g.Period = 8192
	g.RFFreq = 57
	g.Name = "generic_remote"
	g.TransType = transType
	g.Data = ControlsDeviceData{
		SlaveSerial:         0xFFFF,
		SlaveManufacturerID: 0xFFFF,
		CommandStatus:       ControlsStatusUninitialized,
		LastControlCommand:  CmdNoCommand,
		ResponseData:        [4]byte{0xFF, 0xFF, 0xFF, 0xFF},
	}
	g.onProfileTX = g.onTXData
	g.onProfileAck = g.onAckData
	if err := g.openChannel(true, 0); err != nil {
		return nil, err
	}
	return g, nil
}

// GenericControllableDevice is a master controls device with generic control.
type GenericControllableDevice struct {
	ControlsDevice
}

// NewGenericControllableDevice creates the controllable device preset.
func NewGenericControllableDevice(node *easy.Node, deviceID int, transType int) (*GenericControllableDevice, error) {
	g := &GenericControllableDevice{}
	g.node = node
	g.log = defaultLogger()
	g.DeviceType = int(DeviceTypeControlsDevice)
	g.DeviceID = deviceID
	g.Period = 8192
	g.RFFreq = 57
	g.Name = "generic_controllable_device"
	g.TransType = transType
	g.master = true
	g.Data = ControlsDeviceData{
		SlaveSerial:         0xFFFF,
		SlaveManufacturerID: 0xFFFF,
		CommandStatus:       ControlsStatusUninitialized,
		LastControlCommand:  CmdNoCommand,
		ResponseData:        [4]byte{0xFF, 0xFF, 0xFF, 0xFF},
		Capabilities:        ControlCapabilitiesToByte(map[ControlCapability]bool{CapGenericControl: true}),
	}
	g.onProfileTX = g.onTXData
	g.onProfileAck = g.onAckData
	if err := g.openChannel(true, 0); err != nil {
		return nil, err
	}
	return g, nil
}
