// Example: scan for ANT+ devices in range, printing those found.
//
// Go port of openant examples/scanner.py.
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

	var scanner *devices.Scanner
	var deviceType = devices.DeviceTypeUnknown
	var deviceID int
	if len(os.Args) > 1 {
		deviceType = devices.DeviceTypeByName(os.Args[1])
		if deviceType == devices.DeviceTypeUnknown {
			fmt.Fprintln(os.Stderr, "usage: scanner [DeviceType] [device_id]")
			os.Exit(2)
		}
		if len(os.Args) > 2 {
			fmt.Sscanf(os.Args[2], "%d", &deviceID)
		}
	}

	scanner, err = devices.NewScanner(node, deviceID, deviceType, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	scanner.OnScanFound = func(t devices.DeviceTuple) {
		fmt.Printf("Found new device %s\n", t.String())
	}
	scanner.OnScanUpdate = func(t devices.DeviceTuple, c devices.CommonData) {
		fmt.Printf("Device %s update: %+v\n", t.String(), c)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Scanning for devices, press Ctrl-C to quit (optionally save with: scanner save <file>)")
	node.Run(ctx)

	scanner.Save("devices.json")
	fmt.Println("Saved found devices to devices.json")
}
