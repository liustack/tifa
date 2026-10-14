package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/liustack/tifa/internal/config"
)

// devPrefix holds the paths of tifa dev's requests to the app. The dev server never forwards
// browser requests under it (see internal/devserver).
const devPrefix = "/__tifa/dev/"

// reloadPath is where tifa dev asks the app to load the SSR bundle again, after it rebuilt
// only that bundle. internal/devserver sends the request.
const reloadPath = devPrefix + "reload"

// DevMain is Main for the main that tifa dev generates. The application also answers
// POST /__tifa/dev/reload: it loads the front end again from the directory --dist names, with
// the same openFront that started it, and swaps it in. OnStart does not run again and the
// state of the application stays. The answer is 204, or 500 with the error, in which case the
// front end loaded before keeps serving. tifa build generates a main that calls Main, so a
// production program has no such path.
func DevMain(app App) {
	start(app, reloader)
}

// reloader answers tifa dev's requests to the app for the front end in dist.
func reloader(s *Server, dist fs.FS, cfg config.SSR) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reloadPath || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		if err := s.openFront(dist, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// serveDev answers a request of tifa dev and reports whether it did. Only DevMain sets s.dev.
func (s *Server) serveDev(w http.ResponseWriter, r *http.Request) bool {
	if s.dev == nil || !strings.HasPrefix(r.URL.Path, devPrefix) {
		return false
	}
	s.dev.ServeHTTP(w, r)
	return true
}
