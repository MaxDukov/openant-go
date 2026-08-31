// Example: electronic shifting status display.
//
// Go port of openant examples/shift.py.
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

	shift, err := devices.NewShifting(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	shift.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		d := data.(devices.ShiftData)
		fmt.Printf("Shift page %d (%s): front %d/%d, rear %d/%d, trim r%df%d\n",
			page, pageName, d.GearFront, d.TotalFront, d.GearRear, d.TotalRear,
			d.CurrentTrimRear, d.CurrentTrimFront)
	}

	shift.OnBattery = func(data devices.BatteryData) {
		fmt.Printf("Battery (system %s): %+v\n", devices.ShiftingSystemID(data.BatteryID), data)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting shifting example, press Ctrl-C to quit")
	node.Run(ctx)

	shift.CloseChannel()
}
