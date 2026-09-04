package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
)

// flagSetAlias keeps the per-command flag registration helpers readable.
type flagSetAlias = *flag.FlagSet

// datatargetArgs collects the arguments shared by the influx and mqtt
// subcommands (openant base.datatarget.add_general_arguments + the scan
// stick selection flags).
type datatargetArgs struct {
	deviceType string
	config     string
	deviceID   int
	transType  int
	serial     string
	serials    string
	allSticks  bool
	logLevel   string
	verbose    bool
}

func addDatatargetFlags(fs flagSetAlias, a *datatargetArgs) {
	fs.StringVar(&a.config, "config", "", "JSON config file with a device list (format of 'goant scan -o')")
	fs.IntVar(&a.deviceID, "id", 0, "device id to attach to (0 = first found)")
	fs.IntVar(&a.deviceID, "I", 0, "shorthand for -id")
	fs.IntVar(&a.transType, "transtype", 0, "transmission type to attach to (0 = wildcard)")
	fs.IntVar(&a.transType, "T", 0, "shorthand for -transtype")
	fs.StringVar(&a.serial, "serial", "", "serial number of the USB stick to use (see 'goant sticks'); empty = first found")
	fs.StringVar(&a.serials, "serials", "", "comma-separated list of sticks to scan on; enables multi-dongle")
	fs.BoolVar(&a.allSticks, "all", false, "use every attached ANT stick (multi-dongle)")
	fs.StringVar(&a.logLevel, "logging", "WARN", "log level: DEBUG, INFO, WARN, ERROR")
	fs.BoolVar(&a.verbose, "verbose", false, "verbose output")
}

// deviceSpec is one resolved profile to instantiate.
type deviceSpec struct {
	name  string
	id    int
	trans int
	dtype devices.DeviceType
}

// deviceSpecs resolves the positional device name or the -config file
// into concrete profile specs.
func deviceSpecs(a *datatargetArgs) ([]deviceSpec, error) {
	if a.config != "" {
		jsonDevices, err := devices.ReadJSONDevices(a.config)
		if err != nil {
			return nil, err
		}
		if jsonDevices == nil {
			return nil, fmt.Errorf("invalid or missing config file %s", a.config)
		}
		specs := make([]deviceSpec, 0, len(jsonDevices))
		for _, jd := range jsonDevices {
			dt := devices.DeviceType(jd.Type)
			if jd.Device != "" {
				dt = devices.DeviceTypeByName(jd.Device)
			}
			if dt == devices.DeviceTypeUnknown {
				return nil, fmt.Errorf("config: unknown device %q", jd.Device)
			}
			specs = append(specs, deviceSpec{name: jd.Device, id: jd.ID, trans: jd.TransmissionType, dtype: dt})
		}
		return specs, nil
	}
	if a.deviceType == "" {
		return nil, fmt.Errorf("a device name (e.g. HeartRate) or -config is required; run 'goant help' for usage")
	}
	dt := devices.DeviceTypeByName(a.deviceType)
	if dt == devices.DeviceTypeUnknown {
		return nil, fmt.Errorf("unknown device %q; known types: %s", a.deviceType, deviceTypeNames())
	}
	return []deviceSpec{{name: a.deviceType, id: a.deviceID, trans: a.transType, dtype: dt}}, nil
}

// attachDevices opens one easy.Node per stick launcher and instantiates
// every device spec on each node, wiring device data events into write.
// The returned function starts the nodes and blocks until Ctrl-C.
func attachDevices(launchers []stickLauncher, specs []deviceSpec, write func(spec deviceSpec, page int, name string, data devices.DeviceData)) (func() error, error) {
	var nodes []*easy.Node
	for _, l := range launchers {
		node, err := l.open()
		if err != nil {
			for _, n := range nodes {
				n.Stop()
			}
			return nil, err
		}
		nodes = append(nodes, node)
		for _, spec := range specs {
			prof, err := devices.AutoCreateDevice(node, spec.id, spec.dtype, spec.trans)
			if err != nil {
				for _, n := range nodes {
					n.Stop()
				}
				return nil, fmt.Errorf("create %s: %w", spec.name, err)
			}
			if dd, ok := prof.(interface {
				SetOnDeviceData(func(page int, name string, data devices.DeviceData))
			}); ok {
				spec := spec
				dd.SetOnDeviceData(func(page int, name string, data devices.DeviceData) {
					write(spec, page, name, data)
				})
			}
			fmt.Printf("Attached %s device %d (trans %d)\n", spec.name, spec.id, spec.trans)
		}
	}
	return func() error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		done := make(chan struct{}, len(nodes))
		for _, n := range nodes {
			go func(n *easy.Node) {
				n.Start(ctx)
				done <- struct{}{}
			}(n)
		}
		fmt.Println("Streaming device data, press Ctrl-C to finish")
		for range nodes {
			<-done
		}
		return nil
	}, nil
}

// resolveDatatargetSticks shares the scan stick selection (-serial,
// -serials, -all) with the influx/mqtt targets.
func resolveDatatargetSticks(a *datatargetArgs) ([]stickLauncher, error) {
	return resolveSticks(scanArgs{serial: a.serial, serials: a.serials, allSticks: a.allSticks})
}
