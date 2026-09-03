package anttest

import (
	"github.com/maxdukov/openant-go/ant"
)

// SimDriver wraps a MockDriver and emulates a basic ANT USB stick: it
// answers configuration commands with the proper channel event responses
// and remembers the configured channel state. It allows the easy and fs
// layers to be exercised without hardware.
type SimDriver struct {
	*MockDriver
}

// NewSimDriver returns a stick simulator.
func NewSimDriver() *SimDriver {
	s := &SimDriver{MockDriver: NewMockDriver()}
	// The core opens the driver and issues a reset followed by capability
	// and metadata requests; answer them once the frames arrive.
	return s
}

var _ ant.Driver = (*SimDriver)(nil)

// Open opens the underlying mock and starts auto-responding.
func (s *SimDriver) Open() error {
	if err := s.MockDriver.Open(); err != nil {
		return err
	}
	return nil
}

// Write inspects outgoing frames and queues canned responses.
func (s *SimDriver) Write(p []byte) (int, error) {
	n, err := s.MockDriver.Write(p)
	if err != nil {
		return n, err
	}
	msgs, _ := ant.ParseFrames(p)
	for _, m := range msgs {
		s.respond(m)
	}
	return n, nil
}

func (s *SimDriver) respond(m *ant.Message) {
	switch m.ID {
	case ant.IDResetSystem:
		s.QueueMessage(ant.NewMessage(ant.IDStartupMessage, []byte{0x00}))
	case ant.IDRequestMessage:
		if len(m.Data) < 2 {
			return
		}
		s.queueRequested(ant.MessageID(m.Data[1]))
	case ant.IDCloseChannel:
		// A real stick sends the command response followed by the
		// EVENT_CHANNEL_CLOSED channel event.
		if len(m.Data) < 1 {
			return
		}
		s.okResponse(m.Data[0], m.ID)
		s.EmitAckEvent(m.Data[0], ant.EventChannelClosed)
	case ant.IDAssignChannel, ant.IDUnassignChannel, ant.IDOpenChannel,
		ant.IDChannelPeriod, ant.IDChannelSearchTimeout,
		ant.IDChannelRFFrequency, ant.IDSetNetworkKey, ant.IDSetTransmitPower,
		ant.IDSetSearchWaveform, ant.IDOpenRxScanMode,
		ant.IDEnableExtendedMessages, ant.IDEnableLED, ant.IDSetChannelID,
		ant.IDSetProximitySearch, ant.IDChannelIDList, ant.IDAddChannelID:
		if len(m.Data) < 1 {
			return
		}
		s.okResponse(m.Data[0], m.ID)
	}
}

// queueRequested fabricates a response for REQUEST_MESSAGE.
func (s *SimDriver) queueRequested(id ant.MessageID) {
	switch id {
	case ant.IDCapabilities:
		s.QueueMessage(ant.NewMessage(ant.IDCapabilities, []byte{
			8, // max channels
			2, // max networks
			0, 0, 0, 0,
		}))
	case ant.IDSerialNumber:
		s.QueueMessage(ant.NewMessage(ant.IDSerialNumber, []byte{0x11, 0x22, 0x33, 0x44}))
	case ant.IDAntVersion:
		s.QueueMessage(ant.NewMessage(ant.IDAntVersion, append([]byte("1.0.0"), 0)))
	case ant.IDChannelStatus:
		s.QueueMessage(ant.NewMessage(ant.IDChannelStatus, []byte{0x00, 0x00}))
	case ant.IDSetChannelID:
		s.QueueMessage(ant.NewMessage(ant.IDSetChannelID, []byte{0x00, 0x01, 0x00, 0x78, 0x00}))
	default:
		s.okResponse(0, id)
	}
}

// okResponse queues a channel event response with code 0.
func (s *SimDriver) okResponse(ch byte, msgID ant.MessageID) {
	s.QueueMessage(ant.NewMessage(ant.IDChannelEvent, []byte{ch, byte(msgID), 0x00}))
}

// EmitBroadcast simulates a received broadcast page on the given channel.
func (s *SimDriver) EmitBroadcast(ch byte, data []byte) {
	payload := append([]byte{ch}, data...)
	s.QueueMessage(ant.NewMessage(ant.IDBroadcastData, payload))
}

// EmitBurst simulates a received burst transfer on the given channel,
// splitting data into sequenced packets like a real device.
func (s *SimDriver) EmitBurst(ch byte, data []byte) {
	packets := (len(data) + 7) / 8
	for i := 0; i < packets; i++ {
		end := (i + 1) * 8
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i*8 : end]
		padded := make([]byte, 8)
		copy(padded, chunk)
		var seq byte
		if i == 0 {
			seq = 0
		} else {
			seq = byte((i-1)%3) + 1
		}
		if i == packets-1 {
			seq |= 0b100
		}
		s.QueueMessage(ant.NewMessage(ant.IDBurstTransferData, append([]byte{ch | seq<<5}, padded...)))
	}
}

// EmitAckEvent injects a raw channel event (e.g. EVENT_TRANSFER_TX_COMPLETED).
func (s *SimDriver) EmitAckEvent(ch byte, code ant.Code) {
	s.QueueMessage(ant.NewMessage(ant.IDChannelEvent, []byte{ch, 0x01, byte(code)}))
}
