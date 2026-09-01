// Package easy provides the high-level ANT node: channels with callbacks,
// request/response handling and automatic reconnect.
//
// It is the Go equivalent of openant.easy (github.com/Tigge/openant).
//
// # Node and channels
//
// [New] finds and opens an ANT stick, resets it and returns a running
// [Node]. Channels are allocated with [Node.NewChannel], configured through
// [Channel] methods and receive data via callbacks (OnBroadcastData and
// friends) served from [Node.Run]. Where openant uses wait_for_response
// helpers, this package offers [Node.WaitForResponse] and
// [Node.WaitForEvent].
//
// # Automatic reconnect
//
// [New] enables automatic reconnect: if the stick fails or is re-plugged,
// the node re-opens the device and replays all network keys and channel
// configuration, then calls [Node.OnReconnect]. The stick forgets all
// state on power cycle, so this restores the exact setup (openant issues
// #51/#122). Custom drivers opt in via [WithReopen]; without a reopen
// function driver errors only back off.
//
// # Example
//
// A complete heart-rate monitor listener (uses the anttest simulator here;
// with easy.New it runs on real hardware):
//
//	n, err := easy.New()
//	...
//	ch, err := n.NewChannel(easy.ChannelBidirectionalReceive, 0, nil)
//	...
//	ch.SetID(0, 0x78, 0)          // any HRM, device type 120
//	ch.SetPeriod(8070)            // 4.06 Hz
//	ch.SetRFFrequency(57)         // 2466 MHz
//	ch.SetSearchTimeout(0xFF)     // search forever
//	ch.OnBroadcastData = func(data []byte) { ... }
//	ch.Open()
//	n.Run(ctx)
//
// The devices package builds on this layer and offers ready-made ANT+
// device profiles; the fs package implements ANT-FS on top of a channel.
package easy
