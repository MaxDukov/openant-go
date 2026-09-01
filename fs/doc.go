// Package fs implements the ANT-FS host: beacon tracking, authentication,
// directory download and file transfer to ANT-FS devices (for example
// Garmin fitness watches).
//
// It is the Go equivalent of openant.fs (github.com/Tigge/openant).
//
// # Layers
//
// The transport side lives in sub-organised files: beacon parsing,
// command pipes and the file transfer primitives (Download, Upload,
// CreateFile, Erase). The user-facing entry point is [Application]: it
// opens a channel on the ANT-FS network (key A8A423B9F55E63C1), binds to
// the first beacon it sees and drives the command/response state machine.
//
// # Typical usage
//
//	n, _ := easy.New()
//	app, _ := fs.NewApplication(n, fs.WithSetupChannel(func(ch *easy.Channel) error {
//	    if err := ch.SetID(0, 0x01, 0); err != nil { return err } // any ANT-FS device
//	    if err := ch.SetPeriod(4096); err != nil { return err }
//	    if err := ch.SetRFFrequency(9); err != nil { return err } // 2409 MHz
//	    return ch.SetSearchTimeout(0xFF)
//	}))
//
//	app.OnAuth = func(beacon *fs.Beacon) error {
//	    return app.AuthenticationSerial() // or pass/fetch a pairing code
//	}
//	app.OnDownloadDirectory = func(dir *fs.Directory) error {
//	    for _, f := range dir.Files { fmt.Println(f) }
//	    return fs.ErrDownloadFinished
//	}
//	app.Start()
//	defer app.Stop()
//
// The `goant` CLI (cmd/goant) exposes the same flow as `goant antfs`.
// Garmin devices authenticate as "client" with a passkey exchange; see
// docs/PROTOCOL.md for the ANT-FS message flow and quirks (TAI time
// offset, download CRC seed widths).
package fs
