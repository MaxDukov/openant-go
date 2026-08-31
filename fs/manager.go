package fs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxdukov/openant-go/ant"
	"github.com/maxdukov/openant-go/easy"
)

// ANT-FS network key (public ANT-FS key, cf. openant.fs.manager).
var FSNetworkKey = []byte{0xA8, 0xA4, 0x23, 0xB9, 0xF5, 0x5E, 0x63, 0xC1}

// Default session parameters.
const (
	DefaultHostSerial uint32 = 1337
	DefaultFrequency  byte   = 19 // RF 2400 + 19 MHz
	// DefaultCommandTimeout is the wait for ANT-FS command responses
	// (openant uses 15 s). Pairing uses PairingTimeout.
	DefaultCommandTimeout = 15 * time.Second
	// PairingTimeout covers the user confirming pairing on the device.
	PairingTimeout = 30 * time.Second
	// authBeaconLimit bounds the beacon loop waiting for AUTHENTICATION.
	authBeaconLimit = 5
)

// ProtocolError is an ANT-FS level failure carrying the response code.
type ProtocolError struct {
	Op   string
	Code byte
	Msg  string
}

func (e *ProtocolError) Error() string {
	if e.Code != 0 || e.Msg != "" {
		return fmt.Sprintf("fs: %s failed: %s (code %d)", e.Op, e.Msg, e.Code)
	}
	return fmt.Sprintf("fs: %s failed", e.Op)
}

// Sentinel errors.
var (
	ErrSessionClosed = errors.New("fs: session closed")
)

// ProgressFn receives transfer progress in [0, 1].
type ProgressFn func(progress float64)

// Application drives an ANT-FS session over an easy node: link ->
// authentication -> transport, then download/upload/erase operations. Hook
// functions replace the Python subclassing of openant.fs.manager.Application.
type Application struct {
	node    *easy.Node
	channel *easy.Channel
	log     *slog.Logger

	SerialNumber uint32
	Frequency    byte

	beacons  chan Beacon
	commands chan Command

	ownsNode bool
	runDone  chan struct{}

	// SetupChannel configures the channel before the session starts
	// (period, search timeout, rf frequency, channel id, open).
	SetupChannel func(ch *easy.Channel) error
	// OnLink is called with the first beacon; return true to establish the
	// link (call app.Link inside).
	OnLink func(app *Application, beacon Beacon) bool
	// OnAuthentication is called when the device reaches AUTHENTICATION
	// state; return true on successful authentication.
	OnAuthentication func(app *Application, beacon Beacon) bool
	// OnTransport is called once authenticated (TRANSPORT state).
	OnTransport func(app *Application, beacon Beacon)
}

// AppOption configures an Application before its channel is set up.
type AppOption func(*Application)

// WithSetupChannel installs the channel configuration hook (period,
// search timeout, rf frequency, channel id, open).
func WithSetupChannel(fn func(ch *easy.Channel) error) AppOption {
	return func(a *Application) { a.SetupChannel = fn }
}

// WithOnLink installs the link-phase hook.
func WithOnLink(fn func(app *Application, beacon Beacon) bool) AppOption {
	return func(a *Application) { a.OnLink = fn }
}

// WithOnAuthentication installs the authentication-phase hook.
func WithOnAuthentication(fn func(app *Application, beacon Beacon) bool) AppOption {
	return func(a *Application) { a.OnAuthentication = fn }
}

// WithOnTransport installs the transport-phase hook.
func WithOnTransport(fn func(app *Application, beacon Beacon)) AppOption {
	return func(a *Application) { a.OnTransport = fn }
}

// WithAppLogger sets a custom logger.
func WithAppLogger(l *slog.Logger) AppOption {
	return func(a *Application) { a.log = l }
}

// NewApplication creates an ANT-FS application on the given node. If node
// is nil a new one is created (and owned) via easy.New(). The channel is
// configured with the ANT-FS network key and the node run loop is started
// in a background goroutine.
func NewApplication(node *easy.Node, opts ...AppOption) (*Application, error) {
	owns := false
	if node == nil {
		var err error
		node, err = easy.New()
		if err != nil {
			return nil, err
		}
		owns = true
	}
	app := &Application{
		node:         node,
		log:          slog.Default(),
		SerialNumber: DefaultHostSerial,
		Frequency:    DefaultFrequency,
		beacons:      make(chan Beacon, 16),
		commands:     make(chan Command, 16),
		ownsNode:     owns,
		runDone:      make(chan struct{}),
	}
	for _, o := range opts {
		o(app)
	}
	if err := node.SetNetworkKey(0x00, FSNetworkKey); err != nil {
		if owns {
			node.Stop()
		}
		return nil, fmt.Errorf("fs: set network key: %w", err)
	}
	ch, err := node.NewChannel(easy.ChannelBidirectionalReceive, 0x00, nil)
	if err != nil {
		if owns {
			node.Stop()
		}
		return nil, fmt.Errorf("fs: new channel: %w", err)
	}
	app.channel = ch
	ch.OnBroadcastData = app.onData
	ch.OnBurstData = app.onData

	// Start the run loop before the user hook so Stop (which joins it via
	// runDone) cannot deadlock when SetupChannel fails (code review PR #1,
	// P1-10).
	go func() {
		node.Run(context.Background())
		close(app.runDone)
	}()

	if app.SetupChannel != nil {
		if err := app.SetupChannel(ch); err != nil {
			app.Stop()
			return nil, fmt.Errorf("fs: setup channel: %w", err)
		}
	}
	return app, nil
}

// Channel returns the ANT-FS session channel.
func (a *Application) Channel() *easy.Channel { return a.channel }

// Node returns the underlying easy node.
func (a *Application) Node() *easy.Node { return a.node }

// onData classifies incoming data into beacons and commands, mirroring
// openant's _on_data. The payload length is controlled by the RF peer,
// so every branch must be bounds-checked (code review PR #1, P0-1).
func (a *Application) onData(data []byte) {
	if len(data) < 8 {
		// Beacons are 8 bytes; shorter payloads cannot carry one. A bare
		// command is longer still, so drop anything short.
		return
	}
	switch data[0] {
	case BeaconMark:
		if b, err := ParseBeacon(data[:8]); err == nil {
			select {
			case a.beacons <- b:
			default:
				a.log.Warn("beacon queue full, dropping")
			}
		}
		if len(data) > 8 {
			a.onCommand(data[8:])
		}
	case CommandMark:
		a.onCommand(data)
	}
}

func (a *Application) onCommand(data []byte) {
	cmd, err := ParseCommand(data)
	if err != nil {
		a.log.Warn("bad ANT-FS command", "error", err)
		return
	}
	select {
	case a.commands <- cmd:
	default:
		a.log.Warn("command queue full, dropping")
	}
}

// getBeacon blocks until the next beacon or ctx is done.
func (a *Application) getBeacon(ctx context.Context) (Beacon, error) {
	select {
	case b := <-a.beacons:
		return b, nil
	case <-ctx.Done():
		return Beacon{}, ctx.Err()
	}
}

// getCommand waits for a command response with timeout.
func (a *Application) getCommand(timeout time.Duration) (Command, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case c := <-a.commands:
		return c, nil
	case <-timer.C:
		return nil, ErrWaitTimeoutFS
	}
}

// ErrWaitTimeoutFS mirrors queue.Empty in openant's command wait.
var ErrWaitTimeoutFS = errors.New("fs: timed out waiting for command")

// sendCommand sends an 8 byte command as acknowledged data and longer ones
// as burst transfers.
func (a *Application) sendCommand(c Command) error {
	data := c.Bytes()
	if len(data) == 8 {
		return a.channel.SendAcknowledgedData(data)
	}
	return a.channel.SendBurstTransfer(data)
}

// Start runs the session state machine (blocking): wait for beacon,
// OnLink, up to five beacons to reach AUTHENTICATION, OnAuthentication,
// OnTransport, then disconnect. Mirrors openant's Application.start().
func (a *Application) Start(ctx context.Context) error {
	defer a.Stop()
	a.log.Debug("link level")
	beacon, err := a.getBeacon(ctx)
	if err != nil {
		return err
	}
	if a.OnLink == nil || !a.OnLink(a, beacon) {
		return nil
	}
	for i := 0; i < authBeaconLimit; i++ {
		beacon, err := a.getBeacon(ctx)
		if err != nil {
			return err
		}
		if beacon.ClientDeviceState() != StateAuthentication {
			continue
		}
		a.log.Debug("authentication layer")
		if a.OnAuthentication == nil || !a.OnAuthentication(a, beacon) {
			a.Disconnect()
			return nil
		}
		a.log.Debug("authenticated")
		beacon, err = a.getBeacon(ctx)
		if err != nil {
			return err
		}
		if a.OnTransport != nil {
			a.OnTransport(a, beacon)
		}
		a.Disconnect()
		return nil
	}
	return nil
}

// Stop shuts the application (and its node, when owned) down.
func (a *Application) Stop() {
	a.node.Stop()
	<-a.runDone
}

// Link establishes the transport channel, switching to the ANT-FS
// frequency and period.
func (a *Application) Link() error {
	if err := a.channel.RequestMessage(ant.IDSetChannelID); err != nil {
		return err
	}
	if err := a.sendCommand(Link{Frequency: a.Frequency, Period: 4, HostSerial: a.SerialNumber}); err != nil {
		return err
	}
	// New period, search timeout and RF frequency.
	if err := a.channel.SetPeriod(4096); err != nil {
		return err
	}
	if err := a.channel.SetSearchTimeout(10); err != nil {
		return err
	}
	return a.channel.SetRFFrequency(a.Frequency)
}

// AuthenticationSerial performs serial-number-based authentication,
// returning the client serial and its friendly name.
func (a *Application) AuthenticationSerial() (uint32, string, error) {
	if err := a.sendCommand(Authenticate{Type: AuthSerial, SerialNumber: a.SerialNumber}); err != nil {
		return 0, "", err
	}
	resp, err := a.getCommand(DefaultCommandTimeout)
	if err != nil {
		return 0, "", err
	}
	auth, ok := resp.(Authenticate)
	if !ok {
		return 0, "", &ProtocolError{Op: "authenticate", Msg: "unexpected response"}
	}
	return auth.SerialNumber, auth.DataString(), nil
}

// AuthenticationPasskey authenticates with a passkey obtained from a
// previous pairing.
func (a *Application) AuthenticationPasskey(passkey []byte) ([]byte, error) {
	if err := a.sendCommand(Authenticate{Type: AuthPasskeyExchange, SerialNumber: a.SerialNumber, Data: passkey}); err != nil {
		return nil, err
	}
	return a.awaitAuthAccept("passkey")
}

// AuthenticationPair pairs with the device, presenting a friendly name.
// The user may have to confirm on the device; the wait is therefore longer.
func (a *Application) AuthenticationPair(friendlyName string) ([]byte, error) {
	if err := a.sendCommand(Authenticate{Type: AuthPairing, SerialNumber: a.SerialNumber, Data: []byte(friendlyName)}); err != nil {
		return nil, err
	}
	return a.awaitAuthAccept("pair")
}

func (a *Application) awaitAuthAccept(op string) ([]byte, error) {
	timeout := DefaultCommandTimeout
	if op == "pair" {
		timeout = PairingTimeout
	}
	resp, err := a.getCommand(timeout)
	if err != nil {
		return nil, err
	}
	auth, ok := resp.(Authenticate)
	if !ok || !auth.Response {
		return nil, &ProtocolError{Op: op, Msg: "unexpected response"}
	}
	if auth.Type != AuthRespAccept {
		return nil, &ProtocolError{Op: op, Code: auth.Type, Msg: "authentication refused"}
	}
	return auth.Data, nil
}

// Disconnect returns the device to link mode.
func (a *Application) Disconnect() {
	d := Disconnect{CommandType: 0} // RETURN_LINK
	if err := a.sendCommand(d); err != nil {
		a.log.Warn("disconnect", "error", err)
	}
}

// MaxTransferBytes caps the total download size accepted from a device,
// protecting against crafted responses allocating unbounded memory
// (code review PR #1, P0-4).
const MaxTransferBytes = 64 << 20 // 64 MiB

// Download downloads a file by index, reporting progress via callback
// (may be nil). It retries on command timeouts, like openant. All response
// fields are device-controlled and validated before use.
func (a *Application) Download(index uint16, callback ProgressFn) ([]byte, error) {
	var (
		offset uint32
		crc    uint16
		data   []byte
	)
	for {
		a.log.Debug("download", "index", index, "offset", offset, "crc", crc)
		if err := a.sendCommand(DownloadRequest{DataIndex: index, DataOffset: offset, InitialRequest: true, CRCSeed: crc}); err != nil {
			return nil, err
		}
		resp, err := a.getCommand(DefaultCommandTimeout)
		if err != nil {
			if errors.Is(err, ErrWaitTimeoutFS) {
				a.log.Debug("download timeout, retrying", "index", index)
				continue
			}
			return nil, err
		}
		dr, ok := resp.(DownloadResponse)
		if !ok {
			return nil, &ProtocolError{Op: "download", Msg: "unexpected response"}
		}
		if dr.Response != DownloadOK {
			return nil, &ProtocolError{Op: "download", Code: dr.Response, Msg: "request refused"}
		}
		// Validate device-controlled offsets in 64 bit arithmetic so a
		// crafted response cannot overflow or force a huge allocation.
		total := uint64(dr.Offset) + uint64(dr.Remaining)
		if dr.Offset != offset {
			return nil, &ProtocolError{Op: "download", Code: dr.Response,
				Msg: fmt.Sprintf("unexpected offset %d (want %d)", dr.Offset, offset)}
		}
		if total > uint64(dr.Size) {
			return nil, &ProtocolError{Op: "download", Code: dr.Response,
				Msg: fmt.Sprintf("offset+remaining %d exceeds size %d", total, dr.Size)}
		}
		if total > MaxTransferBytes {
			return nil, &ProtocolError{Op: "download", Code: dr.Response,
				Msg: fmt.Sprintf("size %d exceeds limit %d", total, MaxTransferBytes)}
		}
		// Grow the buffer to `total` and copy the received block in place.
		if uint64(len(data)) < total {
			grown := make([]byte, total)
			copy(grown, data)
			data = grown
		}
		block := dr.Data
		if uint64(len(block)) > uint64(dr.Remaining) {
			block = block[:dr.Remaining]
		}
		copy(data[dr.Offset:total], block)

		if callback != nil && dr.Size != 0 {
			callback(float64(total) / float64(dr.Size))
		}
		if total == uint64(dr.Size) {
			return data, nil
		}
		crc = dr.CRC
		offset = uint32(total)
	}
}

// DownloadDirectory downloads and parses the directory (file index 0).
func (a *Application) DownloadDirectory(callback ProgressFn) (*Directory, error) {
	data, err := a.Download(0, callback)
	if err != nil {
		return nil, err
	}
	return ParseDirectory(data)
}

// Upload uploads data to the file with the given index. Response fields
// are device-controlled and validated before use (code review PR #1, P0-5).
func (a *Application) Upload(index uint16, data []byte, callback ProgressFn) error {
	iteration := 0
	for {
		// Continue using the Last Data Offset (special MAX_ULONG value).
		requestOffset := uint32(0)
		if iteration != 0 {
			requestOffset = 0xFFFFFFFF
		}
		if err := a.sendCommand(UploadRequest{DataIndex: index, MaxSize: uint32(len(data)), DataOffset: requestOffset}); err != nil {
			return err
		}
		resp, err := a.getCommand(DefaultCommandTimeout)
		if err != nil {
			return err
		}
		ur, ok := resp.(UploadResponse)
		if !ok {
			return &ProtocolError{Op: "upload", Msg: "unexpected response"}
		}
		if ur.Response != UploadOK {
			return &ProtocolError{Op: "upload", Code: ur.Response, Msg: "request refused"}
		}

		offset := ur.LastDataOffset
		maxBlock := uint64(ur.MaximumBlockSize)
		if uint64(offset) > uint64(len(data)) {
			return &ProtocolError{Op: "upload", Code: ur.Response,
				Msg: fmt.Sprintf("device offset %d beyond data length %d", offset, len(data))}
		}
		if maxBlock == 0 {
			return &ProtocolError{Op: "upload", Code: ur.Response, Msg: "device returned zero block size"}
		}
		end := uint64(offset) + maxBlock
		if end > uint64(len(data)) {
			end = uint64(len(data))
		}
		packet := append([]byte(nil), data[offset:end]...)
		crcSeed := ur.CRCSeed
		crcVal := CRC16(packet, crcSeed)
		packet = padTo8(packet)

		if err := a.sendCommand(UploadData{CRCSeed: crcSeed, DataOffset: offset, Data: packet, CRC: crcVal}); err != nil {
			return err
		}
		resp2, err := a.getCommand(DefaultCommandTimeout)
		if err != nil {
			return err
		}
		udr, ok := resp2.(UploadDataResponse)
		if !ok {
			return &ProtocolError{Op: "upload data", Msg: "unexpected response"}
		}
		if udr.Response != UploadDataOK {
			return &ProtocolError{Op: "upload data", Code: udr.Response, Msg: "block refused"}
		}

		if callback != nil && len(data) != 0 {
			callback((float64(offset) + float64(len(packet))) / float64(len(data)))
		}
		if uint64(offset)+uint64(len(packet)) >= uint64(len(data)) {
			return nil
		}
		iteration++
	}
}

// Create creates a new FIT file of the given type, uploads data and
// returns the new file index.
func (a *Application) Create(typ byte, data []byte, callback ProgressFn) (uint16, error) {
	req := PipeCreateFile{
		Seq:            NextPipeSequence(),
		Size:           uint32(len(data)),
		DataType:       FileFIT,
		Identifier:     [3]byte{typ, 0x00, 0x00},
		IdentifierMask: [3]byte{0x00, 0xFF, 0xFF},
	}
	if err := a.Upload(CommandPipeFileIndex, req.PipeBytes(), nil); err != nil {
		return 0, err
	}
	result, err := a.getCommandPipe()
	if err != nil {
		return 0, err
	}
	cr, ok := result.(PipeCreateFileResponse)
	if !ok {
		return 0, &ProtocolError{Op: "create", Msg: "unexpected response"}
	}
	if cr.Response != PipeRespOK {
		return 0, &ProtocolError{Op: "create", Code: cr.Response, Msg: "create refused"}
	}
	if callback != nil {
		callback(0)
	}
	if err := a.Upload(cr.Index, data, callback); err != nil {
		return 0, err
	}
	return cr.Index, nil
}

func (a *Application) sendCommandPipe(data []byte) error {
	return a.Upload(CommandPipeFileIndex, data, nil)
}

func (a *Application) getCommandPipe() (PipeCommand, error) {
	data, err := a.Download(CommandPipeFileIndex, nil)
	if err != nil {
		return nil, err
	}
	return ParsePipeCommand(data)
}

// SetTime sets the device time (defaults to now, UTC). The TAI offset is
// applied like in openant.
func (a *Application) SetTime(t ...time.Time) error {
	now := time.Now().UTC()
	if len(t) > 0 {
		now = t[0].UTC()
	}
	seconds := uint32(now.Sub(AntFSEpoch)/time.Second) + uint32(TAIOffset/time.Second)
	cmd := PipeTime{Seq: NextPipeSequence(), CurrentTime: seconds, SystemTime: 0xFFFFFFFF, TimeFormat: TimeFormatDirectory}
	if err := a.sendCommandPipe(cmd.PipeBytes()); err != nil {
		return err
	}
	result, err := a.getCommandPipe()
	if err != nil {
		return err
	}
	tr, ok := result.(PipeTimeResponse)
	if !ok {
		return &ProtocolError{Op: "set time", Msg: "unexpected response"}
	}
	if tr.Response != PipeRespOK {
		return &ProtocolError{Op: "set time", Code: tr.Response, Msg: "refused"}
	}
	return nil
}

// Erase erases the file with the given index.
func (a *Application) Erase(index uint16) error {
	if err := a.sendCommand(EraseRequest{DataFileIndex: uint32(index)}); err != nil {
		return err
	}
	resp, err := a.getCommand(DefaultCommandTimeout)
	if err != nil {
		return err
	}
	er, ok := resp.(EraseResponse)
	if !ok {
		return &ProtocolError{Op: "erase", Msg: "unexpected response"}
	}
	if er.Response != EraseSuccessful {
		return &ProtocolError{Op: "erase", Code: er.Response, Msg: "erase refused"}
	}
	return nil
}
