package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// embeddedRules is a byte-for-byte copy of resources/42-ant-usb-sticks.rules
// (kept in sync by udev_test.go). The binary embeds it because `go install`
// builds do not ship repository files.
//
//go:embed 42-ant-usb-sticks.rules
var embeddedRules []byte

const defaultUdevDest = "/etc/udev/rules.d/42-ant-usb-sticks.rules"

func runUdev(args []string) error {
	fs := flag.NewFlagSet("udev", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Install udev rules granting access to ANT USB sticks without root.

Usage:

	goant udev [options]

Copies the bundled rules file (ANTUSB2 0fcf:1008, ANTUSB-m 0fcf:1009 and
serial/CDC sticks 0fcf:1004; mode 0660 with group plugdev) to
/etc/udev/rules.d/ and reloads the udev rules.

Options:
`)
		fs.PrintDefaults()
	}
	dest := fs.String("dest", defaultUdevDest, "destination rules file path")
	dryRun := fs.Bool("dry_run", false, "print what would be done without writing anything")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if runtime.GOOS != "linux" && *dest == defaultUdevDest {
		return fmt.Errorf("udev only exists on Linux; use -dest to write the rules file somewhere else (e.g. for packaging)")
	}
	if *dest == defaultUdevDest && os.Geteuid() != 0 {
		return fmt.Errorf("writing %s requires root; run: sudo goant udev", *dest)
	}

	if existing, err := os.ReadFile(*dest); err == nil && string(existing) == string(embeddedRules) {
		fmt.Printf("rules already installed at %s\n", *dest)
		return nil
	}

	if *dryRun {
		fmt.Printf("would write %d bytes to %s and run: udevadm control --reload-rules; udevadm trigger --subsystem-match=usb\n", len(embeddedRules), *dest)
		return nil
	}
	if err := os.WriteFile(*dest, embeddedRules, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *dest, err)
	}
	fmt.Printf("wrote %s\n", *dest)

	reloadUdev()

	fmt.Println(`next steps:
  - add your user to the plugdev group and log out/in:
      sudo usermod -aG plugdev $USER
  - unplug and replug the ANT stick (or run: udevadm trigger --subsystem-match=usb)`)
	return nil
}

// reloadUdev reloads the udev rules and re-triggers usb devices, printing
// a warning instead of failing when udevadm is not available (e.g. cross
// installs or minimal systems).
func reloadUdev() {
	if _, err := exec.LookPath("udevadm"); err != nil {
		fmt.Println("udevadm not found: reload the rules manually (or replug the stick)")
		return
	}
	if out, err := exec.Command("udevadm", "control", "--reload-rules").CombinedOutput(); err != nil {
		fmt.Printf("udevadm control --reload-rules failed: %v\n%s", err, out)
		return
	}
	if out, err := exec.Command("udevadm", "trigger", "--subsystem-match=usb").CombinedOutput(); err != nil {
		fmt.Printf("udevadm trigger failed: %v\n%s", err, out)
		return
	}
	fmt.Println("udev rules reloaded and usb devices re-triggered")
}
