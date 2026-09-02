package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
)

const version = "0.1.1"

func deviceTypeName(id int) string { return devices.DeviceType(id).String() }

type scanArgs struct {
	outfile    string
	deviceType string
	deviceID   int
	autoCreate bool
	logLevel   string
	serial     string
	serials    string
	allSticks  bool
}

// stickLauncher describes one node to run: how to open it and how to
// label its output when several sticks scan in parallel (openant issues
// #67/#91).
type stickLauncher struct {
	label string
	open  func() (*easy.Node, error)
}

// resolveSticks builds the launcher list from the -serial, -serials and
// -all flags. Sticks are picked either by serial number or, for sticks
// with unreadable serial descriptors, by "bus:addr" spec.
func resolveSticks(a scanArgs) ([]stickLauncher, error) {
	if a.serials == "" && !a.allSticks {
		return []stickLauncher{{
			label: "",
			open:  func() (*easy.Node, error) { return easy.NewSerial(a.serial) },
		}}, nil
	}
	var specs []string
	if a.allSticks {
		for _, info := range ant.Sticks() {
			specs = append(specs, info.Serial, fmt.Sprintf("%d:%d", info.Bus, info.Address))
		}
		if len(specs) == 0 {
			return nil, fmt.Errorf("no ANT sticks attached")
		}
	} else {
		for _, s := range strings.Split(a.serials, ",") {
			if s = strings.TrimSpace(s); s != "" {
				specs = append(specs, s)
			}
		}
	}
	infos := ant.Sticks()
	var out []stickLauncher
	seen := make(map[ant.StickInfo]bool)
	for _, spec := range specs {
		info, err := matchStick(infos, spec)
		if err != nil {
			return nil, err
		}
		if seen[info] {
			continue
		}
		seen[info] = true
		label := info.Serial
		if label == "" {
			label = fmt.Sprintf("%d:%d", info.Bus, info.Address)
		}
		out = append(out, stickLauncher{
			label: label,
			open:  func() (*easy.Node, error) { return easy.NewStick(info) },
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no matching ANT sticks; list them with 'goant sticks'")
	}
	return out, nil
}

// matchStick finds a stick by serial number or "bus:addr" spec.
func matchStick(infos []ant.StickInfo, spec string) (ant.StickInfo, error) {
	for _, info := range infos {
		if info.Serial == spec {
			return info, nil
		}
		bus, addr, ok := strings.Cut(spec, ":")
		if !ok {
			continue
		}
		b, errB := strconv.Atoi(strings.TrimSpace(bus))
		ad, errA := strconv.Atoi(strings.TrimSpace(addr))
		if errB == nil && errA == nil && info.Bus == b && info.Address == ad {
			return info, nil
		}
	}
	return ant.StickInfo{}, fmt.Errorf("no ANT stick matching %q (see 'goant sticks')", spec)
}

func runScan(argv []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Scan for nearby devices and optionally print device data.

Usage:

	goant scan [options]

Options:
`)
		fs.PrintDefaults()
	}
	var a scanArgs
	fs.StringVar(&a.outfile, "outfile", "", "capture devices found to a json file for use with other tools")
	fs.StringVar(&a.outfile, "o", "", "shorthand for -outfile")
	fs.StringVar(&a.deviceType, "device_type", "Unknown", "device type to search for: "+deviceTypeNames())
	fs.StringVar(&a.deviceType, "t", "Unknown", "shorthand for -device_type")
	fs.IntVar(&a.deviceID, "device_id", 0, "device id to search for (0 = all)")
	fs.IntVar(&a.deviceID, "i", 0, "shorthand for -device_id")
	fs.BoolVar(&a.autoCreate, "auto_create", false, "instantiate device object when found so that device data is also printed")
	fs.BoolVar(&a.autoCreate, "a", false, "shorthand for -auto_create")
	fs.StringVar(&a.logLevel, "logging", "WARN", "log level: DEBUG, INFO, WARN, ERROR")
	fs.StringVar(&a.serial, "serial", "", "serial number of the USB stick to use (see 'goant sticks'); empty = first found")
	fs.StringVar(&a.serial, "s", "", "shorthand for -serial")
	fs.StringVar(&a.serials, "serials", "", "comma-separated list of sticks to scan on (serial numbers or bus:addr); enables multi-dongle scanning")
	fs.BoolVar(&a.allSticks, "all", false, "scan on every attached ANT stick (multi-dongle)")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	if a.serials != "" && a.serial != "" {
		return fmt.Errorf("-serial and -serials are mutually exclusive")
	}
	if a.allSticks && a.outfile != "" {
		return fmt.Errorf("-outfile is not supported with -all (use it with a single stick)")
	}

	level := slog.LevelWarn
	switch a.logLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR", "CRITICAL":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	dt := devices.DeviceTypeByName(a.deviceType)
	if a.deviceType != "Unknown" && dt == devices.DeviceTypeUnknown {
		return fmt.Errorf("unknown device type %q (choose from %s)", a.deviceType, deviceTypeNames())
	}

	launchers, err := resolveSticks(a)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(launchers) == 1 {
		return scanNode(ctx, launchers[0], a, dt, logger)
	}

	// Multi-dongle: one goroutine per stick, output labelled by stick.
	fmt.Printf("Scanning on %d sticks\n", len(launchers))
	var wg sync.WaitGroup
	for _, l := range launchers {
		wg.Add(1)
		l := l
		go func() {
			defer wg.Done()
			if err := scanNode(ctx, l, a, dt, logger); err != nil {
				fmt.Fprintf(os.Stderr, "[%s] scan stopped: %v\n", l.label, err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// scanNode runs the scanner loop on one node.
func scanNode(ctx context.Context, l stickLauncher, a scanArgs, dt devices.DeviceType, logger *slog.Logger) error {
	node, err := l.open()
	if err != nil {
		return fmt.Errorf("open stick: %w", err)
	}
	defer node.Stop()
	if err := node.SetNetworkKey(0x00, devices.ANTPLUS_NETWORK_KEY); err != nil {
		return err
	}
	p := func(format string, args ...any) {
		if l.label != "" {
			fmt.Printf("[%s] "+format+"\n", append([]any{l.label}, args...)...)
			return
		}
		fmt.Printf(format+"\n", args...)
	}

	var autoDev any
	scanner, err := devices.NewScanner(node, a.deviceID, dt, 0)
	if err != nil {
		return err
	}
	scanner.OnScanFound = func(t devices.DeviceTuple) {
		p("Found new device %s", t.String())
		if a.autoCreate && autoDev == nil && t.Type != int(devices.DeviceTypeUnknown) {
			// Attach a concrete profile for the first matching device.
			d, err := devices.AutoCreateDevice(node, t.ID, devices.DeviceType(t.Type), t.Trans)
			if err != nil {
				logger.Warn("auto create failed", "error", err)
				return
			}
			autoDev = d
			if dd, ok := d.(interface {
				SetOnDeviceData(func(int, string, devices.DeviceData))
			}); ok {
				dd.SetOnDeviceData(func(page int, name string, data devices.DeviceData) {
					p("DeviceData %v page %d (%s): %+v", devices.DeviceType(t.Type), page, name, data)
				})
			}
			logger.Info("auto created device", "type", devices.DeviceType(t.Type).String(), "id", t.ID)
		}
	}
	scanner.OnScanUpdate = func(t devices.DeviceTuple, c devices.CommonData) {
		// Print the vendor name once the device sent common page 80
		// (openant issue #69).
		if c.ManufacturerID != 0 && c.ManufacturerID != 0xFFFF {
			p("Device %s (%s) update: %+v", t.String(), devices.ManufacturerName(c.ManufacturerID), c)
			return
		}
		p("Device %s update: %+v", t.String(), c)
	}

	done := node.Start(ctx)
	<-done

	if a.outfile != "" {
		if err := scanner.Save(a.outfile); err != nil {
			return err
		}
		p("Saved %d devices to %s", len(scanner.FoundDevices()), a.outfile)
	}
	return nil
}
