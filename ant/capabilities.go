package ant

import "encoding/binary"

// StandardOptions, AdvancedOptions, AdvancedOptionsTwo and AdvancedOptionsThree
// are the capability bitsets returned in the CAPABILITIES response (0x54).
// They mirror the enums in openant.base.driver.

// StandardOptions is a set of standard capability flags.
type StandardOptions map[string]bool

// ParseStandardOptions decodes the standard options byte.
func ParseStandardOptions(b byte) StandardOptions {
	return StandardOptions{
		"NoRxChannels":    b&0x01 != 0,
		"NoTxChannels":    b&0x02 != 0,
		"NoRxMessages":    b&0x04 != 0,
		"NoTxMessages":    b&0x08 != 0,
		"NoAckMessages":   b&0x10 != 0,
		"NoBurstMessages": b&0x20 != 0,
	}
}

// AdvancedOptions is a set of advanced capability flags.
type AdvancedOptions map[string]bool

// ParseAdvancedOptions decodes the first advanced options byte.
func ParseAdvancedOptions(b byte) AdvancedOptions {
	return AdvancedOptions{
		"NetworkEnabled":            b&0x01 != 0,
		"SerialNumberEnabled":       b&0x08 != 0,
		"PerChannelTxPowerEnabled":  b&0x10 != 0,
		"LowPrioritySearchNEnabled": b&0x20 != 0,
		"ScriptEnabled":             b&0x40 != 0,
		"SearchListEnabled":         b&0x80 != 0,
	}
}

// AdvancedOptionsTwo is a set of second-generation capability flags.
type AdvancedOptionsTwo map[string]bool

// ParseAdvancedOptionsTwo decodes the second advanced options byte.
func ParseAdvancedOptionsTwo(b byte) AdvancedOptionsTwo {
	return AdvancedOptionsTwo{
		"LedEnabled":             b&0x01 != 0,
		"ExtMessageEnabled":      b&0x02 != 0,
		"ScanModeEnabled":        b&0x04 != 0,
		"ProximitySearchEnabled": b&0x10 != 0,
		"ExtAssignEnabled":       b&0x20 != 0,
		"FsAntFsEnabled":         b&0x40 != 0,
		"Fit1Enabled":            b&0x80 != 0,
	}
}

// AdvancedOptionsThree is a set of third-generation capability flags.
type AdvancedOptionsThree map[string]bool

// ParseAdvancedOptionsThree decodes the third advanced options byte. Unlike
// openant (which incorrectly reads byte 4 again), this reads the correct
// byte 6 of the capabilities response.
func ParseAdvancedOptionsThree(b byte) AdvancedOptionsThree {
	return AdvancedOptionsThree{
		"AdvancedBurstEnabled":       b&0x01 != 0,
		"EventBufferingEnabled":      b&0x01 != 0,
		"EventFilteringEnabled":      b&0x02 != 0,
		"HighDutySearchEnabled":      b&0x04 != 0,
		"SearchSharingEnabled":       b&0x10 != 0,
		"SelectiveDataUpdateEnabled": b&0x40 != 0,
		"EncryptedChannelEnabled":    b&0x80 != 0,
	}
}

// Capabilities is the decoded form of the CAPABILITIES response (0x54).
type Capabilities struct {
	MaxChannels           int
	MaxNetworks           int
	StandardOptions       StandardOptions
	AdvancedOptions       AdvancedOptions
	AdvancedOptionsTwo    AdvancedOptionsTwo
	MaxSensorcoreChannels int
	AdvancedOptionsThree  AdvancedOptionsThree
}

// ParseCapabilities decodes the payload of a CAPABILITIES response. At least
// 6 bytes are expected; the 7th byte (advanced options three) is optional.
func ParseCapabilities(data []byte) (*Capabilities, error) {
	if len(data) < 6 {
		return nil, errShortPayload("CAPABILITIES", 6, len(data))
	}
	c := &Capabilities{
		MaxChannels:           int(data[0]),
		MaxNetworks:           int(data[1]),
		StandardOptions:       ParseStandardOptions(data[2]),
		AdvancedOptions:       ParseAdvancedOptions(data[3]),
		AdvancedOptionsTwo:    ParseAdvancedOptionsTwo(data[4]),
		MaxSensorcoreChannels: int(data[5]),
	}
	if len(data) >= 7 {
		c.AdvancedOptionsThree = ParseAdvancedOptionsThree(data[6])
	}
	return c, nil
}

// AntVersion returns the ASCII version string from an ANT_VERSION response
// payload (NUL terminated).
func AntVersion(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

// SerialNumber returns the little-endian uint32 serial number from a
// SERIAL_NUMBER response payload.
func SerialNumber(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(data[:4])
}
