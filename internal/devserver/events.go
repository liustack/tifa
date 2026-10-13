package devserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// status is what the browser needs to know about the dev build.
type status struct {
	// Version changes each time a new build of the app is up. A page reloads when it sees a
	// version other than the first one it saw.
	Version string `json:"version"`
	// Title names what failed, such as "Build failed", and Error is the full message. Both are
	// empty when everything works.
	Title string `json:"title,omitempty"`
	Error string `json:"error,omitempty"`
}

// hub keeps the current status and streams it to every connected browser over SSE. A new
// connection gets the current status first, so it shows a build error that happened before.
type hub struct {
	mu     sync.Mutex
	st     status
	subs   map[chan status]struct{}
	closed chan struct{}
}

func newHub() *hub {
	return &hub{subs: map[chan status]struct{}{}, closed: make(chan struct{})}
}

func (h *hub) current() status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st
}

// publish replaces the status and sends it to every connection. A connection that has not
// taken the previous status yet gets only the newest one.
func (h *hub) publish(st status) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.st = st
	for ch := range h.subs {
		select {
		case <-ch:
		default:
		}
		ch <- st
	}
}

// close ends every stream, so an http.Server shutdown does not wait for them.
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.closed:
	default:
		close(h.closed)
	}
}

func (h *hub) subscribe() chan status {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan status, 1)
	ch <- h.st
	h.subs[ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(ch chan status) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, ch)
}

func (h *hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// The browser reconnects by itself after a lost connection, retry sets how soon.
	if _, err := fmt.Fprint(w, "retry: 500\n\n"); err != nil {
		return
	}
	ch := h.subscribe()
	defer h.unsubscribe(ch)
	for {
		select {
		case st := <-ch:
			data, err := json.Marshal(st)
			if err != nil {
				panic(err) // two strings always encode
			}
			if _, err := fmt.Fprintf(w, "event: status\ndata: %s\n\n", data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-h.closed:
			return
		}
	}
}
