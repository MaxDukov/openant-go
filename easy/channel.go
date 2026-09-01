package easy

import (
	"log/slog"

	"github.com/maxdukov/openant-go/ant"
)

// channelConfig records every configuration step applied to a channel so
// the channel can be replayed onto a fresh stick after an automatic
// reconnect (the stick forgets all state on power cycle). Guarded by the
// owning node's mutex.
type channelConfig struct {
	ctype, network byte
	extAssign      *byte

	hasID      bool
	deviceNum  uint16
	deviceType byte
	transType  byte

	period    uint16
	hasPeriod bool

	searchTimeout    byte
	hasSearchTimeout bool

	rfFreq    byte
	hasRFFreq bool

	extended    bool
	hasExtended bool

	waveform []byte

	opened bool
	rxScan bool
}

// Channel is a configured ANT channel with user-settable data callbacks.
// Callbacks are invoked from the node Run loop goroutine. It is the Go
// equivalent of openant.easy.channel.Channel; where the Python version is
// subclassed to override hooks, here the hooks are function fields.
type Channel struct {
	ID   byte
	node *Node
	log  *slog.Logger

	cfg channelConfig

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
	c.node.mu.Lock()
	c.cfg.ctype, c.cfg.network = ctype, network
	c.cfg.extAssign = nil
	if extAssign != nil {
		e := *extAssign
		c.cfg.extAssign = &e
	}
	c.node.mu.Unlock()
	c.logger().Debug("channel assigned", "channel", c.ID, "type", ctype)
	return nil
}

// Unassign unassigns the channel.
func (c *Channel) Unassign() error {
	c.node.Core.UnassignChannel(c.ID)
	if err := c.node.WaitForResponse(ant.IDUnassignChannel); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg = channelConfig{}
	c.node.mu.Unlock()
	return nil
}

// Open opens the channel.
func (c *Channel) Open() error {
	c.node.Core.OpenChannel(c.ID)
	if err := c.node.WaitForResponse(ant.IDOpenChannel); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.opened, c.cfg.rxScan = true, false
	c.node.mu.Unlock()
	return nil
}

// OpenRxScanMode enables continuous RX scan mode on the channel.
func (c *Channel) OpenRxScanMode() error {
	c.node.Core.OpenRxScanMode(c.ID)
	if err := c.node.WaitForResponse(ant.IDOpenRxScanMode); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.opened, c.cfg.rxScan = true, true
	c.node.mu.Unlock()
	return nil
}

// Close closes the channel.
func (c *Channel) Close() error {
	c.node.Core.CloseChannel(c.ID)
	if err := c.node.WaitForResponse(ant.IDCloseChannel); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.opened, c.cfg.rxScan = false, false
	c.node.mu.Unlock()
	return nil
}

// SetID sets the channel id: device number (0 = any), device type and
// transmission type.
func (c *Channel) SetID(deviceNum int, deviceType, transmissionType byte) error {
	c.node.Core.SetChannelID(c.ID, uint16(deviceNum), deviceType, transmissionType)
	if err := c.node.WaitForResponse(ant.IDSetChannelID); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.hasID = true
	c.cfg.deviceNum, c.cfg.deviceType, c.cfg.transType = uint16(deviceNum), deviceType, transmissionType
	c.node.mu.Unlock()
	return nil
}

// SetPeriod sets the channel period in 1/32768 s units.
func (c *Channel) SetPeriod(period int) error {
	c.node.Core.SetChannelPeriod(c.ID, uint16(period))
	if err := c.node.WaitForResponse(ant.IDChannelPeriod); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.period, c.cfg.hasPeriod = uint16(period), true
	c.node.mu.Unlock()
	return nil
}

// SetSearchTimeout sets the search timeout (2.5 s units, 255 = infinite).
func (c *Channel) SetSearchTimeout(timeout byte) error {
	c.node.Core.SetChannelSearchTimeout(c.ID, timeout)
	if err := c.node.WaitForResponse(ant.IDChannelSearchTimeout); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.searchTimeout, c.cfg.hasSearchTimeout = timeout, true
	c.node.mu.Unlock()
	return nil
}

// SetRFFrequency sets the RF frequency offset from 2400 MHz.
func (c *Channel) SetRFFrequency(freq byte) error {
	c.node.Core.SetChannelRFFrequency(c.ID, freq)
	if err := c.node.WaitForResponse(ant.IDChannelRFFrequency); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.rfFreq, c.cfg.hasRFFreq = freq, true
	c.node.mu.Unlock()
	return nil
}

// EnableExtendedMessages enables or disables extended receive messages.
func (c *Channel) EnableExtendedMessages(enable bool) error {
	c.node.Core.EnableExtendedMessages(c.ID, enable)
	if err := c.node.WaitForResponse(ant.IDEnableExtendedMessages); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.extended, c.cfg.hasExtended = enable, true
	c.node.mu.Unlock()
	return nil
}

// SetSearchWaveform sets the search waveform (usually [0x53, 0x00]).
func (c *Channel) SetSearchWaveform(waveform []byte) error {
	c.node.Core.SetSearchWaveform(c.ID, waveform)
	if err := c.node.WaitForResponse(ant.IDSetSearchWaveform); err != nil {
		return err
	}
	c.node.mu.Lock()
	c.cfg.waveform = append([]byte(nil), waveform...)
	c.node.mu.Unlock()
	return nil
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

// restore replays the recorded configuration onto the (freshly reset)
// stick, in dependency order: assign, channel parameters, then open. It is
// called from the reconnect path, once per live channel.
func (c *Channel) restore() error {
	c.node.mu.Lock()
	cfg := c.cfg // snapshot under lock; setters below re-lock briefly
	c.node.mu.Unlock()

	if err := c.Assign(cfg.ctype, cfg.network, cfg.extAssign); err != nil {
		return err
	}
	if cfg.hasID {
		if err := c.SetID(int(cfg.deviceNum), cfg.deviceType, cfg.transType); err != nil {
			return err
		}
	}
	if cfg.hasPeriod {
		if err := c.SetPeriod(int(cfg.period)); err != nil {
			return err
		}
	}
	if cfg.hasSearchTimeout {
		if err := c.SetSearchTimeout(cfg.searchTimeout); err != nil {
			return err
		}
	}
	if cfg.hasRFFreq {
		if err := c.SetRFFrequency(cfg.rfFreq); err != nil {
			return err
		}
	}
	if cfg.hasExtended {
		if err := c.EnableExtendedMessages(cfg.extended); err != nil {
			return err
		}
	}
	if cfg.waveform != nil {
		if err := c.SetSearchWaveform(cfg.waveform); err != nil {
			return err
		}
	}
	if cfg.rxScan {
		return c.OpenRxScanMode()
	}
	if cfg.opened {
		return c.Open()
	}
	return nil
}
