package compare

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The ships of the staff go to orders placed for them before the clock (setupOrders): each ship of
// the plan, by its visit and its page view, has an order of its own, so the order a ship ships is
// the same on every framework and never depends on which answers came first. The orders of the data
// that wait to ship are left to the checks (verifyAdmin), and the orders a checkout of the
// storefront places are orders of the back office that no ship takes. An edit of a product needs no
// order.
type pool struct {
	mu      sync.Mutex
	ships   map[shipKey]string // the order made for each ship
	created map[string]int     // orders placed since the server started, by project
}

// shipKey is a ship of the plan: of visit seq, after page view after.
type shipKey struct{ seq, after int }

// made keeps the order id placed for the ship w.
func (p *pool) made(w PlannedWrite, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ships == nil {
		p.ships = map[shipKey]string{}
	}
	p.ships[shipKey{w.Seq, w.After}] = id
	p.countLocked(w.Project)
}

// ship returns the order made for the ship w, or false when the setup made none.
func (p *pool) ship(w PlannedWrite) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, ok := p.ships[shipKey{w.Seq, w.After}]
	return id, ok
}

// countCreated notes an order placed in a project.
func (p *pool) countCreated(project string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.countLocked(project)
}

func (p *pool) countLocked(project string) {
	if p.created == nil {
		p.created = map[string]int{}
	}
	p.created[project]++
}

// orderCounts is how many orders each project holds: those of the data and the ones placed.
func (p *pool) orderCounts() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]int{}
	for project, r := range OrderIDs {
		out[project] = r[1] - r[0] + 1 + p.created[project]
	}
	return out
}

// answered is what a write got back.
type answered struct {
	status int
	kind   string // the media type of the answer, "-" for none
	body   []byte // as sent, with the compression removed
	wire   int    // bytes on the wire
	// location is the Location header, where a checkout sends the visitor.
	location string
}

// request sends a JSON write and reads the whole answer. It takes one of the connections of the
// visit when the visit has them.
func (r *runner) request(ctx context.Context, v *visitState, method, path string, body []byte) (answered, time.Time, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, strings.NewReader(string(body)))
	if err != nil {
		return answered{}, time.Time{}, time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	if v.sem != nil {
		select {
		case v.sem <- struct{}{}:
		case <-ctx.Done():
			return answered{}, time.Time{}, time.Time{}, ctx.Err()
		}
		defer func() { <-v.sem }()
	}
	start := time.Now()
	r.inflight.Add(1)
	defer r.inflight.Add(-1)
	res, err := v.client.Do(req)
	if err != nil {
		return answered{}, start, time.Time{}, err
	}
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	end := time.Now()
	a := answered{status: res.StatusCode, kind: mediaType(res.Header.Get("Content-Type")), body: b, wire: len(b), location: res.Header.Get("Location")}
	if err == nil && res.Header.Get("Content-Encoding") == "gzip" {
		var zr *gzip.Reader
		if zr, err = gzip.NewReader(bytes.NewReader(b)); err == nil {
			a.body, err = io.ReadAll(zr)
		}
	}
	return a, start, end, err
}

// doWrite sends a write of the plan and times it from when it was scheduled. A write of a shopper
// sends the request of the plan to the path of this app, with the cookies of the visit, and the status
// must be the one the plan says (a redirect is not followed). A ship of the staff ships the order made
// for it before the clock (setupOrders), and an edit
// reads the products of its shop and sends one back changed (productChange).
func (r *runner) doWrite(ctx context.Context, v *visitState, a *acc, w PlannedWrite, scheduled time.Time) error {
	kind := w.Kind
	var method, path string
	var body []byte
	want := 200       // the status the shop answers with
	var read answered // the list of products an edit reads first
	switch {
	case w.Store != nil:
		st := w.Store
		method, path, body, want = st.Method, r.storeWrite(st.Path), st.Body, st.Status
	case kind == WriteEdit:
		// The edits of a shop go one at a time, so that two never read the same stock and the later
		// sends the smaller.
		mu, _ := r.editing.LoadOrStore(w.Project, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		var err error
		read, _, _, err = r.request(ctx, v, http.MethodGet, r.products(w.Project, r.buildID), nil)
		a.refuse(read.status)
		if err != nil {
			return err
		}
		if read.status != 200 {
			return fmt.Errorf("write %s: the list of products answered %d: %.200s", kind, read.status, read.body)
		}
		list, err := productList(w.Project, read.body)
		if err != nil {
			return fmt.Errorf("write %s: %w", kind, err)
		}
		e := productChange(r.seed, w, list)
		method, path, body = http.MethodPatch, r.editProduct(w.Project, e.Slug), productBody(e)
	case kind == WriteShip:
		id, ok := r.pool.ship(w)
		if !ok {
			return fmt.Errorf("write %s of visit %d after view %d: no order was made for it before the clock", kind, w.Seq, w.After)
		}
		method, path, body = http.MethodPost, r.write(w.Project, id), mustJSON(AdminBody(r.seed, w.Seq, w.After, kind, w.Project))
	case kind != WriteCreate:
		panic("no write of the staff of the kind " + kind)
	}
	if kind == WriteCreate {
		method, path, body = http.MethodPost, r.create(w.Project), mustJSON(AdminBody(r.seed, w.Seq, w.After, kind, w.Project))
	}
	ans, _, end, err := r.request(ctx, v, method, path, body)
	// The status counts once it arrives, even when the body that follows cannot be read.
	a.refuse(ans.status)
	if err != nil {
		return err
	}
	if ans.status != want {
		return fmt.Errorf("write %s %s: status %d, want %d: %.200s", method, path, ans.status, want, ans.body)
	}
	if kind == WriteCreate {
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(ans.body, &created); err != nil || created.ID == "" {
			return fmt.Errorf("write %s %s: no order id in the answer: %v %.200s", method, path, err, ans.body)
		}
		r.pool.countCreated(w.Project)
	}
	// A checkout places a studio order of the back office, which no ship takes.
	if kind == WriteCheckout {
		if !placedLocationOf.MatchString(ans.location) {
			return fmt.Errorf("write %s %s: the checkout sent the visitor to %q, not to the page of an order", method, path, ans.location)
		}
		r.pool.countCreated(storefrontShop)
	}
	took := ms(end.Sub(scheduled))
	a.mu.Lock()
	a.requests++
	a.bytes += int64(ans.wire)
	if read.status != 0 {
		a.requests++
		a.bytes += int64(read.wire)
	}
	a.wrote++
	a.responses["write "+kind+" "+strconv.Itoa(ans.status)+" "+ans.kind]++
	a.kinds[kind]++
	a.lat["write"] = append(a.lat["write"], took)
	a.lat["write "+kind] = append(a.lat["write "+kind], took)
	a.mu.Unlock()
	return nil
}

// productChange is what the edit w of a staff visit makes of a product of the list of its shop, as
// staff do on the edit page of a product: one product, drawn from the seed, the visit and the view,
// with every field as listed but one change, half the time a price one yuan higher and half the
// time a delivery of 5 to 50 pieces on top of the stock it has. The stock never goes down, so the
// checkouts and orders after it still find what the restock gave them, and no edit gives a product
// a price it had before.
func productChange(seed uint64, w PlannedWrite, list []shopEntry) shopEntry {
	rng := rand.New(rand.NewPCG(seed^0x2545f4914f6cdd1d, uint64(w.Seq)<<20|uint64(w.After)))
	e := list[rng.IntN(len(list))]
	delivery := 5 + rng.IntN(46)
	if (rng.IntN(2) == 0 || e.Stock >= maxStock) && e.PriceCents < maxPrice {
		e.PriceCents = min(e.PriceCents+100, maxPrice)
	} else {
		e.Stock = min(e.Stock+delivery, maxStock)
	}
	return e
}
