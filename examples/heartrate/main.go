// Example: attach to the first heart rate monitor found and print data.
//
// Go port of openant examples/heart_rate.py.
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

	hr, err := devices.NewHeartRate(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	hr.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		d := data.(devices.HeartRateData)
		fmt.Printf("HeartRate (page %d (%s)): %d bpm (beat count %d, beat time %.3fs)\n",
			page, pageName, d.HeartRate, d.BeatCount, d.BeatTime)
	}

	hr.OnBattery = func(data devices.BatteryData) {
		fmt.Printf("Battery: ID %d, %.2f V (coarse %d V), status %s, %d s\n",
			data.BatteryID, data.VoltageFractional, data.VoltageCoarse,
			data.Status, data.OperatingTime)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting heart rate example, press Ctrl-C to quit")
	node.Run(ctx)

	hr.CloseChannel()
}
