// Package fs implements the ANT-FS protocol (beacons, commands, command
// pipe, directory handling and a session state machine). It is a Go port
// of the openant.fs Python module.
package fs

import "errors"

// ANT-FS protocol constants.
const (
	// BeaconMark is the first byte of every ANT-FS beacon.
	BeaconMark byte = 0x43
	// CommandMark is the first byte of every ANT-FS command.
	CommandMark byte = 0x44
)

// ClientDeviceState from the beacon status byte 2.
const (
	StateLink           byte = 0x00
	StateAuthentication byte = 0x01
	StateTransport      byte = 0x02
	StateBusy           byte = 0x03
)

// Beacon is the 8 byte ANT-FS beacon broadcast by the device.
type Beacon struct {
	Status1    byte
	Status2    byte
	AuthType   byte
	Descriptor [4]byte
}

// Errors reported while parsing ANT-FS structures.
var (
	ErrBadBeacon  = errors.New("fs: invalid beacon")
	ErrBadCommand = errors.New("fs: invalid command")
)

// ParseBeacon decodes an 8 byte beacon payload.
func ParseBeacon(data []byte) (Beacon, error) {
	var b Beacon
	if len(data) < 8 {
		return b, ErrBadBeacon
	}
	if data[0] != BeaconMark {
		return b, ErrBadBeacon
	}
	b.Status1 = data[1]
	b.Status2 = data[2]
	b.AuthType = data[3]
	copy(b.Descriptor[:], data[4:8])
	return b, nil
}

// ClientDeviceState returns the device session state (link/auth/transport/busy).
func (b Beacon) ClientDeviceState() byte { return b.Status2 & 0x0F }

// DataAvailable reports whether the device has new data for the host.
func (b Beacon) DataAvailable() bool { return b.Status1&0x20 != 0 }

// UploadEnabled reports whether uploads are permitted.
func (b Beacon) UploadEnabled() bool { return b.Status1&0x10 != 0 }

// PairingEnabled reports whether pairing is enabled.
func (b Beacon) PairingEnabled() bool { return b.Status1&0x08 != 0 }

// ChannelPeriod returns the transport channel period exponent.
func (b Beacon) ChannelPeriod() int { return int(b.Status1 & 0x07) }

// Serial returns the beacon descriptor as the device serial number.
func (b Beacon) Serial() uint32 {
	return uint32(b.Descriptor[0]) | uint32(b.Descriptor[1])<<8 |
		uint32(b.Descriptor[2])<<16 | uint32(b.Descriptor[3])<<24
}

// DescriptorPair returns the beacon descriptor as two 16 bit values.
func (b Beacon) DescriptorPair() (uint16, uint16) {
	lo := uint16(b.Descriptor[0]) | uint16(b.Descriptor[1])<<8
	hi := uint16(b.Descriptor[2]) | uint16(b.Descriptor[3])<<8
	return lo, hi
}
