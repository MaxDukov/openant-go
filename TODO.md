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
- ⬜ **FE-C Command Status page 0x47 RX** — decode last-command verdict
  (mode, sequence, status pass/pending/failed) like python openant
  fe.py; warn on failed commands. Useful: without it there is no
  feedback whether the trainer accepted SetTargetPower etc. (enum
  FECommandStatus already exists, the onData branch does not).
- ⬜ **FE-C User Configuration page 0x46 TX** — cyclist weight (0.01 kg
  LSB, 655.35 kg max), wheel diameter (1 mm LSB), gear ratio (0.03);
  sent after attach (needs an acknowledged write + optional RequestDP
  retry loop). Useful: many trainers give wrong resistance/power
  simulation without it.
- ⬜ **FE-C Capabilities page 0x48 RX** — which resistance modes the
  trainer supports (basic/target power/wind/track/user config) and its
  max power. Useful: pick the right command page automatically.
- ⬜ **FE-C Metabolic page 0x12 RX** — instantaneous/net calories
  (health-related: kcal burn during a workout).
- ⬜ **FE-C Trainer Torque page 0x13 RX** — accumulated torque + cycles;
  lets a client compute true power independent of the power meter page.

### Heart rate / health (high priority)
- ⬜ **HRM master (sensor emulation) mode** — HR/battery TX pages 0x00-
  0x04 on a master channel (baseDevice already has master support);
  useful for testing FE-C trainers that receive HR from a "sensor" and
  for simulating a belt without hardware.
- ⬜ **HRM pages 0x05-0x07 (sport/profile settings)** — optional
  sport type, HR zone config; rarely broadcast, low value.

### Low-level ANT protocol (medium/low priority)
- ⬜ **Proximity search (0x60)** — limit search radius in dB; useful to
  auto-pick the closest HR belt among several.
- ⬜ **Channel ID lists (0x59 config / 0x5A add)** — hardware filter for
  "listen to these N device IDs" (multi-device scans without RF noise).
- ⬜ **Advanced burst config (0x6E)** — longer burst payloads; speeds up
  ANT-FS file downloads from trainers/watches up to ~4x.
- ⬜ **Channel search sharing (0x71)** — several channels share one
  search (battery/bandwidth saving with many channels).
- ⬜ **LIB config (0x70)** — hardware filtering of non-matching traffic.
- ⬜ **Blood Pressure profile** — device type 42 (measurement download
  over ANT-FS client); low value: requires ANT-FS client emulation and
  very few devices broadcast it.



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
- ⬜ udev rules installer command (`goant udev` or `make install-udev`).

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
- ⬜ **#42/#103 USB timeouts on Raspberry Pi / kernel driver warnings** —
      configurable read timeout, clearer log messages when the kernel
      driver cannot be detached (needs udev rules hint).
- ⬜ EVENT_TX not reported by ANTUSB2 firmware BLJ06.01.01 in master mode
      (verified: openant 1.3.4 receives none either). Investigate
      CONFIG_EVENT_BUFFERING / LIB_CONFIG (0x6E) enabling of TX event
      reporting for master channels; broadcast data itself transmits fine.
- ⬜ **#6/#111 Missed readings** — buffer caps landed (burst 1 MiB, event
      buffer drop-oldest); remaining: instrument drop counters/metrics.
- ⬜ **#39 set_time on Garmin vívofit** — investigate TAI offset handling
      (`fs.SetTime` applies +35 s; may need device quirks).
- ⬜ **#119 Timezone handling** — document/verify that all timestamps are
      UTC; optionally allow local-time rendering in CLI.
- ⬜ **#117 Influx list fields** — pointer/array fields (calculated speed
      etc.) need stable serialisation for the future influx CLI.
- ⬜ **#109 datetime out of bounds** — guarded in `tryDateTime`; fuzz
      tests added, keep corpus growing.
- ⬜ **#84/#44 BSC sensors not connecting** — document wildcard vs exact
      device id usage; verify period/transmission type quirks.
- ⬜ **#40 serial port permission errors** — friendlier error message
      pointing to the udev rules on Linux.
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

