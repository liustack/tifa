package devserver

import (
	"context"
	"time"
)

// maxBatchWaits is how many times the debounce a batch waits at most for the files to go quiet,
// and how long a file may keep changing before it goes into a batch anyway.
const maxBatchWaits = 10

// loop hands batches of changes to rebuild once they settled, see changes.due: each file must stay
// unchanged for debounce, a file that keeps changing does not hold the batch back for longer than
// maxBatchWaits times debounce, and a file in the middle of a save waits for the next batch.
// Rebuilds run one at a time on the calling goroutine. Changes that arrive during a rebuild form
// the next batch. loop returns when ctx is done.
func loop(ctx context.Context, c *changes, debounce time.Duration, rebuild func([]change)) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		batch, wait := c.due(time.Now(), debounce, maxBatchWaits*debounce)
		if batch != nil {
			if len(batch) > 0 {
				c.setBuilding(true)
				rebuild(batch)
				c.setBuilding(false)
			}
			continue
		}
		var expired <-chan time.Time
		if wait > 0 {
			timer.Reset(wait)
			expired = timer.C
		}
		select {
		case <-ctx.Done():
			return
		case <-c.ready:
		case <-expired:
		}
		timer.Stop()
	}
}
