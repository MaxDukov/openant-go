# TODO

Backlog for openant-go, derived from the issue tracker of the Python
[openant](https://github.com/Tigge/openant) library and from code reviews
of the Go port. Issue numbers reference Tigge/openant.

## Code review 2026-08-31 (PR #1: REVIEW.MD + REVIEW2.MD)

Working through the review findings (17 confirmed, 3 refuted — details in
the PR). Fixed items are checked and reference the commit topics.

### P0 — panics/DoS on RF/USB-controlled data
- ✅ Short beacon slice in `fs.Application.onData` (P0-1)
- ✅ `baseDevice.onData`: extended block `len >= 13`, common pages
      `len >= 8` (P0-2)
- ✅ `Scanner.scanData`: `len >= 13` (P0-3)
- ✅ `Download`: uint64 math, offset/size validation, 64 MiB cap (P0-4)
- ✅ `Upload`: offset/block-size validation, uint64 math (P0-5)
- ✅ `recover()` around user callbacks in `easy.Node.Run` (P0-6)
- ✅ Fuzz tests: `ParseFrame`, `ParseBeacon`, `ParseCommand`,
      `ParsePipeCommand`, `ParseDirectory`, `Application.onData`,
      `baseDevice.onData`, `Scanner.scanData` (P0-7) — no crashes found
      in short runs; extend corpus/CI time

### P1 — concurrency, lifecycle, numeric correctness
- ✅ `SerialDriver` port race — mutex added (P1-8)
- ✅ Reader busy-spin on persistent driver errors — exponential backoff
      (P1-9; also mitigates openant #51/#122 until full reconnect exists)
- ✅ `NewApplication` deadlock on `SetupChannel` error (P1-10)
- ✅ Modular uint16 delta for crank/wheel period — openant-inherited
      precedence bug zeroing torque/power (P1-11)
- ✅ `shift` function-set event type mask `0x30` (P1-12)
- ✅ `LevErrorMessageFromByte` inversion (P1-13)

### P2 — protocol API, buffers
- ✅ `Message.Validate` + rejection in `Core.Write`/`WriteTimeslot` (P2-14)
- ✅ `ReadJSONDevices` reads `{"devices":[...]}` like `Scanner.Save`
      and openant (P2-15)
- ✅ Caps: burst reassembly 1 MiB, event buffer 1024 drop-oldest (P2-16)

### P3 — cosmetics
- ✅ `errors.Is` in `consume()` (dead `ErrBadSync` branch) (P3-17)
- ✅ Dead code removed: `uint16LE`/`uint32LE`, `snapshot` (P3-19)
- ✅ udev `0660` + plugdev instead of `0666` (P3-20)
- ✅ README: Go >= 1.25 (P3-21)

### Refuted findings (do not "fix" again)
- `data[7] & 0x70 >> 4` / `data[6] & 0x80 >> 7` are **correct in Go**:
  `&` and `>>` share one precedence level and associate left-to-right,
  so they evaluate as `(x & 0x70) >> 4`. The reviewer assumed C/Python
  precedence (the Python original does have this bug; the port escaped
  it). Parentheses added for readability only.
- `HeartRate.onData` already guards `len < 8`; `BikeSpeed`/`BikeCadence`
  go through `onSpeedCadencePages` which guards as well (P3-18).
- `cmd/goant` exists in the repository (P3-21 partially wrong); only the
  Go version claim in the README was stale.

## Protocol features not yet implemented (gap analysis 2026-09-02)

Priorities follow the maintainer's focus: bike trainers (FE-C) and
heart-rate / health sensors first.

### FE-C / fitness equipment (high priority)
- ✅ **FE-C Command Status page 0x47 RX** — DONE: common page 71 decoded
      in full per Rev 5.0 Tables 8-48/8-49 (command ID, sequence,
      status pass/fail/not-supported/rejected/pending, response data for
      all four control modes); emitted as a "command_status" device data
      event (FECommandStatusData) and stored in LastCommand.
- ✅ **FE-C User Configuration page 0x37 TX** — DONE: SetUserConfig sends
      the page as acknowledged data per Rev 5.0 Table 8-47: user weight
      (0.01 kg), bicycle weight (0.05 kg, 12 bits), wheel diameter with
      millimetre offset nibble, gear ratio (0.03). Note: the page number
      is 55 (0x37) in Rev 5.0 — 0x46 is the common Request Data Page.
- ✅ **FE-C Capabilities page 0x36 RX** — DONE: page 54 (0x36) decoded
      (basic/target power/simulation mode bits, max resistance in
      Newtons); RequestCapabilities() issues the RequestDP(54, 1).
- ✅ **FE-C Metabolic page 0x12 RX** — DONE: METs (0.01), caloric burn
      rate (0.1 kCal/hr), accumulated calories, invalid-field handling
      (Table 8-13), "metabolic" event + Metabolic field.
- ✅ **FE-C Trainer pages 0x13 RX and trainer status** — DONE: treadmill
      page 19 decoded (cadence, +/- vertical distance, Table 8-15);
      page 25 (0x19) now also decodes trainer status bits (power/
      resistance calibration required, user config required), target
      power limit flags and FE state into FETrainerStatusData
      ("trainer_status" event) so a client can react to the
      user-config-required request.

  Also fixed while doing this (verified against the official Rev 5.0
  PDF, docs differ from python openant):
  - wind page 0x32 and track page 0x33 encoded bytes 1-3 instead of
    5-7, missed the -127 km/h offset on wind speed and the -200 %
    offset on grade, used the wrong drafting (levels instead of a
    0.01 scale factor) and rolling resistance (1/1000 instead of
    5x10^-5) resolutions, and clamped grade to +/-40 % instead of the
    specified +/-200 %; SetWindResistance draftingFactor is float64 now.
  - trainer torque page 0x1A decoded the wheel period from bytes 4-5
    and torque from bytes 6-7 (inherited from python openant); the
    spec puts them at bytes 3-4 and 5-6 respectively.

### Heart rate / health (high priority)
- ✅ **HRM master (sensor emulation) mode** — DONE: NewHeartRateMaster
      creates a simulated belt per HRM spec Rev 2.1 section 5.2 (master
      channel, period 8070, RF 57, device type 120, transmission type
      LSN 1, random non-zero device number). SetHeartRate drives the
      beat clock (beat time in 1/1024 s with the 63.999 s rollover,
      beat count, previous-beat page 4 once a previous beat exists,
      default page 0 before that); background pages 1-3 (operating
      time, manufacturer, product info) rotate every 65th message per
      the spec's schedule; the page change toggle bit flips every 4th
      message; pages 6 (capabilities) and 7 (battery status) are served
      on display page requests (common page 70). Useful for testing
      FE-C trainers that receive HR from a sensor.
- ✅ **HRM pages 0x05-0x07** — DONE (display side): page 5 swim interval
      summary decoding added (interval avg/max, session avg; 0x00
      invalid), pages 6/7 were already covered; page 3 product info
      (hw/sw version, model) decoding added too. TX of pages 5-7
      omitted deliberately: they are rarely broadcast and the emulator
      targets the common trainer test scenario (pages 0-4).

### Low-level ANT protocol (medium/low priority)
- ✅ **Proximity search (0x60)** — DONE: Core.SetProximitySearch and
  Channel.SetProximitySearch (threshold 0 disables, 1..255 signal bins);
  replayed after reconnect; limit search radius to auto-pick the closest
  HR belt among several. NOTE: 0x60 is the modern message id; on Rev 5.1
  devices (ANTUSB2/ANTUSB-m era) proximity search is 0x71 — the node
  picks the right one after protocol detection (see below).
- ✅ **Channel ID lists (0x59 config / 0x5A add)** — DONE:
  Core.SetChannelIDList / Core.AddChannelID and Channel.EnableChannelIDList
  (size) / AddChannelID (device number + type wildcard), hardware filter
  for "listen to these N device IDs" (multi-device scans without RF
  noise); list entries replayed after reconnect.
- ✅ **Advanced burst** — DONE: Core.SetAdvancedBurst / easy
  EnableAdvancedBurst-DisableAdvancedBurst / Channel.SendAdvancedBurst;
  received EXTENDED_BURST_DATA packets are reassembled into regular burst
  events (sequence wrap and terminating short/empty packets handled).
  Message ids differ per protocol revision: Rev 5.1 configures with 0x78
  (packet size enum 8/16/24), modern firmware with 0x61 and carries burst
  data in 0x6E; detection picks automatically.
- ✅ **Channel search sharing** — DONE: Core.SetSearchSharing /
  Channel.SetSearchSharing (cycles per search, 0 disables). Rev 5.1 uses
  message 0x81, modern firmware 0x53; picked automatically.
- ✅ **LIB config** — DONE: Core.SetLIBConfig / Channel.SetLIBConfig with
  the flag bits LIBConfigRxTimestamp (0x20), LIBConfigRSSI (0x40),
  LIBConfigChannelID (0x80): what extended data is appended to received
  data messages. Rev 5.1 message 0x6E, modern 0x71; picked automatically.
- ✅ **Protocol revision detection** — DONE: Core.DetectProtocol (called
  automatically by easy.New*): legacy (Rev 5.1) firmware answers a
  0x61 serial request with the 4-byte serial number, modern firmware
  either answers with the 3-byte advanced burst configuration or
  supports the 0x3F serial request instead. Core.SetProtocolLegacy can
  override. Verified on hardware: the CYCPLUS ANTUSB2 clone answers the
  0x61 request with its serial (legacy mode).
- ✅ **Blood Pressure profile** — DONE (display side): device type 18
  (channel period 8192, RF 57), Measurement Data Page 1 decoding
  (systolic/diastolic u16, MAP, heart rate, raw flags byte, invalid
  markers). The full profile specification (pages 2-5, file transfer
  download) is no longer publicly distributed, so only the broadcast
  measurement page is decoded and flagged as unverified; measurement
  download via ANT-FS client emulation remains out of scope.



## Features (from open issues)

- ✅ **#125 Stride-Based Speed and Distance Monitor** — DONE: profile for
      device type 124 (period 8134), main data page 0x01 (update latency,
      distance, speed, stride count) with invalid-field handling; layout
      matches openant's broadcast_send simulation.
- ✅ **#126 Wind / Track Resistance** — DONE: FE-C pages 0x32 (wind
      resistance: coefficient 0..2.54 kg/m, wind speed ±127 km/h,
      drafting factor) and 0x33 (track resistance: grade ±40 %, rolling
      resistance coefficient 0..0.015), sent as acknowledged data like
      SetTargetPower; unit-tested encoders.
- ✅ **#116 Select USB stick by serial** — DONE: ant.FindDriverForSerial
      and ant.NewDriverForSerial existed; added ant.Serials discovery,
      easy.NewSerial (reconnect factory bound to the same stick), CLI
      `goant sticks` lister and `goant scan -serial/-s`.
- ✅ **#69 End device names** — DONE: devices.ManufacturerName covers
      the ANT+ manufacturer ID registry (as published in the Garmin FIT
      SDK profile); `goant scan` prints the vendor name with device
      updates once common page 80 arrives.
- ✅ **#92 WeightScale profile** — DONE: device type 119 decoding TX pages
      0x01 (weight), 0x02 (hydration/body fat %), 0x03 (metabolic rates),
      0x04 (muscle/bone mass) and 0x3A (user profile: gender/age/height),
      with 0xFFFF/0xFFFE invalid handling. Profile parity with openant is
      now complete.
- ✅ **#66 ANT-FS device scanner** — DONE: `goant antfs-scan` listens for
      ANT-FS beacons on the standard search channel (period 4096, RF 50,
      waveform 0x53,0x00, device type 0x01) and prints every unique device
      (serial, descriptor, pairing/data flags) until Ctrl+C or -timeout;
      backed by the new fs.Application.Incoming.
- ✅ **#83 Bicycle lights TX coverage** — DONE (decoding part): mode
      description page 0x05 decoder per the official ANT+ Bike Lights
      spec Rev 2.0 Table 7-18 (mode number, pattern, segment time,
      duration, colour, 12 two-bit pattern segments; verified against
      the spec's own worked example). No factory pages exist in the
      spec, so there is nothing further to decode; TX already covered
      by SetLight/Disconnect/RequestDP.
- ✅ **#35 Connect to a specific device** — DONE: WaitFound helper on
      every profile: create with the known device ID and block until the
      first broadcast (or timeout). Documented in the package docs.
- ✅ **#67/#91 Multiple dongles** — DONE: ant.Sticks enumerates every
      attached stick (serial, product, bus/addr; serial may be empty on
      broken-descriptor clones), ant.NewDriverForStick / FindDriverForStick
      bind a driver to a specific bus:address, easy.NewStick opens a node
      with reconnect bound to that stick, and `goant scan` gained
      -serials a,b / -all multi-dongle scanning (one goroutine per stick,
      output labelled). `goant sticks` shows bus/addr for serialless
      clones; parallel-nodes test added.
- ✅ **#59 Documentation** — DONE: package docs (doc.go for ant/easy/
      devices/fs), godoc examples (easy, devices), docs/PROTOCOL.md with
      frame format, message flow, ANT-FS summary and firmware quirks. A
      hosted docs site remains open-ended.
- ⬜ CLI `influx` and `mqtt` subcommands (streaming device data to
      InfluxDB/MQTT, incl. `--topic-per-field`/`--device-topic` behaviour)
      — deferred by design decision.
- ✅ udev rules installer — DONE: `goant udev` installs the bundled
      resources/42-ant-usb-sticks.rules (embedded into the binary so
      `go install` builds carry it) into /etc/udev/rules.d/, reloads the
      rules via udevadm and prints the plugdev group / replug next steps;
      `-dest` and `-dry_run` flags for packaging and tests; also
      available as `sudo make install-udev`.

## Robustness (from bug reports)

- ✅ **#81/#14 channel cleanup** — DONE: `RemoveChannel` waits for
      EVENT_CHANNEL_CLOSED (bounded 1 s) before unassigning; verified on
      hardware (no more CHANNEL_IN_WRONG_STATE on shutdown).
- ✅ **#68 nil data / malformed pages** — DONE via review P0 guards plus
      fuzz tests; keep extending corpus.
- ✅ USB "device or resource busy" on rapid re-open — DONE: interface
      claim retries 5×200 ms. Note: contention with other processes
      sharing the stick (e.g. a monitoring daemon) can still exceed this
      window; document single-owner usage.
- ✅ **#51/#122 USB pipe errors** — DONE: automatic re-open/reconnect on
      fatal driver errors in a supervisor goroutine (backoff 500 ms→5 s,
      indefinite until Stop). The driver is swapped before the reconnect
      hook so response waits work; easy.Node replays network keys and
      channel config, then fires OnReconnect. Verified on hardware
      (sysfs unbind/bind of ANTUSB2 on the Pi: restored on attempt 3).
- ✅ **#42/#103 USB timeouts on Raspberry Pi / kernel driver warnings** —
      DONE: usbDriver.SetReadTimeout bounds bulk IN transfers (ant.
      SetDriverReadTimeout / easy.Node.SetReadTimeout; re-applied after
      reconnect; 0 = blocking, the previous behaviour). Kernel driver
      detach failures on Linux now log a warning with the udev rules hint
      instead of being swallowed.
- ⬜ EVENT_TX not reported by ANTUSB2 firmware BLJ06.01.01 in master mode
      (verified: openant 1.3.4 receives none either). LIB config is now
      implemented but only controls extended RX message content; the
      remaining hypothesis is event buffering configuration for master
      channels. Broadcast data itself transmits fine (easy.Channel ships
      an EVENT_TX fallback ticker since 0.1.1).
- ✅ **#6/#111 Missed readings** — DONE: buffer caps (burst 1 MiB, event
      buffer drop-oldest) plus Core.Metrics() drop/error counters (bad
      frames, dropped bursts, read/write errors, reconnects), exposed via
      easy.Node.Metrics().
- ⬜ **#39 set_time on Garmin vívofit** — investigate TAI offset handling
      (`fs.SetTime` applies +35 s; may need device quirks).
- ⬜ **#119 Timezone handling** — document/verify that all timestamps are
      UTC; optionally allow local-time rendering in CLI.
- ⬜ **#117 Influx list fields** — pointer/array fields (calculated speed
      etc.) need stable serialisation for the future influx CLI.
- ⬜ **#109 datetime out of bounds** — guarded in `tryDateTime`; fuzz
      tests added, keep corpus growing.
- ✅ **#84/#44 BSC sensors not connecting** — DONE (documentation and
      verified parameters): channel parameters match the ANT+ Bike Speed
      and Cadence profile and python openant exactly (speed: 123/8118,
      cadence: 122/8102, combined: 121/8086, RF 57). Documented the real
      causes reported in the issues in "Connecting to speed / cadence
      sensors" (devices package docs): the printed sensor number often is
      not the ANT+ device number (discover with goant scan), transType 0
      wildcard is preferred because sensors flip the pairing/LSN bit on
      re-pairing, combo vs separate device types, motion wake-up and the
      indefinite default search timeout.
- ✅ **#40 serial port permission errors** — DONE: USB and serial open
      failures caused by missing permissions are wrapped in
      ant.ErrPermission with an actionable hint (udev rules / dialout /
      plugdev group).
- ⬜ **#21 UploadDataCommand truncated args** — verify UploadData parsing
      against a wider set of devices.

## Port quality

- ✅ openant bugs fixed during the port (regression-tested):
      - `advanced_options_three` read from byte 6 (not 4),
      - `DownloadRequest` field widths kept openant-compatible, document
        spec difference (u32 crc seed / u16 max block),
      - `CreateFile` command pipe parsing (openant crashes on it),
      - `shift` page 0x03 out-of-bounds read,
      - `controls_device` 0x47 reply payload assembly.
- ⬜ Continuous fuzzing in CI (longer budgets, corpus caching).
- ⬜ Benchmarks for the reader loop and page decoders.
- ✅ GitHub Actions CI (ubuntu): gofmt, vet, staticcheck 2025.1.1,
      `go test -race`, build examples+CLI; branch protection requires the
      "test" check on PRs (macOS dropped — not a target architecture).
- ⬜ golangci-lint as an additional CI linter (staticcheck already covers
      the core).

