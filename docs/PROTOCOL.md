# ANT protocol notes

Implementation notes for openant-go: frame format, message flow,
state machines and known firmware quirks. Numbers reference the ANT
message protocol (this is not a spec, only what the code relies on).

## Frame format

Every frame on the wire (USB bulk or serial):

    +------+--------+--------+---------+------+
    | 0xA4 | length | msg id | payload | csum |
    +------+--------+--------+---------+------+

- `length`: payload length (0..255); oversized payloads are rejected by
  `Message.Validate` instead of being silently truncated.
- checksum is the XOR of all preceding bytes including the sync byte.
- The parser resynchronises on bad sync/checksum bytes by dropping the
  leading byte (openant asserts here; we do not).

## Message flow

- Configuration commands are answered by a channel event
  (`0x40 <channel> <msg id> <status>`); `status != 0` is an error code.
- Channel events proper use the same `0x40` message with a leading
  `0x01` marker: `0x40 <channel> 0x01 <event> [data]`.
- Data messages carry `<channel> <8 byte payload>` (broadcast `0x4E`,
  acknowledged `0x4F`) or `<ch|seq<<5> <8 bytes>` for burst `0x50`;
  burst sequence is a 3-bit rolling counter with a last-packet flag.
- Acknowledged and burst transmissions are queued and released in the
  timeslot opened by each received broadcast message (openant drains the
  queue on *every* broadcast, duplicates included).
- Extended data (when enabled) appends 5 bytes to the 8-byte payload:
  flag, device number LSB/MSB, device type, transmission type.

## Channel lifecycle

    assign (type, network) -> set id / period / rf / timeout -> open
    close -> wait EVENT_CHANNEL_CLOSED -> unassign

Unassigning before the close event yields `CHANNEL_IN_WRONG_STATE`
(openant #81); `easy.Node.RemoveChannel` waits up to 1 s for the event.

## Search helpers

- **Proximity search** (`0x60`, `Channel.SetProximitySearch`): payload
  `<channel> <threshold>`; 0 disables, 1..255 limits the search to the
  given number of signal bins (~dB of attenuation). The threshold applies
  while the channel is searching and is forgotten once connected.
- **Channel ID list** (`0x59` + `0x5A`, `Channel.EnableChannelIDList` /
  `AddChannelID`): `0x59 <channel> <size>` switches the channel to
  list-based matching, then `0x5A <channel> <num LSB> <num MSB> <type>`
  adds entries (device type 0 = wildcard). The stick then only connects
  to the listed IDs regardless of the id set with `0x51`. Entries are
  added only while the channel is not open. Both are replayed after a
  reconnect together with the rest of the channel configuration.

## Networks and keys

- Network 0 is public (all-zero key).
- ANT+ device network key: `B9A521FBBD72C345`.
- ANT-FS network key: `A8A423B9F55E63C1`.
- Keys are 8 bytes (16-byte keys exist for ANT-FS "enhanced" security).
- **The stick loses all state on power cycle** — network keys, channel
  configuration, everything. This is why reconnect replays the full
  configuration (see below).

## Reconnect (openant #51/#122)

On fatal driver errors (LIBUSB no-device / pipe / IO) `ant.Core`, when
created with `WithDriverFactory`, closes the dead driver and retries
`reopen -> open -> reset` with exponential backoff (500 ms doubling to
5 s, indefinitely until Stop). The driver pointer is swapped **before**
the `WithReconnectHook` callback runs: the hook replays network keys and
channel configuration and must be able to wait for responses, which
requires the reader to keep serving the new driver. A hook error makes
Core retry the whole cycle.

## ANT-FS summary

1. The device broadcasts a beacon (period 4 s typical, page 0x44 with
   session/programming flags).
2. Host: `OpenPipe` -> link auth (`0x43` request serial) -> switch to
   link period/frequency from the response.
3. Authentication (`0x44`): passkey exchange or pairing; on pairing the
   device displays a code that must be sent back within 30 s.
4. Download directory (`0x42` burst): a `CommandPipe` reply followed by
   directory entries (16 bytes each) streamed as burst data.
5. File download (`0x43` burst): CRC-seeded blocks; request payload is
   `offset (u32) <initial CRC (u32)> <size (u16)` — note the widths
   differ between spec revisions; openant (and this port) keep the
   u32/u16 layout.

`fs.Application` drives this state machine; `goant antfs` exposes it on
the CLI.

## Firmware quirks observed

| Symptom | Cause | Workaround |
|---|---|---|
| No EVENT_TX in master mode (BLJ06.01.01, ANTUSB2) | firmware does not report TX events | identical in Python openant; `easy.Channel` starts a ticker at the channel period when `OnBroadcastTxData` is set on a transmit channel (real EVENT_TX suppresses the tick in the same slot); investigate LIB_CONFIG 0x6E |
| `device or resource busy` on open | another process holds the interface | udev rules + single-owner usage; Open retries claim 5x200 ms |
| USB timeouts on Raspberry Pi | host controller timing, kernel driver detach warnings | `ant.SetDriverReadTimeout` / `easy.Node.SetReadTimeout` bound bulk IN reads (re-applied after reconnect); SetAutoDetach failures on Linux now log the udev rules hint (`resources/42-ant-usb-sticks.rules`) |
| Serial "permission denied" | user not in `dialout`/`plugdev` | install udev rules, re-login; open failures are wrapped in `ant.ErrPermission` with the hint |
