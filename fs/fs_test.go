package fs

import (
	"bytes"
	"testing"
	"time"
)

func TestParseBeacon(t *testing.T) {
	// From openant tests: link state beacon.
	data := []byte{0x43, 0x00, 0x00, 0x00, 0x5E, 0x00, 0x00, 0x00}
	b, err := ParseBeacon(data)
	if err != nil {
		t.Fatalf("ParseBeacon: %v", err)
	}
	if b.ClientDeviceState() != StateLink {
		t.Fatalf("state = %#x", b.ClientDeviceState())
	}
	if b.Serial() != 0x5E {
		t.Fatalf("serial = %#x", b.Serial())
	}
	if b.DataAvailable() || b.UploadEnabled() || b.PairingEnabled() {
		t.Fatal("unexpected status flags")
	}
	// Data available + upload enabled + pairing + transport state.
	data2 := []byte{0x43, 0x38, 0x02, 0x02, 0x11, 0x22, 0x33, 0x44}
	b2, err := ParseBeacon(data2)
	if err != nil {
		t.Fatalf("ParseBeacon: %v", err)
	}
	if !b2.DataAvailable() || !b2.UploadEnabled() || !b2.PairingEnabled() {
		t.Fatal("expected status flags set")
	}
	if b2.ClientDeviceState() != StateTransport {
		t.Fatalf("state = %#x", b2.ClientDeviceState())
	}
	if b2.Serial() != 0x44332211 {
		t.Fatalf("serial = %#x", b2.Serial())
	}
	if _, err := ParseBeacon([]byte{0x44, 1, 2, 3, 4, 5, 6, 7}); err == nil {
		t.Fatal("expected error for bad mark")
	}
}

func TestCRC16(t *testing.T) {
	// Known CRC-16/ARC vector: "123456789" -> 0xBB3D.
	if got := CRC16([]byte("123456789"), 0); got != 0xBB3D {
		t.Fatalf("crc = %#04x, want 0xBB3D", got)
	}
	// Chaining equals single computation with seed.
	full := CRC16([]byte("123456789"), 0)
	part1 := CRC16([]byte("1234"), 0)
	part2 := CRC16([]byte("56789"), part1)
	if part2 != full {
		t.Fatalf("chained crc = %#04x, want %#04x", part2, full)
	}
}

func TestAuthenticateCommandBytes(t *testing.T) {
	// Golden bytes from openant.fs.tests.test_command.
	c := Authenticate{Type: AuthSerial, SerialNumber: 123456789}
	want := []byte{0x44, 0x04, 0x01, 0x00, 0x15, 0xCD, 0x5B, 0x07}
	if got := c.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("bytes = % X, want % X", got, want)
	}

	c2 := Authenticate{Type: AuthPairing, SerialNumber: 987654321, Data: []byte("hello")}
	want2 := []byte{
		0x44, 0x04, 0x02, 0x05, 0xB1, 0x68, 0xDE, 0x3A,
		'h', 'e', 'l', 'l', 'o', 0x00, 0x00, 0x00,
	}
	if got := c2.Bytes(); !bytes.Equal(got, want2) {
		t.Fatalf("bytes = % X, want % X", got, want2)
	}

	// Round trip.
	parsed, err := ParseCommand(c2.Bytes())
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	a, ok := parsed.(Authenticate)
	if !ok || a.Type != AuthPairing || a.SerialNumber != 987654321 || a.DataString() != "hello" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestDownloadRequestParse(t *testing.T) {
	// Golden bytes from openant tests.
	data := []byte{
		0x44, 0x09, 0x5F, 0x00, 0x00, 0xBA, 0x00, 0x00,
		0x00, 0x00, 0x9E, 0xC2, 0x00, 0x00, 0x00, 0x00,
	}
	cmd, err := ParseCommand(data)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	req, ok := cmd.(DownloadRequest)
	if !ok {
		t.Fatalf("type = %T", cmd)
	}
	if req.DataIndex != 95 || req.DataOffset != 47616 || req.InitialRequest {
		t.Fatalf("req = %+v", req)
	}
	if req.CRCSeed != 49822 {
		t.Fatalf("crc seed = %d, want 49822", req.CRCSeed)
	}
	if req.MaximumBlockSize != 0 {
		t.Fatalf("max block = %d", req.MaximumBlockSize)
	}
	// Round trip.
	if !bytes.Equal(req.Bytes(), data) {
		t.Fatalf("round trip = % X", req.Bytes())
	}
}

func TestDownloadResponseParse(t *testing.T) {
	// OK response with 8 data bytes and crc.
	data := []byte{
		0x44, 0x89, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00,
		1, 2, 3, 4, 5, 6, 7, 8,
		0, 0, 0, 0, 0, 0, 0xAB, 0xCD,
	}
	cmd, err := ParseCommand(data)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	resp, ok := cmd.(DownloadResponse)
	if !ok {
		t.Fatalf("type = %T", cmd)
	}
	if resp.Response != DownloadOK || resp.Remaining != 8 || resp.Offset != 0 || resp.Size != 16 {
		t.Fatalf("resp = %+v", resp)
	}
	if !bytes.Equal(resp.Data, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("data = % X", resp.Data)
	}
	if resp.CRC != 0xCDAB {
		t.Fatalf("crc = %#x", resp.CRC)
	}

	// Error response: NOT_READABLE.
	errData := []byte{
		0x44, 0x89, 0x02, 0x00, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
	cmd2, err := ParseCommand(errData)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	resp2, _ := cmd2.(DownloadResponse)
	if resp2.Response != DownloadNotReadable || resp2.Data != nil {
		t.Fatalf("resp2 = %+v", resp2)
	}
}

func TestCommandRoundTrips(t *testing.T) {
	cmds := []Command{
		Link{Frequency: 19, Period: 4, HostSerial: 1337},
		Disconnect{CommandType: 1, TimeDuration: 2, ApplicationSpecific: 3},
		Ping{},
		UploadRequest{DataIndex: 5, MaxSize: 100, DataOffset: 0xFFFFFFFF},
		UploadResponse{Response: UploadOK, LastDataOffset: 16, MaximumFileSize: 1024, MaximumBlockSize: 64, CRCSeed: 0xABCD},
		UploadData{CRCSeed: 7, DataOffset: 8, Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}, CRC: 9},
		UploadDataResponse{Response: UploadDataOK},
		EraseRequest{DataFileIndex: 42},
		EraseResponse{Response: EraseFailed},
	}
	for _, c := range cmds {
		parsed, err := ParseCommand(c.Bytes())
		if err != nil {
			t.Fatalf("%T: ParseCommand: %v", c, err)
		}
		// Re-encode and compare.
		if !bytes.Equal(parsed.Bytes(), c.Bytes()) {
			t.Fatalf("%T: round trip mismatch:\n got % X\nwant % X", c, parsed.Bytes(), c.Bytes())
		}
	}
}

func TestParsePipeCommandGolden(t *testing.T) {
	// Request(TIME) with seq 1: 01 00 00 01 03 00 00 00.
	req, err := ParsePipeCommand([]byte{0x01, 0x00, 0x00, 0x01, 0x03, 0x00, 0x00, 0x00})
	if err != nil {
		t.Fatalf("ParsePipeCommand: %v", err)
	}
	if r, ok := req.(PipeRequest); !ok || r.RequestID != PipeTypeTime || r.Sequence() != 1 {
		t.Fatalf("req = %#v", req)
	}

	// TimeResponse golden from openant tests.
	tr, err := ParsePipeCommand([]byte{
		0x02, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	})
	if err != nil {
		t.Fatalf("ParsePipeCommand: %v", err)
	}
	if r, ok := tr.(PipeTimeResponse); !ok || r.RequestID != PipeTypeTime || r.Response != PipeRespOK {
		t.Fatalf("tr = %#v", tr)
	}

	// CreateFileResponse golden from openant tests.
	cr, err := ParsePipeCommand([]byte{
		0x02, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00,
		0x80, 0x04, 0x7B, 0x00, 0x67, 0x00, 0x00, 0x00,
	})
	if err != nil {
		t.Fatalf("ParsePipeCommand: %v", err)
	}
	cfr, ok := cr.(PipeCreateFileResponse)
	if !ok {
		t.Fatalf("type = %T", cr)
	}
	if cfr.RequestID != PipeTypeCreateFile || cfr.Response != PipeRespOK {
		t.Fatalf("cfr = %#v", cfr)
	}
	if cfr.DataType != FileFIT || cfr.Index != 103 {
		t.Fatalf("cfr = %#v", cfr)
	}
	if cfr.Identifier != [3]byte{0x04, 0x7B, 0x00} {
		t.Fatalf("identifier = % X", cfr.Identifier)
	}
}

func TestPipeTimeBytes(t *testing.T) {
	// openant test: Time(seq=1, 42, 42, Time.Format.SYSTEM).
	c := PipeTime{Seq: 1, CurrentTime: 42, SystemTime: 42, TimeFormat: TimeFormatSystem}
	want := []byte{
		0x03, 0x00, 0x00, 0x01, 42, 0x00, 0x00, 0x00,
		42, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
	}
	if got := c.PipeBytes(); !bytes.Equal(got, want) {
		t.Fatalf("bytes = % X, want % X", got, want)
	}
}

func TestCreateFileBytes(t *testing.T) {
	c := PipeCreateFile{
		Seq:            1,
		Size:           2,
		DataType:       FileFIT,
		Identifier:     [3]byte{0x04, 0x00, 0x00},
		IdentifierMask: [3]byte{0x00, 0xFF, 0xFF},
	}
	want := []byte{
		0x04, 0x00, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00,
		0x80, 0x04, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF,
	}
	if got := c.PipeBytes(); !bytes.Equal(got, want) {
		t.Fatalf("bytes = % X, want % X", got, want)
	}
	parsed, err := ParsePipeCommand(c.PipeBytes())
	if err != nil {
		t.Fatalf("ParsePipeCommand: %v", err)
	}
	cf, ok := parsed.(PipeCreateFile)
	if !ok || cf.Size != 2 || cf.DataType != FileFIT {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestParseDirectoryAndFile(t *testing.T) {
	// Build a minimal directory: 16 byte header + 2 entries.
	dir := make([]byte, 16)
	dir[0] = 0x81 // version 1.1
	dir[2] = 0    // time format system
	le32(dir[8:], 100)
	le32(dir[12:], 200)
	entry1 := make([]byte, 16)
	le16(entry1[0:], 1)
	entry1[2] = FileFIT
	entry1[3] = FitActivity
	le32(entry1[8:], 1024)
	le32(entry1[12:], 0x60000000) // some date
	entry2 := make([]byte, 16)
	le16(entry2[0:], 2)
	entry2[2] = FileFIT
	entry2[3] = FitSetting
	entry2[7] = FlagReadable | FlagErasable | FlagArchived
	le32(entry2[8:], 2048)
	le32(entry2[12:], 1000)

	d, err := ParseDirectory(append(append(dir, entry1...), entry2...))
	if err != nil {
		t.Fatalf("ParseDirectory: %v", err)
	}
	if len(d.Files) != 2 {
		t.Fatalf("files = %d", len(d.Files))
	}
	if d.Version != 0x81 || d.TimeFormat != 0 {
		t.Fatalf("header = %+v", d)
	}
	f1 := d.Get(1)
	if f1 == nil || f1.Size != 1024 || f1.FITSubType() != FitActivity {
		t.Fatalf("f1 = %+v", f1)
	}
	f2 := d.Get(2)
	if f2 == nil || f2.FITFileNumber() != 0 {
		t.Fatalf("f2 = %+v", f2)
	}
	if got := f2.FlagString(); got != "r-eA--" {
		t.Fatalf("flags = %q, want r-eA--", got)
	}
	// Time conversion sanity: epoch + 0 == 1989-12-31.
	if tt := (File{Date: 0}).Time(); tt.Year() != 1989 || tt.Month() != time.December || tt.Day() != 31 {
		t.Fatalf("epoch date = %v", tt)
	}
}

func TestTimeConversion(t *testing.T) {
	unix := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	sec := TimeToANTFSSeconds(unix)
	if got := TimeFromANTFSSeconds(sec); !got.Equal(unix) {
		t.Fatalf("round trip = %v, want %v", got, unix)
	}
}
