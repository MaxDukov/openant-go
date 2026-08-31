// Example: bicycle speed and cadence sensor with speed/distance
// calculation from the wheel circumference.
//
// Go port of openant examples/bike_speed_cadence.py.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
)

// Wheel circumference in meters.
const wheelCircumferenceM = 2.3

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

	bsc, err := devices.NewBikeSpeedCadence(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bsc.SetWheelCircumference(wheelCircumferenceM)

	bsc.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		switch d := data.(type) {
		case devices.BikeSpeedData:
			speedStr := "n/a"
			if d.CalculatedSpeed != nil {
				speedStr = fmt.Sprintf("%.2f km/h", *d.CalculatedSpeed)
			}
			distanceStr := "n/a"
			if d.CalculatedDistance != nil {
				distanceStr = fmt.Sprintf("%.2f m", *d.CalculatedDistance)
			}
			fmt.Printf("Speed page %d (%s): %s, distance %s (revs %d)\n",
				page, pageName, speedStr, distanceStr, d.CumulativeSpeedRevolution[1])
		case devices.BikeCadenceData:
			cadStr := "n/a"
			if d.CalculatedCadence != nil {
				cadStr = fmt.Sprintf("%.2f rpm", *d.CalculatedCadence)
			}
			fmt.Printf("Cadence page %d (%s): %s (revs %d)\n",
				page, pageName, cadStr, d.CumulativeCadenceRevolution[1])
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting speed/cadence example, press Ctrl-C to quit")
	node.Run(ctx)

	bsc.CloseChannel()
}
