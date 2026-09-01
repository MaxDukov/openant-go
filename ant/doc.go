// Package ant implements the ANT transport: USB/serial drivers, frame
// parsing, message encoding and the low-level Core engine.
//
// It is the Go equivalent of openant.base (github.com/Tigge/openant).
//
// # Architecture
//
// A Driver (see [FindDriver], [NewDriver]) talks to an ANT USB stick
// (ANTUSB2 0fcf:1008, ANTUSB-m 0fcf:1009) or a serial-connected node.
// [Core] wraps an opened driver with a reader and a dispatcher goroutine:
// the reader parses frames ([ParseFrame]) and reassembles burst transfers,
// the dispatcher classifies messages into [Event] values and delivers them
// to the handler installed with [WithEventHandler]. Acknowledged and burst
// transmissions are queued and sent in the channel timeslot triggered by
// the peer's broadcast messages, exactly like openant.
//
// # Reconnect
//
// By default a dead driver only triggers read backoff. When created with
// [WithDriverFactory], Core automatically re-opens the device on fatal
// driver errors (stick unplugged, LIBUSB pipe/IO errors), resets it and
// calls the [WithReconnectHook] callback so higher layers can restore the
// configuration — the stick forgets all state on power cycle
// (openant issues #51/#122).
//
// # Typical usage
//
// Most applications should start at the easy package, which handles
// responses, timeouts and channel state management on top of Core.
//
// This package is covered by the protocol notes in docs/PROTOCOL.md
// (frame format, message flow, firmware quirks).
package ant
