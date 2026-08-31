package fs

import (
	"encoding/binary"
	"fmt"
	"time"
)

// File type/flag constants.
const (
	// FileFIT is the data type of FIT files in the directory.
	FileFIT byte = 0x80
)

// File permission flags (openant file flags byte).
const (
	FlagReadable  byte = 0x80
	FlagWritable  byte = 0x40
	FlagErasable  byte = 0x20
	FlagArchived  byte = 0x10
	FlagAppend    byte = 0x08
	FlagEncrypted byte = 0x04
)

// Well-known FIT file identifier sub-types (identifier byte 0).
const (
	FitDevice          byte = 1
	FitSetting         byte = 2
	FitSport           byte = 3
	FitActivity        byte = 4
	FitWorkout         byte = 5
	FitCourse          byte = 6
	FitSchedules       byte = 7
	FitWaypoints       byte = 8
	FitWeight          byte = 9
	FitTotals          byte = 10
	FitGoals           byte = 11
	FitBloodPressure   byte = 14
	FitMonitoringA     byte = 15
	FitActivitySummary byte = 20
	FitMonitoringDaily byte = 28
	FitMonitoringB     byte = 32
	FitSegment         byte = 34
	FitSegmentList     byte = 35
)

// File is one 16 byte directory entry.
type File struct {
	Index      uint16
	DataType   byte
	Identifier [3]byte // [sub_type, num_lo, num_hi]
	DataFlags  byte
	Flags      byte
	Size       uint32
	Date       uint32 // ANT-FS epoch seconds
}

// ParseFile decodes a directory entry.
func ParseFile(data []byte) (File, error) {
	var f File
	if len(data) < 16 {
		return f, ErrBadCommand
	}
	f.Index = binary.LittleEndian.Uint16(data[0:2])
	f.DataType = data[2]
	copy(f.Identifier[:], data[3:6])
	f.DataFlags = data[6]
	f.Flags = data[7]
	f.Size = binary.LittleEndian.Uint32(data[8:12])
	f.Date = binary.LittleEndian.Uint32(data[12:16])
	return f, nil
}

// FITSubType returns the FIT sub type (identifier byte 0).
func (f File) FITSubType() byte { return f.Identifier[0] }

// FITFileNumber returns the FIT file number from identifier bytes 1-2.
func (f File) FITFileNumber() uint16 {
	return uint16(f.Identifier[1]) | uint16(f.Identifier[2])<<8
}

// Time returns the file date in UTC.
func (f File) Time() time.Time { return TimeFromANTFSSeconds(f.Date) }

// FlagString renders the permission flags like openant ("r-eA--").
func (f File) FlagString() string {
	s := []byte("------")
	set := func(i int, c byte) {
		if f.Flags&(1<<(7-uint(i))) != 0 {
			s[i] = c
		}
	}
	set(0, 'r')
	set(1, 'w')
	set(2, 'e')
	set(3, 'A')
	set(4, 'a')
	set(5, 'c')
	return string(s)
}

// Directory is the parsed ANT-FS directory (file index 0).
type Directory struct {
	Version           byte // hi nibble major, lo nibble minor
	TimeFormat        byte
	CurrentSystemTime uint32
	LastModified      uint32
	Files             []File
}

// ParseDirectory decodes a downloaded directory file.
func ParseDirectory(data []byte) (*Directory, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("fs: directory too short: %d", len(data))
	}
	d := &Directory{
		Version:           data[0],
		TimeFormat:        data[2],
		CurrentSystemTime: binary.LittleEndian.Uint32(data[8:12]),
		LastModified:      binary.LittleEndian.Uint32(data[12:16]),
	}
	rest := data[16:]
	for len(rest) >= 16 {
		f, err := ParseFile(rest[:16])
		if err != nil {
			return nil, err
		}
		d.Files = append(d.Files, f)
		rest = rest[16:]
	}
	return d, nil
}

// Get returns the file with the given index.
func (d *Directory) Get(index uint16) *File {
	for i := range d.Files {
		if d.Files[i].Index == index {
			return &d.Files[i]
		}
	}
	return nil
}
