package core

import (
	"sync"
	"time"
)

// Stats is what the running server reports about its VM pool and its render memo, see ReadStats.
type Stats struct {
	SSR SSRStats
	// Memo counts the renders of pages.
	Memo MemoStats
	// MemoBytes is what the render memo holds now, and MemoSize what it may hold (ssr.memo_size).
	MemoBytes, MemoSize int64
}

// SSRStats is the state of the VM pool now, and what it did since the program started.
type SSRStats struct {
	VMs    int // VMs loaded now: ssr.vms, one more while a grown VM is replaced, fewer while a failed one is
	Busy   int // VMs rendering now
	Queued int // renders waiting for a VM now
	// Renders counts the renders that ran in a VM, Failed those of them that failed (an exception,
	// ssr.timeout, ssr.vm_memory), and RenderTime is how long they took together.
	Renders, Failed uint64
	RenderTime      time.Duration
	// Rejected counts the renders refused with ErrSSRBusy, answered 503: the queue was full or the
	// wait for a VM ran past ssr.wait_timeout.
	Rejected uint64
}

// MemoStats counts what the render memo did since the program started, and holds how many entries it
// keeps now.
type MemoStats struct {
	Entries int
	// Hits are answered from an entry. Misses made the page, and Shared waited for a page another
	// request was making at the time. Evictions are the entries dropped, the least recently used
	// first, to make room for another.
	Hits, Misses, Shared, Evictions uint64
	// Routes holds the counts of each route, by its pattern (Ctx.Pattern), with "" for the URLs that
	// match no page. Hits, Misses and Shared are their sums. Nil until a page went through the memo.
	Routes map[string]MemoRouteStats
}

// MemoRouteStats counts what the render memo did for the pages of one route, see MemoStats.
type MemoRouteStats struct {
	Hits, Misses, Shared uint64
}

// Server is a server that runs, as ReadStats sees it.
type Server interface {
	Stats() Stats
}

var running struct {
	mu      sync.Mutex
	servers []Server
}

// Running adds s to the servers ReadStats reaches, until the function it returns is
// called.
func Running(s Server) (stop func()) {
	running.mu.Lock()
	running.servers = append(running.servers, s)
	running.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			running.mu.Lock()
			defer running.mu.Unlock()
			for i, r := range running.servers {
				if r == s {
					running.servers = append(running.servers[:i:i], running.servers[i+1:]...)
					return
				}
			}
		})
	}
}

func servers() []Server {
	running.mu.Lock()
	defer running.mu.Unlock()
	return running.servers
}

// ReadStats returns the stats of the running server, the sum of all of them when a test runs
// several, and the zero Stats when none runs.
func ReadStats() Stats {
	var out Stats
	for _, s := range servers() {
		st := s.Stats()
		out.SSR.VMs += st.SSR.VMs
		out.SSR.Busy += st.SSR.Busy
		out.SSR.Queued += st.SSR.Queued
		out.SSR.Renders += st.SSR.Renders
		out.SSR.Failed += st.SSR.Failed
		out.SSR.RenderTime += st.SSR.RenderTime
		out.SSR.Rejected += st.SSR.Rejected
		out.Memo = out.Memo.add(st.Memo)
		out.MemoBytes += st.MemoBytes
		out.MemoSize += st.MemoSize
	}
	return out
}

func (a MemoStats) add(b MemoStats) MemoStats {
	out := MemoStats{Entries: a.Entries + b.Entries, Hits: a.Hits + b.Hits, Misses: a.Misses + b.Misses, Shared: a.Shared + b.Shared, Evictions: a.Evictions + b.Evictions}
	if len(a.Routes)+len(b.Routes) > 0 {
		out.Routes = make(map[string]MemoRouteStats, max(len(a.Routes), len(b.Routes)))
		for _, routes := range []map[string]MemoRouteStats{a.Routes, b.Routes} {
			for route, r := range routes {
				out.Routes[route] = out.Routes[route].add(r)
			}
		}
	}
	return out
}

func (a MemoRouteStats) add(b MemoRouteStats) MemoRouteStats {
	return MemoRouteStats{Hits: a.Hits + b.Hits, Misses: a.Misses + b.Misses, Shared: a.Shared + b.Shared}
}
