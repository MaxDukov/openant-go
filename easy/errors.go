// Package easy provides a blocking, callback based API on top of the ant
// package. It is a Go port of the openant.easy Python module.
package easy

import (
	"errors"
	"fmt"

	"github.com/maxdukov/openant-go/ant"
)

// Errors returned by the easy layer.
var (
	// ErrWaitTimeout is returned when a message wait times out (openant
	// raises AntException("Timed out while waiting for message")).
	ErrWaitTimeout = errors.New("easy: timed out while waiting for message")
	// ErrTransferFailed signals a failed transfer (ack/burst); senders
	// retry on it, matching openant TransferFailedException.
	ErrTransferFailed = errors.New("easy: transfer failed")
	// ErrTooManyChannels is returned when the node has no free channels.
	ErrTooManyChannels = errors.New("easy: no more channels available")
	// ErrInvalidNetwork is returned when the network number is out of range.
	ErrInvalidNetwork = errors.New("easy: invalid network number")
)

// ResponseError reports a non-zero response code from the ANT node.
type ResponseError struct {
	Code ant.Code
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("easy: responded with error %d: %s", e.Code, e.Code.String())
}

// Channel type constants (ANT channel types).
const (
	ChannelBidirectionalReceive        byte = 0x00
	ChannelBidirectionalTransmit       byte = 0x10
	ChannelSharedBidirectionalReceive  byte = 0x20
	ChannelSharedBidirectionalTransmit byte = 0x30
	ChannelUnidirectionalReceiveOnly   byte = 0x40
	ChannelUnidirectionalTransmitOnly  byte = 0x50
)
