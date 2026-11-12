//go:build unix

package compare

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

// holdLocks takes an exclusive flock on each file, in the order given, and returns what gives them
// all back. It waits for a file another process holds, keeping the ones it took, as a chain of
// `flock -o a flock -o b` does, so two runs that take the same files in the same order cannot
// deadlock. A run on a shared machine holds the files only while one of its servers runs, so the
// work of others fits in between its rounds. The files are not passed on to the servers (Go opens
// them close-on-exec), so a server that outlives the run holds nothing.
func holdLocks(ctx context.Context, paths []string) (release func(), err error) {
	var files []*os.File
	release = func() {
		for i := len(files) - 1; i >= 0; i-- {
			syscall.Flock(int(files[i].Fd()), syscall.LOCK_UN)
			files[i].Close()
		}
		files = nil
	}
	for _, path := range paths {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			release()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		files = append(files, f)
		for {
			err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				break
			}
			if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
				release()
				return nil, fmt.Errorf("lock %s: %w", path, err)
			}
			select {
			case <-ctx.Done():
				release()
				return nil, fmt.Errorf("lock %s: %w", path, ctx.Err())
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return release, nil
}
