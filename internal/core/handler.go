package core

import (
	"fmt"
	"net/http"
	"sync"
)

var httpWrap struct {
	mu     sync.Mutex
	wrap   func(http.Handler) http.Handler
	active int // servers that have started to listen and not stopped
}

// SetHTTPHandler sets the function that wraps the root handler of the server, nil for none. It
// panics while a server listens.
func SetHTTPHandler(wrap func(http.Handler) http.Handler) {
	httpWrap.mu.Lock()
	defer httpWrap.mu.Unlock()
	if httpWrap.active > 0 {
		panic("tifa: SetHTTPHandler must be called in OnStart, before the server listens")
	}
	httpWrap.wrap = wrap
}

// SealHTTPHandler returns the root handler: app wrapped by what SetHTTPHandler set when it is
// called, or app itself. Until release is called, which the server does when it stops listening,
// SetHTTPHandler panics. Every server seals for itself, so one that stops does not unseal another.
// The wrapper runs outside the lock, so it may panic or call SetHTTPHandler, which panics.
func SealHTTPHandler(app http.Handler) (root http.Handler, release func()) {
	httpWrap.mu.Lock()
	wrap := httpWrap.wrap
	httpWrap.active++
	httpWrap.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			httpWrap.mu.Lock()
			httpWrap.active--
			httpWrap.mu.Unlock()
		})
	}
	if wrap == nil {
		return app, release
	}
	root, panicked := callWrap(wrap, app)
	if panicked != nil {
		release()
		panic(fmt.Sprintf("tifa: the function given to SetHTTPHandler panicked: %v", panicked))
	}
	if root == nil {
		release()
		panic("tifa: the function given to SetHTTPHandler returned a nil handler")
	}
	return root, release
}

func callWrap(wrap func(http.Handler) http.Handler, app http.Handler) (root http.Handler, panicked any) {
	defer func() { panicked = recover() }()
	return wrap(app), nil
}

var sessionStore SessionStore // guarded by httpWrap.mu

// SetSessionStore sets the store of sessions, nil for the default in-memory one. It panics while a
// server listens, like SetHTTPHandler.
func SetSessionStore(s SessionStore) {
	httpWrap.mu.Lock()
	defer httpWrap.mu.Unlock()
	if httpWrap.active > 0 {
		panic("tifa: SetSessionStore must be called in OnStart, before the server listens")
	}
	sessionStore = s
}

// SealedSessionStore returns what SetSessionStore set, nil for none. A server calls it after
// SealHTTPHandler, which is when SetSessionStore starts to panic.
func SealedSessionStore() SessionStore {
	httpWrap.mu.Lock()
	defer httpWrap.mu.Unlock()
	return sessionStore
}
