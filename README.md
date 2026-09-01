# openant-go

ANT and ANT-FS library for Go — a port of the Python
[openant](https://github.com/Tigge/openant) library.

> A note on ANT/ANT-FS/ANT+: this library is for development and testing of
> devices and is not intended to be used as a reference. Refer to
> [thisisant.com](https://www.thisisant.com/) for full ANT documentation and
> ANT+ device profiles. It is not an official tool.

## Features

- ANT base interface (framing, USB/serial drivers, event pipeline).
- ANT-FS (command pipe, directory listings, download, upload, erase, ...).
- ANT+ device profiles and a base type for custom ones (`devices`).
- Four packages mirroring openant:
  - `ant` — basic ANT library (openant.base),
  - `easy` — blocking interface with callbacks (openant.easy),
  - `fs` — ANT-FS library (openant.fs),
  - `devices` — ANT+ profiles (openant.devices).
- `anttest` — scriptable in-memory driver and stick simulator for tests.
- CLI `goant` with the `scan` subcommand (influx/mqtt: see TODO.md).
- 14 example applications under `examples/` (ports of openant's examples).

## Requirements

- Go >= 1.25 with cgo enabled and libusb installed (macOS:
  `brew install libusb`, Debian/Ubuntu: `sudo apt install libusb-1.0-0-dev`).
- An ANT USB stick (optional for tests):
  - ANTUSB2 (0fcf:1008) or ANTUSB-m (0fcf:1009),
  - serial/CDC sticks (0fcf:1004) on Linux.
- On Linux, install `resources/42-ant-usb-sticks.rules` into
  `/etc/udev/rules.d/` so the sticks are usable without root.

## Installation

```sh
go get github.com/maxdukov/openant-go
```

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

See `examples/` for the full set (heart rate, scanner, ANT-FS listing,
master-mode broadcast, continuous scan, trainer workouts, ...).

## CLI

```sh
go install github.com/maxdukov/openant-go/cmd/goant@latest

goant scan                     # print devices found to the terminal
goant scan --auto_create       # also print device data pages
goant scan -t HeartRate        # search only heart rate monitors
goant scan -o devices.json     # save found devices to a file
```

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
