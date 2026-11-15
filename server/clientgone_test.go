package server

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

// A request whose own context the client canceled is not a failure of the app: OnError does not
// hear of it and nothing is logged as an error. A context.Canceled that comes from somewhere else
// while the client is still there is one.
func TestClientThatLeavesIsNotAnAppFailure(t *testing.T) {
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/gone", Path: "/gone", Page: true, SSR: true, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { return nil, context.Canceled }})
	var reported atomic.Int32
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		reported.Add(1)
		return tifa.Error{Status: 500, Message: "failed"}
	}
	s := newAppServer(t, app)
	s.handle("GET", "/download", func(c *tifa.Ctx) error {
		if _, err := c.Writer().Write([]byte("part")); err != nil {
			return err
		}
		// What a write to a connection the client closed returns.
		return errors.New("write tcp: broken pipe")
	})
	if err := s.setFront(dist, htmlOnly(func(ctx context.Context, in ssr.Input) (string, error) {
		// A client that left is a canceled context, whatever the render was doing.
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "<html></html>", nil
	})); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		left       bool // the client canceled the request
		reported   bool
	}{
		{"render of a page with Memo = false", "/live", true, false},
		{"Load returns context.Canceled", "/gone", true, false},
		{"write after the client left", "/download", true, false},
		{"Load returns context.Canceled with the client there", "/gone", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logged bytes.Buffer
			log.SetOutput(&logged)
			defer log.SetOutput(os.Stderr)
			reported.Store(0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.left {
				cancel()
			}
			w, panicked := serve(s, httptest.NewRequest("GET", tc.path, nil).WithContext(ctx))
			if tc.left && panicked != http.ErrAbortHandler {
				t.Errorf("panicked with %v, want http.ErrAbortHandler (code %d)", panicked, w.Code)
			}
			if got := reported.Load() > 0; got != tc.reported {
				t.Errorf("OnError called: %v, want %v", got, tc.reported)
			}
			if tc.left && strings.Contains(logged.String(), "ERROR") {
				t.Errorf("logged %q", logged.String())
			}
		})
	}
}
