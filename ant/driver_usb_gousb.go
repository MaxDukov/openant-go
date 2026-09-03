//go:build cgo

package ant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/gousb"
)

// usbDriver talks to ANT USB sticks (ANTUSB2 0fcf:1008 and ANTUSB-m
// 0fcf:1009) via libusb bulk transfers, mirroring openant.base.driver.USBDriver.
// A driver whose want field is set binds to that specific stick
// (bus/address), which is how several identical sticks coexist.
type usbDriver struct {
	pid  uint16
	want StickInfo

	// readTimeout bounds every bulk IN transfer; 0 (default) blocks until
	// data arrives (openant issue #42).
	readTimeout time.Duration

	mu     sync.Mutex
	ctx    *gousb.Context
	dev    *gousb.Device
	cfg    *gousb.Config
	intf   *gousb.Interface
	in     *gousb.InEndpoint
	out    *gousb.OutEndpoint
	closed bool
}

// SetReadTimeout bounds every subsequent Read (ant.ReadTimeoutSetter).
// A non-positive duration restores blocking reads.
func (u *usbDriver) SetReadTimeout(d time.Duration) {
	u.mu.Lock()
	u.readTimeout = d
	u.mu.Unlock()
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

// listUSB enumerates every attached stick with the given product id,
// reading each serial number (empty when the descriptor is broken, as on
// some CYCPLUS clones).
func listUSB(pid uint16) []StickInfo {
	ctx := gousb.NewContext()
	defer ctx.Close()
	var out []StickInfo
	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return desc.Vendor == gousb.ID(ANTVendorID) && desc.Product == gousb.ID(pid)
	})
	// err reports the last open failure, if any; successfully opened
	// devices are still returned and must be closed.
	if err != nil {
		for _, d := range devs {
			d.Close()
		}
		return nil
	}
	for _, d := range devs {
		serial, _ := d.SerialNumber()
		out = append(out, StickInfo{
			Serial:  serial,
			Product: usbFactoryName(pid),
			Bus:     d.Desc.Bus,
			Address: d.Desc.Address,
		})
		d.Close()
	}
	return out
}

// usbFactoryName maps a product id to the registered driver name.
func usbFactoryName(pid uint16) string {
	if pid == ANTProductUSBm {
		return "usb3"
	}
	return "usb2"
}

// openStickFor opens the stick selected by the want filter (bus/address).
// want.Bus == 0 means "any stick", which preserves the old behaviour of
// taking the first matching device.
func openStickFor(ctx *gousb.Context, pid uint16, want StickInfo) (*gousb.Device, error) {
	if want.Bus == 0 {
		dev, err := ctx.OpenDeviceWithVIDPID(gousb.ID(ANTVendorID), gousb.ID(pid))
		if err != nil || dev == nil {
			if err == nil {
				err = ErrDriverNotFound
			}
			return nil, wrapUSBOpenErr(err)
		}
		return dev, nil
	}
	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return desc.Vendor == gousb.ID(ANTVendorID) && desc.Product == gousb.ID(pid)
	})
	if err != nil && len(devs) == 0 {
		return nil, wrapUSBOpenErr(err)
	}
	var found *gousb.Device
	for _, d := range devs {
		if found == nil && d.Desc.Bus == want.Bus && d.Desc.Address == want.Address {
			found = d
			continue
		}
		d.Close()
	}
	if found == nil {
		return nil, fmt.Errorf("usb: no ANT stick at bus %d addr %d: %w", want.Bus, want.Address, ErrDriverNotFound)
	}
	return found, nil
}

// wrapUSBOpenErr annotates permission errors with an actionable hint
// (openant issue #40).
func wrapUSBOpenErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gousb.ErrorAccess) || strings.Contains(err.Error(), "access denied") || strings.Contains(err.Error(), "insufficient permission") {
		return fmt.Errorf("%w: %v (install the ANT udev rules from resources/42-ant-usb-sticks.rules or add the user to the plugdev group)", ErrPermission, err)
	}
	return err
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
				var out []string
				for _, s := range listUSB(spec.pid) {
					if s.Serial != "" {
						out = append(out, s.Serial)
					}
				}
				return out
			},
			List: func() []StickInfo { return listUSB(spec.pid) },
			NewForStick: func(s StickInfo) Driver {
				return &usbDriver{pid: spec.pid, want: s}
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
	dev, err := openStickFor(ctx, u.pid, u.want)
	if err != nil {
		ctx.Close()
		return fmt.Errorf("usb: open 0x%04X:0x%04X: %w", ANTVendorID, u.pid, wrapUSBOpenErr(err))
	}
	// Best-effort kernel driver auto-detach (Linux); failures are not fatal,
	// matching openant which only logs a warning (see openant issue #103).
	if err := dev.SetAutoDetach(true); err != nil {
		if runtime.GOOS == "linux" {
			// On Linux this usually means missing privileges for the
			// running user: the kernel driver (usbhid/cdc_acm) stays
			// attached and transfers fail with "resource busy".
			slog.Warn("usb: kernel driver detach failed; transfers may fail with 'resource busy'. Install the ANT udev rules (resources/42-ant-usb-sticks.rules) or run as root",
				"error", err)
		} else {
			// Not implemented on some platforms; expected there.
			slog.Default().Debug("usb: kernel driver auto-detach unsupported", "error", err)
		}
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

// Read performs a bulk IN transfer. It blocks until data arrives, the
// configured read timeout elapses (see SetReadTimeout) or the driver is
// closed.
func (u *usbDriver) Read(p []byte) (int, error) {
	u.mu.Lock()
	in, closed, timeout := u.in, u.closed, u.readTimeout
	u.mu.Unlock()
	if closed || in == nil {
		return 0, ErrDriverClosed
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	n, err := in.ReadContext(ctx, p)
	if err != nil {
		if u.isClosed() || errors.Is(err, io.EOF) {
			return 0, ErrDriverClosed
		}
		// Cancellation comes back as a TransferStatus from gousb; the
		// deadline and libusb timeouts both map to the non-fatal
		// ErrTimeout the reader loop treats as a poll tick.
		if errors.Is(err, context.DeadlineExceeded) ||
			err == gousb.TransferCancelled || err == gousb.TransferTimedOut {
			return n, ErrTimeout
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
