// Example: raw low-level device data without a device profile, using the
// easy channel API directly.
//
// Go port of openant examples/raw_device_data.py.
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

	// Wildcard search for heart rate monitors (device type 120).
	ch, err := node.NewChannel(easy.ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ch.OnBroadcastData = func(data []byte) {
		fmt.Printf("Raw data: % X\n", data)
	}
	ch.OnBurstData = func(data []byte) {
		fmt.Printf("Raw burst: % X\n", data)
	}

	if err := ch.SetSearchTimeout(0xFF); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetID(0, 120, 0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.EnableExtendedMessages(true); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetPeriod(8070); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetRFFrequency(57); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.Open(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Receiving raw data, press Ctrl-C to quit")
	node.Run(ctx)

	node.RemoveChannel(ch)
}
