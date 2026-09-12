# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Planned work is tracked in [TODO.md](TODO.md).

### Added

- Phased roadmap: robustness/technical debt, protocol/profiles, ecosystem.
- README demo GIF of a live `goant scan` finding a heart rate monitor.

### Fixed

- `goant sticks` and scan labels printed raw control bytes from broken
  USB serial descriptors (NULs garbled terminal output); they are now
  stripped (`ant.SanitizeSerial`).

## [0.1.2] - 2026-09

### Added

- `goant influx` and `goant mqtt` streaming subcommands (InfluxDB v1/v2
  line protocol, MQTT JSON events, topic-per-field mode).
- `goant udev` installer for the bundled ANT USB stick udev rules
  (embedded into the binary).
- Bike Speed and Cadence connection troubleshooting documentation.

### Changed

- README refresh covering the 0.1.x feature set.

## [0.1.1] - 2026-09

### Added

- Multi-dongle support: `ant.Sticks`, stick selection by serial or
  bus:addr, parallel multi-stick scanning (`goant scan -all`).
- `goant antfs-scan` — passive ANT-FS beacon listener.
- `WaitFound` helper on every device profile.
- Automatic USB reconnect with configuration replay and `OnReconnect` hook.
- Configurable USB read timeout (`easy.Node.SetReadTimeout`).
- Full FE-C coverage per ANT+ fitness equipment Rev 5.0: command status
  (0x47), user configuration (0x37), capabilities (0x36), metabolic (0x12),
  treadmill (0x13), trainer status, wind (0x32) and track (0x33)
  resistance, corrected trainer torque (0x1A) field layout.
- HRM sensor emulation (`devices.NewHeartRateMaster`) and remaining HR
  pages (swim interval summary, product info).
- Proximity search, channel ID lists, read timeouts and core metrics.
- Stride Speed and Distance Monitor and Weight Scale profiles —
  profile parity with openant complete.
- Bike Lights mode description page decoding.
- Manufacturer name registry; `goant sticks` lister.

### Fixed

- Security hardening of all device/RF-controlled input paths (bounds
  checks, uint64-safe ANT-FS math, recover() around user callbacks).
- Concurrency, lifecycle and numeric bugs from the 2026-08 code review.
- Clearer errors when a stick cannot be selected by serial.
- GitHub Actions CI for PRs and pushes.

## [0.1.0] - 2026-08

### Added

- Initial release: ANT base (framing, USB/serial drivers, event
  pipeline), easy blocking API, ANT-FS (link/auth/transport, download,
  upload, erase), ANT+ device profiles, `anttest` simulator, `goant`
  CLI (`scan`, `sticks`), 14 example applications.

[Unreleased]: https://github.com/MaxDukov/openant-go/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/MaxDukov/openant-go/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/MaxDukov/openant-go/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/MaxDukov/openant-go/releases/tag/v0.1.0
