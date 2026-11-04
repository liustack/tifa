package tifa

import "github.com/liustack/tifa/internal/core"

// ServerStats is what Stats returns: the VM pool that renders pages, the render cache and the
// whole-page cache. Counts run from the start of the program, the other fields are the state now.
//
//	SSR          the VM pool, see SSRStats
//	RenderCache  renders of pages: Hits skip the VM
//	PageCache    requests for pages with Cache: Hits skip the Loads and the render
//	CacheBytes   what the two caches hold now, in bytes
//	CacheSize    what they may hold, cache.size
type ServerStats = core.Stats

// SSRStats is the VM pool in ServerStats.
//
//	VMs         VMs loaded now: ssr.vms, one more while a VM that grew is replaced, one fewer while a failed one is
//	Busy        VMs rendering now
//	Queued      renders waiting for a VM now, at most ssr.queue_limit
//	Renders     renders that ran in a VM
//	Failed      of those, renders that failed: an exception, ssr.timeout or ssr.vm_memory
//	RenderTime  how long the renders took, together. RenderTime / Renders is the mean
//	Rejected    renders refused with ErrSSRBusy and answered 503: the queue was full, or the wait passed ssr.wait_timeout
type SSRStats = core.SSRStats

// CacheStats is a cache in ServerStats.
//
//	Entries  entries kept now
//	Hits     answered from an entry
//	Misses   made by the request: rendered, or for the whole-page cache, Loads and a render
//	Shared   waited for the same page another request was making at the time
//	Stale    whole-page cache only: an old page sent because making the new one failed
type CacheStats = core.CacheStats

// Stats returns the state of the server now. It is cheap and safe to call from many requests at
// once, such as from a route that a monitoring system reads. Before the server starts and after
// it stops it returns the zero ServerStats. An app without pages has a zero SSR.
func Stats() ServerStats { return core.ReadStats() }
