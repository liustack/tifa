package core

import (
	"fmt"
	"net/http"
	"sync"
)

// wiring is what an app sets for the whole process in OnStart with the SetXxx functions, and the
// servers that listen with it. Everything such a function sets goes into set, so SaveWiring puts
// all of it back at once.
var wiring struct {
	mu     sync.Mutex
	set    wired
	active int // servers that have started to listen and not stopped
}

// wired is what the SetXxx functions set.
type wired struct {
	wrap         func(http.Handler) http.Handler // SetHTTPHandler
	sessionStore SessionStore                    // SetSessionStore
}

// SaveWiring returns a function that puts back what the SetXxx functions have set now. A server of
// a test calls it before OnStart and the function when it stops, so the next one starts as the
// process did.
func SaveWiring() (restore func()) {
	wiring.mu.Lock()
	saved := wiring.set
	wiring.mu.Unlock()
	return func() {
		wiring.mu.Lock()
		wiring.set = saved
		wiring.mu.Unlock()
	}
}

// SetHTTPHandler sets the function that wraps the root handler of the server, nil for none. It
// panics while a server listens.
func SetHTTPHandler(wrap func(http.Handler) http.Handler) {
	wiring.mu.Lock()
	defer wiring.mu.Unlock()
	if wiring.active > 0 {
		panic("tifa: SetHTTPHandler must be called in OnStart, before the server listens")
	}
	wiring.set.wrap = wrap
}

// SealHTTPHandler returns the root handler: app wrapped by what SetHTTPHandler set when it is
// called, or app itself. Until release is called, which the server does when it stops listening,
// SetHTTPHandler panics. Every server seals for itself, so one that stops does not unseal another.
// The wrapper runs outside the lock, so it may panic or call SetHTTPHandler, which panics.
func SealHTTPHandler(app http.Handler) (root http.Handler, release func()) {
	wiring.mu.Lock()
	wrap := wiring.set.wrap
	wiring.active++
	wiring.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			wiring.mu.Lock()
			wiring.active--
			wiring.mu.Unlock()
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

// SetSessionStore sets the store of sessions, nil for the default in-memory one. It panics while a
// server listens, like SetHTTPHandler.
func SetSessionStore(s SessionStore) {
	wiring.mu.Lock()
	defer wiring.mu.Unlock()
	if wiring.active > 0 {
		panic("tifa: SetSessionStore must be called in OnStart, before the server listens")
	}
	wiring.set.sessionStore = s
}

// SealedSessionStore returns what SetSessionStore set, nil for none. A server calls it after
// SealHTTPHandler, which is when SetSessionStore starts to panic.
func SealedSessionStore() SessionStore {
	wiring.mu.Lock()
	defer wiring.mu.Unlock()
	return wiring.set.sessionStore
}
