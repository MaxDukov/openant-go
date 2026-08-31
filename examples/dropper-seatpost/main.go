// Example: dropper seatpost control. Unlocks the valve when the device is
// found and reports state changes.
//
// Go port of openant examples/dropper_seatpost.py.
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

	seatpost, err := devices.NewDropperSeatpost(node, 0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	seatpost.OnFound = func() {
		fmt.Println("Seat post found, sending unlock command")
		seatpost.SetValve(devices.ValveUnlocked)
	}

	seatpost.OnDeviceData = func(page int, pageName string, data devices.DeviceData) {
		fmt.Printf("Dropper page %d (%s): %+v\n", page, pageName, data)
	}

	seatpost.OnBattery = func(data devices.BatteryData) {
		fmt.Printf("Battery: %+v\n", data)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Starting dropper seatpost example, press Ctrl-C to quit")
	node.Run(ctx)

	seatpost.CloseChannel()
}
