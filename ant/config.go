package ant

import (
	"fmt"
	"time"
)

// ---- Configuration and control commands (fire and forget; the easy layer
// waits for the responses). ----

// ResetSystem resets the ANT node.
func (c *Core) ResetSystem() { _ = c.Write(NewMessage(IDResetSystem, []byte{0x00})) }

// AssignChannel assigns a channel with the given type and network.
func (c *Core) AssignChannel(ch, channelType, network byte, extAssign *byte) {
	data := []byte{ch, channelType, network}
	if extAssign != nil {
		data = append(data, *extAssign)
	}
	_ = c.Write(NewMessage(IDAssignChannel, data))
}

// UnassignChannel unassigns a channel.
func (c *Core) UnassignChannel(ch byte) {
	_ = c.Write(NewMessage(IDUnassignChannel, []byte{ch}))
}

// OpenChannel opens a previously configured channel.
func (c *Core) OpenChannel(ch byte) {
	_ = c.Write(NewMessage(IDOpenChannel, []byte{ch}))
}

// CloseChannel closes a channel.
func (c *Core) CloseChannel(ch byte) {
	_ = c.Write(NewMessage(IDCloseChannel, []byte{ch}))
}

// RequestMessage requests a specific message from the node.
func (c *Core) RequestMessage(ch byte, msgID MessageID) {
	_ = c.Write(NewMessage(IDRequestMessage, []byte{ch, byte(msgID)}))
}

// OpenRxScanMode enables continuous RX scan mode.
func (c *Core) OpenRxScanMode(ch byte) {
	_ = c.Write(NewMessage(IDOpenRxScanMode, []byte{ch, 0x01}))
}

// SetChannelID configures the channel id (device number, type and
// transmission type; 0 wildcards as a slave).
func (c *Core) SetChannelID(ch byte, deviceNum uint16, deviceType, transmissionType byte) {
	data := []byte{ch, byte(deviceNum), byte(deviceNum >> 8), deviceType, transmissionType}
	_ = c.Write(NewMessage(IDSetChannelID, data))
}

// SetChannelPeriod sets the channel messaging period in 1/32768 s units.
func (c *Core) SetChannelPeriod(ch byte, period uint16) {
	_ = c.Write(NewMessage(IDChannelPeriod, []byte{ch, byte(period), byte(period >> 8)}))
}

// SetChannelSearchTimeout sets the search timeout in 2.5 s units (255 = infinite).
func (c *Core) SetChannelSearchTimeout(ch, timeout byte) {
	_ = c.Write(NewMessage(IDChannelSearchTimeout, []byte{ch, timeout}))
}

// SetChannelRFFrequency sets the RF frequency offset from 2400 MHz.
func (c *Core) SetChannelRFFrequency(ch, freq byte) {
	_ = c.Write(NewMessage(IDChannelRFFrequency, []byte{ch, freq}))
}

// SetNetworkKey sets the key for a network number (key is 8 or 16 bytes).
func (c *Core) SetNetworkKey(network byte, key []byte) error {
	if len(key) != 8 && len(key) != 16 {
		return fmt.Errorf("ant: network key must be 8 or 16 bytes, got %d", len(key))
	}
	data := append([]byte{network}, key...)
	return c.Write(NewMessage(IDSetNetworkKey, data))
}

// SetTransmitPower sets the global transmit power (0..4).
func (c *Core) SetTransmitPower(power byte) {
	_ = c.Write(NewMessage(IDSetTransmitPower, []byte{0x00, power}))
}

// SetSearchWaveform configures the search waveform (default [0x53, 0x00]).
func (c *Core) SetSearchWaveform(ch byte, waveform []byte) {
	data := append([]byte{ch}, waveform...)
	_ = c.Write(NewMessage(IDSetSearchWaveform, data))
}

// SetProximitySearch limits the search radius of a searching slave channel:
// 0 disables the proximity search (normal search), 1..255 restricts it to
// the given number of signal bins (~dB of RX attenuation). Useful to pick
// the closest sensor among several identical ones.
//
// The message id depends on the protocol revision (0x60 modern,
// 0x71 Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetProximitySearch(ch, threshold byte) {
	id := IDSetProximitySearch
	if c.protoLegacy.Load() {
		id = IDSetProximitySearchLegacy
	}
	_ = c.Write(NewMessage(id, []byte{ch, threshold}))
}

// SetChannelIDList switches the channel to list-based search matching: the
// stick then only connects to the device IDs added with AddChannelID
// (up to size entries), instead of matching the single channel id set with
// SetChannelID.
func (c *Core) SetChannelIDList(ch, size byte) {
	_ = c.Write(NewMessage(IDChannelIDList, []byte{ch, size}))
}

// AddChannelID adds one entry to the channel search list (see
// SetChannelIDList). deviceNum 0 is not allowed here; use deviceType 0 as
// a type wildcard.
func (c *Core) AddChannelID(ch byte, deviceNum uint16, deviceType byte) {
	_ = c.Write(NewMessage(IDAddChannelID, []byte{ch, byte(deviceNum), byte(deviceNum >> 8), deviceType}))
}

// SetSearchSharing makes several channels share one search: the shared
// search runs every cyclesPerSearch channel periods on each channel in
// turn, saving bandwidth and battery when many slave channels search
// simultaneously. 0 disables search sharing.
//
// The message id depends on the protocol revision (0x53 modern,
// 0x81 Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetSearchSharing(ch, cyclesPerSearch byte) {
	id := IDChannelSearchSharing
	if c.protoLegacy.Load() {
		id = IDChannelSearchSharingLegacy
	}
	_ = c.Write(NewMessage(id, []byte{ch, cyclesPerSearch}))
}

// LIBConfig flag bits for SetLIBConfig: they select what is appended to
// extended RX data messages (identical in both protocol revisions).
const (
	LIBConfigRxTimestamp byte = 0x20
	LIBConfigRSSI        byte = 0x40
	LIBConfigChannelID   byte = 0x80
)

// SetLIBConfig sets the library configuration: the flag bits
// (LIBConfigRxTimestamp, LIBConfigRSSI, LIBConfigChannelID) select which
// extended data (timestamp, RSSI, channel ID) is appended to received
// data messages.
//
// The message id depends on the protocol revision (0x71 modern,
// 0x6E Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetLIBConfig(ch, config byte) {
	id := IDLIBConfig
	if c.protoLegacy.Load() {
		id = IDLIBConfigLegacy
	}
	_ = c.Write(NewMessage(id, []byte{ch, config}))
}

// SetProtocolLegacy forces the Rev 5.1 message spellings (nRF24AP2 /
// ANTUSB2-era devices: proximity search 0x71, LIB config 0x6E, search
// sharing 0x81, advanced burst config 0x78). Modern firmware uses 0x60 /
// 0x71 / 0x53 / 0x61 respectively. DetectProtocol chooses automatically.
func (c *Core) SetProtocolLegacy(legacy bool) {
	c.protoLegacy.Store(legacy)
}

// ProtocolLegacy reports whether Rev 5.1 message spellings are in use.
func (c *Core) ProtocolLegacy() bool { return c.protoLegacy.Load() }

// DetectProtocol auto-detects the protocol revision of the stick: legacy
// (Rev 5.1) firmware answers a serial number request (0x61) with the
// 4-byte serial number, while modern firmware either answers with the
// 3-byte advanced burst configuration or does not implement that request
// at all (in which case a 0x3F serial request is tried). It returns true
// when the stick uses the Rev 5.1 spellings. Call it once right after
// NewCore, before any channels are configured; easy.New does this
// automatically.
func (c *Core) DetectProtocol(timeout time.Duration) bool {
	ch := make(chan []byte, 4)
	c.detectMu.Lock()
	c.detectCh = ch
	c.detectMu.Unlock()
	defer func() {
		c.detectMu.Lock()
		c.detectCh = nil
		c.detectMu.Unlock()
	}()

	_ = c.Write(NewMessage(IDRequestMessage, []byte{0x00, byte(IDSerialNumber)}))
	select {
	case data := <-ch:
		// 4 bytes = serial number (Rev 5.1); 3 bytes = the modern
		// firmware reporting its advanced burst configuration.
		c.protoLegacy.Store(len(data) == 4)
		return c.protoLegacy.Load()
	case <-time.After(timeout):
		// No answer at 0x61: try the modern serial number request.
		_ = c.Write(NewMessage(IDRequestMessage, []byte{0x00, byte(IDSerialNumberNew)}))
		select {
		case <-ch:
			c.protoLegacy.Store(false)
			return false
		case <-time.After(timeout):
			// Undetectable; keep the modern default.
			return c.protoLegacy.Load()
		}
	}
}

// SetAdvancedBurst enables or disables advanced burst transfers on the
// node with maxPacketSize payload bytes per packet. maxPacketSize is
// rounded to what the device supports: modern firmware accepts any size
// up to 24 bytes, Rev 5.1 devices support 8, 16 or 24 bytes only (0 = the
// 24 byte maximum in both cases). When enabled, data can be sent with
// SendAdvancedBurst and received transfers are reassembled into regular
// burst events.
//
// The configuration message id depends on the protocol revision (0x61
// modern, 0x78 Rev 5.1); see SetProtocolLegacy / DetectProtocol.
func (c *Core) SetAdvancedBurst(enabled bool, maxPacketSize uint16) error {
	if maxPacketSize > 24 {
		return fmt.Errorf("ant: advanced burst packet size %d exceeds the 24 byte maximum", maxPacketSize)
	}
	e := byte(0)
	if enabled {
		e = 1
	}
	var err error
	if c.protoLegacy.Load() {
		// Rev 5.1: [filler][enable][max packet enum][required features
		// (3 bytes)][optional features (3 bytes)].
		if maxPacketSize == 0 {
			maxPacketSize = 24
		}
		enum := byte(1)
		switch {
		case maxPacketSize > 16:
			enum = 3
		case maxPacketSize > 8:
			enum = 2
		}
		err = c.Write(NewMessage(IDConfigAdvancedBurstLegacy, []byte{
			0x00, e, enum, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}))
	} else {
		if maxPacketSize == 0 {
			maxPacketSize = 24
		}
		err = c.Write(NewMessage(IDConfigAdvancedBurst, []byte{0x00, e, byte(maxPacketSize), byte(maxPacketSize >> 8)}))
	}
	if err != nil {
		return err
	}
	if !enabled {
		c.advBurstMax.Store(0)
		return nil
	}
	c.advBurstMax.Store(uint32(maxPacketSize))
	return nil
}

// EnableExtendedMessages enables/disables extended (16 byte) receive messages.
func (c *Core) EnableExtendedMessages(ch byte, enable bool) {
	e := byte(0)
	if enable {
		e = 1
	}
	_ = c.Write(NewMessage(IDEnableExtendedMessages, []byte{ch, e}))
}

// EnableLED enables/disables the stick LED.
func (c *Core) EnableLED(enable bool) {
	e := byte(0)
	if enable {
		e = 1
	}
	_ = c.Write(NewMessage(IDEnableLED, []byte{0x00, e}))
}
