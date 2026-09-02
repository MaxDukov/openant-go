package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
)

const version = "0.1.0"

func deviceTypeName(id int) string { return devices.DeviceType(id).String() }

type scanArgs struct {
	outfile    string
	deviceType string
	deviceID   int
	autoCreate bool
	logLevel   string
	serial     string
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
	if err := fs.Parse(argv); err != nil {
		return err
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	node, err := easy.NewSerial(a.serial, easy.WithNodeLogger(logger))
	if err != nil {
		return err
	}
	defer node.Stop()
	if err := node.SetNetworkKey(0x00, devices.ANTPLUS_NETWORK_KEY); err != nil {
		return err
	}

	var autoDev any
	scanner, err := devices.NewScanner(node, a.deviceID, dt, 0)
	if err != nil {
		return err
	}
	scanner.OnScanFound = func(t devices.DeviceTuple) {
		fmt.Printf("Found new device %s\n", t.String())
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
					fmt.Printf("DeviceData %v page %d (%s): %+v\n", devices.DeviceType(t.Type), page, name, data)
				})
			}
			logger.Info("auto created device", "type", devices.DeviceType(t.Type).String(), "id", t.ID)
		}
	}
	scanner.OnScanUpdate = func(t devices.DeviceTuple, c devices.CommonData) {
		// Print the vendor name once the device sent common page 80
		// (openant issue #69).
		if c.ManufacturerID != 0 && c.ManufacturerID != 0xFFFF {
			fmt.Printf("Device %s (%s) update: %+v\n", t.String(), devices.ManufacturerName(c.ManufacturerID), c)
			return
		}
		fmt.Printf("Device %s update: %+v\n", t.String(), c)
	}

	done := node.Start(ctx)
	<-done

	if a.outfile != "" {
		if err := scanner.Save(a.outfile); err != nil {
			return err
		}
		fmt.Printf("Saved %d devices to %s\n", len(scanner.FoundDevices()), a.outfile)
	}
	return nil
}
