package fs

import (
	"context"
	"testing"
	"time"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/anttest"
	"github.com/maxdukov/openant-go/easy"
)

// fakeDevice emulates an ANT-FS device on top of the stick simulator: it
// answers link/auth/download/upload commands and beacons.
type fakeDevice struct {
	sim     *anttest.SimDriver
	node    *easy.Node
	channel byte
	serial  uint32
}

func newFakeDevice(t *testing.T) (*fakeDevice, *easy.Node) {
	t.Helper()
	sim := anttest.NewSimDriver()
	node, err := easy.NewWithDriver(sim)
	if err != nil {
		t.Fatalf("NewWithDriver: %v", err)
	}
	fd := &fakeDevice{sim: sim, node: node, channel: 0, serial: 42}
	// Beacon every 100 ms in link state until the test tears down.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			fd.emitBeacon(StateLink)
		}
	}()
	return fd, node
}

func (fd *fakeDevice) emitBeacon(state byte) {
	beacon := []byte{BeaconMark, 0x00, state, 0x00, byte(fd.serial), 0, 0, 0}
	fd.sim.EmitBroadcast(fd.channel, beacon)
}

func (fd *fakeDevice) emitCommand(c Command) {
	data := c.Bytes()
	// Commands arrive appended to a beacon or standalone.
	page := append([]byte{BeaconMark, 0x00, StateTransport, 0x00, byte(fd.serial), 0, 0, 0}, data...)
	fd.sim.EmitBurst(fd.channel, page)
}

// confirmTX emits the transfer events a real stick produces after the host
// sends acknowledged or burst data.
func (fd *fakeDevice) confirmTX(m *ant.Message) {
	if m.ID == ant.IDAcknowledgedData && len(m.Data) >= 1 {
		fd.sim.EmitAckEvent(m.Data[0]&0x1F, ant.EventTransferTxCompleted)
	}
	if m.ID == ant.IDBurstTransferData && len(m.Data) >= 1 {
		ch := m.Data[0] & 0x1F
		last := m.Data[0]&0x80 != 0
		if last {
			fd.sim.EmitAckEvent(ch, ant.EventTransferTxCompleted)
		}
	}
}

func TestApplicationSessionOnSimulator(t *testing.T) {
	fd, node := newFakeDevice(t)
	defer node.Stop()

	ctxRun, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	done := node.Start(ctxRun)
	defer func() { cancelRun(); <-done }()

	app, err := NewApplication(node,
		WithSetupChannel(func(ch *easy.Channel) error {
			if err := ch.SetPeriod(4096); err != nil {
				return err
			}
			return ch.Open()
		}),
	)
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	linkCalled := make(chan struct{}, 1)
	authCalled := make(chan struct{}, 1)
	transportCalled := make(chan struct{}, 1)

	// Respond to commands the app sends: when it sends Link, reply by
	// moving to AUTHENTICATION; Authenticate serial -> ACCEPT; then
	// TRANSPORT state. Ack/burst writes are confirmed with the transfer
	// complete events a real stick emits.
	go func() {
		seen := 0
		for {
			time.Sleep(50 * time.Millisecond)
			all := fd.sim.Writes()
			for ; seen < len(all); seen++ {
				msgs, _ := ant.ParseFrames(all[seen])
				for _, m := range msgs {
					fd.confirmTX(m)
					if m.ID != ant.IDAcknowledgedData && m.ID != ant.IDBurstTransferData {
						continue
					}
					if len(m.Data) < 2 {
						continue
					}
					cmd, err := ParseCommand(m.Data[1:])
					if err != nil {
						continue
					}
					switch cmd.Kind() {
					case KindLink:
						fd.emitBeacon(StateAuthentication)
					case KindAuthenticate:
						if !cmd.(Authenticate).Response {
							fd.emitCommand(Authenticate{Response: true, Type: AuthRespAccept, SerialNumber: fd.serial})
							fd.emitBeacon(StateTransport)
						}
					case KindPing, KindDisconnect:
						fd.emitBeacon(StateLink)
					}
				}
			}
		}
	}()

	app.OnLink = func(a *Application, beacon Beacon) bool {
		select {
		case linkCalled <- struct{}{}:
		default:
		}
		return a.Link() == nil
	}
	app.OnAuthentication = func(a *Application, beacon Beacon) bool {
		select {
		case authCalled <- struct{}{}:
		default:
		}
		_, _, err := a.AuthenticationSerial()
		return err == nil
	}
	app.OnTransport = func(a *Application, beacon Beacon) {
		select {
		case transportCalled <- struct{}{}:
		default:
		}
	}

	go func() {
		time.Sleep(3 * time.Second)
		cancelRun()
	}()

	if err := app.Start(ctxRun); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for _, stage := range []struct {
		name string
		ch   chan struct{}
	}{
		{"link", linkCalled},
		{"auth", authCalled},
		{"transport", transportCalled},
	} {
		select {
		case <-stage.ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("session did not reach %s stage", stage.name)
		}
	}
}
