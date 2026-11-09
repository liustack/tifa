package compare

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
)

// The carts of the returning visitors are data the shop already holds when a round starts, like the
// rows of a database before a benchmark: a returning visitor comes back to a cart of their own. After
// the server starts and before the clock, the runner makes one cart for each returning visit of the
// round, through the shop's own cart API (returningCart), the same on every framework. Making them must
// put nothing into a render memo: every answer of the setup must render no page (X-Tifa-Render
// absent or none), or the round is not valid. Each cart is used by exactly one returning visit.

// setupWorkers is how many carts are made at once.
const setupWorkers = 8

// setupCarts makes the carts of the returning visitors among visits seq0 to seq0+n-1, keeps the
// cookies of each for its visit, and notes in round how many requests it took. It reports whether
// they were made: when not, round says why, and the round is not played.
func (r *runner) setupCarts(ctx context.Context, round *Round, seq0, n int) bool {
	type job struct {
		seq  int
		reqs []StoreRequest
	}
	var jobs []job
	for s := seq0; s < seq0+n; s++ {
		if p := r.wl.planFor(s); p.Role == RoleShopper && p.Returning {
			reqs, _ := returningCart(r.wl.Seed, s, p)
			jobs = append(jobs, job{s, reqs})
		}
	}
	r.carts = map[int]http.CookieJar{}
	if len(jobs) == 0 {
		return true
	}
	tr := &http.Transport{MaxIdleConnsPerHost: setupWorkers, IdleConnTimeout: idleConn}
	defer tr.CloseIdleConnections()
	var mu sync.Mutex
	var firstErr error
	next := make(chan job)
	var wg sync.WaitGroup
	for range setupWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range next {
				jar, _ := cookiejar.New(nil)
				c := &http.Client{Transport: tr, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				err := r.makeCart(ctx, c, j.reqs)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = fmt.Errorf("visit %d: %w", j.seq, err)
				}
				r.carts[j.seq] = jar
				round.SetupRequests += len(j.reqs)
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		next <- j
	}
	close(next)
	wg.Wait()
	if firstErr != nil {
		round.SetupError = firstErr.Error()
		return false
	}
	return true
}

// makeCart sends the writes that make a cart with the cookies of c, and fails on another status
// than the plan's or on an answer that rendered a page.
func (r *runner) makeCart(ctx context.Context, c *http.Client, reqs []StoreRequest) error {
	for _, q := range reqs {
		path := r.storeWrite(q.Path)
		req, err := http.NewRequestWithContext(ctx, q.Method, r.base+path, bytes.NewReader(q.Body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != q.Status {
			return fmt.Errorf("%s %s: status %d, want %d", q.Method, path, res.StatusCode, q.Status)
		}
		if how := res.Header.Get(RenderHeader); how != "" && how != RenderNone {
			return fmt.Errorf("%s %s rendered a page (%s %s): the setup must put nothing into the render memo", q.Method, path, RenderHeader, how)
		}
	}
	return nil
}

// shopMaxCarts is how many carts the shop keeps (maxCarts in its cart store, and in the ports).
// Beyond it the shop drops the cart used longest ago, so a run that makes more would lose carts it
// counts on: the cart store stands for the database of a real shop, which drops none.
const shopMaxCarts = 100_000
