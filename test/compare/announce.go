package compare

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// announced counts, by folder, the rounds of this process that are announced, so that the file of
// the process goes only when the last of them is over.
var announced = struct {
	sync.Mutex
	n map[string]int
}{n: map[string]int{}}

// announce says that this process waits for or runs a round that needs the machine to itself: it
// leaves an empty file named by its pid in dir, and the function it returns takes the file away.
// A machine that others share reads the folder: while a file of a live process is there, no new
// heavy job starts in front of the round (the lock files keep others out of the round itself, see
// holdLocks). With no folder it does nothing.
func announce(dir string) (done func(), err error) {
	if dir == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file := filepath.Join(dir, strconv.Itoa(os.Getpid()))
	announced.Lock()
	defer announced.Unlock()
	if announced.n[dir] == 0 {
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			return nil, err
		}
	}
	announced.n[dir]++
	var once sync.Once
	return func() {
		once.Do(func() {
			announced.Lock()
			defer announced.Unlock()
			if announced.n[dir]--; announced.n[dir] == 0 {
				os.Remove(file)
			}
		})
	}, nil
}
