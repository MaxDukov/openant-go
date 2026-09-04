// Command goant is the openant-go command line tool. It mirrors the
// `openant` CLI from the Python library; currently the `scan`,
// `influx` and `mqtt` subcommands stream device data.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const usage = `goant - ANT and ANT-FS Go library command line tool

Usage:

	goant <command> [arguments]

Commands:

	scan	Scan for nearby ANT+ devices and optionally print device data
	antfs-scan
		Search for nearby ANT-FS devices by listening for beacons
	sticks	List attached ANT USB sticks by serial number
	influx	Stream device data to an InfluxDB database
	mqtt	Stream device data to an MQTT broker
	udev	Install udev rules for ANT USB sticks (Linux, needs root)
	version	Print the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "scan":
		if err := runScan(args); err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err)
			os.Exit(1)
		}
	case "antfs-scan":
		if err := runAntfsScan(args); err != nil {
			fmt.Fprintln(os.Stderr, "antfs-scan:", err)
			os.Exit(1)
		}
	case "sticks":
		runSticks()
	case "influx":
		if err := runInflux(args); err != nil {
			fmt.Fprintln(os.Stderr, "influx:", err)
			os.Exit(1)
		}
	case "mqtt":
		if err := runMqtt(args); err != nil {
			fmt.Fprintln(os.Stderr, "mqtt:", err)
			os.Exit(1)
		}
	case "udev":
		if err := runUdev(args); err != nil {
			fmt.Fprintln(os.Stderr, "udev:", err)
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Println("goant", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "goant: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

// deviceTypeNames renders the profile names for help text.
func deviceTypeNames() string {
	names := []string{}
	for _, t := range []int{11, 16, 17, 25, 34, 35, 48, 115, 119, 120, 121, 122, 123, 124, 20, 127} {
		if n := deviceTypeName(t); n != "Unknown" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

var _ = flag.ContinueOnError
