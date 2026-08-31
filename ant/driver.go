package ant

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Driver communicates with an ANT USB stick (or serial port based node).
// A Driver must be safe for use from a single reader and single writer
// goroutine concurrently.
type Driver interface {
	// Open prepares the device for I/O.
	Open() error
	// Close releases the device. Concurrent and subsequent Read calls must
	// return an error so that reader goroutines can unwind.
	Close() error
	// Read reads up to len(p) bytes from the device. It should block until
	// data is available and may return a timeout error, which the caller
	// treats as a non-fatal condition.
	Read(p []byte) (int, error)
	// Write writes p to the device.
	Write(p []byte) (int, error)
}

// Errors reported by drivers.
var (
	// ErrDriverNotFound is returned when no ANT device is present.
	ErrDriverNotFound = errors.New("ant: no ANT device found")
	// ErrDriverClosed is returned by I/O on a closed driver.
	ErrDriverClosed = errors.New("ant: driver closed")
	// ErrTimeout signals a read timeout; it is not fatal.
	ErrTimeout = errors.New("ant: driver read timeout")
)

// Driver priorities. Higher priority drivers are preferred when several
// ANT sticks are attached (matching openant: USB-m before USB2 before serial).
const (
	PrioritySerial byte = 10
	PriorityUSB2   byte = 20
	PriorityUSB3   byte = 30
)

// DriverFactory creates drivers of one kind and can probe for the presence
// of matching hardware.
type DriverFactory struct {
	// Name is a human readable driver name, e.g. "usb3".
	Name string
	// Priority prefers newer hardware when multiple sticks are attached.
	Priority byte
	// Probe reports whether a matching device is currently attached.
	Probe func() bool
	// New returns a ready to open driver instance.
	New func() Driver
	// Serials returns serial numbers of attached devices if the driver can
	// enumerate them (optional, may be nil).
	Serials func() []string
}

var (
	driversMu sync.Mutex
	drivers   []DriverFactory
)

// RegisterDriver appends a driver factory to the global registry. The
// built-in drivers register themselves via init; probing order is defined
// by the factory priority regardless of registration order.
func RegisterDriver(f DriverFactory) {
	driversMu.Lock()
	defer driversMu.Unlock()
	drivers = append(drivers, f)
}

// Drivers returns the registered driver factories sorted by descending
// priority.
func Drivers() []DriverFactory {
	driversMu.Lock()
	out := make([]DriverFactory, len(drivers))
	copy(out, drivers)
	driversMu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

// FindDriver returns a driver for the first matching device found. The
// returned driver is opened and ready for use.
func FindDriver() (Driver, error) {
	return findDriver("")
}

// FindDriverForSerial returns an opened driver for the attached device with
// the given serial number (as reported by the factory Serials hook), falling
// back to any device when serial is empty. This addresses openant issue #116
// (selecting a specific ANT USB stick).
func FindDriverForSerial(serial string) (Driver, error) {
	return findDriver(serial)
}

func findDriver(serial string) (Driver, error) {
	for _, f := range Drivers() {
		if !f.Probe() {
			continue
		}
		if serial != "" && f.Serials != nil {
			match := false
			for _, s := range f.Serials() {
				if s == serial {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		d := f.New()
		if err := d.Open(); err != nil {
			return nil, fmt.Errorf("ant: open %s driver: %w", f.Name, err)
		}
		return d, nil
	}
	return nil, ErrDriverNotFound
}
