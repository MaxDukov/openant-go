package ant

import "time"

// readBufferSize is the size of USB read chunks (openant uses 4096).
const readBufferSize = 4096

func (c *Core) reader() {
	defer c.wg.Done()
	buf := make([]byte, 0, readBufferSize*2)
	chunk := make([]byte, readBufferSize)
	myGen := c.gen.Load()
	var errDelay time.Duration // backoff on consecutive driver errors
	for c.running.Load() {
		d := c.currentDriver()
		if d == nil {
			// Torn down between generations; wait for a swap or stop.
			select {
			case <-c.stopCh:
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		n, err := d.Read(chunk)
		if err != nil {
			if !c.running.Load() {
				return
			}
			if err == ErrTimeout {
				continue // timeout is the normal poll tick
			}
			c.mReadErrors.Add(1)
			// Fatal driver failure (stick unplugged, USB pipe/IO error,
			// serial port gone): with a driver factory configured, hand
			// over to the reconnect supervisor. It swaps the driver and
			// restores the stick configuration while this goroutine
			// keeps serving the new driver so the hook's response waits
			// can complete.
			if c.reopen != nil {
				if c.reconnecting.CompareAndSwap(false, true) {
					c.wg.Add(1)
					go c.reconnectLoop(err)
				}
				select {
				case <-c.stopCh:
					return
				case <-time.After(10 * time.Millisecond):
				}
				continue
			}
			// Persistent driver failures must not busy-spin the loop
			// (code review PR #1, P1-9): back off exponentially up to
			// one second.
			c.log.Debug("driver read", "error", err, "backoff", errDelay)
			if errDelay < time.Second {
				errDelay = errDelay*2 + 10*time.Millisecond
			}
			time.Sleep(errDelay)
			continue
		}
		errDelay = 0
		// A new generation means a fresh stick: drop any state left
		// over from the dead one, including a partially read frame.
		if g := c.gen.Load(); g != myGen {
			buf = buf[:0]
			c.burst = c.burst[:0]
			c.lastData = nil
			c.advActive = false
			myGen = g
		}
		buf = append(buf, chunk[:n]...)
		buf = c.consume(buf)
	}
}
