package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
)

// htmlCache keeps rendered pages of Cache = true routes by the hash of their render input, up
// to size bytes of HTML and its compressed forms. It drops the least recently used pages first.
// Requests that miss the same page at the same time wait for one render of it (fetch).
type htmlCache struct {
	mu    sync.Mutex
	size  int64
	used  int64
	order *list.List // of *cached, most recently used first
	pages map[string]*list.Element
	// flights holds the renders of pages not in the cache yet, by key.
	flights map[string]*flight
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

type cached struct {
	key  string
	page cachedPage
}

// cachedPage is a rendered page as the cache keeps it: the HTML and, with server.compress, its
// gzip and zstd forms (compressPage), made once by the render, so a hit is not compressed again.
// A page whose forms do not fit is kept as HTML, and hits compress it as they send it.
type cachedPage struct {
	html       string
	gzip, zstd []byte
}

func (p cachedPage) size() int64 { return int64(len(p.html) + len(p.gzip) + len(p.zstd)) }

func newHTMLCache(size int64) *htmlCache {
	return &htmlCache{size: size, order: list.New(), pages: map[string]*list.Element{}, flights: map[string]*flight{}}
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

// holds reports whether the cache can keep the HTML of a page. Pages it cannot hold are not
// compressed for it.
func (c *htmlCache) holds(html string) bool { return int64(len(html)) <= c.size }

// put stores a page. A page whose compressed forms do not fit in the whole cache is kept without
// them, and one whose HTML does not fit is not kept.
func (c *htmlCache) put(key string, page cachedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, page)
}

func (c *htmlCache) putLocked(key string, page cachedPage) {
	if e, ok := c.pages[key]; ok {
		c.used -= e.Value.(*cached).page.size()
		c.order.Remove(e)
		delete(c.pages, key)
	}
	if page.size() > c.size {
		page.gzip, page.zstd = nil, nil
	}
	if page.size() > c.size {
		return
	}
	c.pages[key] = c.order.PushFront(&cached{key: key, page: page})
	c.used += page.size()
	for c.used > c.size {
		oldest := c.order.Back()
		entry := oldest.Value.(*cached)
		c.order.Remove(oldest)
		delete(c.pages, entry.key)
		c.used -= entry.page.size()
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
		return fetched{page: e.Value.(*cached).page, held: true, how: "cache"}, nil
	}
	how := "shared"
	f := c.flights[key]
	if f == nil {
		how = "vm"
		f = &flight{done: make(chan struct{})}
		var flightCtx context.Context
		flightCtx, f.cancel = context.WithCancel(context.WithoutCancel(ctx))
		c.flights[key] = f
		go c.fly(flightCtx, key, f, unheard, render)
	}
	f.waiters++
	c.mu.Unlock()
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
