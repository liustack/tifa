package devserver

import (
	"context"
	"sync"
	"time"
)

// maxBatchWaits is how many times the debounce a batch waits at most for the files to go quiet,
// and how long a file may keep changing before it goes into a batch anyway.
const maxBatchWaits = 10

// loop hands batches of changes to rebuild once they settled, see changes.due: each file must stay
// unchanged for debounce, a file that keeps changing does not hold the batch back for longer than
// maxBatchWaits times debounce, and a file in the middle of a save waits for the next batch.
// Rebuilds run one at a time on the calling goroutine. Changes that arrive during a rebuild form
// the next batch. rebuild calls began once the rebuild began, which tells changes.outstanding
// that the changes of the batch are in a rebuild. A rebuild that does not call it does so when it
// returns, and so does a batch that turned out empty. Quiet is what the watcher has handled, and it
// may lag behind the files, such as when the machine was too busy to run it for longer than the
// debounce. Before it takes a batch, loop calls catchUp, which returns once the watcher handled the
// events that came before. Only then is a file that went quiet one that is done changing. loop
// returns when ctx is done.
func loop(ctx context.Context, c *changes, debounce time.Duration, catchUp func(context.Context), rebuild func(batch []change, began func())) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		if c.ripe(time.Now(), debounce, maxBatchWaits*debounce) {
			catchUp(ctx)
		}
		batch, wait := c.due(time.Now(), debounce, maxBatchWaits*debounce)
		if batch != nil {
			began := sync.OnceFunc(c.begin)
			if len(batch) > 0 {
				c.setBuilding(true)
				rebuild(batch, began)
				c.setBuilding(false)
			}
			began()
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
