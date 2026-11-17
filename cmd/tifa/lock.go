package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/liustack/tifa/internal/codegen"
)

// lockHolder is what the holder of the app lock writes into .tifa/dev.lock.
type lockHolder struct {
	PID int `json:"pid"`
	// Kind is "dev" or "build". A record from before it was added has none.
	Kind    string    `json:"kind,omitempty"`
	Started time.Time `json:"started,omitzero"`
	URL     string    `json:"url,omitempty"` // empty until tifa dev listens
	// Log is the file the output of tifa dev goes to, when its stderr is .tifa/dev.log, which is so
	// for tifa dev --background. Empty for a tifa dev that prints to its terminal.
	Log string `json:"log,omitempty"`
}

// isDev says that the record is one of a tifa dev. Only tifa dev records an address, so a record of
// a version without Kind that has one is a tifa dev too.
func (h lockHolder) isDev() bool { return h.Kind == kindDev || h.Kind == "" && h.URL != "" }

const (
	kindDev   = "dev"
	kindBuild = "build"
)

// busyError says that a tifa dev or tifa build holds the lock of the app.
type busyError struct{ holder lockHolder }

// Detail says where the holder is: "at URL (pid N)", with the parts that are known.
func (e *busyError) Detail() string {
	switch {
	case e.holder.PID == 0:
		return ""
	case e.holder.URL == "":
		return fmt.Sprintf("(pid %d)", e.holder.PID)
	}
	return fmt.Sprintf("at %s (pid %d)", e.holder.URL, e.holder.PID)
}

// who names the holder, as far as the record tells.
func (e *busyError) who() string {
	switch {
	case e.holder.isDev():
		return "tifa dev is running"
	case e.holder.Kind == kindBuild:
		return "tifa build is running"
	}
	return "tifa dev or tifa build is running"
}

func (e *busyError) Error() string {
	return strings.TrimSpace(e.who() + " in this app " + e.Detail() + ". Stop it first: both write .tifa/dist. `go tool tifa check` and `go tool tifa gen` can run meanwhile")
}

// appLock is the exclusive lock on .tifa/dev.lock. tifa dev holds it while it runs, and tifa build
// while it builds, since both write .tifa/dist.
type appLock struct {
	f *os.File
	h lockHolder
}

// lockWait is how long a try for the lock keeps trying before it takes the lock for held. A look
// at the lock (lockHeld) takes it for an instant, and a try that meets such a look must not
// mistake it for a holder.
const lockWait = 100 * time.Millisecond

// tryLockFor is tryLock that tries again for up to wait.
func tryLockFor(f *os.File, wait time.Duration) (bool, error) {
	deadline := time.Now().Add(wait)
	for {
		ok, err := tryLock(f)
		if ok || err != nil || !time.Now().Before(deadline) {
			return ok, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lockApp takes the lock of the app at root for a tifa build, creating .tifa if needed, and records
// this process in it. It returns a *busyError when someone else holds it.
func lockApp(root string) (*appLock, error) { return lockAs(root, kindBuild) }

// lockDev is lockApp for tifa dev.
func lockDev(root string) (*appLock, error) { return lockAs(root, kindDev) }

func lockAs(root, kind string) (*appLock, error) {
	dir := filepath.Join(root, codegen.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// No O_TRUNC: the record of the holder must survive a failed attempt.
	f, err := os.OpenFile(filepath.Join(dir, codegen.LockFile), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	ok, err := tryLockFor(f, lockWait)
	if err != nil {
		f.Close()
		return nil, err
	}
	if !ok {
		defer f.Close()
		var holder lockHolder
		if data, err := io.ReadAll(f); err == nil {
			// A holder that is still writing its record leaves one that does not parse: pid 0.
			_ = json.Unmarshal(data, &holder)
		}
		return nil, &busyError{holder}
	}
	l := &appLock{f: f}
	if err := l.write(lockHolder{PID: os.Getpid(), Kind: kind, Started: time.Now().UTC().Truncate(time.Second)}); err != nil {
		f.Close()
		return nil, err
	}
	return l, nil
}

// lockHeld says whether a process holds the lock of the app at root, and what it recorded. The
// answer comes from the lock, not from the record: a record stays after its holder ended, and its
// process ID may belong to another program by then. It takes the lock for an instant when it is
// free, and creates nothing: an app without .tifa has no holder. The record of a holder that has
// just started may still be the one before it, so a caller that signals the holder checks it
// first (see findDev).
func lockHeld(root string) (lockHolder, bool, error) {
	var holder lockHolder
	f, err := os.Open(filepath.Join(root, codegen.Dir, codegen.LockFile))
	if errors.Is(err, fs.ErrNotExist) {
		return holder, false, nil
	}
	if err != nil {
		return holder, false, err
	}
	defer f.Close()
	ok, err := tryLockFor(f, lockWait)
	if err != nil || ok {
		return holder, false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return holder, true, err
	}
	// A holder that is still writing its record leaves one that does not parse: pid 0.
	_ = json.Unmarshal(data, &holder)
	return holder, true, nil
}

// SetURL records the address tifa dev listens on. Only tifa dev has one, so it also makes the
// record a tifa dev's.
func (l *appLock) SetURL(url string) error {
	l.h.Kind, l.h.URL = kindDev, url
	return l.write(l.h)
}

// SetLog records the file the output of tifa dev goes to.
func (l *appLock) SetLog(path string) error {
	l.h.Log = path
	return l.write(l.h)
}

func (l *appLock) write(h lockHolder) error {
	l.h = h
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	_, err = l.f.WriteAt(data, 0)
	return err
}

// Close releases the lock. The file stays: removing it would let a second process lock a new file
// while the first one still holds the old one.
func (l *appLock) Close() error { return l.f.Close() }

// maxDevPorts is how many ports, counting from the default one, tifa dev tries.
const maxDevPorts = 10

// listenDev listens on addr. With strict false, addr is the default one: when its port is in use,
// the next ports are tried, maxDevPorts in all. An address the user named (strict) is never changed.
func listenDev(addr string, strict bool) (net.Listener, error) {
	tries := maxDevPorts
	if strict {
		tries = 1
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("listen %s: bad port %q", addr, portText)
	}
	for i := range tries {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port+i)))
		if err == nil {
			return ln, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	if strict {
		return nil, fmt.Errorf("the address %s is already in use. Stop the program that uses it or pass another --addr", addr)
	}
	return nil, fmt.Errorf("the ports %s to %s are all in use. Pass a free address with --addr", addr, net.JoinHostPort(host, strconv.Itoa(port+maxDevPorts-1)))
}
