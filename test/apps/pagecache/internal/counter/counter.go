// Package counter is the state the pagecache test app shares between its pages, its hook and its
// API routes: how often each Load ran, what the hook saw of the last request for a page with Cache,
// and what the Load of the fast page does next.
package counter

import (
	"sync"
	"sync/atomic"
)

// Seen is what OnRequest saw of the last GET or HEAD for a page with Cache.
type Seen struct {
	Path              string `json:"path"`
	Cookie            bool   `json:"cookie"`
	Authorization     bool   `json:"authorization"`
	RequestCookie     bool   `json:"requestCookie"`
	Session           bool   `json:"session"`
	SetSessionError   string `json:"setSessionError"`
	ClearSessionError string `json:"clearSessionError"`
}

// Snapshot is the answer of /api/stats.
type Snapshot struct {
	NewsLoads  int64 `json:"newsLoads"`
	FastLoads  int64 `json:"fastLoads"`
	OnRequests int64 `json:"onRequests"`
	Seen       Seen  `json:"seen"`
}

var (
	NewsLoads, FastLoads, OnRequests atomic.Int64

	mu   sync.Mutex
	seen Seen
	mode = "ok"
)

// SetSeen records what the hook saw.
func SetSeen(s Seen) {
	mu.Lock()
	seen = s
	mu.Unlock()
}

// Get returns the counters and what was seen last.
func Get() Snapshot {
	mu.Lock()
	defer mu.Unlock()
	return Snapshot{NewsLoads: NewsLoads.Load(), FastLoads: FastLoads.Load(), OnRequests: OnRequests.Load(), Seen: seen}
}

// SetMode sets what the Load of the fast page does: ok, slow, error, notfound or redirect.
func SetMode(m string) {
	mu.Lock()
	mode = m
	mu.Unlock()
}

// Mode returns what the Load of the fast page does.
func Mode() string {
	mu.Lock()
	defer mu.Unlock()
	return mode
}
