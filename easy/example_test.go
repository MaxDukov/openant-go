package easy_test

import (
	"context"
	"fmt"
	"time"

	"github.com/maxdukov/openant-go/anttest"
	"github.com/maxdukov/openant-go/easy"
)

// This example listens for broadcast data on a receive channel. It uses
// the anttest simulator; with easy.New the same code runs on real
// hardware.
func Example() {
	sim := anttest.NewSimDriver()
	n, err := easy.NewWithDriver(sim)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer n.Stop()

	ch, err := n.NewChannel(easy.ChannelBidirectionalReceive, 0, nil)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// Heart rate monitor profile: any device number, device type 120.
	if err := ch.SetID(0, 0x78, 0); err != nil {
		fmt.Println("error:", err)
		return
	}
	if err := ch.SetPeriod(8070); err != nil { // 4.06 Hz
		fmt.Println("error:", err)
		return
	}
	if err := ch.SetRFFrequency(57); err != nil { // 2466 MHz
		fmt.Println("error:", err)
		return
	}
	if err := ch.Open(); err != nil {
		fmt.Println("error:", err)
		return
	}

	got := make(chan struct{})
	ch.OnBroadcastData = func(data []byte) {
		fmt.Printf("broadcast page 0x%02X, %d bytes\n", data[0], len(data))
		close(got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := n.Start(ctx)

	// Simulate a sensor broadcast; a real device transmits by itself.
	sim.EmitBroadcast(0, []byte{0x0E, 0x01, 0xB0, 0x00, 0x48, 0x00, 0x00, 0x00})

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		fmt.Println("no data received")
	}
	cancel()
	<-done

	// Output: broadcast page 0x0E, 8 bytes
}
