package core

import (
	"sync"
	"time"
)

// Stats is what the running server reports about its VM pool and its caches, see ReadStats.
type Stats struct {
	SSR SSRStats
	// RenderCache counts the renders of pages, PageCache the requests for pages with Cache.
	RenderCache, PageCache CacheStats
	// CacheBytes is what the two caches hold now, and CacheSize what they may hold (cache.size).
	CacheBytes, CacheSize int64
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

// CacheStats counts what a cache did since the program started, and holds how many entries it
// keeps now.
type CacheStats struct {
	Entries int
	// Hits are answered from an entry. Misses made the page, and Shared waited for a page another
	// request was making at the time.
	Hits, Misses, Shared uint64
	// Stale counts the old pages of the whole-page cache sent because making a new one failed.
	Stale uint64
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
		out.RenderCache = out.RenderCache.add(st.RenderCache)
		out.PageCache = out.PageCache.add(st.PageCache)
		out.CacheBytes += st.CacheBytes
		out.CacheSize += st.CacheSize
	}
	return out
}

func (a CacheStats) add(b CacheStats) CacheStats {
	return CacheStats{Entries: a.Entries + b.Entries, Hits: a.Hits + b.Hits, Misses: a.Misses + b.Misses, Shared: a.Shared + b.Shared, Stale: a.Stale + b.Stale}
}
