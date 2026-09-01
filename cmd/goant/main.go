// Command goant is the openant-go command line tool. It mirrors the
// `openant` CLI from the Python library; currently the `scan` subcommand is
// implemented (influx/mqtt streaming is tracked in TODO.md).
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
	names := []string{"Unknown"}
	for _, t := range []int{11, 16, 17, 25, 34, 35, 48, 115, 120, 121, 122, 123, 124, 20, 127} {
		if n := deviceTypeName(t); n != "Unknown" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

var _ = flag.ContinueOnError
