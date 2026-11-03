package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/liustack/tifa/internal/codegen"
)

// lockHolder is what the holder of the app lock writes into .tifa/dev.lock.
type lockHolder struct {
	PID int    `json:"pid"`
	URL string `json:"url,omitempty"` // empty until tifa dev listens
}

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

func (e *busyError) Error() string {
	who := "tifa dev is running"
	if e.holder.URL == "" {
		who = "tifa dev or tifa build is running"
	}
	return strings.TrimSpace(who + " in this app " + e.Detail() + ". Stop it first: both write .tifa/dist")
}

// appLock is the exclusive lock on .tifa/dev.lock. tifa dev holds it while it runs, and tifa build
// while it builds, since both write .tifa/dist.
type appLock struct{ f *os.File }

// lockApp takes the lock of the app at root, creating .tifa if needed, and records this process in
// it. It returns a *busyError when someone else holds it.
func lockApp(root string) (*appLock, error) {
	dir := filepath.Join(root, codegen.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// No O_TRUNC: the record of the holder must survive a failed attempt.
	f, err := os.OpenFile(filepath.Join(dir, codegen.LockFile), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	ok, err := tryLock(f)
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
	l := &appLock{f}
	if err := l.write(lockHolder{PID: os.Getpid()}); err != nil {
		f.Close()
		return nil, err
	}
	return l, nil
}

// SetURL records the address tifa dev listens on.
func (l *appLock) SetURL(url string) error {
	return l.write(lockHolder{PID: os.Getpid(), URL: url})
}

func (l *appLock) write(h lockHolder) error {
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
