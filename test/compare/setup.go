package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"sync"
)

// The data a round needs is made after the server starts and before the clock, like the rows of a
// database before a benchmark, the same on every framework:
//
//   - the restock (prepare, restock.go) gives every product the stock the round can buy,
//   - then one order of the back office for each ship the round plans (setupOrders), placed with the
//     create of the staff, so that each ship of the plan has an order of its own to ship,
//   - then one cart for each returning visit of the round (setupCarts), through the shop's own cart
//     API (returningCart): a returning visitor comes back to a cart of their own, used by that visit
//     alone.
//
// Making them must put nothing into a render memo: every answer must be the status planned and render
// no page (X-Tifa-Render absent or none), or the round is not valid.

// setupWorkers is how many carts are made at once.
const setupWorkers = 8

// setup makes the orders of the ships and the carts of the returning visitors among visits seq0 to
// seq0+n-1, after the restock. It reports whether they were made: when not, round says why, and the
// round is not played.
func (r *runner) setup(ctx context.Context, round *Round, seq0, n int) bool {
	return r.setupOrders(ctx, round, seq0, n) && r.setupCarts(ctx, round, seq0, n)
}

// noRedirect is the CheckRedirect of the clients of the setup: an answer is checked as the server
// sent it, and a redirect is not followed to a page that would render.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// sendSetup sends one request of the setup with c and returns the body of its answer. It fails on an
// answer with another status than want or one that rendered a page.
func sendSetup(ctx context.Context, c *http.Client, base, method, path string, body []byte, want int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if res.StatusCode != want {
		return nil, fmt.Errorf("%s %s: status %d, want %d: %.200s", method, path, res.StatusCode, want, data)
	}
	if how := res.Header.Get(RenderHeader); how != "" && how != RenderNone {
		return nil, fmt.Errorf("%s %s rendered a page (%s %s): the setup must put nothing into the render memo", method, path, RenderHeader, how)
	}
	return data, nil
}

// setupOrders places, for each ship of visits seq0 to seq0+n-1 in the order of the plan, an order of
// the project of the ship with the create of the staff and the items AdminBody gives it, and keeps it
// for that ship (pool.made). It notes in round how many requests it took, and reports whether they
// were placed: when not, round says why.
func (r *runner) setupOrders(ctx context.Context, round *Round, seq0, n int) bool {
	c := &http.Client{CheckRedirect: noRedirect}
	for s := seq0; s < seq0+n; s++ {
		writes := r.wl.writesOf(s)
		for _, after := range slices.Sorted(maps.Keys(writes)) {
			w := writes[after]
			if w.Kind != WriteShip {
				continue
			}
			round.SetupRequests++
			data, err := sendSetup(ctx, c, r.base, http.MethodPost, r.create(w.Project), mustJSON(AdminBody(r.seed, w.Seq, w.After, WriteCreate, w.Project)), 200)
			var placed struct {
				ID string `json:"id"`
			}
			if err == nil {
				if err = json.Unmarshal(data, &placed); err == nil && placed.ID == "" {
					err = fmt.Errorf("no order id in the answer: %.200s", data)
				}
			}
			if err != nil {
				round.SetupError = fmt.Sprintf("the order of the ship of visit %d after view %d: %v", w.Seq, w.After, err)
				return false
			}
			r.pool.made(w, placed.ID)
		}
	}
	return true
}

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
				c := &http.Client{Transport: tr, Jar: jar, CheckRedirect: noRedirect}
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
		if _, err := sendSetup(ctx, c, r.base, q.Method, r.storeWrite(q.Path), q.Body, q.Status); err != nil {
			return err
		}
	}
	return nil
}

// shopMaxCarts is how many carts the shop keeps (maxCarts in its cart store, and in the ports).
// Beyond it the shop drops the cart used longest ago, so a run that makes more would lose carts it
// counts on: the cart store stands for the database of a real shop, which drops none.
const shopMaxCarts = 100_000

// checkCarts is how many carts the check after a round makes on its server (verifyStore).
const checkCarts = 1

// cartsOf is the most carts visits seq0 to seq0+n-1 make on a fresh server, with the check after
// them: one for each returning visit, made before the clock, one for each other visit that writes
// to its cart (its first write turns the demonstration cart into a cart of its own), and checkCarts.
func (w *Workload) cartsOf(seq0, n int) int {
	carts := checkCarts
	for s := seq0; s < seq0+n; s++ {
		if p := w.planFor(s); p.Role == RoleShopper && p.Returning {
			carts++
			continue
		}
		for _, x := range w.writesOf(s) {
			if x.Store != nil {
				carts++
				break
			}
		}
	}
	return carts
}

// cartsFit fails when visits seq0 to seq0+n-1 can make more carts than a shop that keeps keep: the
// shop would drop carts the round counts on.
func (w *Workload) cartsFit(seq0, n, keep int) error {
	if c := w.cartsOf(seq0, n); c > keep {
		return fmt.Errorf("%d visits make up to %d carts, more than the %d the shop keeps", n, c, keep)
	}
	return nil
}
