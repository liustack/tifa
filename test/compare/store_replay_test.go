package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStoreServer is the part of the storefront that its writes touch, in memory: a cart for each
// cookie, the four writes with the status codes of the shop, and a cart page that shows the cart.
// The writes are at the paths that write maps the paths of the shop to. It refuses a write the shop
// would refuse, so a plan that sends one fails the replay.
type fakeStoreServer struct {
	mu    sync.Mutex
	carts map[string]*shopCart
	next  int
	log   []string
	// stale makes the cart page ignore the cart, as a cache that is not cleared would.
	stale bool
	// answer, when set, is the status that replaces the right one for a kind of write.
	answer map[string]int
	posted []string
	// placed are the orders of the checkouts, which the back office shows unless staleOffice.
	placed      []string
	staleOffice bool
}

func newFakeStore(t *testing.T, write func(string) string) (*fakeStoreServer, *httptest.Server) {
	f := &fakeStoreServer{carts: map[string]*shopCart{}}
	mux := http.NewServeMux()
	for _, base := range []string{"/store", "/zh-CN/store"} {
		mux.HandleFunc("POST "+write(base+"/products/{slug}"), f.write(WriteAdd))
		mux.HandleFunc("PATCH "+write(base+"/cart"), f.write(WriteQuantity))
		mux.HandleFunc("POST "+write(base+"/cart"), f.write(WriteCoupon))
		mux.HandleFunc("POST "+write(base+"/checkout"), f.write(WriteCheckout))
		mux.HandleFunc("POST "+base+"/mine", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.posted = append(f.posted, string(b))
			f.mu.Unlock()
			w.WriteHeader(204)
		})
		mux.HandleFunc("GET "+base+"/cart", f.page)
		mux.HandleFunc("GET "+base+"/checkout", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "order ", r.URL.Query().Get("placed")) })
	}
	office := func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.staleOffice {
			fmt.Fprint(w, strings.Join(f.placed, " "))
		}
	}
	mux.HandleFunc("GET /projects/studio", office)
	mux.HandleFunc("GET /projects/studio/orders/{order}", office)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if !answerOld(w, r) {
			w.Write([]byte("ok"))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

// cartOf finds or makes the cart of the cookie of a request. A visitor with no cart gets the
// demonstration cart in a cart of their own at the first write.
func (f *fakeStoreServer) cartOf(w http.ResponseWriter, r *http.Request, create bool) *shopCart {
	if c, err := r.Cookie("cart"); err == nil && f.carts[c.Value] != nil {
		return f.carts[c.Value]
	}
	if !create {
		return &shopCart{lines: demoCart()}
	}
	f.next++
	id := fmt.Sprint("c", f.next)
	f.carts[id] = &shopCart{lines: demoCart()}
	http.SetCookie(w, &http.Cookie{Name: "cart", Value: id, Path: "/"})
	return f.carts[id]
}

func (f *fakeStoreServer) write(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var in struct {
			Color    string `json:"color"`
			Size     string `json:"size"`
			Quantity int    `json:"quantity"`
			Line     string `json:"line"`
			Code     string `json:"code"`
		}
		if err := json.UnmarshalRead(r.Body, &in); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		c := f.cartOf(w, r, true)
		refuse := func(why string) {
			f.log = append(f.log, "refused "+kind+": "+why)
			http.Error(w, why, 422)
		}
		status := map[string]int{WriteAdd: 200, WriteQuantity: 200, WriteCoupon: 204, WriteCheckout: 303}[kind]
		switch kind {
		case WriteAdd:
			slug := r.PathValue("slug")
			i := slices.IndexFunc(StoreVariants(), func(p StoreProduct) bool { return p.Slug == slug })
			if i < 0 || in.Quantity < 1 || in.Quantity > 10 {
				refuse("no such variant")
				return
			}
			p := StoreVariants()[i]
			if !slices.Contains(p.Colors, in.Color) || len(p.Sizes) == 0 && in.Size != "" || len(p.Sizes) > 0 && !slices.Contains(p.Sizes, in.Size) {
				refuse("no such variant")
				return
			}
			id := storeLineID(slug, in.Color, in.Size)
			if j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == id }); j >= 0 {
				c.lines[j].quantity = min(c.lines[j].quantity+in.Quantity, 10)
			} else {
				c.lines = append(c.lines, cartLine{id, slug, min(in.Quantity, 10)})
			}
		case WriteQuantity:
			j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == in.Line })
			if j < 0 || in.Quantity < 0 || in.Quantity > 10 {
				refuse("bad line or quantity")
				return
			}
			if in.Quantity == 0 {
				c.lines = slices.Delete(c.lines, j, j+1)
			} else {
				c.lines[j].quantity = in.Quantity
			}
		case WriteCoupon:
			if in.Code != "" && !slices.Contains(StoreCoupons, in.Code) {
				refuse("no such coupon")
				return
			}
			c.coupon = in.Code
		case WriteCheckout:
			if len(c.lines) == 0 {
				refuse("empty cart")
				return
			}
			c.lines, c.coupon = nil, ""
			w.Header().Set("Location", "/store/checkout?placed=SH-1043")
			f.placed = append(f.placed, "SH-1043")
		}
		f.log = append(f.log, kind)
		if s, ok := f.answer[kind]; ok {
			status = s
		}
		if status == 200 {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		if status == 200 {
			w.Write([]byte("{}"))
		}
	}
}

func (f *fakeStoreServer) page(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.cartOf(w, r, false)
	if f.stale {
		c = &shopCart{lines: demoCart()}
	}
	var lines []string
	for _, l := range c.lines {
		lines = append(lines, fmt.Sprint(l.id, "x", l.quantity))
	}
	fmt.Fprintf(w, "cart %s coupon %s", strings.Join(lines, ","), c.coupon)
}

func storeRunner(base string, write func(string) string) *runner {
	r := newTestRunner(base, BrowseScenario{Name: "s"})
	r.storeWrite = write
	return r
}

// traceOf is a trace that loads every page of a plan.
func traceOf(p Plan) Trace {
	t := Trace{Plan: p.ID}
	for _, s := range p.Steps {
		t.Steps = append(t.Steps, TraceStep{Kind: s.Kind, Page: s.Page, Reqs: []TraceReq{{Method: "GET", URL: s.Page, Class: "doc", End: 1, Status: 200}}})
	}
	return t
}

// The shopper visits of a block, played against a fake storefront: every write goes to the path of
// the app with the cookies of its visit, and the shop takes it. Before the clock the round makes a
// cart for each returning visit, and that visit comes back to it: its writes go to the cart made for
// it, not to a new one.
func TestStoreWritesGoToThePathOfTheAppWithTheCookiesOfTheVisit(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	traces := formTraces(set.Plans, traceOf)
	sc := mustScenario("shop")
	sc.Staff = 0 // the shopper visits alone: the fake has no back office
	returning, newAdds := 0, 0
	for _, p := range set.Plans[:ShopperBlockVisits] {
		if p.Returning {
			returning++
		} else if len(p.Writes) > 0 {
			newAdds++
		}
	}
	for name, write := range map[string]func(string) string{"shop": SameStorePath, "nextjs": NextStorePath, "reactrouter": ReactRouterStorePath} {
		t.Run(name, func(t *testing.T) {
			f, srv := newFakeStore(t, write)
			wl, err := NewWorkload(&set, traces, sc, 1)
			if err != nil {
				t.Fatal(err)
			}
			r := storeRunner(srv.URL, write)
			r.wl = wl
			round := r.RunClosed(context.Background(), 4, ShopperBlockVisits, time.Minute)
			if round.Failures != 0 || round.Visits != ShopperBlockVisits || round.SetupError != "" {
				t.Fatalf("%d failures (%s), %d visits, setup %q", round.Failures, round.FirstErr, round.Visits, round.SetupError)
			}
			planned := 66 + 13
			if round.SetupRequests != ReturningCartLines*returning || len(f.log) != planned+round.SetupRequests || round.Writes != planned || round.WritesPlanned != planned {
				t.Fatalf("the shop saw %d writes, the round counted %d and %d of setup, %d were planned", len(f.log), round.Writes, round.SetupRequests, round.WritesPlanned)
			}
			for _, line := range f.log {
				if strings.HasPrefix(line, "refused") {
					t.Error(line)
				}
			}
			if f.next != returning+newAdds {
				t.Errorf("the shop made %d carts: want %d for the returning visits and %d for the new visitors that add", f.next, returning, newAdds)
			}
			if round.WriteKinds[WriteAdd] != 66 || round.WriteKinds[WriteCheckout] != 13 {
				t.Errorf("writes counted %v", round.WriteKinds)
			}
			// 200 and 303 are both answers to count by their status.
			for _, key := range []string{"write add 200 application/json", "write checkout 303 -"} {
				if round.Responses[key] == 0 {
					t.Errorf("no %q in %v", key, round.Responses)
				}
			}
			if round.Types["write checkout"].N != 13 || round.Types["write"].N != planned {
				t.Errorf("latencies %v", round.Types)
			}
			round.Judge(sc)
			if !round.Valid {
				t.Errorf("the round of a whole block is not valid: %s", round.WhyInvalid)
			}
		})
	}
}

// Making the carts of the returning visitors must put nothing into a render memo: a round whose
// setup got an answer that rendered a page is not valid, and is not played.
func TestASetupAnswerThatRendersMakesTheRoundInvalid(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	traces := formTraces(set.Plans, traceOf)
	sc := mustScenario("shop")
	sc.Staff = 0
	for _, how := range []string{"", RenderNone, RenderVM, RenderHit} {
		var gets atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				gets.Add(1)
			}
			if how != "" {
				w.Header().Set(RenderHeader, how)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
		}))
		wl, err := NewWorkload(&set, traces, sc, 1)
		if err != nil {
			t.Fatal(err)
		}
		r := storeRunner(srv.URL, SameStorePath)
		r.wl = wl
		round := r.RunClosed(context.Background(), 4, ShopperBlockVisits, time.Minute)
		srv.Close()
		round.Judge(sc)
		renders := how == RenderVM || how == RenderHit
		if renders != (round.SetupError != "") || renders && round.Valid {
			t.Errorf("setup answered %q: setup error %q, valid %v (%s)", how, round.SetupError, round.Valid, round.WhyInvalid)
		}
		if renders && (gets.Load() != 0 || !strings.Contains(round.WhyInvalid, "the setup before the clock failed")) {
			t.Errorf("setup answered %q: %d GETs were played after it, why %q", how, gets.Load(), round.WhyInvalid)
		}
	}
}

func TestStoreWriteMustAnswerWithTheStatusOfThePlan(t *testing.T) {
	f, srv := newFakeStore(t, SameStorePath)
	f.answer = map[string]int{WriteCoupon: 200}
	r := storeRunner(srv.URL, SameStorePath)
	v := &visitState{client: &http.Client{}}
	err := r.doWrite(context.Background(), v, newAcc(), PlannedWrite{Kind: WriteCoupon, Store: &StoreRequest{Kind: WriteCoupon, Method: "POST", Path: "/store/cart", Body: []byte(`{"code":""}`), Status: 204}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "want 204") {
		t.Errorf("a coupon answered 200: %v", err)
	}
}

func TestReplaySendsTheBodyOfARecordedPostAndTakesA204(t *testing.T) {
	f, srv := newFakeStore(t, SameStorePath)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/store/products/oak-tray", Reqs: []TraceReq{
		{Method: "GET", URL: "/store/products/oak-tray", Class: "doc", End: 5, Status: 200},
		{Method: "POST", URL: "/store/mine", Class: "nav", Start: 10, End: 15, Status: 204, Headers: map[string]string{"content-type": "application/json"}, Body: `{"slug":"oak-tray"}`},
	}}}}
	r := storeRunner(srv.URL, SameStorePath)
	r.wl = newTestRunner(srv.URL, r.wl.Scenario, tr).wl
	a := newAcc()
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 1 || f.posted[0] != `{"slug":"oak-tray"}` {
		t.Errorf("the shop was sent %q", f.posted)
	}
	if a.responses["nav 204 -"] != 1 {
		t.Errorf("responses %v", a.responses)
	}
}

func TestVerifyStoreChecksEachWriteShowsAndFailsOnAStalePage(t *testing.T) {
	for name, write := range map[string]func(string) string{"shop": SameStorePath, "nextjs": NextStorePath} {
		t.Run(name, func(t *testing.T) {
			f, srv := newFakeStore(t, write)
			r := storeRunner(srv.URL, write)
			if err := r.verifyStore(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(f.log, ","); got != "add,coupon,quantity,checkout" {
				t.Errorf("the shop saw %s", got)
			}
			f.staleOffice = true
			if err := r.verifyStore(context.Background()); err == nil || !strings.Contains(err.Error(), "back office") {
				t.Errorf("a server whose back office does not show the order of a checkout passed: %v", err)
			}
			f.staleOffice = false
			f.stale = true
			if err := r.verifyStore(context.Background()); err == nil || !strings.Contains(err.Error(), "same after the write") {
				t.Errorf("a server whose cart page ignores the cart passed: %v", err)
			}
		})
	}
}

func TestVerifyStoreFailsOnAWrongStatus(t *testing.T) {
	f, srv := newFakeStore(t, SameStorePath)
	f.answer = map[string]int{WriteCheckout: 200}
	r := storeRunner(srv.URL, SameStorePath)
	if err := r.verifyStore(context.Background()); err == nil || !strings.Contains(err.Error(), "check out") {
		t.Errorf("a checkout that answers 200 passed: %v", err)
	}
}

func TestEveryScenarioRunsEachServerOnce(t *testing.T) {
	env := Env{Shop: "/shop"}
	for _, sc := range BrowseScenarios {
		specs, err := env.browseSpecs(sc, BrowseFrameworks)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range specs {
			names = append(names, s.Name)
		}
		if got := strings.Join(names, ","); got != "tifa,nextjs,start,reactrouter,tifa-nocache,start-gzip,nextjsnoprefetch" {
			t.Errorf("%s runs %s", sc.Name, got)
		}
	}
}

// The browse comparison has one scenario: the shop with the mix of mix.go, shoppers and staff on one
// server, with its throughput and its latency. The scenario with twice the staff and the scenarios of
// the two sites are gone.
func TestThereIsOneScenario(t *testing.T) {
	if len(BrowseScenarios) != 1 {
		t.Fatalf("%d scenarios", len(BrowseScenarios))
	}
	sc := BrowseScenarios[0]
	if sc.Name != "shop" || sc.Staff != StaffShare || sc.Edit != StaffEditShare || sc.Ship != StaffShipShare || !sc.Throughput || !sc.Latency || sc.ReadOnly {
		t.Errorf("the scenario is %+v", sc)
	}
	for _, gone := range []string{"shop-staff-x2", "default", "store-w5", "admin-w20", "r95w5", ""} {
		if _, ok := BrowseScenarioByName(gone); ok {
			t.Errorf("%q is a scenario", gone)
		}
	}
}

// The read-only control runs once, on the first scenario of a run.
func TestTheControlRunsOnTheFirstScenario(t *testing.T) {
	list := []BrowseScenario{BrowseScenarios[0], moreStaff}
	var got []string
	for _, sc := range list {
		if controlled(list, sc) {
			got = append(got, sc.Name)
		}
	}
	if strings.Join(got, ",") != "shop" {
		t.Errorf("the control runs on %v", got)
	}
}

// The rate of the open run is half of what Next.js sustains on the scenario, whatever the others
// sustain: a server that cannot hold it fails at it.
func TestTheOpenRateIsHalfOfNextjs(t *testing.T) {
	sc := BrowseScenario{Name: "shop"}
	res := &BrowseResult{Sustain: map[string]*Sustain{
		"shop/nextjs": {Rate: 21.5, Done: true}, "shop/tifa-nocache": {Rate: 5, Done: true}, "shop/start": {Rate: 400, Done: true}}}
	if rate, err := openRate(res, sc); err != nil || rate != 10.75 {
		t.Errorf("rate %v, %v", rate, err)
	}
	res.Sustain["shop/nextjs"] = &Sustain{Done: true, Note: "no rate passed"}
	if _, err := openRate(res, sc); err == nil || !strings.Contains(err.Error(), "Next.js") {
		t.Errorf("a Next.js with no rate: %v", err)
	}
	delete(res.Sustain, "shop/nextjs")
	if _, err := openRate(res, sc); err == nil {
		t.Error("a scenario whose Next.js was not searched has a rate")
	}
}

func TestTifaVariantsReplayTheTracesOfTifa(t *testing.T) {
	env := Env{Shop: "/shop"}
	for name, want := range map[string]string{"tifa": "tifa", "tifa-nocache": "tifa", "nextjs": "nextjs", "start": "start", "reactrouter": "reactrouter", "start-gzip": "start", NextNoPrefetch: NextNoPrefetch} {
		i := slices.IndexFunc(env.Specs(), func(s Spec) bool { return s.Name == name })
		if got := traceName(env.Specs()[i]); got != want {
			t.Errorf("%s replays the traces of %s, want %s", name, got, want)
		}
	}
	nocache, _ := env.Target("tifa-nocache")
	if i := slices.Index(nocache.Args, "--ssr-memo-size"); i < 0 || nocache.Args[i+1] != "0" {
		t.Errorf("tifa-nocache runs with %v", nocache.Args)
	}
	// Start with gzip is the build of Start, started with the switch of the app that adds gzip.
	start, _ := env.Target("start")
	gzip, _ := env.Target("start-gzip")
	if gzip.Dir != start.Dir || !slices.Equal(gzip.Args, start.Args) || !slices.Contains(gzip.Env, "COMPARE_GZIP=1") || slices.Contains(start.Env, "COMPARE_GZIP=1") {
		t.Errorf("start-gzip runs %v in %s with %v, and start with %v", gzip.Args, gzip.Dir, gzip.Env, start.Env)
	}
	// Next.js with prefetching off is the same app, started from the folder of its other build.
	next, _ := env.Target("nextjs")
	off, _ := env.Target(NextNoPrefetch)
	if off.Dir != next.Dir || !slices.Contains(off.Env, "COMPARE_NEXT_DIST=.next-noprefetch") || slices.ContainsFunc(next.Env, func(v string) bool { return strings.HasPrefix(v, "COMPARE_NEXT_DIST=") }) {
		t.Errorf("%s runs in %s with %v, and nextjs with %v", NextNoPrefetch, off.Dir, off.Env, next.Env)
	}
}

// The site replays its plans and traces, and refuses a set in which a framework lacks a plan.
func TestTheSiteReplaysOnlyAWholeSetOfItsTraces(t *testing.T) {
	dir := t.TempDir()
	set := GenerateSitePlanSet(1, 1)
	data, _ := json.Marshal(set)
	os.WriteFile(PlansPath(dir), data, 0o644)
	write := func(framework string, skip int) {
		var b strings.Builder
		for _, p := range set.Plans {
			if p.ID == skip {
				continue
			}
			for _, tr := range browserForms(p, func(form Plan) Trace { return browserTrace(form) }) {
				line, _ := json.Marshal(tr)
				b.Write(line)
				b.WriteString("\n")
			}
		}
		os.WriteFile(filepath.Join(dir, framework+".jsonl"), []byte(b.String()), 0o644)
	}
	write("tifa", -1)
	write("nextjs", 9)
	got, traces, err := LoadTraces(dir, []string{"tifa"})
	if err != nil || len(traces["tifa"]) != SiteBlockPlans || got.Site != SiteShop {
		t.Fatalf("a whole set: %v", err)
	}
	if _, _, err := LoadTraces(dir, []string{"tifa", "nextjs"}); err == nil || !strings.Contains(err.Error(), "nextjs.jsonl") {
		t.Errorf("a set without plan 9 for Next.js: %v", err)
	}
}

// A click can fetch more than one piece of data: TanStack Start, going from one project to another,
// asks for the project of the layout and then for the orders of the page. The navigation data of a
// page is all of it, so the order of a checkout shows in the second answer and the check passes.
func TestTheNavigationDataOfAPageIsEveryRequestOfItsClick(t *testing.T) {
	f, srv := newFakeStore(t, SameStorePath)
	f.placed = []string{"SH-1043"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fn/project", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"project":"studio"}`) })
	mux.Handle("/", srv.Config.Handler)
	srv.Config.Handler = mux
	click := TraceStep{Kind: StepClick, Page: "/projects/studio", Reqs: []TraceReq{
		{Method: "GET", URL: "/fn/project", Class: "nav", End: 1, Status: 200},
		{Method: "GET", URL: "/projects/studio", Class: "nav", End: 2, Status: 200},
	}}
	load := TraceStep{Kind: StepLoad, Page: "/projects/weekend", Reqs: []TraceReq{{Method: "GET", URL: "/projects/weekend", Class: "doc", End: 1, Status: 200}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "s"}, Trace{Steps: []TraceStep{load, click}})
	if err := r.shows(context.Background(), "check out, in the back office", "/projects/studio", "SH-1043"); err != nil {
		t.Fatal(err)
	}
	f.staleOffice = true
	if err := r.shows(context.Background(), "check out, in the back office", "/projects/studio", "SH-1043"); err == nil {
		t.Error("a back office that does not show the order passed")
	}
}
