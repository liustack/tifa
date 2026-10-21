package server

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

func TestBusyRenderAnswers503(t *testing.T) {
	for _, path := range []string{"/secret", "/stats", "/nowhere", "/secret?deny=1"} {
		t.Run(path, func(t *testing.T) {
			var reported []error
			app := secretApp(nil, &reported)
			app.Routes = append(app.Routes, Route{ID: "app/stats", Path: "/stats", Page: true, SSR: true, Cache: true})
			s, r := newFrontServer(t, app)
			r.result = func(ssr.Input) (string, error) { return "", fmt.Errorf("waiting: %w", tifa.ErrSSRBusy) }
			for range 2 {
				w := get(s, path)
				if w.Code != 503 {
					t.Fatalf("status %d, body %s", w.Code, w.Body.String())
				}
				if w.Header().Get("Retry-After") != "1" {
					t.Errorf("Retry-After: %v", w.Header())
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
					t.Errorf("cache headers: %v", w.Header())
				}
			}
			if r.count() != 2 {
				t.Fatalf("busy response was cached or retried in a VM: %d renders", r.count())
			}
			for _, err := range reported {
				if errors.Is(err, tifa.ErrSSRBusy) {
					t.Errorf("OnError received overload: %v", err)
				}
			}
			want := 0
			if path == "/secret?deny=1" {
				want = 2
			}
			if len(reported) != want {
				t.Errorf("OnError called %d times, want %d", len(reported), want)
			}
		})
	}
}

func TestMiddlewareCanHandleBusyRender(t *testing.T) {
	var reported []error
	app := secretApp(func(c *tifa.Ctx) error {
		if err := c.Next(); errors.Is(err, tifa.ErrSSRBusy) {
			return tifa.Error{Status: 429, Message: "try later"}
		} else {
			return err
		}
	}, &reported)
	s, r := newFrontServer(t, app)
	r.result = func(ssr.Input) (string, error) { return "", tifa.ErrSSRBusy }
	w := get(s, "/secret")
	if w.Code != 429 || w.Header().Get("Retry-After") != "" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}

// The second Err check runs with a VM acquired, before any JavaScript starts.
type heldVMContext struct {
	context.Context
	err func() error
}

func (c heldVMContext) Err() error { return c.err() }

func TestSharedRequestsTakeOnePoolWaitSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.Default()
		cfg.SSR.VMs, cfg.SSR.QueueLimit, cfg.Cache.Size = 1, 1, 0
		pool, err := ssr.NewPool(`globalThis.startRender = () => { globalThis.renderResult = "<main>ok</main>" }`, nil, cfg.SSR, false)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		gate := make(chan struct{})
		release := sync.OnceFunc(func() { close(gate) })
		defer release()
		checks := 0
		ctx := heldVMContext{Context: context.Background(), err: func() error {
			checks++
			if checks == 2 {
				<-gate
			}
			return nil
		}}
		held := make(chan error, 1)
		go func() { _, err := pool.Render(ctx, ssr.Input{}); held <- err }()
		synctest.Wait()
		app := frontApp()
		s := New(cfg, app.Hooks)
		s.register(app.Layouts, app.Routes)
		if err := s.setFront(dist, pool.Render); err != nil {
			t.Fatal(err)
		}
		answers := make(chan *httptest.ResponseRecorder, 20)
		for range cap(answers) {
			go func() { answers <- get(s, "/stats") }()
		}
		synctest.Wait()
		if n := waiting(s); n != cap(answers) {
			t.Fatalf("%d shared waiters, want %d", n, cap(answers))
		}
		other := make(chan *httptest.ResponseRecorder, 1)
		go func() { other <- get(s, "/stats?other") }()
		synctest.Wait()
		select {
		case w := <-other:
			if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
				t.Fatalf("another key: %d %v", w.Code, w.Header())
			}
		default:
			t.Fatal("the shared render did not occupy the only wait slot")
		}
		release()
		if err := <-held; err != nil {
			t.Fatal(err)
		}
		by := map[string]int{}
		for range cap(answers) {
			w := <-answers
			if w.Code != 200 {
				t.Fatalf("shared request: %d %s", w.Code, w.Body.String())
			}
			by[w.Header().Get("X-Tifa-Render")]++
		}
		if by["vm"] != 1 || by["shared"] != 19 {
			t.Fatalf("renders: %v", by)
		}
	})
}
