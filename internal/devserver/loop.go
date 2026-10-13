package devserver

import (
	"context"
	"time"
)

// loop waits until no change arrived for debounce, then hands the batch to rebuild. Rebuilds run
// one at a time on the calling goroutine. Changes that arrive during a rebuild form the next batch.
// loop returns when ctx is done.
func loop(ctx context.Context, c *changes, debounce time.Duration, rebuild func([]change)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.ready:
		}
		timer := time.NewTimer(debounce)
	quiet:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-c.ready:
				timer.Reset(debounce)
			case <-timer.C:
				break quiet
			}
		}
		if batch := c.take(); len(batch) > 0 {
			rebuild(batch)
		}
	}
}
