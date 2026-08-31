// Example: fitness equipment trainer plus power meter on one node, with a
// ramp workout driving the trainer target power.
//
// Go port of openant examples/multi_dev_fe_power_meter.py.
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

	trainer, err := devices.NewFitnessEquipment(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	powerMeter, err := devices.NewPowerMeter(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	trainer.OnFound = func() {
		fmt.Println("Trainer found, starting ramp workout 100-300 W")
		workout, err := devices.WorkoutFromRamp(100, 300, 50, 30, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		workout.Cycles = 2
		trainer.StartWorkouts(context.Background(), workout)
	}
	trainer.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		fmt.Printf("Trainer %s page %d: %+v\n", pageName, page, data)
	}

	powerMeter.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		d := data.(devices.PowerData)
		fmt.Printf("Power %s page %d: %d W (avg %d W), %d rpm\n",
			pageName, page, d.InstantaneousPower, d.AveragePower, d.Cadence)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting trainer + power meter example, press Ctrl-C to quit")
	node.Run(ctx)

	trainer.CloseChannel()
	powerMeter.CloseChannel()
}
