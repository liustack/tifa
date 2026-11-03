//go:build unix

package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/codegen"
)

// A second lock on the same app fails and says who holds the first.
func TestAppLockConflict(t *testing.T) {
	root := t.TempDir()
	first, err := lockApp(root)
	if err != nil {
		t.Fatal(err)
	}
	// The holder has no address until it listens, and the record has its pid already.
	_, err = lockApp(root)
	var busy *busyError
	if !errors.As(err, &busy) || busy.holder.PID != os.Getpid() || busy.holder.URL != "" {
		t.Fatalf("holder before listen: %v", err)
	}
	if err := first.SetURL("http://127.0.0.1:9527"); err != nil {
		t.Fatal(err)
	}
	_, err = lockApp(root)
	if !errors.As(err, &busy) {
		t.Fatalf("second lock: %v, want *busyError", err)
	}
	if busy.holder.PID != os.Getpid() || busy.holder.URL != "http://127.0.0.1:9527" {
		t.Fatalf("holder = %+v", busy.holder)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := lockApp(root)
	if err != nil {
		t.Fatalf("lock after close: %v", err)
	}
	// The record of the first holder is gone with its address.
	_, err = lockApp(root)
	if !errors.As(err, &busy) || busy.holder.URL != "" {
		t.Fatalf("holder after reacquire: %v", err)
	}
	again.Close()
}

// Writing the generated files keeps the lock file.
func TestCodegenWriteKeepsTheLock(t *testing.T) {
	root := t.TempDir()
	l, err := lockApp(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := codegen.Write(root, map[string][]byte{"a.txt": []byte("a")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, codegen.Dir, codegen.LockFile)); err != nil {
		t.Fatalf("lock file after Write: %v", err)
	}
	if _, err := lockApp(root); err == nil {
		t.Fatal("lock is free after Write")
	}
}

func holdDevLock(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	l, err := lockApp(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	if err := l.SetURL("http://127.0.0.1:9527"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBuildStopsWhenDevRuns(t *testing.T) {
	root := holdDevLock(t)
	_, _, err := run(t, "build", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "tifa dev is running") || !strings.Contains(err.Error(), "pid "+strconv.Itoa(os.Getpid())) || !strings.Contains(err.Error(), "http://127.0.0.1:9527") {
		t.Fatalf("build: %v", err)
	}
}

func TestDevStopsWhenDevRuns(t *testing.T) {
	root := holdDevLock(t)
	_, stderr, err := run(t, "dev", "--dir", root)
	if err == nil {
		t.Fatal("dev started twice")
	}
	want := "tifa dev: already running at http://127.0.0.1:9527 (pid " + strconv.Itoa(os.Getpid()) + ")"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

// holdPorts listens on count consecutive loopback ports and returns the first.
func holdPorts(t *testing.T, count int) int {
	t.Helper()
	for start := 20000; start < 60000; start += 50 {
		var lns []net.Listener
		for i := 0; i < count; i++ {
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(start+i)))
			if err != nil {
				break
			}
			lns = append(lns, ln)
		}
		if len(lns) == count {
			for _, ln := range lns {
				t.Cleanup(func() { ln.Close() })
			}
			return start
		}
		for _, ln := range lns {
			ln.Close()
		}
	}
	t.Fatal("no free ports")
	return 0
}

// The default address moves on to the next port when it is taken.
func TestListenDevMovesOn(t *testing.T) {
	start := holdPorts(t, 2)
	ln, err := listenDev(net.JoinHostPort("127.0.0.1", strconv.Itoa(start)), false)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if got := ln.Addr().(*net.TCPAddr).Port; got != start+2 {
		t.Fatalf("listening on port %d, want %d", got, start+2)
	}
}

func TestListenDevGivesUpAfterTenPorts(t *testing.T) {
	start := holdPorts(t, 10)
	ln, err := listenDev(net.JoinHostPort("127.0.0.1", strconv.Itoa(start)), false)
	if err == nil {
		ln.Close()
		t.Fatal("listened with all ten ports taken")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(start)) || !strings.Contains(err.Error(), strconv.Itoa(start+9)) {
		t.Fatalf("error = %v", err)
	}
}

// An address the user named is not changed.
func TestListenDevStrict(t *testing.T) {
	start := holdPorts(t, 1)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(start))
	ln, err := listenDev(addr, true)
	if err == nil {
		ln.Close()
		t.Fatal("listened on a taken address")
	}
	if !strings.Contains(err.Error(), addr) || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("error = %v", err)
	}
}

// Another error is not a reason to try the next port.
func TestListenDevOtherErrors(t *testing.T) {
	if ln, err := listenDev("127.0.0.1:notaport", false); err == nil {
		ln.Close()
		t.Fatal("listened on a bad address")
	}
}
