package compare

import (
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeShop is the part of the shop that the office writes touch, in memory: the orders of the data
// that matter (SH-1039 and SH-1040 shipped, SH-1041, SH-1042 and WK-0201 waiting), the four writes
// with the status codes of the shop, and pages that show what was written.
type fakeShop struct {
	mu     sync.Mutex
	orders map[string]*fakeOrder
	next   map[string]int
	// stale makes the pages ignore shipments, as a cache that is not cleared would.
	stale bool
	log   []string
}

type fakeOrder struct {
	project, customer, address, note, tracking string
	notes                                      []string
}

func newFakeShop(t *testing.T) (*fakeShop, *httptest.Server) {
	f := &fakeShop{orders: map[string]*fakeOrder{
		"SH-1039": {project: "studio", customer: "许知", tracking: "JD1"},
		"SH-1040": {project: "studio", customer: "周宁", tracking: "SF1"},
		"SH-1041": {project: "studio", customer: "陈默"},
		"SH-1042": {project: "studio", customer: "林夏"},
		"WK-0201": {project: "weekend", customer: "苏禾"},
	}, next: map[string]int{"studio": 1043, "weekend": 202}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /compare-write/{project}", f.create)
	mux.HandleFunc("POST /compare-write/{project}/{order}", f.write)
	mux.HandleFunc("PUT /compare-write/{project}/{order}", f.write)
	mux.HandleFunc("PATCH /compare-write/{project}/{order}", f.write)
	mux.HandleFunc("GET /projects/{project}", f.list)
	mux.HandleFunc("GET /projects/{project}/orders/{order}", f.page)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeShop) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Customer string `json:"customer"`
		Note     string `json:"note"`
		Items    []struct {
			Slug     string `json:"slug"`
			Quantity int    `json:"quantity"`
		} `json:"items"`
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	project := r.PathValue("project")
	if err := json.UnmarshalRead(r.Body, &in); err != nil || len(in.Items) < 1 || len(in.Items) > 3 || f.next[project] == 0 {
		http.Error(w, "refused", 422)
		return
	}
	id := fmt.Sprintf("%s-%04d", map[string]string{"studio": "SH", "weekend": "WK"}[project], f.next[project])
	f.next[project]++
	f.orders[id] = &fakeOrder{project: project, customer: in.Customer, note: in.Note}
	f.log = append(f.log, "create "+id)
	json.MarshalWrite(w, map[string]string{"id": id, "customer": in.Customer})
}

func (f *fakeShop) write(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := r.PathValue("order")
	o := f.orders[id]
	if o == nil || o.project != r.PathValue("project") {
		http.Error(w, "not found", 404)
		return
	}
	var in map[string]string
	if err := json.UnmarshalRead(r.Body, &in); err != nil {
		http.Error(w, "bad", 400)
		return
	}
	switch r.Method {
	case "POST":
		if o.tracking != "" {
			http.Error(w, "shipped", 409)
			return
		}
		o.tracking = in["trackingNo"]
	case "PATCH":
		if o.tracking != "" {
			http.Error(w, "shipped", 409)
			return
		}
		if in["customer"] != o.customer {
			http.Error(w, "the name changed", 422)
			return
		}
		o.address = in["address"]
	case "PUT":
		o.notes = append(o.notes, in["text"])
	}
	f.log = append(f.log, r.Method+" "+id)
	json.MarshalWrite(w, map[string]string{"id": id})
}

func (f *fakeShop) shipped() int {
	n := 0
	for _, o := range f.orders {
		if o.tracking != "" {
			n++
		}
	}
	return n
}

func (f *fakeShop) list(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, o := range f.orders {
		if o.project == r.PathValue("project") && !(f.stale && o.tracking != "" && id[3] > '1') {
			fmt.Fprintf(w, "%s %s;", id, o.tracking)
		}
	}
}

func (f *fakeShop) page(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.orders[r.PathValue("order")]
	if o == nil {
		http.Error(w, "not found", 404)
		return
	}
	shipped := f.shipped()
	if f.stale {
		shipped = 0
	}
	fmt.Fprintf(w, "%s %s %s %s %s %s carriers %d", r.PathValue("order"), o.customer, o.address, o.note, strings.Join(o.notes, ","), o.tracking, shipped)
}

func officeReplayer(t *testing.T) (*replayer, *fakeShop) {
	f, srv := newFakeShop(t)
	return newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4, WriteRate: 1, Mix: MixOffice}), f
}

func TestOfficeWritesShipTheOldestPickTheRestAndFallBackToACreate(t *testing.T) {
	r, f := officeReplayer(t)
	v := &visitState{client: &http.Client{}}
	n := 0
	send := func(kind, project string) {
		t.Helper()
		n++
		if err := r.doWrite(context.Background(), v, PlannedWrite{After: n, Plan: 1, Kind: kind, Project: project}); err != nil {
			t.Fatalf("%s %s: %v", kind, project, err)
		}
	}
	send(WriteShip, "studio")
	send(WriteShip, "studio")
	send(WriteShip, "studio") // the pool of studio is empty now
	send(WriteNote, "studio") // goes to the order that the last write placed
	send(WriteRecipient, "studio")
	send(WriteNote, "studio")
	send(WriteShip, "studio") // takes the order placed, which is the only one left
	send(WriteShip, "weekend")
	send(WriteRecipient, "weekend") // the pool of weekend is empty: a create
	send(WriteCreate, "weekend")
	want := []string{"POST SH-1041", "POST SH-1042", "create SH-1043", "PUT SH-1043", "PATCH SH-1043", "PUT SH-1043", "POST SH-1043", "POST WK-0201", "create WK-0202", "create WK-0203"}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("the shop saw %v, want %v", f.log, want)
	}
	if r.res.Writes[WriteCreate] != 3 || r.res.Writes[WriteShip] != 4 || r.res.Writes[WriteNote] != 2 || r.res.Writes[WriteRecipient] != 1 || r.res.PoolEmpty != 2 {
		t.Errorf("writes %v, %d with an empty pool", r.res.Writes, r.res.PoolEmpty)
	}
	if got := r.pool.orderCounts(); got["studio"] != 43 || got["weekend"] != 14 {
		t.Errorf("orders %v", got)
	}
	if len(r.res.lat["write"]) != 10 || len(r.res.lat["write create"]) != 3 || r.res.Responses["write ship 200 application/json"] != 4 {
		t.Errorf("latencies %v, responses %v", r.res.lat, r.res.Responses)
	}
}

// Writes of a visit run while other users write: a recipient write must not meet an order that a
// ship write took at the same moment.
func TestOfficeWritesOfManyUsersNeverMeetAShippedOrder(t *testing.T) {
	r, _ := officeReplayer(t)
	kinds := []string{WriteCreate, WriteShip, WriteNote, WriteRecipient, WriteCreate, WriteRecipient, WriteShip}
	var wg sync.WaitGroup
	errs := make(chan error, 400)
	for u := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := &visitState{client: &http.Client{}}
			for i := range 50 {
				w := PlannedWrite{After: i, Plan: u, Kind: kinds[(u+i)%len(kinds)], Project: []string{"studio", "weekend"}[(u+i/3)%2]}
				if err := r.doWrite(context.Background(), v, w); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestVerifyOfficeChecksEachWriteShowsAndFailsOnAStalePage(t *testing.T) {
	r, f := officeReplayer(t)
	if err := r.verifyOffice(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The pool is what it was: the order that the check placed was shipped by it.
	if got := r.pool.orderCounts(); got["studio"] != 43 {
		t.Errorf("orders %v", got)
	}
	if o := f.orders["SH-1043"]; o == nil || o.tracking == "" || !strings.Contains(o.note, "核对订单") {
		t.Errorf("the order placed by the check: %+v", o)
	}
	if f.orders["SH-1041"].tracking != "" || f.orders["SH-1041"].address == "" || len(f.orders["SH-1040"].notes) != 1 {
		t.Errorf("the check left the data of SH-1040 and SH-1041 as %+v and %+v", f.orders["SH-1040"], f.orders["SH-1041"])
	}
	f.stale = true
	if err := r.verifyOffice(context.Background()); err == nil {
		t.Error("a server whose pages ignore shipments passed")
	}
}

// The replayer asks for compression, as a browser does, and the id of a placed order is in the answer.
func TestOfficeWritesReadACompressedAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		zw.Write([]byte(`{"id":"SH-1043","customer":"林夏"}`))
		zw.Close()
	}))
	defer srv.Close()
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4, WriteRate: 1, Mix: MixOffice})
	v := &visitState{client: &http.Client{}}
	if err := r.doWrite(context.Background(), v, PlannedWrite{Kind: WriteCreate, Project: "studio"}); err != nil {
		t.Fatal(err)
	}
	if got := r.pool.orderCounts()["studio"]; got != 43 {
		t.Errorf("studio holds %d orders", got)
	}
}
