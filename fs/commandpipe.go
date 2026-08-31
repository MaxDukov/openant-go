package fs

import (
	"encoding/binary"
	"sync/atomic"
	"time"
)

// Command pipe constants. The command pipe is a virtual file (index 0xFFFE)
// through which the host and client exchange structured commands.
const (
	// CommandPipeFileIndex is the virtual file index of the command pipe.
	CommandPipeFileIndex uint16 = 0xFFFE

	PipeTypeRequest                  byte = 0x01
	PipeTypeResponse                 byte = 0x02
	PipeTypeTime                     byte = 0x03
	PipeTypeCreateFile               byte = 0x04
	PipeTypeDirectoryFilter          byte = 0x05
	PipeTypeSetAuthenticationPasskey byte = 0x06
	PipeTypeSetClientFriendlyName    byte = 0x07
	PipeTypeFactoryReset             byte = 0x08
)

// Pipe response codes.
const (
	PipeRespOK           byte = 0
	PipeRespFailed       byte = 1
	PipeRespRejected     byte = 2
	PipeRespNotSupported byte = 3
)

// Time formats for the pipe Time command.
const (
	TimeFormatDirectory byte = 0
	TimeFormatSystem    byte = 1
	TimeFormatCounter   byte = 2
)

// AntFSEpoch is the ANT-FS time epoch: 1989-12-31 00:00:00 UTC.
var AntFSEpoch = time.Unix(631065600, 0).UTC()

// TAIOffset is the TAI-UTC offset added when setting device time
// (matching openant).
const TAIOffset = 35 * time.Second

// TimeFromANTFSSeconds converts ANT-FS epoch seconds to UTC time.
func TimeFromANTFSSeconds(sec uint32) time.Time {
	return AntFSEpoch.Add(time.Duration(sec) * time.Second)
}

// TimeToANTFSSeconds converts UTC time to ANT-FS epoch seconds.
func TimeToANTFSSeconds(t time.Time) uint32 {
	return uint32(t.Sub(AntFSEpoch) / time.Second)
}

// pipeSeq is a session sequence counter for outgoing pipe commands.
// (openant uses a class-global counter; a package-level atomic keeps the
// same wire behaviour without races.)
var pipeSeq atomic.Uint32

// NextPipeSequence returns the next outgoing command sequence number.
func NextPipeSequence() byte {
	return byte(pipeSeq.Add(1))
}

// PipeCommand is a command pipe message.
type PipeCommand interface {
	PipeType() byte
	Sequence() byte
	PipeBytes() []byte
}

func pipeHeader(b []byte, t, seq byte) {
	b[0], b[1], b[2], b[3] = t, 0, 0, seq
}

// ---- REQUEST ----

// PipeRequest asks the client to run a request type command (e.g. TIME).
type PipeRequest struct {
	Seq       byte
	RequestID byte
}

func (PipeRequest) PipeType() byte   { return PipeTypeRequest }
func (c PipeRequest) Sequence() byte { return c.Seq }

func (c PipeRequest) PipeBytes() []byte {
	b := make([]byte, 8)
	pipeHeader(b, PipeTypeRequest, c.Seq)
	b[4] = c.RequestID
	return b
}

// ---- RESPONSE ----

// PipeResponse is the client's answer to a request.
type PipeResponse struct {
	Seq       byte
	RequestID byte
	Response  byte
}

func (PipeResponse) PipeType() byte   { return PipeTypeResponse }
func (c PipeResponse) Sequence() byte { return c.Seq }

func (c PipeResponse) PipeBytes() []byte {
	b := make([]byte, 8)
	pipeHeader(b, PipeTypeResponse, c.Seq)
	b[4] = c.RequestID
	b[6] = c.Response
	return b
}

// ---- TIME ----

// PipeTime sets or reports time.
type PipeTime struct {
	Seq         byte
	CurrentTime uint32 // ANT-FS epoch seconds
	SystemTime  uint32
	TimeFormat  byte
}

func (PipeTime) PipeType() byte   { return PipeTypeTime }
func (c PipeTime) Sequence() byte { return c.Seq }

func (c PipeTime) PipeBytes() []byte {
	b := make([]byte, 16)
	pipeHeader(b, PipeTypeTime, c.Seq)
	le32(b[4:], c.CurrentTime)
	le32(b[8:], c.SystemTime)
	b[12] = c.TimeFormat
	return b
}

// ---- TIME RESPONSE ----

// PipeTimeResponse acknowledges a TIME request (Response + padding).
type PipeTimeResponse struct {
	Seq       byte
	RequestID byte
	Response  byte
}

func (PipeTimeResponse) PipeType() byte   { return PipeTypeResponse }
func (c PipeTimeResponse) Sequence() byte { return c.Seq }

func (c PipeTimeResponse) PipeBytes() []byte {
	b := make([]byte, 16)
	pipeHeader(b, PipeTypeResponse, c.Seq)
	b[4] = c.RequestID
	b[6] = c.Response
	return b
}

// ---- CREATE FILE ----

// PipeCreateFile creates a new file on the device.
type PipeCreateFile struct {
	Seq            byte
	Size           uint32
	DataType       byte // e.g. File.TypeFIT (0x80)
	Identifier     [3]byte
	IdentifierMask [3]byte
}

func (PipeCreateFile) PipeType() byte   { return PipeTypeCreateFile }
func (c PipeCreateFile) Sequence() byte { return c.Seq }

func (c PipeCreateFile) PipeBytes() []byte {
	b := make([]byte, 16)
	pipeHeader(b, PipeTypeCreateFile, c.Seq)
	le32(b[4:], c.Size)
	b[8] = c.DataType
	copy(b[9:12], c.Identifier[:])
	// b[12] reserved zero
	copy(b[13:16], c.IdentifierMask[:])
	return b
}

// ---- CREATE FILE RESPONSE ----

// PipeCreateFileResponse reports the created file index.
type PipeCreateFileResponse struct {
	Seq        byte
	RequestID  byte
	Response   byte
	DataType   byte
	Identifier [3]byte
	Index      uint16
}

func (PipeCreateFileResponse) PipeType() byte   { return PipeTypeResponse }
func (c PipeCreateFileResponse) Sequence() byte { return c.Seq }

func (c PipeCreateFileResponse) PipeBytes() []byte {
	b := make([]byte, 16)
	pipeHeader(b, PipeTypeResponse, c.Seq)
	b[4] = c.RequestID
	b[6] = c.Response
	b[8] = c.DataType
	copy(b[9:12], c.Identifier[:])
	le16(b[12:], c.Index)
	return b
}

// ParsePipeCommand decodes a command pipe payload. Fixed openant bug: the
// CreateFile parser used the ANT-FS command format and crashed; here every
// type parses per its actual layout.
func ParsePipeCommand(data []byte) (PipeCommand, error) {
	if len(data) < 8 {
		return nil, ErrBadCommand
	}
	t, seq := data[0], data[3]
	switch t {
	case PipeTypeRequest:
		return PipeRequest{Seq: seq, RequestID: data[4]}, nil
	case PipeTypeResponse:
		resp := PipeResponse{Seq: seq, RequestID: data[4], Response: data[6]}
		// Longer responses are typed replies (time / create file).
		if len(data) > 8 {
			switch data[4] {
			case PipeTypeTime:
				return PipeTimeResponse{Seq: seq, RequestID: data[4], Response: data[6]}, nil
			case PipeTypeCreateFile:
				if len(data) < 14 {
					return nil, ErrBadCommand
				}
				r := PipeCreateFileResponse{
					Seq:       seq,
					RequestID: data[4],
					Response:  data[6],
					DataType:  data[8],
					Index:     binary.LittleEndian.Uint16(data[12:14]),
				}
				copy(r.Identifier[:], data[9:12])
				return r, nil
			}
		}
		return resp, nil
	case PipeTypeTime:
		if len(data) < 16 {
			return nil, ErrBadCommand
		}
		return PipeTime{
			Seq:         seq,
			CurrentTime: binary.LittleEndian.Uint32(data[4:8]),
			SystemTime:  binary.LittleEndian.Uint32(data[8:12]),
			TimeFormat:  data[12],
		}, nil
	case PipeTypeCreateFile:
		if len(data) < 16 {
			return nil, ErrBadCommand
		}
		c := PipeCreateFile{
			Seq:      seq,
			Size:     binary.LittleEndian.Uint32(data[4:8]),
			DataType: data[8],
		}
		copy(c.Identifier[:], data[9:12])
		copy(c.IdentifierMask[:], data[13:16])
		return c, nil
	}
	return nil, ErrBadCommand
}
