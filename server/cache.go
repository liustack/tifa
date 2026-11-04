package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// cacheNow is the clock of the whole-page cache. Tests set it.
var cacheNow = time.Now

// htmlCache keeps rendered pages by the hash of their render input, up
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
	// bypass holds, by page key, until when requests skip the cache of that page: its last result
	// could not be stored, see markBypass.
	bypass map[string]time.Time
	// bodies counts, for the pages that share their HTML and compressed forms (the render cache
	// and the whole-page cache keep the same render), how many pages use each, so the bytes count once.
	bodies map[*bodyID]*bodyUse
	// madeAfter is the moment the content collections last changed: a page of the whole-page
	// cache made before it is no longer fresh, see expireMadeBefore.
	madeAfter time.Time
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
	// unshared is set when the result belongs to the request that made it: its headers say it is
	// not for everyone, so the requests that waited make their own, see effects.
	unshared bool
}

// effects are what the result of making a page of the whole-page cache does to the cache besides
// being stored. They are applied when the flight ends, and only if it is still the flight of its
// key: a make that every request gave up on, and another one replaced, must not touch what the
// newer one did.
type effects struct {
	set      bool // the other fields are meant
	remove   bool // the old page is gone: an answer of the app replaces it
	bypass   bool // requests skip the cache for a while, see markBypass
	unshared bool
}

// madeError is the error of making a page of the whole-page cache. headers are the response headers the
// Load and Layout functions set before it failed.
type madeError struct {
	err     error
	headers http.Header
	fx      effects
}

func (e *madeError) Error() string { return e.err.Error() }
func (e *madeError) Unwrap() error { return e.err }

// bodyID stands for the HTML and compressed forms one render made. It is not empty, because Go may
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

// cachedPage is a rendered page as the cache keeps it: the HTML and, with server.compress, its
// gzip and zstd forms (compressPage), made once by the render, so a hit is not compressed again.
// A page whose forms do not fit is kept as HTML, and hits compress it as they send it.
type cachedPage struct {
	html       string
	gzip, zstd []byte
	// body is shared by the pages made from one render, see htmlCache.bodies. Nil for a page that
	// shares nothing.
	body *bodyID
	// whole is set for a page of the whole-page cache: what it stands for besides the HTML.
	whole *wholePage
}

func (p cachedPage) size() int64 {
	n := p.bodySize()
	if p.whole != nil {
		n += p.whole.size()
	}
	return n
}

func (p cachedPage) bodySize() int64 { return int64(len(p.html) + len(p.gzip) + len(p.zstd)) }

// meta is what the page costs besides its HTML and compressed forms.
func (p cachedPage) meta() int64 {
	if p.whole != nil {
		return p.whole.size()
	}
	return 0
}

// effects returns what the result of a make does to the cache, from its page or its error.
func effectsOf(page cachedPage, err error) effects {
	var made *madeError
	switch {
	case page.whole != nil:
		return page.whole.fx
	case errors.As(err, &made):
		return made.fx
	}
	return effects{}
}

// fresh reports whether a page may be served at now: a page of the render cache always may, a
// page of the whole-page cache until its time is over, if it was made after madeAfter.
func (p cachedPage) fresh(now, madeAfter time.Time) bool {
	return p.whole == nil || now.Before(p.whole.expires) && p.whole.created.After(madeAfter)
}

// expireMadeBefore makes every page of the whole-page cache made before t, or at t, no longer
// fresh: the next request makes it again. Such a page still stands in for one that fails (stale).
// The makes and renders under way read their data before t, so later requests do not wait for
// them and start their own: the requests that already wait get the result as before, and a make
// that is replaced so does not enter the cache (see fly). A render of the render cache that is
// under way may be done twice, which is rare, since content is published seldom.
func (c *htmlCache) expireMadeBefore(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.madeAfter) {
		c.madeAfter = t
	}
	clear(c.flights)
}

func newHTMLCache(size int64) *htmlCache {
	return &htmlCache{size: size, order: list.New(), pages: map[string]*list.Element{}, flights: map[string]*flight{}, bypass: map[string]time.Time{}, bodies: map[*bodyID]*bodyUse{}}
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
	c.removeLocked(key)
	if page.size() > c.size {
		// The forms the other pages share are dropped from this one, so it keeps a body of its own.
		page.gzip, page.zstd, page.body = nil, nil, nil
	}
	if page.size() > c.size {
		return
	}
	c.pages[key] = c.order.PushFront(&cached{key: key, page: page})
	c.charge(page)
	for c.used > c.size {
		oldest := c.order.Back()
		entry := oldest.Value.(*cached)
		c.order.Remove(oldest)
		delete(c.pages, entry.key)
		c.refund(entry.page)
	}
}

// charge counts a page that enters the cache. Its HTML and compressed forms count once for all the
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
	}
}

// stale returns the page of key even when its time is over, as long as it is within the time a
// page may stand in for a failed one (staleIfError after its time ended). Only pages of the
// whole-page cache qualify.
func (c *htmlCache) stale(key string, now time.Time) (cachedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.pages[key]
	if !ok {
		return cachedPage{}, false
	}
	page := e.Value.(*cached).page
	if page.whole == nil || !now.Before(page.whole.staleUntil) {
		return cachedPage{}, false
	}
	c.order.MoveToFront(e)
	return page, true
}

// markBypassLocked has requests for key skip the cache until the time given: a page whose result
// cannot be stored would otherwise make every request wait for the one before it (Varnish calls
// this hit-for-miss).
func (c *htmlCache) markBypassLocked(key string, until time.Time) {
	if len(c.bypass) >= maxBypass {
		now := cacheNow()
		for k, t := range c.bypass {
			if !now.Before(t) {
				delete(c.bypass, k)
			}
		}
		if len(c.bypass) >= maxBypass {
			return
		}
	}
	c.bypass[key] = until
}

// bypassed reports whether requests for key skip the cache at now.
func (c *htmlCache) bypassed(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.bypass[key]
	if ok && !now.Before(until) {
		delete(c.bypass, key)
		return false
	}
	return ok
}

// maxBypass bounds the pages whose cache is skipped.
const maxBypass = 4096

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
	if e, ok := c.pages[key]; ok && e.Value.(*cached).page.fresh(cacheNow(), c.madeAfter) {
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
		if f.unshared && how == "shared" {
			return fetched{how: "retry"}, nil
		}
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
		fx := effectsOf(f.page, f.err)
		f.unshared = fx.unshared
		// A make that was given up on and replaced leaves the cache as the newer one made it.
		if current := c.flights[key] == f; current || !(fx.set || f.page.whole != nil) {
			if f.err == nil && f.held {
				c.putLocked(key, f.page)
			} else if f.err == nil {
				// A result that cannot be kept replaces the page it came instead of: the page is
				// gone or has changed to something else, and must not come back as the old one.
				c.removeLocked(key)
			}
			if fx.remove {
				c.removeLocked(key)
			}
			if fx.bypass {
				c.markBypassLocked(key, cacheNow().Add(hitForMiss))
			}
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
