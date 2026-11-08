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

// The writes of the staff go to orders that exist on the server at that moment, which only the
// server knows: shipping uses an order up, and a new order exists once its create is answered. So
// each runner keeps a pool of the orders that have not shipped, per project. It starts with the
// orders of the data that wait to ship and lives as long as the server does.
//
//   - create adds the id that the server answered with,
//   - ship takes the oldest order of the pool out and ships it,
//   - an edit (productEdit, for now) picks one order of the pool at random and leaves it there.
//
// A write that finds no order to go to is sent as a create instead, and counted.
//
// TODO(shop-merge): the orders that shoppers place at checkout do not reach the pool, so ships drain
// it and the creates refill it. Once the back office lists the orders of the storefront, add each
// checkout's order to the pool instead.
type pool struct {
	mu        sync.Mutex
	unshipped map[string][]*poolOrder // oldest first
	created   map[string]int          // orders placed since the server started
	rng       *rand.Rand
}

type poolOrder struct {
	ID       string
	Customer string
	busy     int // recipient writes in flight: a ship leaves the order alone until they are done
}

// start fills the pool on first use. The caller holds mu.
func (p *pool) start(seed uint64) {
	if p.unshipped != nil {
		return
	}
	p.unshipped, p.created = map[string][]*poolOrder{}, map[string]int{}
	p.rng = rand.New(rand.NewPCG(seed^0x5851f42d4c957f2d, 1))
	// WritableOrders are newest first, the pool is oldest first.
	for i := len(WritableOrders) - 1; i >= 0; i-- {
		o := WritableOrders[i]
		p.unshipped[o.Project] = append(p.unshipped[o.Project], &poolOrder{ID: o.Order, Customer: o.Customer})
	}
}

// take removes the oldest order of a project that no recipient write is working on, or returns nil.
func (p *pool) take(seed uint64, project string) *poolOrder {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start(seed)
	for i, o := range p.unshipped[project] {
		if o.busy == 0 {
			p.unshipped[project] = append(p.unshipped[project][:i:i], p.unshipped[project][i+1:]...)
			return o
		}
	}
	return nil
}

// borrow picks an order of a project at random and keeps it in the pool. release gives it back.
func (p *pool) borrow(seed uint64, project string) *poolOrder {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start(seed)
	list := p.unshipped[project]
	if len(list) == 0 {
		return nil
	}
	o := list[p.rng.IntN(len(list))]
	o.busy++
	return o
}

func (p *pool) release(o *poolOrder) {
	p.mu.Lock()
	o.busy--
	p.mu.Unlock()
}

// add puts an order that the server answered for into the pool.
func (p *pool) add(seed uint64, project, id, customer string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start(seed)
	p.unshipped[project] = append(p.unshipped[project], &poolOrder{ID: id, Customer: customer})
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
// must be the one the plan says (a redirect is not followed). A write of the staff goes to an order of
// the pool (productEdit, ship), or is a create when the pool has none.
func (r *runner) doWrite(ctx context.Context, v *visitState, a *acc, w PlannedWrite, scheduled time.Time) error {
	kind, empty := w.Kind, false
	var method, path string
	var body []byte
	want := 200 // the status the shop answers with
	switch {
	case w.Store != nil:
		st := w.Store
		method, path, body, want = st.Method, r.storeWrite(st.Path), st.Body, st.Status
	case kind == WriteEdit:
		m, p, b, done, ok := r.productEdit(w)
		if !ok {
			kind, empty = WriteCreate, true
			break
		}
		defer done()
		method, path, body = m, p, b
	case kind == WriteShip:
		entry := r.pool.take(r.seed, w.Project)
		if entry == nil {
			kind, empty = WriteCreate, true
			break
		}
		method, path, body = http.MethodPost, r.write(w.Project, entry.ID), mustJSON(AdminBody(r.seed, w.Seq, w.After, kind, w.Project))
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
			ID       string `json:"id"`
			Customer string `json:"customer"`
		}
		if err := json.Unmarshal(ans.body, &created); err != nil || created.ID == "" {
			return fmt.Errorf("write %s %s: no order id in the answer: %v %.200s", method, path, err, ans.body)
		}
		r.pool.add(r.seed, w.Project, created.ID, created.Customer)
	}
	took := ms(end.Sub(scheduled))
	a.mu.Lock()
	a.requests++
	a.bytes += int64(ans.wire)
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
func (p *pool) countCreated(seed uint64, project string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start(seed)
	p.created[project]++
}

// holds says whether the pool still has the order of a project.
func (p *pool) holds(seed uint64, project, id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.start(seed)
	for _, o := range p.unshipped[project] {
		if o.ID == id {
			return true
		}
	}
	return false
}

// productEdit is the request of a staff visit that edits a product, and done gives back what it took.
// ok is false when there is nothing to send it to.
//
// TODO(shop-merge): the back office has no product edit yet. Until it does, this is the closest write
// it has: it changes the recipient of an order of the pool (the phone and the address, the name is
// the order's own), and ok is false when the pool of the project has no order. Swap the body of this
// function for the product edit once the shop has it.
func (r *runner) productEdit(w PlannedWrite) (method, path string, body []byte, done func(), ok bool) {
	entry := r.pool.borrow(r.seed, w.Project)
	if entry == nil {
		return "", "", nil, nil, false
	}
	payload := AdminBody(r.seed, w.Seq, w.After, WriteEdit, w.Project).(map[string]any)
	payload["customer"] = entry.Customer
	return http.MethodPatch, r.write(w.Project, entry.ID), mustJSON(payload), func() { r.pool.release(entry) }, true
}
