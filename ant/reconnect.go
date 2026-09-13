package ant

import "time"

// ReopenFunc creates a fresh driver instance, used to re-open the device
// after a fatal driver error (USB stick unplugged, LIBUSB pipe/IO errors,
// serial port disappearing). The returned driver must not be opened yet;
// Core opens it as part of the reconnect procedure.
type ReopenFunc func() (Driver, error)

// ReconnectHook is invoked from the reader goroutine after a successful
// reconnect, before regular operation resumes. Returning an error signals
// that post-reconnect reconfiguration failed and makes Core retry the
// whole re-open procedure.
type ReconnectHook func(attempt int, lastErr error) error

// Reconnect timing: first retry after reconnectBaseDelay, doubling up to
// reconnectMaxDelay, indefinitely until Stop (long-running service
// semantics; openant issue #51/#122). Tests override the base delay.
var (
	reconnectBaseDelay = 500 * time.Millisecond
	reconnectMaxDelay  = 5 * time.Second
)

// driverRef boxes a Driver so the active instance can be swapped atomically
// during a reconnect (the concrete type may change between generations).
type driverRef struct {
	d Driver
}

// maxReconnectAttempts caps reconnect cycles; 0 means retry forever.
var maxReconnectAttempts = 0

// reconnectLoop closes the dead driver and re-opens a fresh one, retrying
// with exponential backoff until it succeeds or Core is stopped. It runs
// in its own goroutine so the reader can continue serving the new driver
// while the hook restores the configuration. The driver pointer is swapped
// BEFORE the hook: response waits inside the hook require an active reader.
func (c *Core) reconnectLoop(cause error) {
	defer c.wg.Done()
	defer c.reconnecting.Store(false)

	c.log.Warn("driver failure, reconnecting", "error", cause)
	if d := c.currentDriver(); d != nil {
		_ = d.Close() // best effort; the device may already be gone
	}

	delay := reconnectBaseDelay
	for attempt := 1; maxReconnectAttempts == 0 || attempt <= maxReconnectAttempts; attempt++ {
		select {
		case <-c.stopCh:
			return
		case <-time.After(delay):
		}
		if !c.running.Load() {
			return
		}

		nd, err := c.reopen()
		if err != nil {
			c.log.Debug("re-open driver", "attempt", attempt, "error", err)
		} else if err := nd.Open(); err != nil {
			c.log.Debug("open driver", "attempt", attempt, "error", err)
		} else {
			c.driver.Store(&driverRef{d: nd})
			c.gen.Add(1) // reader drops stale state on the next frame
			c.mReconnects.Add(1)
			c.ResetSystem()
			time.Sleep(resetWait)
			if c.hook != nil {
				if herr := c.hook(attempt, cause); herr != nil {
					c.log.Warn("reconnect hook failed, retrying", "attempt", attempt, "error", herr)
					_ = nd.Close()
					delay = c.nextDelay(delay)
					continue
				}
			}
			c.log.Info("driver reconnected", "attempt", attempt)
			return
		}
		delay = c.nextDelay(delay)
	}
}

func (c *Core) nextDelay(delay time.Duration) time.Duration {
	if delay *= 2; delay > reconnectMaxDelay {
		return reconnectMaxDelay
	}
	return delay
}
