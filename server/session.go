package server

import (
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
