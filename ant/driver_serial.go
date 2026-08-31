package ant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.bug.st/serial"
)

// Known Dynastream ANT device USB identifiers.
const (
	ANTVendorID    = 0x0FCF
	ANTProductCDC  = 0x1004
	ANTProductUSB2 = 0x1008
	ANTProductUSBm = 0x1009
)

// SerialBaud is the baud rate of the CDC based ANT sticks.
const SerialBaud = 115200

// SerialDriver talks to a serial (CDC) ANT node, e.g. the original Dynastream
// stick (0fcf:1004) exposed as /dev/ttyUSB* or /dev/cu.*.
type SerialDriver struct {
	path string
	port serial.Port
}

// NewSerialDriver returns a serial driver bound to the given device path
// (e.g. "/dev/ttyUSB0"). An empty path means auto-detect on Linux.
func NewSerialDriver(path string) *SerialDriver {
	return &SerialDriver{path: path}
}

// probeSerialPath scans Linux sysfs for CDC ANT sticks, mirroring
// openant.base.driver.SerialDriver.find. On other platforms it returns an
// empty list and an explicit path must be used.
func probeSerialPaths() []string {
	entries, err := os.ReadDir("/sys/bus/usb-serial/devices")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		// Resolve the USB device ancestor to read idVendor/idProduct.
		link, err := filepath.EvalSymlinks(filepath.Join("/sys/bus/usb-serial/devices", e.Name()))
		if err != nil {
			continue
		}
		for dir := link; dir != "/" && strings.HasPrefix(dir, "/sys"); dir = filepath.Dir(dir) {
			if readID(dir, "idVendor") == formatUSBID(ANTVendorID) &&
				readID(dir, "idProduct") == formatUSBID(ANTProductCDC) {
				out = append(out, "/dev/"+e.Name())
				break
			}
		}
	}
	return out
}

func readID(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func formatUSBID(id int) string { return fmt.Sprintf("%04x", id) }

func init() {
	RegisterDriver(DriverFactory{
		Name:     "serial",
		Priority: PrioritySerial,
		Probe: func() bool {
			return len(probeSerialPaths()) > 0
		},
		New: func() Driver {
			paths := probeSerialPaths()
			path := ""
			if len(paths) > 0 {
				path = paths[0]
			}
			return NewSerialDriver(path)
		},
	})
}

// Open opens the serial port at 115200 baud with a short read deadline.
// It is idempotent: the driver registry opens devices during probing.
func (s *SerialDriver) Open() error {
	if s.port != nil {
		return nil // already open
	}
	if s.path == "" {
		return errors.New("serial: no ANT serial device path (auto-detection is Linux only)")
	}
	mode := &serial.Mode{BaudRate: SerialBaud}
	p, err := serial.Open(s.path, mode)
	if err != nil {
		return fmt.Errorf("serial: open %s: %w", s.path, err)
	}
	// Non-blocking style reads: the reader loop treats timeouts as ticks.
	if err := p.SetReadTimeout(100 * time.Millisecond); err != nil {
		p.Close()
		return fmt.Errorf("serial: set read timeout: %w", err)
	}
	s.port = p
	return nil
}

// Close closes the serial port.
func (s *SerialDriver) Close() error {
	if s.port == nil {
		return nil
	}
	err := s.port.Close()
	s.port = nil
	return err
}

// Read reads bytes from the port. It returns ErrTimeout when the read
// deadline elapses without data, mirroring pyserial timeout behaviour.
func (s *SerialDriver) Read(p []byte) (int, error) {
	if s.port == nil {
		return 0, ErrDriverClosed
	}
	n, err := s.port.Read(p)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return 0, ErrTimeout
		}
		return 0, err
	}
	if n == 0 {
		return 0, ErrTimeout
	}
	return n, nil
}

// Write writes bytes to the port.
func (s *SerialDriver) Write(p []byte) (int, error) {
	if s.port == nil {
		return 0, ErrDriverClosed
	}
	return s.port.Write(p)
}
