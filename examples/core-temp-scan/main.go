// Example: core body temperature sensor.
//
// Go port of openant examples/core_temp_scan.py.
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

	core, err := devices.NewCoreTemperature(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	core.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		d := data.(devices.CoreTemperatureData)
		fmt.Printf("CoreTemp page %d (%s): core %.2f C, skin %.2f C, quality %d\n",
			page, pageName, d.CoreTemp, d.SkinTemp, d.Quality)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting core temperature example, press Ctrl-C to quit")
	node.Run(ctx)

	core.CloseChannel()
}
