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

// The writes of the office mix go to orders that exist on the server at that moment, which only the
// server knows: shipping uses an order up, and a new order exists once its create is answered. So
// each replayer keeps a pool of the orders that have not shipped, per project. It starts with the
// orders of the data that wait to ship and lives as long as the server does.
//
//   - create adds the id that the server answered with,
//   - ship takes the oldest order of the pool out and ships it,
//   - note and recipient pick one order of the pool at random and leave it there.
//
// A write that finds no order to go to is sent as a create instead, and counted.
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
	body   []byte // as sent, with the compression removed
	wire   int    // bytes on the wire
}

// request sends a JSON write and reads the whole answer.
func (r *replayer) request(ctx context.Context, v *visitState, method, path string, body []byte) (answered, time.Time, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, strings.NewReader(string(body)))
	if err != nil {
		return answered{}, time.Time{}, time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	start := time.Now()
	defer r.track()()
	res, err := v.client.Do(req)
	if err != nil {
		return answered{}, start, time.Time{}, err
	}
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	end := time.Now()
	a := answered{status: res.StatusCode, body: b, wire: len(b)}
	if err == nil && res.Header.Get("Content-Encoding") == "gzip" {
		var zr *gzip.Reader
		if zr, err = gzip.NewReader(bytes.NewReader(b)); err == nil {
			a.body, err = io.ReadAll(zr)
		}
	}
	return a, start, end, err
}

// doWrite sends a write of the plan and times it. In the rename mix it renames a customer of one of
// the three orders of the data. In the office mix it goes to an order of the pool, or is a create
// when the pool has none.
func (r *replayer) doWrite(ctx context.Context, v *visitState, w PlannedWrite) error {
	kind, empty := w.Kind, false
	var method, path string
	var body []byte
	var entry *poolOrder
	switch {
	case r.sc.mix() == MixRename:
		st := WriteStep(r.write, w.Order, int(r.writes.Add(1)), true)
		method, path, body = st.Method, st.Path, st.Body
	default:
		switch kind {
		case WriteShip:
			entry = r.pool.take(r.seed, w.Project)
		case WriteNote, WriteRecipient:
			entry = r.pool.borrow(r.seed, w.Project)
			if entry != nil {
				defer r.pool.release(entry)
			}
		}
		if entry == nil && kind != WriteCreate {
			kind, empty = WriteCreate, true
		}
		payload := OfficeBody(r.seed, w.Plan, w.After, kind, w.Project)
		switch kind {
		case WriteCreate:
			method, path = http.MethodPost, r.create(w.Project)
		case WriteShip:
			method, path = http.MethodPost, r.write(w.Project, entry.ID)
		case WriteNote:
			method, path = http.MethodPut, r.write(w.Project, entry.ID)
		case WriteRecipient:
			method, path = http.MethodPatch, r.write(w.Project, entry.ID)
			payload.(map[string]any)["customer"] = entry.Customer
		}
		body = mustJSON(payload)
	}
	a, start, end, err := r.request(ctx, v, method, path, body)
	if err != nil {
		return err
	}
	if a.status != 200 {
		return fmt.Errorf("write %s %s: status %d: %.200s", method, path, a.status, a.body)
	}
	if kind == WriteCreate {
		var created struct {
			ID       string `json:"id"`
			Customer string `json:"customer"`
		}
		if err := json.Unmarshal(a.body, &created); err != nil || created.ID == "" {
			return fmt.Errorf("write %s %s: no order id in the answer: %v %.200s", method, path, err, a.body)
		}
		r.pool.add(r.seed, w.Project, created.ID, created.Customer)
	}
	if r.inWindow(end) {
		r.mu.Lock()
		r.res.Requests++
		r.res.Bytes += int64(a.wire)
		r.res.Responses["write "+kind+" "+strconv.Itoa(a.status)+" application/json"]++
		r.res.Writes[kind]++
		if empty {
			r.res.PoolEmpty++
		}
		r.mu.Unlock()
		ms := float64(end.Sub(start)) / float64(time.Millisecond)
		r.addLatency("write", end, ms)
		r.addLatency("write "+kind, end, ms)
	}
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
