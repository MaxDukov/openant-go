// Example: ANT-FS client. Waits for a beacon, pairs, sets the time and
// lists the files on the device.
//
// Go port of openant examples/antfs_list.py.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/maxdukov/openant-go/easy"
	"github.com/maxdukov/openant-go/fs"
)

func main() {
	node, err := easy.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
			return ch.Open()
		}),
		fs.WithOnLink(func(a *fs.Application, beacon fs.Beacon) bool {
			fmt.Printf("Link level: serial %d\n", beacon.Serial())
			if err := a.Link(); err != nil {
				fmt.Fprintln(os.Stderr, "link:", err)
				return false
			}
			return true
		}),
		fs.WithOnAuthentication(func(a *fs.Application, beacon fs.Beacon) bool {
			fmt.Println("Authentication layer")
			serial, name, err := a.AuthenticationSerial()
			if err != nil {
				fmt.Fprintln(os.Stderr, "auth serial:", err)
				return false
			}
			fmt.Printf("Device serial %d, name %q\n", serial, name)
			if _, err := a.AuthenticationPair("openant-go"); err != nil {
				fmt.Fprintln(os.Stderr, "pairing failed:", err)
				return false
			}
			fmt.Println("Paired")
			return true
		}),
		fs.WithOnTransport(func(a *fs.Application, beacon fs.Beacon) {
			fmt.Println("Transport layer")
			if err := a.SetTime(); err != nil {
				fmt.Fprintln(os.Stderr, "set time:", err)
			}
			dir, err := a.DownloadDirectory(func(p float64) {
				fmt.Printf("Downloading directory: %.0f%%\n", p*100)
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "download:", err)
				return
			}
			fmt.Printf("Directory version %d.%d, %d files:\n", dir.Version>>4, dir.Version&0xF, len(dir.Files))
			for _, f := range dir.Files {
				fmt.Printf("  %3d: type %#02x id %v size %d date %v flags %s\n",
					f.Index, f.DataType, f.Identifier, f.Size, f.Time().Format("2006-01-02 15:04"), f.FlagString())
			}
		}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = app

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
