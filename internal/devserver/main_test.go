package devserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestMain turns the test binary into a small application when TIFA_FAKE_APP is set, so the tests
// can start, proxy and stop a real process:
//
//	TIFA_SERVER_ADDR          address to listen on, as tifa dev passes it
//	TIFA_FAKE_APP_VERSION     file whose content, read at start, is the answer to GET /
//	TIFA_FAKE_APP_BUNDLE      file whose content, read at start and on each POST /__tifa/dev/reload,
//	                          is the answer to GET /bundle. A reload of the content "broken" fails.
//	TIFA_FAKE_APP_ON_INTERRUPT "ignore" to keep running after an interrupt
//	TIFA_FAKE_APP_EXIT        exit right away with this message instead of serving
//	TIFA_FAKE_APP_DETACHED_CHILD start a child outside the process group that keeps stdout open,
//	                          and print its PID first
//
// POST requests get their body back, GET /exit makes the app exit with status 3, GET /pid answers
// the process ID. The app prints "shutdown done" once it stopped after an interrupt.
func TestMain(m *testing.M) {
	if os.Getenv("TIFA_TEST_FILE_LIMIT") != "" {
		watchWithFileLimit()
		os.Exit(0)
	}
	if os.Getenv("TIFA_FAKE_APP") != "" {
		if err := fakeApp(); err != nil {
			fmt.Fprintln(os.Stderr, "fake app:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeApp() error {
	if os.Getenv("TIFA_FAKE_APP_DETACHED_CHILD") != "" {
		if err := startDetachedChild(); err != nil {
			return err
		}
	}
	if msg := os.Getenv("TIFA_FAKE_APP_EXIT"); msg != "" {
		return errors.New(msg)
	}
	version := "none"
	if name := os.Getenv("TIFA_FAKE_APP_VERSION"); name != "" {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		version = string(data)
	}
	var bundleMu sync.Mutex
	bundle := ""
	loadBundle := func() error {
		name := os.Getenv("TIFA_FAKE_APP_BUNDLE")
		if name == "" {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if string(data) == "broken" {
			return errors.New("SyntaxError: the bundle is broken")
		}
		bundleMu.Lock()
		bundle = string(data)
		bundleMu.Unlock()
		return nil
	}
	if err := loadBundle(); err != nil {
		return err
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	ln, err := net.Listen("tcp", os.Getenv("TIFA_SERVER_ADDR"))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exit" {
			os.Exit(3)
		}
		switch {
		case r.URL.Path == "/pid":
			_, _ = io.WriteString(w, strconv.Itoa(os.Getpid()))
			return
		case r.URL.Path == "/bundle":
			bundleMu.Lock()
			defer bundleMu.Unlock()
			_, _ = io.WriteString(w, bundle)
			return
		case r.Method == "POST" && r.URL.Path == reloadPath:
			if err := loadBundle(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == "POST" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_, _ = w.Write(body)
			return
		}
		_, _ = io.WriteString(w, version)
	})}
	go func() { _ = srv.Serve(ln) }()
	for range interrupts {
		if os.Getenv("TIFA_FAKE_APP_ON_INTERRUPT") == "ignore" {
			continue
		}
		break
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return err
	}
	fmt.Println("shutdown done")
	return nil
}

// fakeAppCommand runs the test binary as the fake application.
func fakeAppCommand(t *testing.T) []string {
	t.Helper()
	t.Setenv("TIFA_FAKE_APP", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{exe}
}
