package easy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/maxdukov/openant-go/ant"
)

// Node manages an ANT stick: it exposes channels, tracks capabilities and
// serial metadata, dispatches received data to channel callbacks and
// provides the wait_for_* primitives used for command/response handling.
// It is the Go equivalent of openant.easy.node.Node.
type Node struct {
	Core *ant.Core
	log  *slog.Logger

	responses *eventBuffer // responses to configuration commands
	events    *eventBuffer // channel events except data events

	dataCh chan dataEvent

	mu          sync.Mutex
	channels    []*Channel
	maxChannels int
	maxNetworks int
	caps        *ant.Capabilities
	serial      uint32
	antVersion  string

	stopOnce sync.Once
	stopCh   chan struct{}
}

// dataEvent is a received data message routed to a Channel callback.
type dataEvent struct {
	code    ant.Code
	channel byte
	data    []byte
}

// New finds an ANT device, opens it and returns a started Node. The node
// resets the stick, as openant does.
func New(opts ...NodeOption) (*Node, error) {
	d, err := ant.FindDriver()
	if err != nil {
		return nil, fmt.Errorf("easy: find driver: %w", err)
	}
	return NewWithDriver(d, opts...)
}

// NewWithDriver builds a Node on top of a custom driver (e.g. the anttest
// mock). The driver is opened by the node and reset is issued.
func NewWithDriver(d ant.Driver, opts ...NodeOption) (*Node, error) {
	n := &Node{
		log:         slog.Default(),
		responses:   newEventBuffer(),
		events:      newEventBuffer(),
		dataCh:      make(chan dataEvent, eventQueueSize),
		maxChannels: 8,
		maxNetworks: 8,
		stopCh:      make(chan struct{}),
	}
	for _, o := range opts {
		o(n)
	}
	core, err := ant.NewCore(d, ant.WithLogger(n.log), ant.WithEventHandler(n.handleEvent))
	if err != nil {
		return nil, err
	}
	n.Core = core

	// Request capabilities and metadata eagerly, like openant's worker.
	n.Core.RequestMessage(0, ant.IDCapabilities)
	n.Core.RequestMessage(0, ant.IDSerialNumber)
	n.Core.RequestMessage(0, ant.IDAntVersion)
	return n, nil
}

// NodeOption configures a Node.
type NodeOption func(*Node)

// WithNodeLogger sets a custom logger.
func WithNodeLogger(l *slog.Logger) NodeOption { return func(n *Node) { n.log = l } }

// handleEvent is invoked from the core dispatcher goroutine for every
// classified event. It mirrors openant's _worker_response/_worker_event.
func (n *Node) handleEvent(ev ant.Event) {
	switch ev.Kind {
	case ant.KindResponse:
		switch ant.MessageID(ev.Code) {
		case ant.IDCapabilities:
			if caps, err := ant.ParseCapabilities(ev.Data); err == nil {
				n.mu.Lock()
				n.caps = caps
				n.maxChannels = caps.MaxChannels
				n.maxNetworks = caps.MaxNetworks
				n.mu.Unlock()
			} else {
				n.log.Warn("bad capabilities payload", "error", err)
			}
		case ant.IDSerialNumber:
			n.mu.Lock()
			n.serial = ant.SerialNumber(ev.Data)
			n.mu.Unlock()
		case ant.IDAntVersion:
			n.mu.Lock()
			n.antVersion = ant.AntVersion(ev.Data)
			n.mu.Unlock()
		}
		n.responses.push(ev)

	case ant.KindChannel:
		switch ev.Code {
		case ant.EventRxBroadcast:
			n.pushData(ev, ant.EventRxBroadcast)
		case ant.EventRxAcknowledged:
			n.pushData(ev, ant.EventRxAcknowledged)
		case ant.EventTx:
			n.pushData(ev, ant.EventTx)
		case ant.EventRxBurstPacket:
			n.pushData(ev, ant.EventRxBurstPacket)
		default:
			n.events.push(ev)
		}
	}
}

func (n *Node) pushData(ev ant.Event, code ant.Code) {
	select {
	case n.dataCh <- dataEvent{code: code, channel: ev.Channel, data: ev.Data}:
	case <-n.stopCh:
	}
}

// Run dispatches received data to the channel callbacks until the node is
// stopped or ctx is cancelled. It corresponds to openant's node.start().
// Run exits by itself; Stop does not wait for it (the core is joined by
// Stop instead).
func (n *Node) Run(ctx context.Context) {
	ctxDone := ctx.Done()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ctxDone:
			return
		case de := <-n.dataCh:
			ch := n.Channel(de.channel)
			if ch == nil {
				continue
			}
			switch de.code {
			case ant.EventRxBroadcast:
				if ch.OnBroadcastData != nil {
					ch.OnBroadcastData(de.data)
				}
			case ant.EventRxAcknowledged:
				if ch.OnAcknowledgedData != nil {
					ch.OnAcknowledgedData(de.data)
				}
			case ant.EventTx:
				if ch.OnBroadcastTxData != nil {
					ch.OnBroadcastTxData(de.data)
				}
			case ant.EventRxBurstPacket:
				if ch.OnBurstData != nil {
					ch.OnBurstData(de.data)
				}
			}
		}
	}
}

// Start runs Run in a new goroutine and returns a done channel closed when
// the loop exits.
func (n *Node) Start(ctx context.Context) (done <-chan struct{}) {
	d := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(d)
	}()
	return d
}

// Channel returns the channel with the given number, or nil.
func (n *Node) Channel(id byte) *Channel {
	n.mu.Lock()
	defer n.mu.Unlock()
	if int(id) >= len(n.channels) {
		return nil
	}
	return n.channels[id]
}

// Channels returns the live channels.
func (n *Node) Channels() []*Channel {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*Channel, 0, len(n.channels))
	for _, c := range n.channels {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// NewChannel allocates and assigns a new channel of the given type on the
// given network. extAssign may be nil. Channel numbers are reused after
// removal, so the slice index always equals the hardware channel number.
func (n *Node) NewChannel(ctype byte, network byte, extAssign *byte) (*Channel, error) {
	n.mu.Lock()
	num := -1
	for i, c := range n.channels {
		if c == nil {
			num = i
			break
		}
	}
	if num < 0 {
		num = len(n.channels)
	}
	if num >= n.maxChannels {
		n.mu.Unlock()
		return nil, fmt.Errorf("%w: max %d", ErrTooManyChannels, n.maxChannels)
	}
	if int(network) >= n.maxNetworks {
		n.mu.Unlock()
		return nil, fmt.Errorf("%w: %d >= %d", ErrInvalidNetwork, network, n.maxNetworks)
	}
	ch := &Channel{ID: byte(num), node: n}
	if num == len(n.channels) {
		n.channels = append(n.channels, ch)
	} else {
		n.channels[num] = ch
	}
	n.mu.Unlock()

	if err := ch.Assign(ctype, network, extAssign); err != nil {
		// Roll back the allocation on failure.
		n.mu.Lock()
		n.channels[num] = nil
		n.mu.Unlock()
		return nil, err
	}
	return ch, nil
}

// RemoveChannel closes and unassigns the channel and removes it from the
// node. Errors are logged, not propagated (openant behaviour).
func (n *Node) RemoveChannel(ch *Channel) {
	if ch == nil {
		return
	}
	if err := ch.Close(); err != nil {
		n.log.Warn("close channel", "channel", ch.ID, "error", err)
	}
	if err := ch.Unassign(); err != nil {
		n.log.Warn("unassign channel", "channel", ch.ID, "error", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if int(ch.ID) < len(n.channels) {
		n.channels[ch.ID] = nil
	}
}

// RemoveChannelID removes a channel by number.
func (n *Node) RemoveChannelID(id byte) { n.RemoveChannel(n.Channel(id)) }

// RequestMessage requests a message from the node and waits for it.
func (n *Node) RequestMessage(msgID ant.MessageID) error {
	n.Core.RequestMessage(0, msgID)
	return n.WaitForSpecial(msgID)
}

// SetNetworkKey sets a network key (8 or 16 bytes) on the given network.
func (n *Node) SetNetworkKey(network byte, key []byte) error {
	n.mu.Lock()
	max := n.maxNetworks
	n.mu.Unlock()
	if int(network) >= max {
		return fmt.Errorf("%w: %d >= %d", ErrInvalidNetwork, network, max)
	}
	if err := n.Core.SetNetworkKey(network, key); err != nil {
		return err
	}
	return n.WaitForResponse(ant.IDSetNetworkKey)
}

// SetLED enables or disables the stick LED.
func (n *Node) SetLED(enabled bool) error {
	n.Core.EnableLED(enabled)
	return n.WaitForResponse(ant.IDEnableLED)
}

// Capabilities returns the capabilities reported by the stick (may be nil
// before the response arrives).
func (n *Node) Capabilities() *ant.Capabilities {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.caps
}

// SerialNumber returns the stick serial number (0 if unknown yet).
func (n *Node) SerialNumber() uint32 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.serial
}

// AntVersion returns the stick firmware version string.
func (n *Node) AntVersion() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.antVersion
}

// MaxChannels and MaxNetworks report the configured channel/network limits.
func (n *Node) MaxChannels() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.maxChannels
}

func (n *Node) MaxNetworks() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.maxNetworks
}

// WaitForEvent waits for a channel event with one of the given codes.
func (n *Node) WaitForEvent(okCodes ...ant.Code) (ant.Event, error) {
	return n.events.waitFor(func(ev ant.Event) bool {
		if ev.Kind != ant.KindChannel {
			return false
		}
		for _, c := range okCodes {
			if ev.Code == c {
				return true
			}
		}
		return false
	})
}

// WaitForResponse waits for the response to the given command and returns
// an error if the node reported a non-zero code.
func (n *Node) WaitForResponse(msgID ant.MessageID) error {
	ev, err := n.responses.waitFor(func(ev ant.Event) bool {
		return ev.Kind == ant.KindResponse && ev.Code == ant.Code(msgID)
	})
	if err != nil {
		return err
	}
	if len(ev.Data) > 0 && ev.Data[0] != 0 {
		return &ResponseError{Code: ant.Code(ev.Data[0])}
	}
	return nil
}

// WaitForSpecial waits for an unsolicited response message (capabilities,
// channel id, ...) without checking its status byte.
func (n *Node) WaitForSpecial(msgID ant.MessageID) error {
	_, err := n.responses.waitFor(func(ev ant.Event) bool {
		return ev.Kind == ant.KindResponse && ev.Code == ant.Code(msgID)
	})
	return err
}

// Stop shuts the node and the underlying core down and closes the driver.
// Stop is idempotent and safe to call multiple times.
func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
		if n.Core != nil {
			n.Core.Stop()
		}
	})
}
