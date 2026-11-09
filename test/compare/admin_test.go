package compare

import (
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	// edits are the edits of products the fake took, each the slug and the body.
	edits []string
	// createRenders makes the answer of a create say that it rendered a page.
	createRenders bool
	// refuseShips makes every ship a 409, as for an order that has shipped.
	refuseShips bool
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
	for project, slugs := range fakeCatalogue() {
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
	if f.createRenders {
		w.Header().Set(RenderHeader, RenderVM)
	}
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
		if o.tracking != "" || f.refuseShips {
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
	for _, slug := range fakeCatalogue()[project] {
		list = append(list, *f.products[project][slug])
	}
	f.log = append(f.log, "list "+project)
	w.Header().Set("Content-Type", "application/json")
	json.MarshalWrite(w, map[string]any{"routes": map[string]any{productsRoute: map[string]any{"status": 200, "data": list}}})
}

// fakeCatalogue is the slugs of the products of each shop of the fake: the studio shop sells the
// catalogue of the storefront, the weekend shop its own.
func fakeCatalogue() map[string][]string {
	var studio []string
	for _, v := range StoreVariants {
		studio = append(studio, v.Slug)
	}
	return map[string][]string{"studio": studio, "weekend": shopProducts["weekend"]}
}

// fakeChange is an edit of a product as the PATCH of the shop takes it: a field that is missing keeps
// its value.
type fakeChange struct {
	PriceCents    *int    `json:"priceCents"`
	Stock         *int    `json:"stock"`
	NameEN        *string `json:"nameEn"`
	NameZH        *string `json:"nameZh"`
	DescriptionEN *string `json:"descriptionEn"`
	DescriptionZH *string `json:"descriptionZh"`
}

// editProduct takes an edit of a product as the shop does: the fields it has change and the others
// keep their value. It refuses with 422 an edit with no field, or a price or a stock the shop does
// not take, and keeps each edit it took in edits.
func (f *fakeShop) editProduct(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project := r.PathValue("project")
	p := f.products[project][r.PathValue("product")]
	var in fakeChange
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &in, json.RejectUnknownMembers(true)); err != nil || p == nil {
		http.Error(w, "bad", 400)
		return
	}
	if in == (fakeChange{}) || in.PriceCents != nil && (*in.PriceCents < 100 || *in.PriceCents > maxPrice) || in.Stock != nil && (*in.Stock < 0 || *in.Stock > maxStock) {
		http.Error(w, "refused", 422)
		return
	}
	set := func(dst *int, src *int) {
		if src != nil {
			*dst = *src
		}
	}
	text := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&p.PriceCents, in.PriceCents)
	set(&p.Stock, in.Stock)
	text(&p.NameEN, in.NameEN)
	text(&p.NameZH, in.NameZH)
	text(&p.DescriptionEN, in.DescriptionEN)
	text(&p.DescriptionZH, in.DescriptionZH)
	f.log = append(f.log, "edit "+project)
	f.edits = append(f.edits, p.Slug+" "+string(body))
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

// An edit of a product of the storefront needs no order, and a create places one.
func TestStaffWritesEditAProductAndPlaceOrders(t *testing.T) {
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
	send(WriteCreate, "studio")
	send(WriteEdit, "studio")
	send(WriteEdit, "studio")
	send(WriteEdit, "studio")
	send(WriteCreate, "weekend")
	want := []string{"create SH-1043", "edit studio", "edit studio", "edit studio", "create WK-0202"}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("the shop saw %v, want %v", f.log, want)
	}
	if a.kinds[WriteCreate] != 2 || a.kinds[WriteEdit] != 3 {
		t.Errorf("writes %v", a.kinds)
	}
	// An edit changes a price or the stock, never a text.
	for project, products := range f.products {
		for slug, p := range products {
			if p.NameEN != "N "+slug || p.NameZH != "名"+slug || p.DescriptionEN != "D." || p.DescriptionZH != "述。" {
				t.Errorf("an edit changed the texts of %s/%s: %+v", project, slug, *p)
			}
		}
	}
	if got := r.pool.orderCounts(); got["studio"] != 43 || got["weekend"] != 13 {
		t.Errorf("orders %v", got)
	}
	// An edit is one write of one request.
	if len(a.lat["write"]) != 5 || len(a.lat["write create"]) != 2 || len(a.lat["write edit"]) != 3 || a.requests != 5 {
		t.Errorf("latencies %v, responses %v, %d requests", a.lat, a.responses, a.requests)
	}
}

// Writes of a visit run while other users write: a ship never meets an order that another ship took
// at the same moment, and edits of one shop run at once.
func TestStaffWritesOfManyUsersNeverMeetAShippedOrder(t *testing.T) {
	r, _ := adminRunner(t)
	kinds := []string{WriteCreate, WriteShip, WriteEdit, WriteEdit, WriteCreate, WriteEdit, WriteShip}
	r.wl.Writes = map[[2]int]PlannedWrite{}
	for u := range 8 {
		r.wl.Plans = append(r.wl.Plans, Plan{ID: u, Steps: make([]PlanStep, 50)})
		for i := range 50 {
			kind, project := kinds[(u+i)%len(kinds)], []string{"studio", "weekend"}[(u+i/3)%2]
			if kind == WriteEdit {
				project = storefrontShop
			}
			r.wl.Writes[[2]int{u, i}] = PlannedWrite{After: i, Kind: kind, Project: project}
		}
	}
	round := &Round{}
	if !r.setupOrders(context.Background(), round, 0, 8) {
		t.Fatal(round.SetupError)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 400)
	for u := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := &visitState{client: &http.Client{}}
			a := newAcc()
			for i := range 50 {
				if err := r.doWrite(context.Background(), v, a, r.wl.writesOf(u)[i], time.Now()); err != nil {
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
	// The check placed one order of studio (SH-1043, counted with the 42 of the data) and shipped it.
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

// shipPlan gives the workload of r one plan of five page views that ships after views 1 and 3, so
// that every visit has two ships.
func shipPlan(r *runner) {
	r.wl.Plans = []Plan{{ID: 0, Steps: make([]PlanStep, 5)}}
	r.wl.Writes = map[[2]int]PlannedWrite{
		{0, 1}: {After: 1, Kind: WriteShip, Project: "studio"},
		{0, 3}: {After: 3, Kind: WriteShip, Project: "studio"},
	}
}

// Before the clock the runner places one order for each ship the round plans, in the order of the
// plan, and the ship of the plan takes its own order, whenever its answer comes: what a framework
// ships never depends on how fast it answers. A checkout places an order of the back office, which
// no ship takes, and no ship is ever sent as a create.
func TestShipsTakeTheOrdersMadeBeforeTheClock(t *testing.T) {
	r, f := adminRunner(t)
	shipPlan(r)
	round := &Round{}
	if !r.setupOrders(context.Background(), round, 0, 3) {
		t.Fatal(round.SetupError)
	}
	want := []string{"create SH-1043", "create SH-1044", "create SH-1045", "create SH-1046", "create SH-1047", "create SH-1048"}
	if strings.Join(f.log, "|") != strings.Join(want, "|") || round.SetupRequests != 6 {
		t.Fatalf("the shop saw %v in %d requests, want %v", f.log, round.SetupRequests, want)
	}
	f.log = nil
	v := &visitState{client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	a := newAcc()
	checkout := PlannedWrite{Seq: 2, After: 0, Kind: WriteCheckout, Store: &StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/zh-CN/store/checkout", Body: []byte(`{}`), Status: 303}}
	if err := r.doWrite(context.Background(), v, a, checkout, time.Now()); err != nil {
		t.Fatal(err)
	}
	// The ships answer in the reverse order of the plan.
	for seq := 2; seq >= 0; seq-- {
		for _, after := range []int{3, 1} {
			if err := r.doWrite(context.Background(), v, a, PlannedWrite{Seq: seq, After: after, Kind: WriteShip, Project: "studio"}, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	want = []string{"checkout SH-1049", "POST SH-1048", "POST SH-1047", "POST SH-1046", "POST SH-1045", "POST SH-1044", "POST SH-1043"}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("the shop saw %v, want %v", f.log, want)
	}
	if a.kinds[WriteShip] != 6 || a.kinds[WriteCreate] != 0 {
		t.Errorf("writes %v", a.kinds)
	}
	if got := r.pool.orderCounts(); got["studio"] != 49 {
		t.Errorf("orders %v, want 49 in studio: the 42 of the data, the 6 for the ships and the checkout", got)
	}
	// A ship the setup made no order for is a fault of the runner, not of the server.
	if err := r.doWrite(context.Background(), v, a, PlannedWrite{Seq: 3, After: 1, Kind: WriteShip, Project: "studio"}, time.Now()); err == nil || !strings.Contains(err.Error(), "no order") {
		t.Errorf("a ship with no order made for it: %v", err)
	}
	if len(f.log) != len(want) {
		t.Errorf("the ship with no order sent %v", f.log[len(want):])
	}
}

// Placing the orders of the ships must put nothing into the render memo: an answer that rendered a
// page, or that is not a 200, fails the setup, and the round is not played.
func TestTheOrdersOfTheShipsRenderNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(f *fakeShop)
		want string
	}{
		{"a render", func(f *fakeShop) { f.createRenders = true }, "rendered a page"},
		{"a refusal", func(f *fakeShop) { f.next["studio"] = 0 }, "status 422"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, f := adminRunner(t)
			shipPlan(r)
			tc.set(f)
			round := &Round{}
			if r.setupOrders(context.Background(), round, 0, 1) || !strings.Contains(round.SetupError, tc.want) {
				t.Errorf("setup error %q, want %q", round.SetupError, tc.want)
			}
		})
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
	for _, slug := range fakeCatalogue()[r.PathValue("project")] {
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

// The sizing run plays at most the visits it was given, so the setup places an order for each of
// their ships before it, and every ship of the run ships the order made for it.
func TestTheSizingRunShipsTheOrdersMadeBeforeIt(t *testing.T) {
	r, f := adminRunner(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}}}}
	r.wl = newTestRunner(r.base, BrowseScenario{Name: "x"}, tr).wl
	r.wl.Writes[[2]int{0, 0}] = PlannedWrite{After: 0, Kind: WriteShip, Project: "studio"}
	rate, out := r.Size(context.Background(), 2, 5*time.Second, 6)
	if rate <= 0 || out.SetupError != "" || out.Failures != 0 {
		t.Fatalf("rate %.1f, setup %q, %d failures", rate, out.SetupError, out.Failures)
	}
	creates, ships := 0, 0
	for _, l := range f.log {
		switch {
		case strings.HasPrefix(l, "create "):
			creates++
		case strings.HasPrefix(l, "POST "):
			ships++
		}
	}
	if creates != 6 || ships != 6 || !strings.HasPrefix(strings.Join(f.log, "|"), "create SH-1043|create SH-1044|create SH-1045|create SH-1046|create SH-1047|create SH-1048|POST ") {
		t.Errorf("the shop saw %v, want the 6 orders placed first, then their 6 ships", f.log)
	}
}

// An edit sends one field, set by the seed, the visit and its page view alone, so that what a
// framework is sent never depends on what it answered before: half the time a price, half the time
// a stock at or above what the restock gave the product (its stock in the data, or more for the
// round). It reads nothing first, and the same plan sends the same edits to every server.
func TestAnEditSendsOneFieldThePlanSets(t *testing.T) {
	edits := func() ([]string, *fakeShop, *acc) {
		r, f := adminRunner(t)
		r.restocked = map[string]int{"oak-tray": 500}
		v := &visitState{client: &http.Client{}}
		a := newAcc()
		for seq := range 60 {
			if err := r.doWrite(context.Background(), v, a, PlannedWrite{Seq: seq, After: 2, Kind: WriteEdit, Project: "studio"}, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		return f.edits, f, a
	}
	got, f, a := edits()
	if len(f.log) != 60 || slices.ContainsFunc(f.log, func(l string) bool { return l != "edit studio" }) || a.requests != 60 {
		t.Fatalf("the shop saw %v in %d requests, want 60 edits and nothing else", f.log, a.requests)
	}
	prices, stocks := 0, 0
	for _, e := range got {
		slug, body, _ := strings.Cut(e, " ")
		var m map[string]int
		if err := json.Unmarshal([]byte(body), &m); err != nil || len(m) != 1 {
			t.Fatalf("an edit of %s sent %s, want one field", slug, body)
		}
		v := StoreVariants[slices.IndexFunc(StoreVariants, func(v StoreProduct) bool { return v.Slug == slug })]
		target := v.Stock
		if slug == "oak-tray" {
			target = 500
		}
		if p, ok := m["priceCents"]; ok {
			prices++
			if p < EditPriceFen[0] || p > EditPriceFen[1] || p%100 != 0 {
				t.Errorf("an edit of %s sent the price %d", slug, p)
			}
		} else if s, ok := m["stock"]; ok {
			stocks++
			if s < target+EditDelivery[0] || s > min(target+EditDelivery[1], maxStock) {
				t.Errorf("an edit of %s sent the stock %d, its restock gave it %d", slug, s, target)
			}
		} else {
			t.Errorf("an edit of %s sent %s", slug, body)
		}
	}
	if prices < 15 || stocks < 15 {
		t.Errorf("%d price edits and %d stock edits of 60", prices, stocks)
	}
	if again, _, _ := edits(); !slices.Equal(again, got) {
		t.Errorf("the same plan sent other edits:\n%v\n%v", got, again)
	}
}

// A sizing run plays for a time and cannot know what its visits buy, so its restock gives every
// product of the storefront the most stock the shop takes, those its plans never buy too, and the
// edits of its stock stay at that cap.
func TestTheSizingRunRestocksEveryProductToTheCap(t *testing.T) {
	r, f := adminRunner(t)
	r.wl.Plans = []Plan{{ID: 0, Steps: make([]PlanStep, 2)}}
	b := &browse{o: BrowseOptions{Warm: 10 * time.Millisecond}}
	spec := Spec{Products: nodeProducts, EditProduct: nodeEditProduct}
	if err := b.prepare(context.Background(), r, spec, -1, sizingVisits(b.o.Warm)); err != nil {
		t.Fatal(err)
	}
	for _, v := range StoreVariants {
		if got := f.products["studio"][v.Slug].Stock; got != maxStock {
			t.Errorf("%s has %d in stock after the restock of a sizing run, want %d", v.Slug, got, maxStock)
		}
		if got := r.stockTarget(v.Slug); got != maxStock {
			t.Errorf("an edit of the stock of %s sets more than %d, not the cap", v.Slug, got)
		}
	}
}

// A request the shop refuses with 409 or 422 in a sizing run is an error: the restock to the cap
// and the plan rule them out, so a run that met one did not play the mix, and its rate must not
// size a round. A setup that failed is an error too.
func TestASizingRunThatWasRefusedSizesNothing(t *testing.T) {
	for _, tc := range []struct {
		sized *Round
		want  string
	}{
		{&Round{}, ""},
		{&Round{Failures: 3}, ""}, // a 503 ends a visit fast, and counts in the rate
		{&Round{Refused: 1, Failures: 1}, "refused 1 of its requests"},
		{&Round{SetupError: "POST /x: status 500"}, "the setup"},
	} {
		if got := sizingFault(tc.sized); tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("%d refused, %d failures, setup %q: %q, want %q", tc.sized.Refused, tc.sized.Failures, tc.sized.SetupError, got, tc.want)
		}
	}
	// The run counts what the shop refused.
	r, f := adminRunner(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}}}}
	r.wl = newTestRunner(r.base, BrowseScenario{Name: "x"}, tr).wl
	r.wl.Writes[[2]int{0, 0}] = PlannedWrite{After: 0, Kind: WriteShip, Project: "studio"}
	f.refuseShips = true
	_, sized := r.Size(context.Background(), 1, 5*time.Second, 1)
	if sized.SetupError != "" || sized.Refused == 0 {
		t.Errorf("a ship refused with 409: setup %q, %d refused", sized.SetupError, sized.Refused)
	}
}
