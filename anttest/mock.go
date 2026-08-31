// Package anttest provides a scriptable in-memory ANT driver used by unit
// tests and examples that must run without physical hardware.
package anttest

import (
	"errors"
	"sync"

	"github.com/maxdukov/openant-go/ant"
)

// MockDriver is a thread-safe ant.Driver that records written frames and
// replays scripted reads.
type MockDriver struct {
	mu     sync.Mutex
	rxBuf  []byte
	dataCh chan struct{}
	closed bool
	opened bool
	writes [][]byte
}

// NewMockDriver returns an unopened mock driver.
func NewMockDriver() *MockDriver {
	return &MockDriver{dataCh: make(chan struct{}, 1)}
}

var _ ant.Driver = (*MockDriver)(nil)

// Open marks the driver as opened.
func (m *MockDriver) Open() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mock: driver permanently closed")
	}
	m.opened = true
	return nil
}

// Close wakes any blocked readers; subsequent I/O returns ErrDriverClosed.
// Once closed the driver cannot be reopened.
func (m *MockDriver) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.signal() // wake blocked readers
	return nil
}

// QueueRead appends bytes that subsequent Read calls will return. It is
// safe to call concurrently with Read.
func (m *MockDriver) QueueRead(data []byte) {
	m.mu.Lock()
	m.rxBuf = append(m.rxBuf, data...)
	m.mu.Unlock()
	m.signal()
}

// QueueMessage frames and queues a message for reading.
func (m *MockDriver) QueueMessage(msg *ant.Message) {
	m.QueueRead(msg.Encode())
}

// Writes returns a copy of all frames written through the driver so far.
func (m *MockDriver) Writes() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.writes))
	copy(out, m.writes)
	return out
}

// Read returns queued bytes. It blocks until data is available or the
// driver is closed.
func (m *MockDriver) Read(p []byte) (int, error) {
	for {
		m.mu.Lock()
		if len(m.rxBuf) > 0 {
			n := copy(p, m.rxBuf)
			m.rxBuf = m.rxBuf[n:]
			m.mu.Unlock()
			return n, nil
		}
		if m.closed {
			m.mu.Unlock()
			return 0, ant.ErrDriverClosed
		}
		m.mu.Unlock()

		// Wait for new data or closure.
		<-m.dataCh

		m.mu.Lock()
		if m.closed && len(m.rxBuf) == 0 {
			m.mu.Unlock()
			return 0, ant.ErrDriverClosed
		}
		m.mu.Unlock()
	}
}

func (m *MockDriver) signal() {
	select {
	case m.dataCh <- struct{}{}:
	default:
	}
}

// Write records the frame.
func (m *MockDriver) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.opened {
		return 0, ant.ErrDriverClosed
	}
	frame := make([]byte, len(p))
	copy(frame, p)
	m.writes = append(m.writes, frame)
	return len(p), nil
}
