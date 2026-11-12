package tifa

import "github.com/liustack/tifa/internal/core"

// ServerStats is what Stats returns: the VM pool that renders pages and the render memo. Counts
// run from the start of the program, the other fields are the state now. Under tifa dev the counts
// of SSR start again when the front end is rebuilt.
//
//	SSR          the VM pool, see SSRStats
//	Memo         renders of pages: Hits skip the VM
//	MemoBytes    what the render memo holds now, in bytes
//	MemoSize     what it may hold, ssr.memo_size
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

// MemoStats is the render memo in ServerStats.
//
//	Entries    entries kept now
//	Hits       answered from an entry
//	Misses     rendered by the request
//	Shared     waited for the same page another request was rendering at the time
//	Evictions  entries dropped, the least recently used first, to make room for another
//	SamePage   misses whose page came out the same as the last render of that URL: the data changed, the page did not
//	Routes     the counts of each route by its pattern, as Ctx.Pattern gives it, "" for URLs that match no page
type MemoStats = core.MemoStats

// MemoRouteStats is a route in MemoStats.Routes: its Hits, Misses, Shared and SamePage as in
// MemoStats.
type MemoRouteStats = core.MemoRouteStats

// Stats returns the state of the server now. It is cheap and safe to call from many requests at
// once, such as from a route that a monitoring system reads. Before the server starts and after
// it stops it returns the zero ServerStats. An app without pages has a zero SSR.
func Stats() ServerStats { return core.ReadStats() }
