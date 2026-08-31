//go:build integration

package easy_test

import (
	"os"
	"testing"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/easy"
)

func TestNodeWithRealStick(t *testing.T) {
	if v := os.Getenv("ANT_TEST_USB_STICK"); v == "" || v == "0" {
		t.Skip("set ANT_TEST_USB_STICK=1 to run tests against a real ANT USB stick")
	}
	n, err := easy.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Stop()

	if err := n.RequestMessage(ant.IDAntVersion); err != nil {
		t.Fatalf("RequestMessage: %v", err)
	}
	if n.AntVersion() == "" {
		t.Fatal("empty ANT version")
	}
	t.Logf("ANT version %s, serial %d, channels %d, networks %d",
		n.AntVersion(), n.SerialNumber(), n.MaxChannels(), n.MaxNetworks())
}
