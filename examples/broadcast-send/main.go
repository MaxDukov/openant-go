// Example: broadcast (master mode) sender simulating a stride speed and
// distance monitor, transmitting pages 80, 81 and 1 in rotation.
//
// Go port of openant examples/broadcast_send.py.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"time"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/devices"
	"github.com/maxdukov/openant-go/easy"
)

const (
	deviceNumber = 12345
	deviceType   = 124 // stride speed distance monitor
	transType    = 5
	period       = 8134
	rfFreq       = 57
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	node, err := easy.New(easy.WithNodeLogger(logger))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer node.Stop()

	if err := node.SetNetworkKey(0x00, devices.ANTPLUS_NETWORK_KEY); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ch, err := node.NewChannel(easy.ChannelBidirectionalTransmit, 0x00, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var (
		strides      uint16
		timeCount    float64
		stridesInMin uint16
		distance     float64
		pageCount    int
		start        = time.Now()
	)

	ch.OnBroadcastTxData = func(data []byte) {
		var page []byte
		switch {
		case pageCount == 0: // manufacturer
			page = []byte{80, 0xFF, 0xFF, 0x01, 0x59, 0x00, 0x20, 0x00}
		case pageCount == 65: // product info
			page = []byte{81, 0xFF, 0xFF, 0x01, 0x55, 0x00, 0x00, 0x00}
		default:
			// Update cumulative values at ~2 Hz (spec: 1/2 s updates).
			elapsed := time.Since(start).Seconds()
			timeCount = math.Mod(elapsed, 65536) * 1024
			strides = uint16(elapsed * 20) // 20 strides per second for demo
			stridesInMin = uint16(elapsed * 20 * 60)
			distance += 0.01
			page = []byte{
				1,
				byte(strides), byte(strides >> 8),
				byte(timeCount), byte(uint16(timeCount) >> 8),
				byte(stridesInMin), byte(stridesInMin >> 8),
				0x01, // status: active
			}
		}
		_ = distance
		if err := ch.SendBroadcastData(page); err != nil {
			logger.Warn("send broadcast", "error", err)
		}
		logger.Info("sent page", "count", pageCount, "page", fmt.Sprintf("% X", page))
		if pageCount == 129 {
			pageCount = 0
		} else {
			pageCount++
		}
	}

	if err := ch.SetID(deviceNumber, deviceType, transType); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetPeriod(period); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.SetRFFrequency(rfFreq); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ch.Open(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("Broadcasting SDM data, press Ctrl-C to quit")
	node.Run(ctx)
	_ = ant.SyncByte
}
