package bundle

import (
	"io"
	"os"
	"sync"
	"time"
)

// probeTail is a measurement hack for the Windows probe: the driver appends its answers to a file
// and this reader follows the file. It stands in for file descriptor 3, which Windows cannot pass.
type probeTail struct {
	path   string
	f      *os.File
	exited chan struct{}
	once   sync.Once
	done   chan struct{}
}

func newProbeTail() (*probeTail, error) {
	f, err := os.CreateTemp("", "tifa-answers-*")
	if err != nil {
		return nil, err
	}
	return &probeTail{path: f.Name(), f: f, done: make(chan struct{})}, nil
}

func (t *probeTail) Read(p []byte) (int, error) {
	for {
		n, err := t.f.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		select {
		case <-t.done:
			return 0, io.EOF
		case <-t.exited:
			if n, _ := t.f.Read(p); n > 0 {
				return n, nil
			}
			return 0, io.EOF
		case <-time.After(3 * time.Millisecond):
		}
	}
}

func (t *probeTail) Close() error {
	t.once.Do(func() { close(t.done) })
	return nil
}
