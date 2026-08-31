// Example: controllable device (master mode) receiving generic control
// commands from a remote.
//
// Go port of openant examples/control_device.py.
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

	device, err := devices.NewGenericControllableDevice(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	device.OnControlCommand = func(command devices.ControlCommand, raw int) {
		fmt.Printf("Control command %s (raw %d)\n", command, raw)
	}

	device.OnBattery = func(data devices.BatteryData) {
		fmt.Printf("Battery: %+v\n", data)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting control device example, press Ctrl-C to quit")
	node.Run(ctx)

	device.CloseChannel()
}
