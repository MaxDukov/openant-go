// Example: tire pressure monitor (TPMS).
//
// Go port of openant examples/tire_pressure_monitor.py.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
)

const (
	mbarPerKPa = 0.1
	kPaPerPSI  = 0.1450326
)

func main() {
	node, err := easy.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer node.Stop()

	if err := node.SetNetworkKey(0x00, devices.ANTPLUS_NETWORK_KEY); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	tpms, err := devices.NewTirePressureMonitor(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	tpms.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		d := data.(devices.TirePressureData)
		kpa := float64(d.Pressure) * mbarPerKPa
		psi := kpa * kPaPerPSI
		fmt.Printf("TPMS page %d (%s): position %v alarm %v pressure %d mB / %.1f kPa / %.1f psi\n",
			page, pageName, d.Position, d.AlarmState, d.Pressure, kpa, psi)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting tire pressure example, press Ctrl-C to quit")
	node.Run(ctx)

	tpms.CloseChannel()
}
