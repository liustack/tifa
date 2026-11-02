package core

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func panicOf(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

// within fails the test when f does not return, which is what a deadlock looks like.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock")
	}
}

func TestSealHTTPHandlerWithoutWrapper(t *testing.T) {
	SetHTTPHandler(nil)
	app := http.NewServeMux()
	root, release := SealHTTPHandler(app)
	if root != http.Handler(app) {
		t.Fatal("the root is not the app")
	}
	if msg := panicOf(func() { SetHTTPHandler(nil) }); !strings.Contains(msg, "OnStart") {
		t.Fatalf("panic %q", msg)
	}
	release()
	SetHTTPHandler(nil)
}

func TestSealHTTPHandlerNilResult(t *testing.T) {
	defer SetHTTPHandler(nil)
	SetHTTPHandler(func(http.Handler) http.Handler { return nil })
	within(t, func() {
		if msg := panicOf(func() { SealHTTPHandler(http.NewServeMux()) }); !strings.Contains(msg, "nil handler") {
			t.Errorf("panic %q", msg)
		}
	})
	// The failed seal is undone.
	SetHTTPHandler(nil)
}

func TestSealHTTPHandlerWrapperThatSetsTheWrapper(t *testing.T) {
	defer SetHTTPHandler(nil)
	SetHTTPHandler(func(next http.Handler) http.Handler {
		SetHTTPHandler(nil)
		return next
	})
	within(t, func() {
		if msg := panicOf(func() { SealHTTPHandler(http.NewServeMux()) }); !strings.Contains(msg, "OnStart") {
			t.Errorf("panic %q", msg)
		}
	})
	SetHTTPHandler(nil)
}

func TestSealHTTPHandlerWrapperThatPanics(t *testing.T) {
	defer SetHTTPHandler(nil)
	SetHTTPHandler(func(http.Handler) http.Handler { panic("boom") })
	within(t, func() {
		if msg := panicOf(func() { SealHTTPHandler(http.NewServeMux()) }); !strings.Contains(msg, "SetHTTPHandler") || !strings.Contains(msg, "boom") {
			t.Errorf("panic %q", msg)
		}
	})
	SetHTTPHandler(nil)
}

// Each seal counts. SetHTTPHandler works again only when every one is released, and a release
// counts once.
func TestSealHTTPHandlerCountsSeals(t *testing.T) {
	SetHTTPHandler(nil)
	_, a := SealHTTPHandler(http.NewServeMux())
	_, b := SealHTTPHandler(http.NewServeMux())
	a()
	a()
	if msg := panicOf(func() { SetHTTPHandler(nil) }); msg == "" {
		t.Fatal("SetHTTPHandler worked while a seal was held")
	}
	b()
	SetHTTPHandler(nil)
}

func TestSetSessionStoreIsSealedWhileListening(t *testing.T) {
	defer SetSessionStore(nil)
	defer SetHTTPHandler(nil)
	SetHTTPHandler(nil)
	first := NewMemoryStore(1 << 10)
	defer first.Close()
	SetSessionStore(first)
	_, release := SealHTTPHandler(http.NewServeMux())
	if got := SealedSessionStore(); got != SessionStore(first) {
		t.Fatalf("sealed store %v", got)
	}
	if msg := panicOf(func() { SetSessionStore(NewMemoryStore(1 << 10)) }); !strings.Contains(msg, "OnStart") {
		t.Fatalf("panic %q", msg)
	}
	release()
	SetSessionStore(nil)
	if got := SealedSessionStore(); got != nil {
		t.Fatalf("store after reset %v", got)
	}
}
