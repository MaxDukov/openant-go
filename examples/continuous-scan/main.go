// Example: continuous scan mode (ANT mode 7). All nearby devices are
// received on one channel via extended messages.
//
// Go port of openant examples/continuous_scan.py.
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

	// Use a bidirectional receive channel to allow TX (acknowledged data).
	ch, err := node.NewChannel(easy.ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ch.OnBroadcastData = func(data []byte) {
		if len(data) > 8 {
			deviceID := int(data[9]) + int(data[10])<<8
			deviceType := data[11]
			fmt.Printf("Device %d type %d: % X\n", deviceID, deviceType, data[:8])
		} else {
			fmt.Printf("Data: % X\n", data)
		}
	}
	ch.OnBurstData = func(data []byte) {
		fmt.Printf("Burst data: % X\n", data)
	}

	if err := ch.SetID(0, 0, 0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetPeriod(0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.EnableExtendedMessages(true); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetRFFrequency(57); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.OpenRxScanMode(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Scanning continuously, press Ctrl-C to quit")
	node.Run(ctx)
}
