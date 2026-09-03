package ant

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
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
	// ErrPermission is returned (wrapped around the OS error) when the
	// device cannot be opened for lack of user permissions, e.g. missing
	// udev rules on Linux (openant issue #40).
	ErrPermission = errors.New("ant: insufficient permissions")
)

// ReadTimeoutSetter is optionally implemented by drivers that support a
// bounded read (openant issue #42: USB read timeouts on Raspberry Pi).
type ReadTimeoutSetter interface {
	// SetReadTimeout bounds every subsequent Read; 0 restores blocking
	// reads.
	SetReadTimeout(d time.Duration)
}

// SetDriverReadTimeout bounds the read of drivers implementing
// ReadTimeoutSetter (the built-in USB driver does; openant issue #42). It
// reports whether the driver supports timeouts. A non-positive timeout
// restores blocking reads.
func SetDriverReadTimeout(d Driver, timeout time.Duration) bool {
	s, ok := d.(ReadTimeoutSetter)
	if !ok {
		return false
	}
	s.SetReadTimeout(timeout)
	return true
}

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
	// List enumerates all attached sticks of this kind (optional, may be
	// nil). It is the multi-dongle discovery hook used by Sticks; sticks
	// with broken USB string descriptors (some CYCPLUS clones) report an
	// empty serial but are still addressable by bus/address.
	List func() []StickInfo
	// NewForStick returns a driver bound to the specific stick (optional,
	// may be nil). New, by contrast, always opens the first matching
	// device, which makes it unusable when several sticks of the same
	// kind are attached.
	NewForStick func(StickInfo) Driver
}

// StickInfo identifies one attached ANT stick. It is the value type used
// to select a particular stick when several are plugged in (openant
// issues #67/#91).
type StickInfo struct {
	// Serial is the USB serial number; empty when the stick has a broken
	// or missing serial descriptor (seen on some CYCPLUS clones).
	Serial string
	// Product is the driver name, e.g. "usb2", "usb3", "serial".
	Product string
	// Bus and Address locate the device on the USB bus.
	Bus     int
	Address int
}

// String renders the stick as "product serial=X bus=N addr=N"; unreadable
// serials are rendered as "<unreadable>".
func (s StickInfo) String() string {
	serial := s.Serial
	if serial == "" {
		serial = "<unreadable>"
	}
	return fmt.Sprintf("%s serial=%s bus=%d addr=%d", s.Product, serial, s.Bus, s.Address)
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

// Serials returns the serial numbers of all attached devices reported by
// the registered driver factories (unique, sorted). Use it to pick a value
// for FindDriverForSerial / easy.NewSerial when several sticks are attached.
// Sticks with broken USB string descriptors (seen on some CYCPLUS clones)
// report no serial and cannot be selected by serial number.
func Serials() []string {
	seen := make(map[string]bool)
	var out []string
	for _, f := range Drivers() {
		if f.Serials == nil {
			continue
		}
		for _, s := range f.Serials() {
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// NewDriver returns a fresh, not yet opened driver for the first matching
// device found. Unlike FindDriver the caller is responsible for opening it;
// Core does so when created with WithDriverFactory.
func NewDriver() (Driver, error) {
	return newDriver("")
}

// NewDriverForSerial is NewDriver restricted to the device with the given
// serial number (empty matches any device).
func NewDriverForSerial(serial string) (Driver, error) {
	return newDriver(serial)
}

// Sticks enumerates all attached ANT sticks using the List hook of every
// registered driver factory, sorted by bus and address. Use it together
// with NewDriverForStick / easy.NewStick to open several sticks at once.
func Sticks() []StickInfo { return sticksFrom(Drivers()) }

// sticksFrom is the testable core of Sticks.
func sticksFrom(factories []DriverFactory) []StickInfo {
	var out []StickInfo
	for _, f := range factories {
		if f.List == nil {
			continue
		}
		for _, s := range f.List() {
			if s.Product == "" {
				s.Product = f.Name
			}
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Bus != out[j].Bus {
			return out[i].Bus < out[j].Bus
		}
		return out[i].Address < out[j].Address
	})
	return out
}

// NewDriverForStick returns a fresh, unopened driver bound to the specific
// stick (openant issues #67/#91). Unlike NewDriver it can address sticks
// individually, which matters when several sticks of the same kind are
// attached.
func NewDriverForStick(s StickInfo) (Driver, error) { return newDriverForStick(s) }

// FindDriverForStick returns an opened driver bound to the specific stick.
func FindDriverForStick(s StickInfo) (Driver, error) {
	d, err := newDriverForStick(s)
	if err != nil {
		return nil, err
	}
	if err := d.Open(); err != nil {
		return nil, fmt.Errorf("ant: open driver: %w", err)
	}
	return d, nil
}

// newDriverForStick probes the registry and returns an unopened driver
// bound to the given stick.
func newDriverForStick(s StickInfo) (Driver, error) {
	productsSeen := false
	for _, f := range Drivers() {
		if f.Name != s.Product {
			continue
		}
		productsSeen = true
		if f.NewForStick == nil {
			break
		}
		return f.NewForStick(s), nil
	}
	if productsSeen {
		return nil, fmt.Errorf("ant: driver %q cannot address sticks individually", s.Product)
	}
	return nil, fmt.Errorf("ant: no ANT driver named %q; list attached sticks with ant.Sticks or 'goant sticks'", s.Product)
}

// newDriver probes the registry and returns an unopened driver instance.
func newDriver(serial string) (Driver, error) {
	serialsSeen := false
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
			serialsSeen = true
			if !match {
				continue
			}
		}
		return f.New(), nil
	}
	if serial != "" && serialsSeen {
		return nil, fmt.Errorf("ant: no ANT device with serial %q; list readable serials with ant.Serials or 'goant sticks' (sticks with broken USB serial descriptors, e.g. some CYCPLUS clones, cannot be selected)", serial)
	}
	return nil, ErrDriverNotFound
}

func findDriver(serial string) (Driver, error) {
	d, err := newDriver(serial)
	if err != nil {
		return nil, err
	}
	if err := d.Open(); err != nil {
		return nil, fmt.Errorf("ant: open driver: %w", err)
	}
	return d, nil
}
