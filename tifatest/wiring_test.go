package tifatest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

// recordingStore is a session store that counts the records it was given.
type recordingStore struct {
	mu   sync.Mutex
	data map[string][]byte
	puts int
}

func (s *recordingStore) Get(_ context.Context, token string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[token]
	return data, ok, nil
}

func (s *recordingStore) Put(_ context.Context, token string, data []byte, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[token] = data
	s.puts++
	return nil
}

func (s *recordingStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, token)
	return nil
}

func (s *recordingStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// wiredApp is signInApp with an OnStart that sets a wrapper and a session store for the process,
// and then returns err.
func wiredApp(store *recordingStore, err error) server.App {
	app := signInApp()
	app.Hooks.OnStart = func(context.Context) error {
		tifa.SetHTTPHandler(func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Wrapped", "1")
				h.ServeHTTP(w, r)
			})
		})
		tifa.SetSessionStore(store)
		return err
	}
	app.Hooks.OnShutdown = nil
	return app
}

// What OnStart sets for the process lasts as long as its server, also when OnStart fails: the
// next Start begins with the wiring the process had before.
func TestStartPutsBackWhatOnStartSet(t *testing.T) {
	store := &recordingStore{}
	plain := func(t *testing.T) {
		app := signInApp()
		app.Hooks.OnStart, app.Hooks.OnShutdown = nil, nil
		srv := tifatest.Start(t, app)
		before := store.count()
		res := postJSON(t, srv, "/login", `{"name":"Ann","next":"/account"}`)
		if res.StatusCode != 303 || res.Header.Get("X-Wrapped") != "" {
			t.Errorf("sign-in: %d, X-Wrapped %q: the wrapper of an earlier test is in place", res.StatusCode, res.Header.Get("X-Wrapped"))
		}
		if store.count() != before {
			t.Error("the session went into the store of an earlier test")
		}
	}
	t.Run("wired", func(t *testing.T) {
		srv := tifatest.Start(t, wiredApp(store, nil))
		res := postJSON(t, srv, "/login", `{"name":"Ann","next":"/account"}`)
		if res.Header.Get("X-Wrapped") != "1" || store.count() != 1 {
			t.Fatalf("the wiring of OnStart is not in place: X-Wrapped %q, %d puts", res.Header.Get("X-Wrapped"), store.count())
		}
	})
	t.Run("after a test", plain)
	t.Run("failing OnStart", func(t *testing.T) {
		f := &fatal{TB: t}
		if srv := tifatest.Start(f, wiredApp(store, errors.New("no database"))); srv != nil || !strings.Contains(f.msg, "no database") {
			t.Fatalf("Start with a failing OnStart: %v, %q", srv, f.msg)
		}
	})
	t.Run("after a failed start", plain)
}
