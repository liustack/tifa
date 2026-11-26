package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// protectedShare is the part of the memo, in percent, that pages hit more than once can fill
// (the protected segment). The rest is for pages seen once and for those a full protected segment
// sent back. 80 is the usual split of a segmented LRU: a hot page keeps its place against any run of
// new addresses, and a fifth of the memo is still enough for a new page to be hit a second time
// and earn its place.
const protectedShare = 80

// renderMemo keeps rendered pages by the hash of their render input, up
// to size bytes of pages, each in the one form it keeps. It is a segmented LRU, so that a run of
// addresses that come once (a search term, a page number nobody asked before) cannot push out the
// pages that are asked for again and again. A new page enters the probation list. A hit on a page
// in probation moves it to the protected list, which holds up to protectedShare percent of size.
// When protected is over that, its least recently used page goes back to the head of probation,
// not out. Pages are dropped only from the tail of probation. A page larger than the protected
// share can never be protected and stays in probation, where a hit still makes it the most recent.
// Requests that miss the same page at the same time wait for one render of it (fetch).
type renderMemo struct {
	mu        sync.Mutex
	size      int64
	used      int64
	protCap   int64      // the bytes the protected list may hold
	protUsed  int64      // the bytes it holds
	probation *list.List // of *cached, most recently used first
	protected *list.List // of *cached, most recently used first
	pages     map[string]*list.Element
	// flights holds the renders of pages not in the cache yet, by key.
	flights map[string]*flight
	// failed holds the results of renders that landed with keepBrief, by key, for failMemory.
	// failures lists them oldest first, and failUsed is the size of their pages. now is the clock
	// of failMemory, time.Now outside the tests.
	failed   map[string]*list.Element
	failures *list.List // of *failure
	failUsed int64
	now      func() time.Time
	// entries counts the pages kept and evictions the pages dropped to make room, under mu. hits,
	// misses and shared count what fetch found. Every request counts, so these are atomic, and Stats
	// reads them without the lock.
	entries              int
	evictions            uint64
	hits, misses, shared atomic.Uint64
	// Hooks of the tests, nil otherwise: fetching runs as fetch starts, landing between storing
	// a render and ending its flight.
	fetching, landing func()
}

// flight is the one render of a page that every request missing it waits for, see fetch.
type flight struct {
	done    chan struct{}
	waiters int                // requests waiting for it, under renderMemo.mu
	cancel  context.CancelFunc // stops the render while it waits for a VM
	// Set before done is closed: the page and what the cache keeps of the result, or the error.
	page cachedPage
	keep keep
	err  error
}

type cached struct {
	key  string
	page cachedPage
	prot bool // in the protected list
}

// cachedPage is a rendered page as the cache keeps it: one form of it, never changed after the page
// is published. With server.compress a page of at least gzhttp's minimum size is kept as gzip
// (compressPage), made once by the render, and a hit sends it as it is, or unzips it as it sends it
// to a client that takes no gzip. Otherwise the HTML is kept. A page the render has just made has
// both, for the requests that waited for it: the cache drops the HTML when it stores the page.
type cachedPage struct {
	html string
	gzip []byte
	// n is the length of the HTML, for the Content-Length of a page kept as gzip.
	n int
	// policy is the Content-Security-Policy of the page with the hashes of its inline scripts and
	// styles, "" with server.csp off (see renderedPolicy).
	policy string
	// mime is the Content-Type of a file of the site (see siteAnswer), "" for HTML, and link its
	// Link header, "" for none. A page has neither.
	mime, link string
	// redirect is set for a render that the router ended with a redirect: the page is that
	// redirect and has no HTML.
	redirect *ssr.RedirectError
}

// contentType is the Content-Type the page goes out with.
func (p cachedPage) contentType() string {
	if p.mime == "" {
		return "text/html; charset=utf-8"
	}
	return p.mime
}

func (p cachedPage) size() int64 { return p.bodySize() + p.meta() }

// bodySize is what the cache keeps of the page: the gzip form when there is one, else the HTML.
func (p cachedPage) bodySize() int64 {
	if p.gzip != nil {
		return int64(len(p.gzip))
	}
	return int64(len(p.html))
}

// meta is what the page costs besides its body: its policy, which every page keeps for itself, its
// Link header and a redirect.
func (p cachedPage) meta() int64 {
	n := int64(len(p.policy) + len(p.link))
	if p.redirect != nil {
		n += int64(len(p.redirect.Location)) + 32
	}
	return n
}

// stats returns what the cache holds and did, and the bytes it holds.
func (c *renderMemo) stats() (render core.MemoStats, used int64) {
	c.mu.Lock()
	entries, evictions, used := c.entries, c.evictions, c.used
	c.mu.Unlock()
	return core.MemoStats{Entries: entries, Hits: c.hits.Load(), Misses: c.misses.Load(), Shared: c.shared.Load(), Evictions: evictions}, used
}

func newRenderMemo(size int64) *renderMemo {
	// The text config refuses a negative size, but a config built in Go can hold one: it is off, as 0 is.
	size = max(size, 0)
	return &renderMemo{size: size, protCap: size / 100 * protectedShare, probation: list.New(), protected: list.New(), pages: map[string]*list.Element{}, flights: map[string]*flight{},
		failed: map[string]*list.Element{}, failures: list.New(), now: time.Now}
}

func (c *renderMemo) get(key string) (cachedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.pages[key]
	if !ok {
		return cachedPage{}, false
	}
	c.hitLocked(e)
	return e.Value.(*cached).page, true
}

// hitLocked records a hit on e. A page in probation that fits the protected list moves to its head,
// and the protected pages that no longer fit go back to the head of probation.
func (c *renderMemo) hitLocked(e *list.Element) {
	v := e.Value.(*cached)
	switch {
	case v.prot:
		c.protected.MoveToFront(e)
	case v.page.size() > c.protCap:
		c.probation.MoveToFront(e)
	default:
		c.probation.Remove(e)
		v.prot = true
		c.pages[v.key] = c.protected.PushFront(v)
		c.protUsed += v.page.size()
		c.demoteLocked()
	}
}

// demoteLocked sends the least recently used protected pages back to probation until the protected
// list fits. The total in the memo does not change.
func (c *renderMemo) demoteLocked() {
	for c.protUsed > c.protCap {
		e := c.protected.Back()
		v := c.protected.Remove(e).(*cached)
		v.prot = false
		c.protUsed -= v.page.size()
		c.pages[v.key] = c.probation.PushFront(v)
	}
}

// kept returns the page as the cache stores it: a page that has its gzip form keeps that alone.
func (p cachedPage) kept() cachedPage {
	if p.gzip != nil {
		p.html = ""
	}
	return p
}

// off reports that the cache stores nothing: ssr.memo_size is below 1.
func (c *renderMemo) off() bool { return c.size < 1 }

// holds reports whether the cache can keep the page, in the form it keeps it. A page it cannot hold
// is sent as it comes.
func (c *renderMemo) holds(p cachedPage) bool { return !c.off() && p.kept().size() <= c.size }

// put stores a page in its one form, or does not when that does not fit.
func (c *renderMemo) put(key string, page cachedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, page)
}

func (c *renderMemo) putLocked(key string, page cachedPage) {
	// A page that replaces one of the same key keeps the list of the old one, if it still fits there.
	wasProt := false
	if e, ok := c.pages[key]; ok {
		wasProt = e.Value.(*cached).prot
	}
	c.removeLocked(key)
	page = page.kept()
	if page.size() > c.size {
		return
	}
	v := &cached{key: key, page: page}
	if wasProt && page.size() <= c.protCap {
		v.prot = true
		c.pages[key] = c.protected.PushFront(v)
		c.protUsed += page.size()
		c.demoteLocked()
	} else {
		c.pages[key] = c.probation.PushFront(v)
	}
	c.entries++
	c.used += page.size()
	for c.used > c.size {
		// Probation drops from its tail. When the new page is all it holds, the protected page
		// least recently used goes instead, so the new page is kept: it fits the memo alone.
		l := c.probation
		if l.Len() == 1 && !v.prot {
			l = c.protected
		}
		c.removeLocked(l.Back().Value.(*cached).key)
		c.evictions++
	}
}

func (c *renderMemo) removeLocked(key string) {
	if e, ok := c.pages[key]; ok {
		v := e.Value.(*cached)
		c.used -= v.page.size()
		if v.prot {
			c.protected.Remove(e)
			c.protUsed -= v.page.size()
		} else {
			c.probation.Remove(e)
		}
		delete(c.pages, key)
		c.entries--
	}
}

// fetched is a page fetch answers with. how says where it came from: memo, vm for a render the
// request started, or shared for one another request started.
type fetched struct {
	page cachedPage
	how  string
}

// fetch returns the page of key from the cache, or else waits for the render of it that every
// request missing key at the time waits for. The first of them starts render in a goroutine of
// its own, with a context no request cancels, so the render belongs to none of them. render
// returns the page and what to keep of it. What it keeps enters the cache as the render leaves the
// requests, in one step, so a request comes either in time to wait or late enough to find it.
//
// A request whose ctx ends stops waiting with ctx's error, and the render goes on for the others.
// When no request waits any more, the render's context is canceled, which ends its wait for a
// VM. A render that already runs in a VM runs to the end and its page enters the cache, but the
// next request starts a render of its own. Every waiting request gets the error of a failed
// render, a panic turned into an error. Nothing of it is kept, so the next request renders again,
// unless render said keepBrief: then requests for key get the same error, or page, without a render
// for failMemory. Such an answer counts as none of hits, misses and shared, and its error is a
// *rememberedError.
// A render that fails when no request waits any more goes to unheard, except one that stopped
// because no request waits.
func (c *renderMemo) fetch(ctx context.Context, key string, unheard func(error), render func(context.Context) (cachedPage, keep, error)) (fetched, error) {
	if c.fetching != nil {
		c.fetching()
	}
	c.mu.Lock()
	if e, ok := c.pages[key]; ok {
		c.hitLocked(e)
		page := e.Value.(*cached).page
		c.mu.Unlock()
		c.hits.Add(1)
		return fetched{page: page, how: "memo"}, nil
	}
	if e, ok := c.failed[key]; ok {
		if r := e.Value.(*failure); c.now().Before(r.until) {
			c.mu.Unlock()
			var err error
			if r.err != nil {
				err = &rememberedError{r.err}
			}
			return fetched{page: r.page, how: "memo"}, err
		}
		c.forgetLocked(e)
	}
	how := "shared"
	counter := &c.shared
	f := c.flights[key]
	if f == nil {
		how, counter = "vm", &c.misses
		f = &flight{done: make(chan struct{})}
		var flightCtx context.Context
		flightCtx, f.cancel = context.WithCancel(context.WithoutCancel(ctx))
		c.flights[key] = f
		go c.fly(flightCtx, key, f, unheard, render)
	}
	f.waiters++
	c.mu.Unlock()
	counter.Add(1)
	select {
	case <-f.done:
		return fetched{page: f.page, how: how}, f.err
	case <-ctx.Done():
		c.mu.Lock()
		f.waiters--
		if f.waiters == 0 {
			f.cancel()
			if c.flights[key] == f {
				delete(c.flights, key)
			}
		}
		c.mu.Unlock()
		return fetched{}, ctx.Err()
	}
}

// fly runs the render of f and hands its result to the requests that wait for it.
func (c *renderMemo) fly(ctx context.Context, key string, f *flight, unheard func(error), render func(context.Context) (cachedPage, keep, error)) {
	returned := false
	defer func() {
		if !returned {
			// The panic would end the program in this goroutine, and the waiting requests
			// would never hear of it.
			if p := recover(); p != nil {
				f.err = fmt.Errorf("tifa: panic: %v\n%s", p, debug.Stack())
			} else {
				f.err = errors.New("tifa: the render called runtime.Goexit")
			}
			f.page, f.keep = cachedPage{}, keepNot
		}
		c.mu.Lock()
		switch {
		case f.keep == keepBrief:
			c.rememberLocked(key, f.page, f.err)
		case f.err == nil && f.keep == keepPage:
			c.putLocked(key, f.page)
		case f.err == nil:
			// A result that cannot be kept replaces the page it came instead of: it must not
			// come back as the old one.
			c.removeLocked(key)
		}
		if c.landing != nil {
			c.landing()
		}
		if c.flights[key] == f {
			delete(c.flights, key)
		}
		alone := f.waiters == 0
		c.mu.Unlock()
		stopped := errors.Is(f.err, context.Canceled) && ctx.Err() != nil
		f.cancel()
		close(f.done)
		if f.err != nil && alone && !stopped {
			unheard(f.err)
		}
	}()
	f.page, f.keep, f.err = render(ctx)
	returned = true
}

// failMemory is how long the memo answers a key whose render landed with keepBrief, a render that
// the page's code or the VM's memory failed or the page that shows such an error, with the same
// result and without rendering. A page that fails for every request then costs its renders once
// every failMemory, not on each request, while a fix that changes the build or the data shows at once.
const failMemory = 5 * time.Second

// failCount and failBytes bound what the memo remembers for failMemory: at most failCount results,
// whose pages come to at most failBytes. The oldest go first, and a page larger than failBytes is
// not remembered.
const (
	failCount = 256
	failBytes = 8 << 20
)

// keep says what the memo does with the result of a render.
type keep int

const (
	keepNot   keep = iota // nothing: the next request renders again
	keepPage              // the page, until it is evicted
	keepBrief             // the page or the error, for failMemory
)

// keepIf is keepPage when kept, else keepNot.
func keepIf(kept bool) keep {
	if kept {
		return keepPage
	}
	return keepNot
}

// failure is a result the memo remembers for failMemory: an error, or the page of a render that
// showed one.
type failure struct {
	key   string
	page  cachedPage
	err   error
	until time.Time
}

// rememberedError is the error of a render that the memo answers from a failure it remembers. It
// says that in place of the stack trace of the error, which was reported with the first request.
type rememberedError struct{ err error }

func (e *rememberedError) Error() string {
	head, _, _ := strings.Cut(e.err.Error(), "\n")
	return head + " (the same input failed less than " + failMemory.String() + " ago, so it did not render again)"
}

func (e *rememberedError) Unwrap() error { return e.err }

// rememberLocked keeps the result of a render for failMemory, dropping the oldest results, and
// those whose time is up, to keep within failCount and failBytes. An error keeps no page.
func (c *renderMemo) rememberLocked(key string, page cachedPage, err error) {
	if e, ok := c.failed[key]; ok {
		c.forgetLocked(e)
	}
	if err != nil {
		page = cachedPage{}
	}
	size := page.size()
	if size > failBytes {
		return
	}
	now := c.now()
	for e := c.failures.Front(); e != nil; e = c.failures.Front() {
		if len(c.failed) < failCount && c.failUsed+size <= failBytes && now.Before(e.Value.(*failure).until) {
			break
		}
		c.forgetLocked(e)
	}
	c.failed[key] = c.failures.PushBack(&failure{key: key, page: page, err: err, until: now.Add(failMemory)})
	c.failUsed += size
}

func (c *renderMemo) forgetLocked(e *list.Element) {
	f := c.failures.Remove(e).(*failure)
	delete(c.failed, f.key)
	c.failUsed -= f.page.size()
}
