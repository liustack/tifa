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
	"time"
)

// fakeShop is the part of the shop that the admin writes touch, in memory: the orders of the data
// that matter (SH-1039 and SH-1040 shipped, SH-1041, SH-1042 and WK-0201 waiting), the four writes
// with the status codes of the shop, and pages that show what was written.
type fakeShop struct {
	mu     sync.Mutex
	orders map[string]*fakeOrder
	next   map[string]int
	// stale makes the pages ignore shipments, as a cache that is not cleared would, and staleStats
	// only the stats page.
	stale, staleStats bool
	log               []string
	// products are the products of each shop as the back office lists them, by slug, and
	// staleProducts makes the pages that show them keep the titles of the data.
	products      map[string]map[string]*shopEntry
	staleProducts bool
}

type fakeOrder struct {
	project, customer, address, note, tracking, carrier string
	notes                                               []string
}

func newFakeShop(t *testing.T) (*fakeShop, *httptest.Server) {
	f := &fakeShop{orders: map[string]*fakeOrder{
		"SH-1039": {project: "studio", customer: "许知", tracking: "JD1", carrier: "JD"},
		"SH-1040": {project: "studio", customer: "周宁", tracking: "SF1", carrier: "SF"},
		"SH-1041": {project: "studio", customer: "陈默"},
		"SH-1042": {project: "studio", customer: "林夏"},
		"WK-0201": {project: "weekend", customer: "苏禾"},
	}, next: map[string]int{"studio": 1043, "weekend": 202}, products: map[string]map[string]*shopEntry{}}
	for project, slugs := range shopProducts {
		f.products[project] = map[string]*shopEntry{}
		for i, slug := range slugs {
			f.products[project][slug] = &shopEntry{Slug: slug, PriceCents: 10000 + 100*i, Stock: 5, NameEN: "N " + slug, NameZH: "名" + slug, DescriptionEN: "D.", DescriptionZH: "述。"}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /compare-write/{project}/products", f.productList)
	mux.HandleFunc("PATCH /compare-write/{project}/products/{product}", f.editProduct)
	mux.HandleFunc("POST /zh-CN/store/checkout", f.checkout)
	mux.HandleFunc("POST /compare-write/{project}", f.create)
	mux.HandleFunc("POST /compare-write/{project}/{order}", f.write)
	mux.HandleFunc("PUT /compare-write/{project}/{order}", f.write)
	mux.HandleFunc("PATCH /compare-write/{project}/{order}", f.write)
	mux.HandleFunc("GET /projects/{project}", f.list)
	mux.HandleFunc("GET /projects/{project}/orders/{order}", f.page)
	mux.HandleFunc("GET /projects/{project}/stats", f.stats)
	mux.HandleFunc("GET /store/products/{slug}", f.productPage)
	mux.HandleFunc("GET /projects/{project}/products", f.productPage)
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
	w.Header().Set("Content-Type", "application/json")
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
		o.tracking, o.carrier = in["trackingNo"], in["carrier"]
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
	w.Header().Set("Content-Type", "application/json")
	json.MarshalWrite(w, map[string]string{"id": id})
}

// checkout places a storefront order as a studio order, with the next number of studio, and sends
// the visitor to the page of the order, as the shop does.
func (f *fakeShop) checkout(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("SH-%04d", f.next["studio"])
	f.next["studio"]++
	f.orders[id] = &fakeOrder{project: "studio", customer: "顾客"}
	f.log = append(f.log, "checkout "+id)
	w.Header().Set("Location", "/zh-CN/store/checkout?placed="+id)
	w.WriteHeader(303)
}

// productList lists the products of a shop as the back office does, in the envelope of the data of
// a page.
func (f *fakeShop) productList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project := r.PathValue("project")
	var list []shopEntry
	for _, slug := range shopProducts[project] {
		list = append(list, *f.products[project][slug])
	}
	f.log = append(f.log, "list "+project)
	w.Header().Set("Content-Type", "application/json")
	json.MarshalWrite(w, map[string]any{"routes": map[string]any{productsRoute: map[string]any{"status": 200, "data": list}}})
}

// editProduct takes an edit of a product with all its fields. It refuses with 409 one that changes
// no field or takes stock away, which no edit of a run may do.
func (f *fakeShop) editProduct(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project := r.PathValue("project")
	p := f.products[project][r.PathValue("product")]
	var in shopEntry
	if err := json.UnmarshalRead(r.Body, &in); err != nil || p == nil {
		http.Error(w, "bad", 400)
		return
	}
	in.Slug = p.Slug
	if in == *p || in.Stock < p.Stock {
		http.Error(w, "refused", 409)
		return
	}
	*p = in
	f.log = append(f.log, "edit "+project)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{}`))
}

// stats is the carrier table of the stats page, as React renders it, with the parcels of each
// carrier. A stale one counts only those of the data.
func (f *fakeShop) stats(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range []struct{ code, name string }{{"SF", "SF Express"}, {"JD", "JD Logistics"}, {"ZTO", "ZTO Express"}} {
		n := 0
		for id, o := range f.orders {
			if o.project == r.PathValue("project") && o.carrier == c.code && !((f.stale || f.staleStats) && id > "SH-1042") {
				n++
			}
		}
		fmt.Fprintf(w, `<tr data-slot="table-row"><td class="text-sm">%s</td><td class="text-right">%d</td><td>None</td></tr>`, c.name, n)
	}
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
	fmt.Fprintf(w, "%s %s %s %s %s %s", r.PathValue("order"), o.customer, o.address, o.note, strings.Join(o.notes, ","), o.tracking)
}

func adminRunner(t *testing.T) (*runner, *fakeShop) {
	f, srv := newFakeShop(t)
	return newTestRunner(srv.URL, BrowseScenario{Name: "x"}), f
}

// A ship takes the oldest order of the pool, or places one when the pool is empty. An edit reads the
// products of its shop and sends one back with a new price or more stock, and needs no order.
func TestStaffWritesShipTheOldestEditAProductAndFallBackToACreate(t *testing.T) {
	r, f := adminRunner(t)
	v := &visitState{client: &http.Client{}}
	a := newAcc()
	n := 0
	send := func(kind, project string) {
		t.Helper()
		n++
		if err := r.doWrite(context.Background(), v, a, PlannedWrite{After: n, Seq: 1, Kind: kind, Project: project}, time.Now()); err != nil {
			t.Fatalf("%s %s: %v", kind, project, err)
		}
	}
	send(WriteShip, "studio")
	send(WriteShip, "studio")
	send(WriteShip, "studio") // the pool of studio is empty now: a create
	send(WriteEdit, "studio") // reads the products, then edits one
	send(WriteEdit, "studio")
	send(WriteShip, "studio") // takes the order placed, which is the only one left
	send(WriteShip, "weekend")
	send(WriteEdit, "weekend") // an edit needs no order
	send(WriteShip, "weekend") // the pool of weekend is empty: a create
	send(WriteCreate, "weekend")
	want := []string{"POST SH-1041", "POST SH-1042", "create SH-1043", "list studio", "edit studio", "list studio", "edit studio", "POST SH-1043", "POST WK-0201", "list weekend", "edit weekend", "create WK-0202", "create WK-0203"}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("the shop saw %v, want %v", f.log, want)
	}
	if a.kinds[WriteCreate] != 3 || a.kinds[WriteShip] != 4 || a.kinds[WriteEdit] != 3 || a.poolEmpty != 2 {
		t.Errorf("writes %v, %d with an empty pool", a.kinds, a.poolEmpty)
	}
	// An edit changes a price or the stock, never a text.
	for project, products := range f.products {
		for slug, p := range products {
			if p.NameEN != "N "+slug || p.NameZH != "名"+slug || p.DescriptionEN != "D." || p.DescriptionZH != "述。" {
				t.Errorf("an edit changed the texts of %s/%s: %+v", project, slug, *p)
			}
		}
	}
	if got := r.pool.orderCounts(); got["studio"] != 43 || got["weekend"] != 14 {
		t.Errorf("orders %v", got)
	}
	// An edit is one write of two requests, the list and the edit.
	if len(a.lat["write"]) != 10 || len(a.lat["write create"]) != 3 || len(a.lat["write edit"]) != 3 || a.responses["write ship 200 application/json"] != 4 || a.requests != 13 {
		t.Errorf("latencies %v, responses %v, %d requests", a.lat, a.responses, a.requests)
	}
}

// Writes of a visit run while other users write: a ship never meets an order that another ship took
// at the same moment, and an edit never takes stock away, though edits of one shop run at once.
func TestStaffWritesOfManyUsersNeverMeetAShippedOrder(t *testing.T) {
	r, _ := adminRunner(t)
	kinds := []string{WriteCreate, WriteShip, WriteEdit, WriteEdit, WriteCreate, WriteEdit, WriteShip}
	var wg sync.WaitGroup
	errs := make(chan error, 400)
	for u := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := &visitState{client: &http.Client{}}
			a := newAcc()
			for i := range 50 {
				w := PlannedWrite{After: i, Seq: u, Kind: kinds[(u+i)%len(kinds)], Project: []string{"studio", "weekend"}[(u+i/3)%2]}
				if err := r.doWrite(context.Background(), v, a, w, time.Now()); err != nil {
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

func TestVerifyAdminChecksEachWriteShowsAndFailsOnAStalePage(t *testing.T) {
	r, f := adminRunner(t)
	if err := r.verifyAdmin(context.Background()); err != nil {
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
	f.staleStats = true
	if err := r.verifyAdmin(context.Background()); err == nil || !strings.Contains(err.Error(), "carrier table") {
		t.Errorf("a server whose stats page ignores shipments: %v", err)
	}
	f.stale = true
	if err := r.verifyAdmin(context.Background()); err == nil {
		t.Error("a server whose pages ignore shipments passed")
	}
}

// The replayer asks for compression, as a browser does, and the id of a placed order is in the answer.
func TestAdminWritesReadACompressedAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		zw.Write([]byte(`{"id":"SH-1043","customer":"林夏"}`))
		zw.Close()
	}))
	defer srv.Close()
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"})
	v := &visitState{client: &http.Client{}}
	a := newAcc()
	if err := r.doWrite(context.Background(), v, a, PlannedWrite{Kind: WriteCreate, Project: "studio"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := r.pool.orderCounts()["studio"]; got != 43 {
		t.Errorf("studio holds %d orders", got)
	}
}

// After a round the writes have shipped the orders of the data that waited, and the check must
// not count on them.
func TestVerifyAdminAfterARoundHasShippedTheOrdersOfTheData(t *testing.T) {
	r, f := adminRunner(t)
	v := &visitState{client: &http.Client{}}
	a := newAcc()
	for i := range 2 {
		if err := r.doWrite(context.Background(), v, a, PlannedWrite{After: i, Seq: 1, Kind: WriteShip, Project: "studio"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.verifyAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o := f.orders["SH-1043"]; o == nil || o.address == "" || o.tracking == "" {
		t.Errorf("the order of the check: %+v", o)
	}
}

// The order a checkout of the storefront places is a studio order that waits to ship, so it joins
// the pool: a ship takes it once the older orders are gone, and no create stands in for it.
func TestACheckoutOrderJoinsThePoolAndAShipTakesIt(t *testing.T) {
	r, f := adminRunner(t)
	v := &visitState{client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	a := newAcc()
	checkout := PlannedWrite{Kind: WriteCheckout, Store: &StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/zh-CN/store/checkout", Body: []byte(`{}`), Status: 303}}
	ship := PlannedWrite{Kind: WriteShip, Project: "studio"}
	for _, w := range []PlannedWrite{checkout, ship, ship, ship, ship} {
		w.After++
		if err := r.doWrite(context.Background(), v, a, w, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"checkout SH-1043", "POST SH-1041", "POST SH-1042", "POST SH-1043", "create SH-1044"}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("the shop saw %v, want %v", f.log, want)
	}
	if a.poolEmpty != 1 {
		t.Errorf("%d ships found the pool empty, want 1", a.poolEmpty)
	}
	if got := r.pool.orderCounts(); got["studio"] != 44 {
		t.Errorf("orders %v, want 44 in studio: the 42 of the data, the checkout and the create", got)
	}
}

// productPage is the storefront page of a product and the back office list, as far as an edit
// shows on them: the English title and the price. A stale one shows them as the data had them.
func (f *fakeShop) productPage(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	show := func(p shopEntry) {
		if f.staleProducts {
			p.NameEN = "N " + p.Slug
		}
		fmt.Fprintf(w, "%s %d\n", p.NameEN, p.PriceCents)
	}
	if slug := r.PathValue("slug"); slug != "" {
		p := f.products["studio"][slug]
		if p == nil {
			http.NotFound(w, r)
			return
		}
		show(*p)
		return
	}
	for _, slug := range shopProducts[r.PathValue("project")] {
		show(*f.products[r.PathValue("project")][slug])
	}
}

// An edit of a product shows next on its storefront page and in the list of the back office, and a
// server whose pages keep the old product fails the check.
func TestVerifyProductChecksAnEditShowsAndFailsOnAStalePage(t *testing.T) {
	r, f := adminRunner(t)
	if err := r.verifyProduct(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.log, ","); got != "list studio,edit studio" {
		t.Errorf("the shop saw %s", got)
	}
	f.staleProducts = true
	if err := r.verifyProduct(context.Background()); err == nil || !strings.Contains(err.Error(), "edit a product") {
		t.Errorf("a server whose pages keep the old product passed: %v", err)
	}
}
