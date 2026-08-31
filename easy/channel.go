package easy

import (
	"log/slog"

	"github.com/maxdukov/openant-go/ant"
)

// Channel is a configured ANT channel with user-settable data callbacks.
// Callbacks are invoked from the node Run loop goroutine. It is the Go
// equivalent of openant.easy.channel.Channel; where the Python version is
// subclassed to override hooks, here the hooks are function fields.
type Channel struct {
	ID   byte
	node *Node
	log  *slog.Logger

	// OnBroadcastData is called with the 8 byte payload of received
	// broadcast messages (plus extended bytes when enabled).
	OnBroadcastData func(data []byte)
	// OnBurstData is called with the fully reassembled burst payload.
	OnBurstData func(data []byte)
	// OnAcknowledgedData is called with received acknowledged data.
	OnAcknowledgedData func(data []byte)
	// OnBroadcastTxData is called after broadcast data has been sent
	// (channel EVENT_TX).
	OnBroadcastTxData func(data []byte)
}

func (c *Channel) logger() *slog.Logger {
	if c.log != nil {
		return c.log
	}
	if c.node != nil {
		return c.node.log
	}
	return slog.Default()
}

// Assign assigns the channel on the node with the given type and network.
func (c *Channel) Assign(ctype, network byte, extAssign *byte) error {
	c.node.Core.AssignChannel(c.ID, ctype, network, extAssign)
	if err := c.node.WaitForResponse(ant.IDAssignChannel); err != nil {
		return err
	}
	c.logger().Debug("channel assigned", "channel", c.ID, "type", ctype)
	return nil
}

// Unassign unassigns the channel.
func (c *Channel) Unassign() error {
	c.node.Core.UnassignChannel(c.ID)
	return c.node.WaitForResponse(ant.IDUnassignChannel)
}

// Open opens the channel.
func (c *Channel) Open() error {
	c.node.Core.OpenChannel(c.ID)
	return c.node.WaitForResponse(ant.IDOpenChannel)
}

// OpenRxScanMode enables continuous RX scan mode on the channel.
func (c *Channel) OpenRxScanMode() error {
	c.node.Core.OpenRxScanMode(c.ID)
	return c.node.WaitForResponse(ant.IDOpenRxScanMode)
}

// Close closes the channel.
func (c *Channel) Close() error {
	c.node.Core.CloseChannel(c.ID)
	return c.node.WaitForResponse(ant.IDCloseChannel)
}

// SetID sets the channel id: device number (0 = any), device type and
// transmission type.
func (c *Channel) SetID(deviceNum int, deviceType, transmissionType byte) error {
	c.node.Core.SetChannelID(c.ID, uint16(deviceNum), deviceType, transmissionType)
	return c.node.WaitForResponse(ant.IDSetChannelID)
}

// SetPeriod sets the channel period in 1/32768 s units.
func (c *Channel) SetPeriod(period int) error {
	c.node.Core.SetChannelPeriod(c.ID, uint16(period))
	return c.node.WaitForResponse(ant.IDChannelPeriod)
}

// SetSearchTimeout sets the search timeout (2.5 s units, 255 = infinite).
func (c *Channel) SetSearchTimeout(timeout byte) error {
	c.node.Core.SetChannelSearchTimeout(c.ID, timeout)
	return c.node.WaitForResponse(ant.IDChannelSearchTimeout)
}

// SetRFFrequency sets the RF frequency offset from 2400 MHz.
func (c *Channel) SetRFFrequency(freq byte) error {
	c.node.Core.SetChannelRFFrequency(c.ID, freq)
	return c.node.WaitForResponse(ant.IDChannelRFFrequency)
}

// EnableExtendedMessages enables or disables extended receive messages.
func (c *Channel) EnableExtendedMessages(enable bool) error {
	c.node.Core.EnableExtendedMessages(c.ID, enable)
	return c.node.WaitForResponse(ant.IDEnableExtendedMessages)
}

// SetSearchWaveform sets the search waveform (usually [0x53, 0x00]).
func (c *Channel) SetSearchWaveform(waveform []byte) error {
	c.node.Core.SetSearchWaveform(c.ID, waveform)
	return c.node.WaitForResponse(ant.IDSetSearchWaveform)
}

// RequestMessage requests a message about this channel and waits for it.
func (c *Channel) RequestMessage(msgID ant.MessageID) error {
	c.node.Core.RequestMessage(c.ID, msgID)
	return c.node.WaitForSpecial(msgID)
}

// SendBroadcastData sends 8 bytes of broadcast data immediately.
func (c *Channel) SendBroadcastData(data []byte) error {
	return c.node.Core.SendBroadcastData(c.ID, data)
}

// SendAcknowledgedData sends 8 bytes of acknowledged data, retrying on
// transfer failure as openant does.
func (c *Channel) SendAcknowledgedData(data []byte) error {
	for {
		if err := c.node.Core.SendAcknowledgedData(c.ID, data); err != nil {
			return err
		}
		if _, err := c.node.WaitForEvent(ant.EventTransferTxCompleted); err != nil {
			if err == ErrTransferFailed {
				c.logger().Warn("acknowledged data transfer failed, retrying", "channel", c.ID)
				continue
			}
			return err
		}
		return nil
	}
}

// SendBurstTransfer sends a burst (multiple of 8 bytes), retrying the whole
// burst on transfer failure as openant does.
func (c *Channel) SendBurstTransfer(data []byte) error {
	for {
		if err := c.node.Core.SendBurstTransfer(c.ID, data); err != nil {
			return err
		}
		if _, err := c.node.WaitForEvent(ant.EventTransferTxStart); err != nil {
			if err == ErrTransferFailed {
				c.logger().Warn("burst transfer start failed, retrying", "channel", c.ID)
				continue
			}
			return err
		}
		if _, err := c.node.WaitForEvent(ant.EventTransferTxCompleted); err != nil {
			if err == ErrTransferFailed {
				c.logger().Warn("burst transfer failed, retrying", "channel", c.ID)
				continue
			}
			return err
		}
		return nil
	}
}
