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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The ships of the staff go to orders that exist on the server at that moment, which only the
// server knows: shipping uses an order up, and a new order exists once its create is answered. So
// each runner keeps a pool of the orders that have not shipped, per project. It starts with the
// orders of the data that wait to ship and lives as long as the server does.
//
//   - create adds the id that the server answered with,
//   - ship takes the oldest order of the pool out and ships it.
//
// A ship that finds no order to go to is sent as a create instead, and counted. An edit of a
// product needs no order.
//
// TODO(shop-merge): the orders that shoppers place at checkout do not reach the pool, so ships drain
// it and the creates refill it. Once the back office lists the orders of the storefront, add each
// checkout's order to the pool instead.
type pool struct {
	mu        sync.Mutex
	unshipped map[string][]string // ids, oldest first
	created   map[string]int      // orders placed since the server started
}

// start fills the pool on first use. The caller holds mu.
func (p *pool) start() {
	if p.unshipped != nil {
		return
	}
	p.unshipped, p.created = map[string][]string{}, map[string]int{}
	// WritableOrders are newest first, the pool is oldest first.
	for i := len(WritableOrders) - 1; i >= 0; i-- {
		o := WritableOrders[i]
		p.unshipped[o.Project] = append(p.unshipped[o.Project], o.Order)
	}
}

// take removes the oldest order of a project, or returns "" when it has none.
func (p *pool) take(project string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start()
	if len(p.unshipped[project]) == 0 {
		return ""
	}
	id := p.unshipped[project][0]
	p.unshipped[project] = p.unshipped[project][1:]
	return id
}

// add puts an order that the server answered for into the pool.
func (p *pool) add(project, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start()
	p.unshipped[project] = append(p.unshipped[project], id)
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
	a := answered{status: res.StatusCode, kind: mediaType(res.Header.Get("Content-Type")), body: b, wire: len(b)}
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
// must be the one the plan says (a redirect is not followed), and a checkout adds its order to the pool.
// A ship of the staff takes an order of the pool, or is a create when the pool has none, and an edit
// reads the products of its shop and sends one back changed (productChange).
func (r *runner) doWrite(ctx context.Context, v *visitState, a *acc, w PlannedWrite, scheduled time.Time) error {
	kind, empty := w.Kind, false
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
		id := r.pool.take(w.Project)
		if id == "" {
			kind, empty = WriteCreate, true
			break
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
		r.pool.add(w.Project, created.ID)
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
	if empty {
		a.poolEmpty++
	}
	a.lat["write"] = append(a.lat["write"], took)
	a.lat["write "+kind] = append(a.lat["write "+kind], took)
	a.mu.Unlock()
	return nil
}

// countCreated notes an order that was placed outside the pool, so that the counts of orders stay
// right.
func (p *pool) countCreated(project string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start()
	p.created[project]++
}

// holds says whether the pool still has the order of a project.
func (p *pool) holds(project, id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start()
	return slices.Contains(p.unshipped[project], id)
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
