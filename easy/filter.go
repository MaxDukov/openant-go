package easy

import (
	"sync"
	"time"

	"github.com/maxdukov/openant-go/ant"
)

// waitAttempts and waitInterval reproduce openant's filter.wait_for_message
// semantics: up to ten one-second waits while scanning the event buffer.
const (
	waitAttempts   = 10
	eventQueueSize = 64
)

// maxBufferedEvents caps the event buffer when nobody consumes it
// (drop-oldest, code review PR #1, P2-16).
const maxBufferedEvents = 1024

// eventBuffer is a mutex-protected FIFO of events with broadcast
// notification for waiters. It replaces openant's deque+Condition pairs.
type eventBuffer struct {
	mu       sync.Mutex
	events   []ant.Event
	notify   chan struct{}
	interval time.Duration
}

func newEventBuffer() *eventBuffer {
	return &eventBuffer{notify: make(chan struct{}), interval: time.Second}
}

// push appends an event and wakes all waiters. When the buffer is full
// the oldest event is dropped (a waiting consumer still sees newer
// responses; an unconsumed backlog never grows unbounded).
func (b *eventBuffer) push(ev ant.Event) {
	b.mu.Lock()
	b.events = append(b.events, ev)
	if len(b.events) > maxBufferedEvents {
		excess := len(b.events) - maxBufferedEvents
		b.events = append(b.events[:0], b.events[excess:]...)
	}
	ch := b.notify
	b.notify = make(chan struct{})
	b.mu.Unlock()
	close(ch)
}

// matcher decides whether an event is the one being waited for.
type matcher func(ev ant.Event) bool

// waitFor scans the buffer for a matching event. Non-matching channel
// events carrying EVENT_TRANSFER_TX_FAILED or EVENT_RX_FAIL_GO_TO_SEARCH
// are removed and reported as ErrTransferFailed (openant raises
// TransferFailedException from the same side condition). It returns
// ErrWaitTimeout after ~10 seconds without a match.
func (b *eventBuffer) waitFor(match matcher) (ant.Event, error) {
	return b.waitForAttempts(match, waitAttempts, b.interval)
}

// waitForAttempts is waitFor with custom attempts and wait interval.
func (b *eventBuffer) waitForAttempts(match matcher, attempts int, interval time.Duration) (ant.Event, error) {
	for attempt := 0; attempt < attempts; attempt++ {
		b.mu.Lock()
		for i, ev := range b.events {
			if match(ev) {
				b.removeAt(i)
				b.mu.Unlock()
				return ev, nil
			}
			if ev.Kind == ant.KindChannel &&
				(ev.Code == ant.EventTransferTxFailed || ev.Code == ant.EventRxFailGoToSearch) {
				b.removeAt(i)
				b.mu.Unlock()
				return ant.Event{}, ErrTransferFailed
			}
		}
		ch := b.notify
		b.mu.Unlock()

		select {
		case <-ch:
		case <-time.After(interval):
		}
	}
	return ant.Event{}, ErrWaitTimeout
}

// removeAt removes the i-th event; b.mu must be held.
func (b *eventBuffer) removeAt(i int) {
	b.events = append(b.events[:i], b.events[i+1:]...)
}
