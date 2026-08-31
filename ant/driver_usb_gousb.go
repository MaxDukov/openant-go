//go:build cgo

package ant

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/google/gousb"
)

// usbDriver talks to ANT USB sticks (ANTUSB2 0fcf:1008 and ANTUSB-m
// 0fcf:1009) via libusb bulk transfers, mirroring openant.base.driver.USBDriver.
type usbDriver struct {
	pid uint16

	mu     sync.Mutex
	ctx    *gousb.Context
	dev    *gousb.Device
	cfg    *gousb.Config
	intf   *gousb.Interface
	in     *gousb.InEndpoint
	out    *gousb.OutEndpoint
	closed bool
}

// probeUSB reports whether a device with the given product id is attached
// and returns its serial number.
func probeUSB(pid uint16) (bool, string) {
	ctx := gousb.NewContext()
	defer ctx.Close()
	dev, err := ctx.OpenDeviceWithVIDPID(gousb.ID(ANTVendorID), gousb.ID(pid))
	if err != nil || dev == nil {
		return false, ""
	}
	defer dev.Close()
	serial, _ := dev.SerialNumber()
	return true, serial
}

func init() {
	for _, spec := range []struct {
		name     string
		pid      uint16
		priority byte
	}{
		{"usb2", ANTProductUSB2, PriorityUSB2},
		{"usb3", ANTProductUSBm, PriorityUSB3},
	} {
		f := DriverFactory{
			Name:     spec.name,
			Priority: spec.priority,
			Probe: func() bool {
				ok, _ := probeUSB(spec.pid)
				return ok
			},
			Serials: func() []string {
				ok, serial := probeUSB(spec.pid)
				if !ok {
					return nil
				}
				return []string{serial}
			},
		}
		pid := spec.pid
		f.New = func() Driver { return &usbDriver{pid: pid} }
		RegisterDriver(f)
	}
}

// openEndpoint resolves the first bulk IN and OUT endpoints dynamically,
// exactly like openant does with pyusb.
func openEndpoint(intf *gousb.Interface) (*gousb.InEndpoint, *gousb.OutEndpoint, error) {
	var inNum, outNum = -1, -1
	for _, ep := range intf.Setting.Endpoints {
		if ep.TransferType != gousb.TransferTypeBulk {
			continue
		}
		switch ep.Direction {
		case gousb.EndpointDirectionIn:
			if inNum < 0 {
				inNum = ep.Number
			}
		case gousb.EndpointDirectionOut:
			if outNum < 0 {
				outNum = ep.Number
			}
		}
	}
	if inNum < 0 || outNum < 0 {
		return nil, nil, errors.New("usb: bulk endpoints not found")
	}
	in, err := intf.InEndpoint(inNum)
	if err != nil {
		return nil, nil, fmt.Errorf("usb: IN endpoint: %w", err)
	}
	out, err := intf.OutEndpoint(outNum)
	if err != nil {
		return nil, nil, fmt.Errorf("usb: OUT endpoint: %w", err)
	}
	return in, out, nil
}

// Open claims the ANT USB stick and its bulk endpoints. It is idempotent:
// the driver registry opens devices during probing.
func (u *usbDriver) Open() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.intf != nil {
		return nil // already open
	}
	ctx := gousb.NewContext()
	dev, err := ctx.OpenDeviceWithVIDPID(gousb.ID(ANTVendorID), gousb.ID(u.pid))
	if err != nil || dev == nil {
		ctx.Close()
		if err == nil {
			err = ErrDriverNotFound
		}
		return fmt.Errorf("usb: open 0x%04X:0x%04X: %w", ANTVendorID, u.pid, err)
	}
	// Best-effort kernel driver auto-detach (Linux); failures are not fatal,
	// matching openant which only logs a warning (see openant issue #103).
	if err := dev.SetAutoDetach(true); err != nil {
		// Not implemented on some platforms; ignore.
		_ = err
	}

	// libusb reset, matching openant behaviour.
	if err := dev.Reset(); err == nil && runtime.GOOS == "windows" {
		// Windows re-enumerates the device after reset; give it time.
		time.Sleep(2 * time.Second)
	}

	cfg, err := dev.Config(1)
	if err != nil {
		dev.Close()
		ctx.Close()
		return fmt.Errorf("usb: set configuration: %w", err)
	}
	// Claiming the interface can fail transiently with "device or resource
	// busy" right after another process (or our own probe) released it;
	// retry briefly before giving up.
	var intf *gousb.Interface
	for attempt := 0; ; attempt++ {
		intf, err = cfg.Interface(0, 0)
		if err == nil {
			break
		}
		if attempt >= 4 {
			cfg.Close()
			dev.Close()
			ctx.Close()
			return fmt.Errorf("usb: claim interface: %w", err)
		}
		slog.Default().Debug("usb: claim interface busy, retrying", "attempt", attempt+1, "error", err)
		time.Sleep(200 * time.Millisecond)
	}
	in, out, err := openEndpoint(intf)
	if err != nil {
		intf.Close()
		cfg.Close()
		dev.Close()
		ctx.Close()
		return err
	}
	u.ctx, u.dev, u.cfg, u.intf, u.in, u.out = ctx, dev, cfg, intf, in, out
	u.closed = false
	return nil
}

// Close releases the USB device; pending reads abort with an error.
func (u *usbDriver) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed || u.intf == nil {
		u.closed = true
		return nil
	}
	u.closed = true
	u.intf.Close()
	u.cfg.Close()
	u.dev.Close()
	u.ctx.Close() // aborts in-flight transfers
	return nil
}

// Read performs a bulk IN transfer. It blocks until data arrives or the
// driver is closed.
func (u *usbDriver) Read(p []byte) (int, error) {
	u.mu.Lock()
	in, closed := u.in, u.closed
	u.mu.Unlock()
	if closed || in == nil {
		return 0, ErrDriverClosed
	}
	n, err := in.Read(p)
	if err != nil {
		if u.isClosed() || errors.Is(err, io.EOF) {
			return 0, ErrDriverClosed
		}
		return n, fmt.Errorf("usb: read: %w", err)
	}
	return n, nil
}

// Write performs a bulk OUT transfer.
func (u *usbDriver) Write(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed || u.out == nil {
		return 0, ErrDriverClosed
	}
	return u.out.Write(p)
}

func (u *usbDriver) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}
