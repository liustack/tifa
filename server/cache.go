package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// htmlCache keeps rendered pages by the hash of their render input, up
// to size bytes of pages, each in the one form it keeps. It drops the least recently used pages first.
// Requests that miss the same page at the same time wait for one render of it (fetch).
type htmlCache struct {
	mu    sync.Mutex
	size  int64
	used  int64
	order *list.List // of *cached, most recently used first
	pages map[string]*list.Element
	// flights holds the renders of pages not in the cache yet, by key.
	flights map[string]*flight
	// bodies counts, for the pages that share their body, how many pages use each, so the bytes
	// count once.
	bodies map[*bodyID]*bodyUse
	// entries counts the pages kept, under mu. hits, misses and shared count what fetch found. Every
	// request counts, so these are atomic, and Stats reads them without the lock.
	entries              int
	hits, misses, shared atomic.Uint64
	// Hooks of the tests, nil otherwise: fetching runs as fetch starts, landing between storing
	// a render and ending its flight.
	fetching, landing func()
}

// flight is the one render of a page that every request missing it waits for, see fetch.
type flight struct {
	done    chan struct{}
	waiters int                // requests waiting for it, under htmlCache.mu
	cancel  context.CancelFunc // stops the render while it waits for a VM
	// Set before done is closed: the page and whether the cache can keep it, or the error.
	page cachedPage
	held bool
	err  error
}

// bodyID stands for the body one render made. It is not empty, because Go may
// give every pointer to an empty value the same address, and the pages would count as one.
type bodyID struct{ _ byte }

type bodyUse struct {
	refs  int
	bytes int64
}

type cached struct {
	key  string
	page cachedPage
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
	// body is shared by the pages made from one render, see htmlCache.bodies. Nil for a page that
	// shares nothing.
	body *bodyID
	// redirect is set for a render that the router ended with a redirect: the page is that
	// redirect and has no HTML.
	redirect *ssr.RedirectError
}

func (p cachedPage) size() int64 { return p.bodySize() + p.meta() }

// bodySize is what the cache keeps of the page: the gzip form when there is one, else the HTML.
func (p cachedPage) bodySize() int64 {
	if p.gzip != nil {
		return int64(len(p.gzip))
	}
	return int64(len(p.html))
}

// meta is what the page costs besides its body: its policy, which every page keeps for itself, and
// a redirect.
func (p cachedPage) meta() int64 {
	n := int64(len(p.policy))
	if p.redirect != nil {
		n += int64(len(p.redirect.Location)) + 32
	}
	return n
}

// stats returns what the cache holds and did, and the bytes it holds.
func (c *htmlCache) stats() (render core.CacheStats, used int64) {
	c.mu.Lock()
	entries, used := c.entries, c.used
	c.mu.Unlock()
	return core.CacheStats{Entries: entries, Hits: c.hits.Load(), Misses: c.misses.Load(), Shared: c.shared.Load()}, used
}

func newHTMLCache(size int64) *htmlCache {
	// The text config refuses a negative size, but a config built in Go can hold one: it is off, as 0 is.
	return &htmlCache{size: max(size, 0), order: list.New(), pages: map[string]*list.Element{}, flights: map[string]*flight{}, bodies: map[*bodyID]*bodyUse{}}
}

func (c *htmlCache) get(key string) (cachedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.pages[key]
	if !ok {
		return cachedPage{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cached).page, true
}

// kept returns the page as the cache stores it: a page that has its gzip form keeps that alone.
func (p cachedPage) kept() cachedPage {
	if p.gzip != nil {
		p.html = ""
	}
	return p
}

// off reports that the cache stores nothing: cache.size is below 1.
func (c *htmlCache) off() bool { return c.size < 1 }

// holds reports whether the cache can keep the page, in the form it keeps it. A page it cannot hold
// is sent as it comes.
func (c *htmlCache) holds(p cachedPage) bool { return !c.off() && p.kept().size() <= c.size }

// put stores a page in its one form, or does not when that does not fit.
func (c *htmlCache) put(key string, page cachedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, page)
}

func (c *htmlCache) putLocked(key string, page cachedPage) {
	c.removeLocked(key)
	page = page.kept()
	if page.size() > c.size {
		return
	}
	c.pages[key] = c.order.PushFront(&cached{key: key, page: page})
	c.entries++
	c.charge(page)
	for c.used > c.size {
		c.removeLocked(c.order.Back().Value.(*cached).key)
	}
}

// charge counts a page that enters the cache. Its body counts once for all the
// pages that share them.
func (c *htmlCache) charge(p cachedPage) {
	c.used += p.meta()
	if p.body == nil {
		c.used += p.bodySize()
		return
	}
	if u := c.bodies[p.body]; u != nil {
		u.refs++
		return
	}
	c.bodies[p.body] = &bodyUse{refs: 1, bytes: p.bodySize()}
	c.used += p.bodySize()
}

// refund undoes charge for a page that leaves.
func (c *htmlCache) refund(p cachedPage) {
	c.used -= p.meta()
	if p.body == nil {
		c.used -= p.bodySize()
		return
	}
	u := c.bodies[p.body]
	if u.refs--; u.refs == 0 {
		c.used -= u.bytes
		delete(c.bodies, p.body)
	}
}

func (c *htmlCache) removeLocked(key string) {
	if e, ok := c.pages[key]; ok {
		c.refund(e.Value.(*cached).page)
		c.order.Remove(e)
		delete(c.pages, key)
		c.entries--
	}
}

// fetched is a page fetch answers with. held reports that the cache keeps it or could, and how
// says where it came from: cache, vm for a render the request started, or shared for one another
// request started.
type fetched struct {
	page cachedPage
	held bool
	how  string
}

// fetch returns the page of key from the cache, or else waits for the render of it that every
// request missing key at the time waits for. The first of them starts render in a goroutine of
// its own, with a context no request cancels, so the render belongs to none of them. render
// returns the page and whether to keep it. Its page enters the cache as the render leaves the
// requests, in one step, so a request comes either in time to wait or late enough to find it.
//
// A request whose ctx ends stops waiting with ctx's error, and the render goes on for the others.
// When no request waits any more, the render's context is canceled, which ends its wait for a
// VM. A render that already runs in a VM runs to the end and its page enters the cache, but the
// next request starts a render of its own. Every waiting request gets the error of a failed
// render, a panic turned into an error. Nothing of it is kept, so the next request renders again.
// A render that fails when no request waits any more goes to unheard, except one that stopped
// because no request waits.
func (c *htmlCache) fetch(ctx context.Context, key string, unheard func(error), render func(context.Context) (cachedPage, bool, error)) (fetched, error) {
	if c.fetching != nil {
		c.fetching()
	}
	c.mu.Lock()
	if e, ok := c.pages[key]; ok {
		c.order.MoveToFront(e)
		c.mu.Unlock()
		c.hits.Add(1)
		return fetched{page: e.Value.(*cached).page, held: true, how: "cache"}, nil
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
		return fetched{page: f.page, held: f.held, how: how}, f.err
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
func (c *htmlCache) fly(ctx context.Context, key string, f *flight, unheard func(error), render func(context.Context) (cachedPage, bool, error)) {
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
			f.page, f.held = cachedPage{}, false
		}
		c.mu.Lock()
		if f.err == nil && f.held {
			c.putLocked(key, f.page)
		} else if f.err == nil {
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
	f.page, f.held, f.err = render(ctx)
	returned = true
}
