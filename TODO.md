# TODO

Backlog for openant-go, derived from the issue tracker of the Python
[openant](https://github.com/Tigge/openant) library and from gaps in the
initial Go port. Issue numbers reference Tigge/openant.

## Features (from open issues)

- [ ] **#125 Stride-Based Speed and Distance Monitor** — implement device
      profile for device type 124 (only simulated in `broadcast-send` today).
- [ ] **#126 Wind / Track Resistance** — add FE pages 0x32 (wind) and 0x33
      (track) including `set_wind_resistance` / `set_track_resistance`
      commands (page 0x36/0x37 TX).
- [ ] **#92 WeightScale profile** — device type 119 (weight, hydration,
      metabolic equivalents).
- [ ] **#66 ANT-FS device scanner** — CLI (`goant antfs-scan`) to discover
      ANT-FS beacons alongside `scan`.
- [ ] **#83 Bicycle lights TX coverage** — mode description page (0x05)
      decoding, factory configs.
- [ ] **#69 End device names** — database mapping (manufacturer id, model)
      to human readable names for the scanner/CLI output.
- [ ] **#35 Connect to a specific device** — helper on profiles to attach
      by serial/name (channel reattach logic exists; needs friendly API).
- [ ] **#116 Select USB stick by serial** — `ant.FindDriverForSerial`
      exists; expose it in the CLI (`--serial`) and `easy.New` options.
- [ ] **#67/#91 Multiple dongles** — support several concurrent nodes in
      CLI tools with one goroutine per stick.
- [ ] **#59 Documentation** — package docs site, protocol notes, godoc
      examples.
- [ ] CLI `influx` and `mqtt` subcommands (streaming device data to
      InfluxDB/MQTT, incl. `--topic-per-field`/`--device-topic` behaviour)
      — deferred by design decision.
- [ ] udev rules installer command (`goant udev` or `make install-udev`).

## Robustness (from bug reports)

- [ ] **#51/#122 USB pipe errors** — automatic device re-open/reconnect on
      `usb read/write` failures (LIBUSB_PIPE / IO errors) in the reader
      loop, with backoff.
- [ ] **#42/#103 USB timeouts on Raspberry Pi / kernel driver warnings** —
      configurable read timeout, clearer log messages when the kernel
      driver cannot be detached (needs udev rules hint).
- [ ] **#6/#111 Missed readings / filter improvements** — event buffer
      growth policy; optionally drop-wait strategies for slow consumers.
- [ ] **#39 set_time on Garmin vívofit** — investigate TAI offset handling
      (`fs.SetTime` applies +35 s; may need device quirks).
- [ ] **#119 Timezone handling** — document/verify that all timestamps are
      UTC; optionally allow local-time rendering in CLI.
- [ ] **#117 Influx list fields** — pointer/array fields (calculated speed
      etc.) need stable serialisation for the future influx CLI.
- [ ] **#109 datetime out of bounds** — guarded in `tryDateTime` already;
      add fuzz tests for all page parsers.
- [ ] **#84/#44 BSC sensors not connecting** — document wildcard vs exact
      device id usage; verify period/transmission type quirks.
- [ ] **#40 serial port permission errors** — friendlier error message
      pointing to the udev rules on Linux.
- [ ] USB re-open flakiness after abrupt process termination (SIGKILL of a
      running scan left libusb unable to re-claim the interface briefly) —
      add a short retry/backoff in `usbDriver.Open`.
- [ ] **#68 nil data** — audit every parser for short/malformed pages
      (fuzz test goal: no panics, all `error` returns).
- [ ] **#14/#81 channel cleanup** — CloseChannel ordering (close → wait
      EVENT_CHANNEL_CLOSED → unassign) to avoid CHANNEL_IN_WRONG_STATE.
- [ ] **#21 UploadDataCommand truncated args** — verify UploadData parsing
      against a wider set of devices.

## Port quality

- [ ] Fix openant bug carried over deliberately: none known; but add
      regression tests for the ones fixed during the port:
      - `advanced_options_three` read from byte 6 (not 4),
      - `DownloadRequest` field widths kept openant-compatible, document
        spec difference (u32 crc seed / u16 max block),
      - `CreateFile` command pipe parsing (openant crashes on it),
      - `shift` page 0x03 out-of-bounds read,
      - `controls_device` 0x47 reply payload assembly.
- [ ] Continuous fuzzing (`go-fuzz`/native Fuzz) for `ant.ParseFrame`,
      `fs.ParseCommand`, `fs.ParsePipeCommand`, `fs.ParseDirectory` and
      device page decoders.
- [ ] Benchmarks for the reader loop and page decoders.
- [ ] GitHub Actions CI: vet, test -race, build examples, golangci-lint.
