# openant-go

ANT and ANT-FS library for Go — a port of the Python
[openant](https://github.com/Tigge/openant) library.

> A note on ANT/ANT-FS/ANT+: this library is for development and testing of
> devices and is not intended to be used as a reference. Refer to
> [thisisant.com](https://www.thisisant.com/) for full ANT documentation and
> ANT+ device profiles. It is not an official tool.

## Features

- ANT base interface (framing, USB/serial drivers, event pipeline):
  multi-dongle support (`ant.Sticks`, selection by serial or bus:addr),
  configurable USB read timeouts, drop/error metrics
  (`ant.Core.Metrics`), proximity search, channel ID lists, channel
  search sharing, LIB config (RSSI/timestamp/channel-ID extended data)
  and advanced burst transfers — with automatic protocol-revision
  detection (Rev 5.1 vs modern message ids).
- ANT-FS (command pipe, directory listings, download, upload, erase, ...).
- ANT+ device profiles and a base type for custom ones (`devices`),
  including blood pressure measurement decoding.
- Device emulation (master mode): heart rate belt
  (`devices.NewHeartRateMaster`), trainer control (FE-C target power,
  wind/track resistance, user config, capabilities), generic broadcast
  masters.
- Four packages mirroring openant:
  - `ant` — basic ANT library (openant.base),
  - `easy` — blocking interface with callbacks (openant.easy),
  - `fs` — ANT-FS library (openant.fs),
  - `devices` — ANT+ profiles (openant.devices).
- `anttest` — scriptable in-memory driver and stick simulator for tests.
- CLI `goant` (`scan`, `sticks`, `antfs-scan`, `influx`, `mqtt`, `udev`,
  `version`).
- 14 example applications under `examples/` (ports of openant's examples).

## Requirements

- Go >= 1.25 with cgo enabled and libusb installed (macOS:
  `brew install libusb`, Debian/Ubuntu: `sudo apt install libusb-1.0-0-dev`).
- An ANT USB stick (optional for tests):
  - ANTUSB2 (0fcf:1008) or ANTUSB-m (0fcf:1009),
  - serial/CDC sticks (0fcf:1004) on Linux.
- On Linux, install `resources/42-ant-usb-sticks.rules` into
  `/etc/udev/rules.d/` so the sticks are usable without root — the CLI
  does it for you: `sudo goant udev` (or `sudo make install-udev`).

## Installation

```sh
go get github.com/maxdukov/openant-go
```

Package documentation: https://pkg.go.dev/github.com/maxdukov/openant-go

## Usage

```go
node, err := easy.New()
if err != nil {
    log.Fatal(err)
}
defer node.Stop()
node.SetNetworkKey(0x00, devices.ANTPLUS_NETWORK_KEY)

hr, err := devices.NewHeartRate(node, 0 /* first found */, 0)
if err != nil {
    log.Fatal(err)
}
hr.OnDeviceData = func(page int, name string, data devices.DeviceData) {
    fmt.Printf("Heart rate: %d bpm\n", data.(devices.HeartRateData).HeartRate)
}

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
node.Run(ctx) // blocking dispatch loop
```

Sensor emulation (master mode) is just as compact:

```go
hrm, err := devices.NewHeartRateMaster(node, 0 /* random device id */)
if err != nil {
    log.Fatal(err)
}
hrm.SetHeartRate(150)
```

See `examples/` for the full set (heart rate, scanner, ANT-FS listing,
master-mode broadcast, continuous scan, trainer workouts, ...).

## CLI

```sh
go install github.com/maxdukov/openant-go/cmd/goant@latest

goant version                  # print version
goant sticks                   # list attached ANT sticks (serial, bus:addr)
goant scan                     # print devices found to the terminal
goant scan -auto_create        # also print device data pages
goant scan -t HeartRate        # search only heart rate monitors
goant scan -i 12345            # search a specific device id
goant scan -s <serial>         # scan on a specific stick
goant scan -all                # scan on every attached stick (multi-dongle)
goant scan -o devices.json     # save found devices to a file
goant antfs-scan               # listen for ANT-FS beacons (file transfers)
sudo goant udev                # install udev rules for the sticks (Linux)
goant influx -db ant HeartRate # stream an HRM to InfluxDB (v1 API)
goant influx -token <tok> -bucket ant -org me BikeSpeedCadence
goant mqtt -host broker.local HeartRate   # JSON events on openant/HeartRate/<id>
goant mqtt -topic-per-field -device-topic HeartRate:123:sensors/hr HeartRate
goant influx -config devices.json -all    # stream a saved device list
```

Sticks without a readable USB serial (some CYCPLUS clones) are addressed
by bus:addr, e.g. `goant scan -serials 1:5`.

## Testing

```sh
make test          # unit tests (simulator, no hardware)
make test-race     # with the race detector
make integration   # requires a real ANT USB stick and ANT_TEST_USB_STICK=1
```

The unit tests run entirely against the `anttest.SimDriver` stick
simulator; hardware is only needed for the `integration` build tag.

## Design notes (vs. the Python original)

- Threads → goroutines; `queue.Queue`/`deque+Condition` → channels and a
  mutex-protected event buffer with broadcast notification.
- Stop flags → `context.Context` / `atomic.Bool` / channel close.
- All binary parsers return errors instead of panicking (openant uses
  `assert`), and the frame reader resynchronises on bad sync/checksum.
- Known openant bugs fixed: capabilities byte 6, `CreateFile` pipe parsing,
  `shift` page 3 out-of-bounds read, `controls_device` 0x47 reply payload,
  races on capability fields.
- Callback overriding (Python subclassing) → hook function fields and
  functional options (`fs.WithOnTransport`, ...).

## License

MIT — same as openant.
