// Example: light electric vehicle (e-bike) telemetry.
//
// Go port of openant examples/lev.py.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
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

	lev, err := devices.NewLev(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	lev.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		d := data.(devices.LevData)
		fmt.Printf("Lev page %d (%s): speed %.1f km/h, SoC %d%%, %v km odometer, %v V\n",
			page, pageName, d.Speed, d.BatterySOC, d.Odometer, d.BatteryVoltage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting LEV example, press Ctrl-C to quit")
	node.Run(ctx)

	lev.CloseChannel()
}
