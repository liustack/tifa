package server

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
)

// sessionState is what the server needs to serve sessions: the settings, the store, and the
// environment built from them on the first request.
type sessionState struct {
	cfg   config.Session
	store core.SessionStore // set by Run from tifa.SetSessionStore, nil for a default made on first use
	once  sync.Once
	env   *core.SessionEnv
	owned *core.MemoryStore // the default store sessionEnv made, closed by Close
	// enabled is set for an app that calls tifa.SetSession (App.Session): its pages, navigation
	// answers and calls carry the session to the browser.
	enabled bool
}

// sessionEnv returns the environment of every request. Under tifa dev the cookie has no Secure
// attribute and no __Host- prefix, because dev serves plain HTTP on localhost, where browsers may
// refuse such a cookie.
func (s *Server) sessionEnv() *core.SessionEnv {
	s.sess.once.Do(func() {
		if s.sess.store == nil {
			s.sess.owned = core.NewMemoryStore(s.sess.cfg.MemoryLimit)
			s.sess.store = s.sess.owned
		}
		env := &core.SessionEnv{Store: s.sess.store, Cookie: s.sess.cfg.Cookie, Secure: true, MaxAge: s.sess.cfg.MaxAge}
		if s.dev != nil {
			env.Secure = false
			env.Cookie = strings.TrimPrefix(env.Cookie, "__Host-")
		}
		s.sess.env = env
	})
	return s.sess.env
}

// startSessions picks the store once OnStart has returned: what the app set, or a store in memory
// with the line that says so. It returns the function that stops what it made.
func (s *Server) startSessions() (stop func()) {
	if store := core.SealedSessionStore(); store != nil {
		s.sess.store = store
		return func() {}
	}
	m := core.NewMemoryStore(s.sess.cfg.MemoryLimit)
	s.sess.store = m
	core.Logger().Info("tifa: sessions are kept in memory: a restart signs everyone out")
	return m.Close
}

// Close releases what the server made for itself: the default session store a server that did not go
// through Run made on its first request. Run closes its own on return.
func (s *Server) Close() {
	if s.sess.owned != nil {
		s.sess.owned.Close()
	}
}

// anonymousRequest reports whether the request is for a page with the whole-page cache and
// asks for it to be read: GET and HEAD, with or without the navigation parameter.
func (s *Server) anonymousRequest(pattern string, r *http.Request) bool {
	return pattern != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) && s.pageCache[pattern] > 0
}

// anonymity is what an anonymous request leaves in its context: the session of the browser it came
// from, for the navigation answer to carry (the page itself knows nothing of it), or the error of
// reading it.
type anonymity struct {
	session *core.BrowserSession
	err     error
}

type anonymityKey struct{}

// anonymityOf returns the anonymity of a request, nil for one that is not anonymous.
func anonymityOf(r *http.Request) *anonymity {
	a, _ := r.Context().Value(anonymityKey{}).(*anonymity)
	return a
}

// anonymize returns r without the Cookie and Authorization headers. For a navigation data request
// of an app with sessions it first reads the session cookie, because the answer carries the
// session of the browser, and that is all the cookie is used for.
func (s *Server) anonymize(r *http.Request) (*http.Request, error) {
	a := &anonymity{}
	if s.sess.enabled && core.DataOf(r) != nil {
		a.session, a.err = s.sessionOfCookie(r)
	}
	out := r.Clone(context.WithValue(r.Context(), anonymityKey{}, a))
	for name := range out.Header {
		if !anonymousHeader(name) {
			delete(out.Header, name)
		}
	}
	return out, a.err
}

// anonymousHeader reports whether a request header stays on the request of a page with Cache. It is a
// short list of what the cache and the connection need, and everything else goes: not only Cookie and
// Authorization but also a header a proxy or a login gateway sets (X-User-ID, X-Forwarded-User), and the
// ones that tell visitors apart (Accept-Language, User-Agent, Referer, Forwarded, X-Forwarded-For). What
// still tells visitors apart is the address of the connection and whether it is TLS.
func anonymousHeader(name string) bool {
	switch name = http.CanonicalHeaderKey(name); name {
	case "Host", "Accept-Encoding", "If-None-Match", "If-Modified-Since", "Cache-Control", "Pragma", "Content-Length", "Connection", "Origin":
		return true
	}
	return strings.HasPrefix(name, "Sec-Fetch-")
}

// headerOnly is the response writer of a request whose answer nobody reads.
type headerOnly struct{ h http.Header }

func (w *headerOnly) Header() http.Header         { return w.h }
func (w *headerOnly) WriteHeader(int)             {}
func (w *headerOnly) Write(b []byte) (int, error) { return len(b), nil }

// sessionOfCookie reads the session of the cookie of r in a request of its own, whose cookie
// handling (dropping a dead cookie) goes nowhere.
func (s *Server) sessionOfCookie(r *http.Request) (*core.BrowserSession, error) {
	read := *r
	read.Body = http.NoBody
	c := core.New(&headerOnly{h: http.Header{}}, &read, "", 0)
	if err := core.AttachSession(c, s.sessionEnv()); err != nil {
		return nil, err
	}
	v, err := core.BrowserSessionOf(c)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
