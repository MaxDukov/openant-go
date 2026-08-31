//go:build !cgo

package ant

import "errors"

// init registers USB driver stubs which explain that the binary was built
// without cgo support. Build with cgo enabled and libusb installed to use
// ANT USB sticks (see README).
func init() {
	for _, spec := range []struct {
		name     string
		priority byte
	}{
		{"usb2", PriorityUSB2},
		{"usb3", PriorityUSB3},
	} {
		RegisterDriver(DriverFactory{
			Name:     spec.name,
			Priority: spec.priority,
			Probe:    func() bool { return false },
			New: func() Driver {
				return &usbStub{}
			},
		})
	}
}

type usbStub struct{}

func (usbStub) Open() error {
	return errors.New("usb: driver not compiled (requires cgo and libusb; rebuild with CGO_ENABLED=1 and libusb installed)")
}
func (usbStub) Close() error              { return nil }
func (usbStub) Read([]byte) (int, error)  { return 0, ErrDriverClosed }
func (usbStub) Write([]byte) (int, error) { return 0, ErrDriverClosed }
