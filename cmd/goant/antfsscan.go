package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxdukov/openant-go/easy"
	fs "github.com/maxdukov/openant-go/fs"
)

func runAntfsScan(argv []string) error {
	a := struct {
		timeout  time.Duration
		serial   string
		logLevel string
	}{}
	fsFlags := flag.NewFlagSet("antfs-scan", flag.ContinueOnError)
	fsFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, `Search for nearby ANT-FS devices (watchs, cycling computers)
by listening for their beacons.

Usage:

	goant antfs-scan [options]

Options:
`)
		fsFlags.PrintDefaults()
	}
	fsFlags.DurationVar(&a.timeout, "timeout", 30*time.Second, "how long to search before giving up")
	fsFlags.StringVar(&a.serial, "serial", "", "serial number of the USB stick to use (see 'goant sticks'); empty = first found")
	fsFlags.StringVar(&a.serial, "s", "", "shorthand for -serial")
	fsFlags.StringVar(&a.logLevel, "logging", "WARN", "log level: DEBUG, INFO, WARN, ERROR")
	if err := fsFlags.Parse(argv); err != nil {
		return err
	}

	level := slog.LevelWarn
	switch a.logLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR", "CRITICAL":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	node, err := easy.NewSerial(a.serial, easy.WithNodeLogger(logger))
	if err != nil {
		return err
	}
	app, err := fs.NewApplication(node,
		fs.WithSetupChannel(func(ch *easy.Channel) error {
			if err := ch.SetPeriod(4096); err != nil {
				return err
			}
			if err := ch.SetSearchTimeout(255); err != nil {
				return err
			}
			if err := ch.SetRFFrequency(50); err != nil {
				return err
			}
			if err := ch.SetSearchWaveform([]byte{0x53, 0x00}); err != nil {
				return err
			}
			if err := ch.SetID(0, 0x01, 0); err != nil {
				return err
			}
			fmt.Println("Searching for ANT-FS devices...")
			return ch.Open()
		}),
		fs.WithAppLogger(logger))
	if err != nil {
		node.Stop()
		return err
	}
	defer app.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	seen := make(map[uint32]bool)
loop:
	for {
		select {
		case b := <-app.Incoming():
			if seen[b.Serial()] {
				continue
			}
			seen[b.Serial()] = true
			desc1, desc2 := b.DescriptorPair()
			fmt.Printf("Found ANT-FS device serial=%d descriptor=%04X:%04X pairing=%v data=%v state=%d\n",
				b.Serial(), desc1, desc2, b.PairingEnabled(), b.DataAvailable(), b.ClientDeviceState())
		case <-ctx.Done():
			break loop
		}
	}

	if err := ctx.Err(); err != nil && ctx.Err() != context.Canceled {
		return ctx.Err()
	}
	if len(seen) == 0 {
		fmt.Println("No ANT-FS devices found.")
	} else {
		fmt.Printf("Found %d ANT-FS device(s).\n", len(seen))
	}
	return nil
}
