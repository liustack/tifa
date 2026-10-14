package devserver

import (
	"context"
	"time"
)

// maxBatchWaits is how many times the debounce a batch waits at most for the files to go quiet.
const maxBatchWaits = 10

// loop waits until no change arrived for debounce, then hands the batch to rebuild. A file that
// keeps changing does not hold the batch back for longer than maxBatchWaits times debounce.
// Rebuilds run one at a time on the calling goroutine. Changes that arrive during a rebuild form
// the next batch. loop returns when ctx is done.
func loop(ctx context.Context, c *changes, debounce time.Duration, rebuild func([]change)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.ready:
		}
		timer := time.NewTimer(debounce)
		limit := time.NewTimer(maxBatchWaits * debounce)
	quiet:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				limit.Stop()
				return
			case <-c.ready:
				timer.Reset(debounce)
			case <-timer.C:
				break quiet
			case <-limit.C:
				break quiet
			}
		}
		timer.Stop()
		limit.Stop()
		if batch := c.take(); len(batch) > 0 {
			c.setBuilding(true)
			rebuild(batch)
			c.setBuilding(false)
		}
	}
}
