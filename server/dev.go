package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/internal/deverrors"
)

// devPrefix holds the paths of tifa dev's requests to the app. The dev server never forwards
// browser requests under it (see internal/devserver).
const devPrefix = "/__tifa/dev/"

// clientPath is the dev client that tifa dev serves (clientPath in internal/devserver). The dev
// error page loads it.
const clientPath = devPrefix + "client.js"

// reloadPath is where tifa dev asks the app to load the SSR bundle, the content or both again,
// after it rebuilt only those. internal/devserver sends the request, and reads reloadFailedHeader
// of an answer that failed.
const (
	reloadPath         = devPrefix + "reload"
	reloadFailedHeader = "X-Tifa-Reload-Failed"
)

// DevMain is Main for the main that tifa dev generates. Every content entry can be seen, also one
// whose date is still to come, and the application also answers POST /__tifa/dev/reload: with
// bundle=1 it loads the front end again from the directory --dist names, with the same openFront
// that started it, and swaps it in. With content=1 it loads content.json of that directory again,
// runs Validate on every entry and swaps the data of the collections, which makes every page the
// whole-page cache made before expire. OnStart does not run again and the state of the application stays.
// The answer is 204, or 500 with the error, in which case what was loaded before keeps serving.
// When the content failed, reloadFailedHeader says content. tifa build generates a main that calls
// Main, so a production program has no such path.
func DevMain(app App) {
	start(app, reloader)
}

// reloader answers tifa dev's requests to the app for the front end and the content in dist.
func reloader(s *server, dist fs.FS, cfg config.SSR) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reloadPath || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("bundle") == "1" {
			if err := s.openFront(dist, cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if q.Get("content") == "1" {
			if err := content.Load(dist); err != nil {
				w.Header().Set(reloadFailedHeader, "content")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// serveDev answers a request of tifa dev and reports whether it did. Only DevMain sets s.dev and
// s.devErrors.
func (s *server) serveDev(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, devPrefix) {
		return false
	}
	if s.devErrors != nil && r.URL.Path == deverrors.Path {
		s.devErrors.ServeHTTP(w, r)
		return true
	}
	if s.dev == nil {
		return false
	}
	s.dev.ServeHTTP(w, r)
	return true
}

// devErrors keeps what tifa dev reports to `tifa errors` from the app: the unexpected errors of
// requests and the pages whose two renders differ. tifa dev forgets them (DELETE) when a build
// starts.
type devErrors struct {
	requests deverrors.Requests
	renders  deverrors.Renders
}

func (d *devErrors) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, deverrors.App{Requests: d.requests.List(), Renders: d.renders.List()})
	case http.MethodDelete:
		d.requests.Clear()
		d.renders.Clear()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// devRequestError notes under tifa dev the unexpected error of the request of c, which OnError
// answered with e. The error text is the one the dev error page shows.
func (s *server) devRequestError(c *tifa.Ctx, e tifa.Error, err error) {
	if s.devErrors == nil {
		return
	}
	r := c.Request()
	s.devErrors.requests.Add(deverrors.Request{Method: r.Method, URL: r.URL.RequestURI(), Status: e.Status, Message: e.Message, Error: err.Error()})
}
