//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package devserver

import (
	"errors"
	"math"
	"os"
	"sync"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

// kqueue watches what a file descriptor refers to. A directory reports that its entries changed,
// not that one of its files was written, so every tracked file needs a descriptor of its own.
// tifa opens them itself, one at a time from the watcher's goroutine: fsnotify opens one for every
// entry of a watched directory and keeps opening them for new entries without saying so, which
// left the watcher unable to keep to its budget.

// watchesFiles reports that a directory watch does not report edits of its files: each tracked
// file gets a watch of its own.
const watchesFiles = true

// watchBudget returns how many file descriptors the watches may take: half of what the process
// may open, so tifa dev keeps enough for its own work and leaves the system table alone.
func watchBudget() int {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return math.MaxInt
	}
	n := uint64(lim.Cur) // int64 on some systems
	// macOS takes infinity for the limit but still stops at kern.maxfilesperproc.
	if max, err := syscall.SysctlUint32("kern.maxfilesperproc"); err == nil && uint64(max) < n {
		n = uint64(max)
	}
	if n > math.MaxInt32 {
		return math.MaxInt
	}
	return int(n) / 2
}

// outOfWatches reports whether err means the process or the system has no file descriptor left.
func outOfWatches(err error) bool {
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
}

// noteEvents are the changes a watch asks kqueue for.
const noteEvents = unix.NOTE_DELETE | unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_ATTRIB | unix.NOTE_RENAME

// kqueue is the system watcher on macOS and the BSDs. Each watch holds one file descriptor, from
// add until remove or close.
type kqueue struct {
	kq     int
	wake   [2]int // a pipe, written to by close to stop the reader
	events chan fsnotify.Event
	errs   chan error
	done   chan struct{} // closed by close
	read   chan struct{} // closed when the reader returned

	mu     sync.Mutex
	fds    map[string]int // file descriptor of each watched path
	names  map[int]string // watched path of each file descriptor
	closed bool
	once   sync.Once
}

func newSystemWatcher() (*systemWatcher, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, os.NewSyscallError("kqueue", err)
	}
	k := &kqueue{
		kq: kq, events: make(chan fsnotify.Event), errs: make(chan error), done: make(chan struct{}), read: make(chan struct{}),
		fds: map[string]int{}, names: map[int]string{},
	}
	if err := unix.Pipe(k.wake[:]); err != nil {
		unix.Close(kq)
		return nil, os.NewSyscallError("pipe", err)
	}
	for _, fd := range []int{kq, k.wake[0], k.wake[1]} {
		unix.CloseOnExec(fd)
	}
	var ev unix.Kevent_t
	unix.SetKevent(&ev, k.wake[0], unix.EVFILT_READ, unix.EV_ADD)
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		k.closeFiles()
		return nil, os.NewSyscallError("kevent", err)
	}
	go k.readEvents()
	return &systemWatcher{add: k.add, remove: k.remove, close: k.close, events: k.events, errors: k.errs}, nil
}

// add watches the directory or file at name, following links, unless it is watched already.
func (k *kqueue) add(name string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return errors.New("the file watcher is closed")
	}
	if _, ok := k.fds[name]; ok {
		return nil
	}
	var fd int
	var err error
	for {
		fd, err = unix.Open(name, openWatch, 0)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		return &os.PathError{Op: "open", Path: name, Err: err}
	}
	var ev unix.Kevent_t
	unix.SetKevent(&ev, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR|unix.EV_ENABLE)
	ev.Fflags = noteEvents
	if _, err := unix.Kevent(k.kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(fd)
		return &os.PathError{Op: "kevent", Path: name, Err: err}
	}
	k.fds[name] = fd
	k.names[fd] = name
	return nil
}

// remove stops watching name and closes its file descriptor.
func (k *kqueue) remove(name string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	fd, ok := k.fds[name]
	if !ok {
		return nil
	}
	delete(k.fds, name)
	delete(k.names, fd)
	return unix.Close(fd) // which also removes it from the kqueue
}

// close stops the reader and closes every file descriptor the watcher holds.
func (k *kqueue) close() error {
	k.once.Do(func() {
		close(k.done)
		_, _ = unix.Write(k.wake[1], []byte{0})
		<-k.read
		k.closeFiles()
	})
	return nil
}

func (k *kqueue) closeFiles() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.closed = true
	for fd := range k.names {
		unix.Close(fd)
	}
	clear(k.fds)
	clear(k.names)
	for _, fd := range []int{k.kq, k.wake[0], k.wake[1]} {
		unix.Close(fd)
	}
}

// readEvents turns what kqueue reports into events, named by the path each watch was added for,
// until close.
func (k *kqueue) readEvents() {
	defer close(k.read)
	buf := make([]unix.Kevent_t, 64)
	for {
		n, err := unix.Kevent(k.kq, nil, buf, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			select {
			case k.errs <- os.NewSyscallError("kevent", err):
			case <-k.done:
			}
			return
		}
		for _, ev := range buf[:n] {
			fd := int(ev.Ident)
			if fd == k.wake[0] {
				return
			}
			k.mu.Lock()
			name, ok := k.names[fd]
			k.mu.Unlock()
			if !ok {
				continue // removed since
			}
			op := fsnotify.Write
			switch {
			case ev.Fflags&unix.NOTE_DELETE != 0:
				op = fsnotify.Remove
			case ev.Fflags&unix.NOTE_RENAME != 0:
				op = fsnotify.Rename
			}
			select {
			case k.events <- fsnotify.Event{Name: name, Op: op}:
			case <-k.done:
				return
			}
		}
	}
}
