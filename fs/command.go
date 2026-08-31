package fs

import (
	"encoding/binary"
	"fmt"
)

// CommandKind is an ANT-FS command type byte (bit 7 set for responses).
type CommandKind byte

// ANT-FS command types.
const (
	KindLink                 CommandKind = 0x02
	KindDisconnect           CommandKind = 0x03
	KindAuthenticate         CommandKind = 0x04
	KindPing                 CommandKind = 0x05
	KindDownloadRequest      CommandKind = 0x09
	KindUploadRequest        CommandKind = 0x0A
	KindEraseRequest         CommandKind = 0x0B
	KindUploadData           CommandKind = 0x0C
	KindAuthenticateResponse CommandKind = 0x84
	KindDownloadResponse     CommandKind = 0x89
	KindUploadResponse       CommandKind = 0x8A
	KindEraseResponse        CommandKind = 0x8B
	KindUploadDataResponse   CommandKind = 0x8C
)

// Authenticate request types.
const (
	AuthPassThrough     byte = 0
	AuthSerial          byte = 1
	AuthPairing         byte = 2
	AuthPasskeyExchange byte = 3
)

// Authenticate response types.
const (
	AuthRespNotAvailable byte = 0
	AuthRespAccept       byte = 1
	AuthRespReject       byte = 2
)

// Download response codes.
const (
	DownloadOK           byte = 0
	DownloadNotExist     byte = 1
	DownloadNotReadable  byte = 2
	DownloadNotReady     byte = 3
	DownloadInvalid      byte = 4
	DownloadIncorrectCRC byte = 5
)

// Upload response codes.
const (
	UploadOK             byte = 0
	UploadNotExist       byte = 1
	UploadNotWriteable   byte = 2
	UploadNotEnoughSpace byte = 3
	UploadInvalid        byte = 4
	UploadNotReady       byte = 5
)

// Upload data response codes.
const (
	UploadDataOK     byte = 0
	UploadDataFailed byte = 1
)

// Erase response codes.
const (
	EraseSuccessful byte = 0
	EraseFailed     byte = 1
	EraseNotReady   byte = 2
)

// Command is an ANT-FS command that can serialise itself to wire bytes.
type Command interface {
	Kind() CommandKind
	Bytes() []byte
}

// padTo8 zero-pads data to a multiple of 8 bytes.
func padTo8(data []byte) []byte {
	if r := len(data) % 8; r != 0 {
		return append(append([]byte(nil), data...), make([]byte, 8-r)...)
	}
	return append([]byte(nil), data...)
}

func le16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func le32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }

// ---- LINK ----

// Link switches the beacon channel to the transport frequency/period.
type Link struct {
	Frequency  byte
	Period     byte
	HostSerial uint32
}

func (Link) Kind() CommandKind { return KindLink }

func (c Link) Bytes() []byte {
	b := make([]byte, 8)
	b[0], b[1] = CommandMark, byte(KindLink)
	b[2], b[3] = c.Frequency, c.Period
	le32(b[4:], c.HostSerial)
	return b
}

// ---- DISCONNECT ----

// Disconnect commands return to the link state or broadcast mode.
type Disconnect struct {
	CommandType         byte // 0 = return to link, 1 = return to broadcast
	TimeDuration        byte
	ApplicationSpecific byte
}

func (Disconnect) Kind() CommandKind { return KindDisconnect }

func (c Disconnect) Bytes() []byte {
	return []byte{CommandMark, byte(KindDisconnect), c.CommandType, c.TimeDuration, c.ApplicationSpecific, 0, 0, 0}
}

// ---- AUTHENTICATE ----

// Authenticate is both the request (kind 0x04) and response (0x84) carrier:
// byte layout is identical, only the type constants differ.
type Authenticate struct {
	Response     bool   // true for 0x84, false for 0x04
	Type         byte   // request or response type
	SerialNumber uint32 // host serial
	Data         []byte // unpadded payload (friendly name / passkey)
}

func (c Authenticate) Kind() CommandKind {
	if c.Response {
		return KindAuthenticateResponse
	}
	return KindAuthenticate
}

func (c Authenticate) Bytes() []byte {
	head := make([]byte, 8)
	head[0] = CommandMark
	head[1] = byte(c.Kind())
	head[2] = c.Type
	head[3] = byte(len(c.Data)) // real length, not padded
	le32(head[4:], c.SerialNumber)
	return append(head, padTo8(c.Data)...)
}

// DataString returns the payload as a string (friendly names).
func (c Authenticate) DataString() string { return string(c.Data) }

func parseAuthenticate(data []byte) (Authenticate, error) {
	var c Authenticate
	if len(data) < 8 || data[0] != CommandMark {
		return c, ErrBadCommand
	}
	c.Response = data[1] == byte(KindAuthenticateResponse)
	c.Type = data[2]
	c.SerialNumber = binary.LittleEndian.Uint32(data[4:8])
	n := int(data[3])
	if n > 0 {
		if 8+n > len(data) {
			return c, ErrBadCommand
		}
		c.Data = append([]byte(nil), data[8:8+n]...)
	}
	return c, nil
}

// ---- PING ----

// Ping is a 2 byte keep-alive.
type Ping struct{}

func (Ping) Kind() CommandKind { return KindPing }

func (Ping) Bytes() []byte { return []byte{CommandMark, byte(KindPing)} }

// ---- DOWNLOAD ----

// DownloadRequest asks for a block of a file. The wire layout follows
// openant exactly (crc seed u16 at bytes 10-11, max block size u32 at
// 12-15), which is byte-compatible with real devices.
type DownloadRequest struct {
	DataIndex        uint16
	DataOffset       uint32
	InitialRequest   bool
	CRCSeed          uint16
	MaximumBlockSize uint32
}

func (DownloadRequest) Kind() CommandKind { return KindDownloadRequest }

func (c DownloadRequest) Bytes() []byte {
	b := make([]byte, 16)
	b[0], b[1] = CommandMark, byte(KindDownloadRequest)
	le16(b[2:], c.DataIndex)
	le32(b[4:], c.DataOffset)
	if c.InitialRequest {
		b[9] = 1
	}
	le16(b[10:], c.CRCSeed)
	le32(b[12:], c.MaximumBlockSize)
	return b
}

func parseDownloadRequest(data []byte) (DownloadRequest, error) {
	var c DownloadRequest
	if len(data) < 16 || data[0] != CommandMark {
		return c, ErrBadCommand
	}
	c.DataIndex = binary.LittleEndian.Uint16(data[2:4])
	c.DataOffset = binary.LittleEndian.Uint32(data[4:8])
	c.InitialRequest = data[9] != 0
	c.CRCSeed = binary.LittleEndian.Uint16(data[10:12])
	c.MaximumBlockSize = binary.LittleEndian.Uint32(data[12:16])
	return c, nil
}

// DownloadResponse carries a data block (OK) or an error code.
type DownloadResponse struct {
	Response  byte
	Remaining uint32 // bytes remaining after this block
	Offset    uint32
	Size      uint32 // total file size
	Data      []byte
	CRC       uint16
}

func (DownloadResponse) Kind() CommandKind { return KindDownloadResponse }

func (c DownloadResponse) Bytes() []byte {
	if c.Response != DownloadOK {
		b := make([]byte, 16)
		b[0], b[1] = CommandMark, byte(KindDownloadResponse)
		b[2] = c.Response
		le32(b[4:], c.Remaining)
		le32(b[8:], c.Offset)
		le32(b[12:], c.Size)
		return b
	}
	b := make([]byte, 16, 16+len(c.Data)+8)
	b[0], b[1] = CommandMark, byte(KindDownloadResponse)
	b[2] = c.Response
	le32(b[4:], c.Remaining)
	le32(b[8:], c.Offset)
	le32(b[12:], c.Size)
	b = append(b, c.Data...)
	footer := make([]byte, 8)
	le16(footer[6:], c.CRC)
	return append(b, footer...)
}

func parseDownloadResponse(data []byte) (DownloadResponse, error) {
	var c DownloadResponse
	if len(data) < 16 || data[0] != CommandMark {
		return c, ErrBadCommand
	}
	c.Response = data[2]
	c.Remaining = binary.LittleEndian.Uint32(data[4:8])
	c.Offset = binary.LittleEndian.Uint32(data[8:12])
	c.Size = binary.LittleEndian.Uint32(data[12:16])
	if c.Response == DownloadOK {
		if len(data) < 24 {
			return c, ErrBadCommand
		}
		c.Data = append([]byte(nil), data[16:len(data)-8]...)
		c.CRC = binary.LittleEndian.Uint16(data[len(data)-2:])
	}
	return c, nil
}

// ---- UPLOAD ----

// UploadRequest asks the device for an upload slot. DataOffset 0xFFFFFFFF
// means "continue from the last offset".
type UploadRequest struct {
	DataIndex  uint16
	MaxSize    uint32
	DataOffset uint32
}

func (UploadRequest) Kind() CommandKind { return KindUploadRequest }

func (c UploadRequest) Bytes() []byte {
	b := make([]byte, 16)
	b[0], b[1] = CommandMark, byte(KindUploadRequest)
	le16(b[2:], c.DataIndex)
	le32(b[4:], c.MaxSize)
	le32(b[12:], c.DataOffset)
	return b
}

// UploadResponse tells the host where and how much to write.
type UploadResponse struct {
	Response         byte
	LastDataOffset   uint32
	MaximumFileSize  uint32
	MaximumBlockSize uint32
	CRCSeed          uint16
}

func (UploadResponse) Kind() CommandKind { return KindUploadResponse }

func (c UploadResponse) Bytes() []byte {
	b := make([]byte, 24)
	b[0], b[1] = CommandMark, byte(KindUploadResponse)
	b[2] = c.Response
	le32(b[4:], c.LastDataOffset)
	le32(b[8:], c.MaximumFileSize)
	le32(b[12:], c.MaximumBlockSize)
	le16(b[22:], c.CRCSeed)
	return b
}

func parseUploadResponse(data []byte) (UploadResponse, error) {
	var c UploadResponse
	if len(data) < 24 || data[0] != CommandMark {
		return c, ErrBadCommand
	}
	c.Response = data[2]
	c.LastDataOffset = binary.LittleEndian.Uint32(data[4:8])
	c.MaximumFileSize = binary.LittleEndian.Uint32(data[8:12])
	c.MaximumBlockSize = binary.LittleEndian.Uint32(data[12:16])
	c.CRCSeed = binary.LittleEndian.Uint16(data[22:24])
	return c, nil
}

// ---- UPLOAD DATA ----

// UploadData writes one block of an upload. Data must already be padded to
// a multiple of 8 bytes (see Application.upload).
type UploadData struct {
	CRCSeed    uint16
	DataOffset uint32
	Data       []byte
	CRC        uint16
}

func (UploadData) Kind() CommandKind { return KindUploadData }

func (c UploadData) Bytes() []byte {
	b := make([]byte, 8, 16+len(c.Data)+8)
	b[0], b[1] = CommandMark, byte(KindUploadData)
	le16(b[2:], c.CRCSeed)
	le32(b[4:], c.DataOffset)
	b = append(b, c.Data...)
	footer := make([]byte, 8)
	le16(footer[6:], c.CRC)
	return append(b, footer...)
}

// UploadDataResponse acknowledges an upload block.
type UploadDataResponse struct {
	Response byte
}

func (UploadDataResponse) Kind() CommandKind { return KindUploadDataResponse }

func (c UploadDataResponse) Bytes() []byte {
	return []byte{CommandMark, byte(KindUploadDataResponse), c.Response, 0, 0, 0, 0, 0}
}

// ---- ERASE ----

// EraseRequest erases a file by index.
type EraseRequest struct {
	DataFileIndex uint32
}

func (EraseRequest) Kind() CommandKind { return KindEraseRequest }

func (c EraseRequest) Bytes() []byte {
	b := make([]byte, 8)
	b[0], b[1] = CommandMark, byte(KindEraseRequest)
	le32(b[2:], c.DataFileIndex)
	return b
}

// EraseResponse acknowledges an erase.
type EraseResponse struct {
	Response byte
}

func (EraseResponse) Kind() CommandKind { return KindEraseResponse }

func (c EraseResponse) Bytes() []byte {
	return []byte{CommandMark, byte(KindEraseResponse), c.Response, 0, 0, 0, 0, 0}
}

// ---- Dispatcher ----

// ParseCommand decodes an ANT-FS command from its wire bytes.
func ParseCommand(data []byte) (Command, error) {
	if len(data) < 2 || data[0] != CommandMark {
		return nil, ErrBadCommand
	}
	kind := CommandKind(data[1])
	switch kind {
	case KindLink:
		if len(data) < 8 {
			return nil, ErrBadCommand
		}
		return Link{Frequency: data[2], Period: data[3], HostSerial: binary.LittleEndian.Uint32(data[4:8])}, nil
	case KindDisconnect:
		if len(data) < 5 {
			return nil, ErrBadCommand
		}
		return Disconnect{CommandType: data[2], TimeDuration: data[3], ApplicationSpecific: data[4]}, nil
	case KindAuthenticate, KindAuthenticateResponse:
		return parseAuthenticate(data)
	case KindPing:
		return Ping{}, nil
	case KindDownloadRequest:
		return parseDownloadRequest(data)
	case KindDownloadResponse:
		return parseDownloadResponse(data)
	case KindUploadRequest:
		if len(data) < 16 {
			return nil, ErrBadCommand
		}
		return UploadRequest{
			DataIndex:  binary.LittleEndian.Uint16(data[2:4]),
			MaxSize:    binary.LittleEndian.Uint32(data[4:8]),
			DataOffset: binary.LittleEndian.Uint32(data[12:16]),
		}, nil
	case KindUploadResponse:
		return parseUploadResponse(data)
	case KindUploadData:
		if len(data) < 16 {
			return nil, ErrBadCommand
		}
		return UploadData{
			CRCSeed:    binary.LittleEndian.Uint16(data[2:4]),
			DataOffset: binary.LittleEndian.Uint32(data[4:8]),
			Data:       append([]byte(nil), data[8:len(data)-8]...),
			CRC:        binary.LittleEndian.Uint16(data[len(data)-2:]),
		}, nil
	case KindUploadDataResponse, KindEraseResponse:
		if len(data) < 3 {
			return nil, ErrBadCommand
		}
		if kind == KindUploadDataResponse {
			return UploadDataResponse{Response: data[2]}, nil
		}
		return EraseResponse{Response: data[2]}, nil
	case KindEraseRequest:
		if len(data) < 6 {
			return nil, ErrBadCommand
		}
		return EraseRequest{DataFileIndex: binary.LittleEndian.Uint32(data[2:6])}, nil
	}
	return nil, fmt.Errorf("%w: unknown kind 0x%02X", ErrBadCommand, byte(kind))
}
