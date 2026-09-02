# TODO

Backlog for openant-go, derived from the issue tracker of the Python
[openant](https://github.com/Tigge/openant) library and from code reviews
of the Go port. Issue numbers reference Tigge/openant.

## Code review 2026-08-31 (PR #1: REVIEW.MD + REVIEW2.MD)

Working through the review findings (17 confirmed, 3 refuted — details in
the PR). Fixed items are checked and reference the commit topics.

### P0 — panics/DoS on RF/USB-controlled data
- [x] Short beacon slice in `fs.Application.onData` (P0-1)
- [x] `baseDevice.onData`: extended block `len >= 13`, common pages
      `len >= 8` (P0-2)
- [x] `Scanner.scanData`: `len >= 13` (P0-3)
- [x] `Download`: uint64 math, offset/size validation, 64 MiB cap (P0-4)
- [x] `Upload`: offset/block-size validation, uint64 math (P0-5)
- [x] `recover()` around user callbacks in `easy.Node.Run` (P0-6)
- [x] Fuzz tests: `ParseFrame`, `ParseBeacon`, `ParseCommand`,
      `ParsePipeCommand`, `ParseDirectory`, `Application.onData`,
      `baseDevice.onData`, `Scanner.scanData` (P0-7) — no crashes found
      in short runs; extend corpus/CI time

### P1 — concurrency, lifecycle, numeric correctness
- [x] `SerialDriver` port race — mutex added (P1-8)
- [x] Reader busy-spin on persistent driver errors — exponential backoff
      (P1-9; also mitigates openant #51/#122 until full reconnect exists)
- [x] `NewApplication` deadlock on `SetupChannel` error (P1-10)
- [x] Modular uint16 delta for crank/wheel period — openant-inherited
      precedence bug zeroing torque/power (P1-11)
- [x] `shift` function-set event type mask `0x30` (P1-12)
- [x] `LevErrorMessageFromByte` inversion (P1-13)

### P2 — protocol API, buffers
- [x] `Message.Validate` + rejection in `Core.Write`/`WriteTimeslot` (P2-14)
- [x] `ReadJSONDevices` reads `{"devices":[...]}` like `Scanner.Save`
      and openant (P2-15)
- [x] Caps: burst reassembly 1 MiB, event buffer 1024 drop-oldest (P2-16)

### P3 — cosmetics
- [x] `errors.Is` in `consume()` (dead `ErrBadSync` branch) (P3-17)
- [x] Dead code removed: `uint16LE`/`uint32LE`, `snapshot` (P3-19)
- [x] udev `0660` + plugdev instead of `0666` (P3-20)
- [x] README: Go >= 1.25 (P3-21)

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

## Features (from open issues)

- [x] **#125 Stride-Based Speed and Distance Monitor** — DONE: profile for
      device type 124 (period 8134), main data page 0x01 (update latency,
      distance, speed, stride count) with invalid-field handling; layout
      matches openant's broadcast_send simulation.
- [x] **#126 Wind / Track Resistance** — DONE: FE-C pages 0x32 (wind
      resistance: coefficient 0..2.54 kg/m, wind speed ±127 km/h,
      drafting factor) and 0x33 (track resistance: grade ±40 %, rolling
      resistance coefficient 0..0.015), sent as acknowledged data like
      SetTargetPower; unit-tested encoders.
- [x] **#116 Select USB stick by serial** — DONE: ant.FindDriverForSerial
      and ant.NewDriverForSerial existed; added ant.Serials discovery,
      easy.NewSerial (reconnect factory bound to the same stick), CLI
      `goant sticks` lister and `goant scan -serial/-s`.
- [x] **#69 End device names** — DONE: devices.ManufacturerName covers
      the ANT+ manufacturer ID registry (as published in the Garmin FIT
      SDK profile); `goant scan` prints the vendor name with device
      updates once common page 80 arrives.
- [x] **#92 WeightScale profile** — DONE: device type 119 decoding TX pages
      0x01 (weight), 0x02 (hydration/body fat %), 0x03 (metabolic rates),
      0x04 (muscle/bone mass) and 0x3A (user profile: gender/age/height),
      with 0xFFFF/0xFFFE invalid handling. Profile parity with openant is
      now complete.
- [ ] **#66 ANT-FS device scanner** — CLI (`goant antfs-scan`) to discover
      ANT-FS beacons alongside `scan`.
- [ ] **#83 Bicycle lights TX coverage** — mode description page (0x05)
      decoding, factory configs.
- [ ] **#35 Connect to a specific device** — helper on profiles to attach
      by serial/name (channel reattach logic exists; needs friendly API).
- [ ] **#67/#91 Multiple dongles** — support several concurrent nodes in
      CLI tools with one goroutine per stick.
- [x] **#59 Documentation** — DONE: package docs (doc.go for ant/easy/
      devices/fs), godoc examples (easy, devices), docs/PROTOCOL.md with
      frame format, message flow, ANT-FS summary and firmware quirks. A
      hosted docs site remains open-ended.
- [ ] CLI `influx` and `mqtt` subcommands (streaming device data to
      InfluxDB/MQTT, incl. `--topic-per-field`/`--device-topic` behaviour)
      — deferred by design decision.
- [ ] udev rules installer command (`goant udev` or `make install-udev`).

## Robustness (from bug reports)

- [x] **#81/#14 channel cleanup** — DONE: `RemoveChannel` waits for
      EVENT_CHANNEL_CLOSED (bounded 1 s) before unassigning; verified on
      hardware (no more CHANNEL_IN_WRONG_STATE on shutdown).
- [x] **#68 nil data / malformed pages** — DONE via review P0 guards plus
      fuzz tests; keep extending corpus.
- [x] USB "device or resource busy" on rapid re-open — DONE: interface
      claim retries 5×200 ms. Note: contention with other processes
      sharing the stick (e.g. a monitoring daemon) can still exceed this
      window; document single-owner usage.
- [x] **#51/#122 USB pipe errors** — DONE: automatic re-open/reconnect on
      fatal driver errors in a supervisor goroutine (backoff 500 ms→5 s,
      indefinite until Stop). The driver is swapped before the reconnect
      hook so response waits work; easy.Node replays network keys and
      channel config, then fires OnReconnect. Verified on hardware
      (sysfs unbind/bind of ANTUSB2 on the Pi: restored on attempt 3).
- [ ] **#42/#103 USB timeouts on Raspberry Pi / kernel driver warnings** —
      configurable read timeout, clearer log messages when the kernel
      driver cannot be detached (needs udev rules hint).
- [ ] EVENT_TX not reported by ANTUSB2 firmware BLJ06.01.01 in master mode
      (verified: openant 1.3.4 receives none either). Investigate
      CONFIG_EVENT_BUFFERING / LIB_CONFIG (0x6E) enabling of TX event
      reporting for master channels; broadcast data itself transmits fine.
- [ ] **#6/#111 Missed readings** — buffer caps landed (burst 1 MiB, event
      buffer drop-oldest); remaining: instrument drop counters/metrics.
- [ ] **#39 set_time on Garmin vívofit** — investigate TAI offset handling
      (`fs.SetTime` applies +35 s; may need device quirks).
- [ ] **#119 Timezone handling** — document/verify that all timestamps are
      UTC; optionally allow local-time rendering in CLI.
- [ ] **#117 Influx list fields** — pointer/array fields (calculated speed
      etc.) need stable serialisation for the future influx CLI.
- [ ] **#109 datetime out of bounds** — guarded in `tryDateTime`; fuzz
      tests added, keep corpus growing.
- [ ] **#84/#44 BSC sensors not connecting** — document wildcard vs exact
      device id usage; verify period/transmission type quirks.
- [ ] **#40 serial port permission errors** — friendlier error message
      pointing to the udev rules on Linux.
- [ ] **#21 UploadDataCommand truncated args** — verify UploadData parsing
      against a wider set of devices.

## Port quality

- [x] openant bugs fixed during the port (regression-tested):
      - `advanced_options_three` read from byte 6 (not 4),
      - `DownloadRequest` field widths kept openant-compatible, document
        spec difference (u32 crc seed / u16 max block),
      - `CreateFile` command pipe parsing (openant crashes on it),
      - `shift` page 0x03 out-of-bounds read,
      - `controls_device` 0x47 reply payload assembly.
- [ ] Continuous fuzzing in CI (longer budgets, corpus caching).
- [ ] Benchmarks for the reader loop and page decoders.
- [x] GitHub Actions CI (ubuntu): gofmt, vet, staticcheck 2025.1.1,
      `go test -race`, build examples+CLI; branch protection requires the
      "test" check on PRs (macOS dropped — not a target architecture).
- [ ] golangci-lint as an additional CI linter (staticcheck already covers
      the core).

