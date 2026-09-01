// Package ant implements the low-level ANT protocol used by Dynastream ANT
// USB sticks. It is a Go port of the openant.base Python module.
//
// The package handles the ANT serial message framing (sync byte, length,
// checksum), the USB/serial drivers, and a Core engine which reads frames,
// classifies them into responses/events and schedules timeslot transmission
// of acknowledged and burst data.
package ant

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// SyncByte is the synchronisation byte at the start of every ANT frame.
const SyncByte byte = 0xA4

// MessageID identifies an ANT message type. See the ANT Message Protocol
// and Usage document for the authoritative list.
type MessageID byte

// ANT message identifiers.
const (
	// Responses and notifications sent by the ANT node.
	IDChannelEvent      MessageID = 0x40 // response to a configuration command or channel event
	IDStartupMessage    MessageID = 0x6F
	IDSerialError       MessageID = 0xAE
	IDAntVersion        MessageID = 0x3E
	IDCapabilities      MessageID = 0x54
	IDSerialNumber      MessageID = 0x61
	IDChannelStatus     MessageID = 0x52
	IDChannelIDResponse MessageID = 0x51 // same value as IDSetChannelID

	// Configuration commands.
	IDUnassignChannel          MessageID = 0x41
	IDAssignChannel            MessageID = 0x42
	IDChannelPeriod            MessageID = 0x43
	IDChannelSearchTimeout     MessageID = 0x44
	IDChannelRFFrequency       MessageID = 0x45
	IDSetNetworkKey            MessageID = 0x46
	IDSetTransmitPower         MessageID = 0x47
	IDSetSearchWaveform        MessageID = 0x49
	IDResetSystem              MessageID = 0x4A
	IDOpenChannel              MessageID = 0x4B
	IDCloseChannel             MessageID = 0x4C
	IDRequestMessage           MessageID = 0x4D
	IDSetChannelID             MessageID = 0x51
	IDOpenRxScanMode           MessageID = 0x5B
	IDEnableExtendedMessages   MessageID = 0x66
	IDEnableLED                MessageID = 0x68
	IDLowPrioritySearchTimeout MessageID = 0x63

	// Data messages.
	IDBroadcastData     MessageID = 0x4E
	IDAcknowledgedData  MessageID = 0x4F
	IDBurstTransferData MessageID = 0x50

	// Legacy extended data messages.
	IDLegacyExtendedBroadcast    MessageID = 0x5D
	IDLegacyExtendedAcknowledged MessageID = 0x5E
	IDLegacyExtendedBurst        MessageID = 0x5F
)

// String returns a human readable name for known message IDs.
func (id MessageID) String() string {
	switch id {
	case IDChannelEvent:
		return "CHANNEL_EVENT"
	case IDStartupMessage:
		return "STARTUP_MESSAGE"
	case IDSerialError:
		return "SERIAL_ERROR"
	case IDAntVersion:
		return "ANT_VERSION"
	case IDCapabilities:
		return "CAPABILITIES"
	case IDSerialNumber:
		return "SERIAL_NUMBER"
	case IDChannelStatus:
		return "CHANNEL_STATUS"
	case IDUnassignChannel:
		return "UNASSIGN_CHANNEL"
	case IDAssignChannel:
		return "ASSIGN_CHANNEL"
	case IDChannelPeriod:
		return "CHANNEL_PERIOD"
	case IDChannelSearchTimeout:
		return "CHANNEL_SEARCH_TIMEOUT"
	case IDChannelRFFrequency:
		return "CHANNEL_RF_FREQUENCY"
	case IDSetNetworkKey:
		return "SET_NETWORK_KEY"
	case IDSetTransmitPower:
		return "SET_TRANSMIT_POWER"
	case IDSetSearchWaveform:
		return "SET_SEARCH_WAVEFORM"
	case IDResetSystem:
		return "RESET_SYSTEM"
	case IDOpenChannel:
		return "OPEN_CHANNEL"
	case IDCloseChannel:
		return "CLOSE_CHANNEL"
	case IDRequestMessage:
		return "REQUEST_MESSAGE"
	case IDSetChannelID:
		return "SET_CHANNEL_ID"
	case IDOpenRxScanMode:
		return "OPEN_RX_SCAN_MODE"
	case IDEnableExtendedMessages:
		return "ENABLE_EXT_RX_MESGS"
	case IDEnableLED:
		return "ENABLE_LED"
	case IDLowPrioritySearchTimeout:
		return "LOW_PRIORITY_CHANNEL_SEARCH_TIMEOUT"
	case IDBroadcastData:
		return "BROADCAST_DATA"
	case IDAcknowledgedData:
		return "ACKNOWLEDGED_DATA"
	case IDBurstTransferData:
		return "BURST_TRANSFER_DATA"
	case IDLegacyExtendedBroadcast:
		return "LEGACY_EXTENDED_BROADCAST_DATA"
	case IDLegacyExtendedAcknowledged:
		return "LEGACY_EXTENDED_ACKNOWLEDGED_DATA"
	case IDLegacyExtendedBurst:
		return "LEGACY_EXTENDED_BURST_DATA"
	}
	return fmt.Sprintf("UNKNOWN_0x%02X", byte(id))
}

// Code is a channel event code or a response error code.
type Code int

// Response error codes (returned in CHANNEL_EVENT responses to commands)
// and asynchronous channel event codes.
const (
	ResponseNoError            Code = 0
	EventRxSearchTimeout       Code = 1
	EventRxFail                Code = 2
	EventTx                    Code = 3
	EventTransferRxFailed      Code = 4
	EventTransferTxCompleted   Code = 5
	EventTransferTxFailed      Code = 6
	EventChannelClosed         Code = 7
	EventRxFailGoToSearch      Code = 8
	EventChannelCollision      Code = 9
	EventTransferTxStart       Code = 10
	EventTransferNextDataBlock Code = 17

	ChannelInWrongState         Code = 21
	ChannelNotOpened            Code = 22
	ChannelIDNotSet             Code = 24
	CloseAllChannels            Code = 25
	TransferInProgress          Code = 31
	TransferSequenceNumberError Code = 32
	TransferInError             Code = 33
	MessageSizeExceedsLimit     Code = 39
	InvalidMessage              Code = 40
	InvalidNetworkNumber        Code = 41
	InvalidListID               Code = 48
	InvalidScanTxChannel        Code = 49
	InvalidParameterProvided    Code = 51
	EventSerialQueOverflow      Code = 52
	EventQueOverflow            Code = 53
	EncryptNegotiationFail      Code = 57
	NVMFullError                Code = 64
	NVMWriteError               Code = 65
	USBStringWriteFail          Code = 112
	MesgSerialErrorID           Code = 174
)

// Virtual event codes synthesised by the library for received data, matching
// openant so that filters can select data events uniformly.
const (
	EventRxBroadcast        Code = 1000
	EventRxFlagBroadcast    Code = 1001
	EventRxAcknowledged     Code = 2000
	EventRxFlagAcknowledged Code = 2001
	EventRxBurstPacket      Code = 3000
	EventRxFlagBurstPacket  Code = 3001
)

// String returns the canonical name of a code, or false when unknown.
func (c Code) String() string {
	names := map[Code]string{
		ResponseNoError:             "RESPONSE_NO_ERROR",
		EventRxSearchTimeout:        "EVENT_RX_SEARCH_TIMEOUT",
		EventRxFail:                 "EVENT_RX_FAIL",
		EventTx:                     "EVENT_TX",
		EventTransferRxFailed:       "EVENT_TRANSFER_RX_FAILED",
		EventTransferTxCompleted:    "EVENT_TRANSFER_TX_COMPLETED",
		EventTransferTxFailed:       "EVENT_TRANSFER_TX_FAILED",
		EventChannelClosed:          "EVENT_CHANNEL_CLOSED",
		EventRxFailGoToSearch:       "EVENT_RX_FAIL_GO_TO_SEARCH",
		EventChannelCollision:       "EVENT_CHANNEL_COLLISION",
		EventTransferTxStart:        "EVENT_TRANSFER_TX_START",
		EventTransferNextDataBlock:  "EVENT_TRANSFER_NEXT_DATA_BLOCK",
		ChannelInWrongState:         "CHANNEL_IN_WRONG_STATE",
		ChannelNotOpened:            "CHANNEL_NOT_OPENED",
		ChannelIDNotSet:             "CHANNEL_ID_NOT_SET",
		CloseAllChannels:            "CLOSE_ALL_CHANNELS",
		TransferInProgress:          "TRANSFER_IN_PROGRESS",
		TransferSequenceNumberError: "TRANSFER_SEQUENCE_NUMBER_ERROR",
		TransferInError:             "TRANSFER_IN_ERROR",
		MessageSizeExceedsLimit:     "MESSAGE_SIZE_EXCEEDS_LIMIT",
		InvalidMessage:              "INVALID_MESSAGE",
		InvalidNetworkNumber:        "INVALID_NETWORK_NUMBER",
		InvalidListID:               "INVALID_LIST_ID",
		InvalidScanTxChannel:        "INVALID_SCAN_TX_CHANNEL",
		InvalidParameterProvided:    "INVALID_PARAMETER_PROVIDED",
		EventSerialQueOverflow:      "EVENT_SERIAL_QUE_OVERFLOW",
		EventQueOverflow:            "EVENT_QUE_OVERFLOW",
		EncryptNegotiationFail:      "ENCRYPT_NEGOTIATION_FAIL",
		NVMFullError:                "NVM_FULL_ERROR",
		NVMWriteError:               "NVM_WRITE_ERROR",
		USBStringWriteFail:          "USB_STRING_WRITE_FAIL",
		MesgSerialErrorID:           "MESG_SERIAL_ERROR_ID",
		EventRxBroadcast:            "EVENT_RX_BROADCAST",
		EventRxFlagBroadcast:        "EVENT_RX_FLAG_BROADCAST",
		EventRxAcknowledged:         "EVENT_RX_ACKNOWLEDGED",
		EventRxFlagAcknowledged:     "EVENT_RX_FLAG_ACKNOWLEDGED",
		EventRxBurstPacket:          "EVENT_RX_BURST_PACKET",
		EventRxFlagBurstPacket:      "EVENT_RX_FLAG_BURST_PACKET",
	}
	if n, ok := names[c]; ok {
		return n
	}
	return fmt.Sprintf("UNKNOWN_%d", int(c))
}

// Message is a single ANT protocol message (payload only; framing is added
// on encode).
type Message struct {
	ID   MessageID
	Data []byte
}

// NewMessage creates a message with the given id and payload. The payload
// is copied.
func NewMessage(id MessageID, data []byte) *Message {
	d := make([]byte, len(data))
	copy(d, data)
	return &Message{ID: id, Data: d}
}

// Checksum computes the XOR checksum over the framed bytes of the message.
func Checksum(sync, length, id byte, data []byte) byte {
	c := sync ^ length ^ id
	for _, b := range data {
		c ^= b
	}
	return c
}

// Validate checks that the message can be encoded as a single ANT frame:
// the one byte length field limits payloads to 255 bytes (code review
// PR #1, P2-14).
func (m *Message) Validate() error {
	if len(m.Data) > 255 {
		return fmt.Errorf("ant: payload exceeds 255 bytes: %d", len(m.Data))
	}
	return nil
}

// Encode returns the full wire frame: sync, length, id, data, checksum.
// Payloads longer than 255 bytes cannot be framed; use Validate at the
// API boundaries to reject them earlier.
func (m *Message) Encode() []byte {
	out := make([]byte, 0, len(m.Data)+4)
	out = append(out, SyncByte, byte(len(m.Data)), byte(m.ID))
	out = append(out, m.Data...)
	out = append(out, Checksum(SyncByte, byte(len(m.Data)), byte(m.ID), m.Data))
	return out
}

// Errors returned when parsing frames.
var (
	// ErrShortFrame is returned when the buffer does not contain a full frame.
	ErrShortFrame = errors.New("ant: incomplete frame")
	// ErrBadSync is returned when the first byte is not the sync byte.
	ErrBadSync = errors.New("ant: bad sync byte")
	// ErrBadChecksum is returned when the checksum does not match.
	ErrBadChecksum = errors.New("ant: bad checksum")
	// ErrBadLength is returned when the frame length field is inconsistent.
	ErrBadLength = errors.New("ant: bad frame length")
)

// ParseFrame parses a single frame from the start of buf. It returns the
// parsed message and the number of bytes consumed. If buf is too short
// ErrShortFrame is returned and the caller should read more bytes. If buf
// does not start with the sync byte ErrBadSync is returned together with a
// suggested skip of one byte so the caller can resynchronise.
func ParseFrame(buf []byte) (msg *Message, consumed int, err error) {
	if len(buf) == 0 {
		return nil, 0, ErrShortFrame
	}
	if buf[0] != SyncByte {
		return nil, 1, fmt.Errorf("%w: got 0x%02X", ErrBadSync, buf[0])
	}
	if len(buf) < 4 {
		return nil, 0, ErrShortFrame
	}
	length := int(buf[1])
	if len(buf) < length+4 {
		return nil, 0, ErrShortFrame
	}
	frame := buf[:length+4]
	want := Checksum(frame[0], frame[1], frame[2], frame[3:3+length])
	if frame[length+3] != want {
		return nil, length + 4, fmt.Errorf("%w: frame % X", ErrBadChecksum, frame)
	}
	return &Message{
		ID:   MessageID(frame[2]),
		Data: cloneBytes(frame[3 : 3+length]),
	}, length + 4, nil
}

// ParseFrames is a convenience helper that parses every well-formed frame in
// buf, skipping bad bytes (resynchronising on the next sync byte). It returns
// the messages found and the number of bytes consumed overall. Trailing
// partial frames remain unconsumed.
func ParseFrames(buf []byte) (msgs []*Message, consumed int) {
	for {
		m, n, err := ParseFrame(buf[consumed:])
		if err != nil {
			if errors.Is(err, ErrShortFrame) {
				return msgs, consumed
			}
			if errors.Is(err, ErrBadSync) {
				consumed++
				continue
			}
			// Bad checksum: skip whole frame and continue.
			consumed += n
			continue
		}
		msgs = append(msgs, m)
		consumed += n
	}
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// uint16LE and uint32LE are small helpers used across the package.
func uint16LE(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func uint32LE(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
