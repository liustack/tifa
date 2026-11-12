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

// renderMemo keeps rendered pages by the hash of their render input, up
// to size bytes of pages, each in the one form it keeps. It drops the least recently used pages first.
// Requests that miss the same page at the same time wait for one render of it (fetch).
type renderMemo struct {
	mu    sync.Mutex
	size  int64
	used  int64
	order *list.List // of *cached, most recently used first
	pages map[string]*list.Element
	// urls holds, by the URL the render saw, the entry of the last render of it that the cache keeps,
	// for SamePage (see landLocked). An entry leaves it as it leaves the cache, so it never holds more
	// URLs than the cache holds pages.
	urls map[string]*list.Element
	// flights holds the renders of pages not in the cache yet, by key.
	flights map[string]*flight
	// entries counts the pages kept and evictions the pages dropped to make room, under mu.
	entries   int
	evictions uint64
	// routes holds the counts of each route by its pattern (route). A route enters on its first
	// request and stays, so a request reads it without a lock.
	routes sync.Map // of string to *routeCounts
	// Hooks of the tests, nil otherwise: fetching runs as fetch starts, landing between storing
	// a render and ending its flight.
	fetching, landing func()
}

// flight is the one render of a page that every request missing it waits for, see fetch.
type flight struct {
	done    chan struct{}
	waiters int                // requests waiting for it, under renderMemo.mu
	cancel  context.CancelFunc // stops the render while it waits for a VM
	// Set before done is closed: the page and whether the cache can keep it, or the error.
	page cachedPage
	held bool
	err  error
}

type cached struct {
	key  string
	url  string // the URL the render saw, "" for a page urls does not hold
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
	// redirect is set for a render that the router ended with a redirect: the page is that
	// redirect and has no HTML.
	redirect *ssr.RedirectError
	// shown is the hash of what the page shows (shownHash), for SamePage. A page with HTML that the
	// cache keeps has one.
	shown uint64
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

// routeCounts counts what fetch found for the pages of one route: every request counts, so the
// counts are atomic, and stats reads them without the lock. The counts of the whole memo are their
// sums, so a request adds to one count only.
type routeCounts struct {
	hits, misses, shared atomic.Uint64
	// samePage counts the misses whose page came out the same as the last one of its URL, see
	// landLocked.
	samePage atomic.Uint64
}

// route returns the counts of the route with pattern, the Ctx.Pattern of the request, "" for a URL
// that matches no page. Patterns are those the app registered, so the routes are few.
func (c *renderMemo) route(pattern string) *routeCounts {
	if r, ok := c.routes.Load(pattern); ok {
		return r.(*routeCounts)
	}
	r, _ := c.routes.LoadOrStore(pattern, &routeCounts{})
	return r.(*routeCounts)
}

// stats returns what the cache holds and did, and the bytes it holds.
func (c *renderMemo) stats() (render core.MemoStats, used int64) {
	c.mu.Lock()
	render.Entries, render.Evictions, used = c.entries, c.evictions, c.used
	c.mu.Unlock()
	c.routes.Range(func(pattern, counts any) bool {
		r := counts.(*routeCounts)
		s := core.MemoRouteStats{Hits: r.hits.Load(), Misses: r.misses.Load(), Shared: r.shared.Load(), SamePage: r.samePage.Load()}
		if render.Routes == nil {
			render.Routes = map[string]core.MemoRouteStats{}
		}
		render.Routes[pattern.(string)] = s
		render.Hits += s.Hits
		render.Misses += s.Misses
		render.Shared += s.Shared
		render.SamePage += s.SamePage
		return true
	})
	return render, used
}

func newRenderMemo(size int64) *renderMemo {
	// The text config refuses a negative size, but a config built in Go can hold one: it is off, as 0 is.
	return &renderMemo{size: max(size, 0), order: list.New(), pages: map[string]*list.Element{}, urls: map[string]*list.Element{}, flights: map[string]*flight{}}
}

func (c *renderMemo) get(key string) (cachedPage, bool) {
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
	c.removeLocked(key)
	page = page.kept()
	if page.size() > c.size {
		return
	}
	c.pages[key] = c.order.PushFront(&cached{key: key, page: page})
	c.entries++
	c.used += page.size()
	for c.used > c.size {
		c.removeLocked(c.order.Back().Value.(*cached).key)
		c.evictions++
	}
}

// landLocked stores the page a render of url made under key, as putLocked does, and counts it in
// samePage of route when the page the cache keeps of the last render of url has another key, so
// other data, and shows the same. A URL whose pages the cache dropped is seen for the first time.
// A redirect is not compared and does not become the last render of url.
func (c *renderMemo) landLocked(key, url string, route *routeCounts, page cachedPage) {
	if last, ok := c.urls[url]; ok && page.redirect == nil {
		was := last.Value.(*cached)
		if was.key != key && was.page.shown == page.shown {
			route.samePage.Add(1)
		}
	}
	c.putLocked(key, page)
	if e, ok := c.pages[key]; ok && page.redirect == nil && url != "" {
		e.Value.(*cached).url = url
		c.urls[url] = e
	}
}

func (c *renderMemo) removeLocked(key string) {
	if e, ok := c.pages[key]; ok {
		if url := e.Value.(*cached).url; c.urls[url] == e {
			delete(c.urls, url)
		}
		c.used -= e.Value.(*cached).page.size()
		c.order.Remove(e)
		delete(c.pages, key)
		c.entries--
	}
}

// fetched is a page fetch answers with. held reports that the cache keeps it or could, and how
// says where it came from: memo, vm for a render the request started, or shared for one another
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
// requests, in one step, so a request comes either in time to wait or late enough to find it. The
// request counts as a hit, a miss or a shared render of route. url is the URL the render sees, whose
// last render the page is compared with as it enters the cache (landLocked).
//
// A request whose ctx ends stops waiting with ctx's error, and the render goes on for the others.
// When no request waits any more, the render's context is canceled, which ends its wait for a
// VM. A render that already runs in a VM runs to the end and its page enters the cache, but the
// next request starts a render of its own. Every waiting request gets the error of a failed
// render, a panic turned into an error. Nothing of it is kept, so the next request renders again.
// A render that fails when no request waits any more goes to unheard, except one that stopped
// because no request waits.
func (c *renderMemo) fetch(ctx context.Context, key, url string, route *routeCounts, unheard func(error), render func(context.Context) (cachedPage, bool, error)) (fetched, error) {
	if c.fetching != nil {
		c.fetching()
	}
	c.mu.Lock()
	if e, ok := c.pages[key]; ok {
		c.order.MoveToFront(e)
		c.mu.Unlock()
		route.hits.Add(1)
		return fetched{page: e.Value.(*cached).page, held: true, how: "memo"}, nil
	}
	how := "shared"
	counter := &route.shared
	f := c.flights[key]
	if f == nil {
		how, counter = "vm", &route.misses
		f = &flight{done: make(chan struct{})}
		var flightCtx context.Context
		flightCtx, f.cancel = context.WithCancel(context.WithoutCancel(ctx))
		c.flights[key] = f
		go c.fly(flightCtx, key, url, route, f, unheard, render)
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
func (c *renderMemo) fly(ctx context.Context, key, url string, route *routeCounts, f *flight, unheard func(error), render func(context.Context) (cachedPage, bool, error)) {
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
			c.landLocked(key, url, route, f.page)
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
