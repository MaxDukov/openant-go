//go:build integration

// Integration tests against a real ANT USB stick. Run with:
//
//	go test -tags integration ./...
//
// They are additionally gated on the ANT_TEST_USB_STICK environment
// variable (set it to a non-zero value to enable), mirroring openant's
// tests/easy/test.py behaviour.
package ant_test

import (
	"os"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/ant"
)

func stickAvailable() bool {
	v := os.Getenv("ANT_TEST_USB_STICK")
	return v != "" && v != "0"
}

func skipWithoutStick(t *testing.T) {
	t.Helper()
	if !stickAvailable() {
		t.Skip("set ANT_TEST_USB_STICK=1 to run tests against a real ANT USB stick")
	}
}

func TestFindDriver(t *testing.T) {
	skipWithoutStick(t)
	d, err := ant.FindDriver()
	if err != nil {
		t.Fatalf("FindDriver: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCoreCapabilities(t *testing.T) {
	skipWithoutStick(t)
	d, err := ant.FindDriver()
	if err != nil {
		t.Fatalf("FindDriver: %v", err)
	}
	var gotCaps chan *ant.Capabilities = make(chan *ant.Capabilities, 1)
	core, err := ant.NewCore(d, ant.WithEventHandler(func(ev ant.Event) {
		if ev.Kind == ant.KindResponse && ev.Code == ant.Code(ant.IDCapabilities) {
			if caps, err := ant.ParseCapabilities(ev.Data); err == nil {
				select {
				case gotCaps <- caps:
				default:
				}
			}
		}
	}))
	if err != nil {
		t.Fatalf("NewCore: %v", err)
	}
	defer core.Stop()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case caps := <-gotCaps:
			if caps.MaxChannels == 0 || caps.MaxNetworks == 0 {
				t.Fatalf("suspicious capabilities: %+v", caps)
			}
			return
		case <-deadline:
			t.Fatal("timeout waiting for capabilities response")
		default:
			core.RequestMessage(0, ant.IDCapabilities)
			time.Sleep(200 * time.Millisecond)
		}
	}
}
