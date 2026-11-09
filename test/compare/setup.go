package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strconv"
	"strings"
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
// seq0+n-1, after the restock, and reads the render memo of Tifa last (startMemo), before the
// warm-up. It reports whether they were made: when not, round says why, and the round is not played.
func (r *runner) setup(ctx context.Context, round *Round, seq0, n int) bool {
	// The data is made for the visits the run was prepared for (browse.with) and those of its warm-up,
	// which come after them, when they are more.
	if seq0 == 0 {
		n = max(n, r.prepared+r.warm)
	}
	return r.setupOrders(ctx, round, seq0, n) && r.setupCarts(ctx, round, seq0, n) && r.startMemo(ctx, round)
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
			reqs := returningCart(r.wl.Seed, s, p)
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

// MemoCounts is what the render memo of Tifa counted, as the metrics route of the shop answers it
// (tifa.Stats().Memo and MemoBytes): the pages it holds and their bytes, and its hits, misses,
// shared renders and the pages it dropped to make room since the server started.
type MemoCounts struct {
	Entries   int64 `json:"entries"`
	Bytes     int64 `json:"bytes"`
	Hits      int64 `json:"hits"`
	Misses    int64 `json:"misses"`
	Shared    int64 `json:"shared"`
	Evictions int64 `json:"evictions"`
}

// MemoRound is the render memo of Tifa at the start of a round, after its setup and before its
// warm-up, after the warm-up right before its clock (the same as Start in a run with no warm-up), and
// at its end.
type MemoRound struct {
	Start MemoCounts `json:"start"`
	Warm  MemoCounts `json:"warm"`
	End   MemoCounts `json:"end"`
}

// Hits is the hits of the memo over the round, from its clock on, as the server counted them.
func (m *MemoRound) Hits() int64 { return m.End.Hits - m.Warm.Hits }

// memoMetrics are the lines of the metrics route that MemoCounts reads, by name.
var memoMetrics = map[string]func(*MemoCounts) *int64{
	"tifa_memo_entries":         func(m *MemoCounts) *int64 { return &m.Entries },
	"tifa_memo_hits_total":      func(m *MemoCounts) *int64 { return &m.Hits },
	"tifa_memo_misses_total":    func(m *MemoCounts) *int64 { return &m.Misses },
	"tifa_memo_shared_total":    func(m *MemoCounts) *int64 { return &m.Shared },
	"tifa_memo_bytes":           func(m *MemoCounts) *int64 { return &m.Bytes },
	"tifa_memo_evictions_total": func(m *MemoCounts) *int64 { return &m.Evictions },
}

// readMemo reads the counts of the render memo from the metrics route of the server (Spec.Metrics),
// which renders nothing.
func (r *runner) readMemo(ctx context.Context) (MemoCounts, error) {
	var m MemoCounts
	data, err := sendSetup(ctx, &http.Client{CheckRedirect: noRedirect}, r.base, http.MethodGet, r.metrics, nil, 200)
	if err != nil {
		return m, err
	}
	found := 0
	for line := range strings.Lines(string(data)) {
		name, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		field := memoMetrics[name]
		if !ok || field == nil || strings.HasPrefix(name, "#") {
			continue
		}
		// The route writes a value as Prometheus does, a float (%g): a million is 1e+06.
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || n != math.Trunc(n) {
			return m, fmt.Errorf("%s: %s is not a count: %q", r.metrics, name, value)
		}
		*field(&m) = int64(n)
		found++
	}
	if found != len(memoMetrics) {
		return m, fmt.Errorf("%s: %d of the %d counts of the render memo: %.200s", r.metrics, found, len(memoMetrics), data)
	}
	return m, nil
}

// startMemo reads the render memo of Tifa right before the warm-up of a round (right before the
// clock in a run with none), when the server has the metrics route. It must hold nothing then: a
// page in it was put there before. It reports whether the round can start: when not, round says why.
func (r *runner) startMemo(ctx context.Context, round *Round) bool {
	if r.metrics == "" {
		return true
	}
	m, err := r.readMemo(ctx)
	switch {
	case err != nil:
		round.SetupError = "the metrics: " + err.Error()
	case m.Entries != 0:
		round.SetupError = fmt.Sprintf("the render memo held %d pages before the warm-up: something filled it before", m.Entries)
	default:
		round.Memo = &MemoRound{Start: m, Warm: m}
		return true
	}
	return false
}

// warmMemo reads the render memo of Tifa again after the warm-up of a round that read it before,
// right before the clock: what the warm-up left in it. It reports whether the round can start: when
// not, round says why.
func (r *runner) warmMemo(ctx context.Context, round *Round) bool {
	if round.Memo == nil {
		return true
	}
	m, err := r.readMemo(ctx)
	if err != nil {
		round.SetupError = "the metrics after the warm-up: " + err.Error()
		return false
	}
	round.Memo.Warm = m
	return true
}

// endMemo reads the render memo of Tifa at the end of a round that read it at its start.
func (r *runner) endMemo(ctx context.Context, round *Round) {
	if round.Memo == nil {
		return
	}
	m, err := r.readMemo(ctx)
	if err != nil {
		round.MemoError = err.Error()
		return
	}
	round.Memo.End = m
}
