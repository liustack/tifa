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
	"testing"
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
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
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
			i := slices.IndexFunc(StoreVariants, func(p StoreProduct) bool { return p.Slug == slug })
			if i < 0 || StoreVariants[i].Stock == 0 || in.Quantity < 1 || in.Quantity > 10 {
				refuse("no such variant")
				return
			}
			p := StoreVariants[i]
			if !slices.Contains(p.Colors, in.Color) || len(p.Sizes) == 0 && in.Size != "" || len(p.Sizes) > 0 && !slices.Contains(p.Sizes, in.Size) {
				refuse("no such variant")
				return
			}
			id := storeLineID(slug, in.Color, in.Size)
			if j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == id }); j >= 0 {
				c.lines[j].quantity = min(c.lines[j].quantity+in.Quantity, 10, p.Stock)
			} else {
				c.lines = append(c.lines, cartLine{id, slug, min(in.Quantity, p.Stock)})
			}
		case WriteQuantity:
			j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == in.Line })
			if j < 0 || in.Quantity < 0 || in.Quantity > 10 || in.Quantity > storeStock(c.lines[j].slug) {
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
			w.Header().Set("Location", "/store/checkout?placed=SS-ABCDEFGHJK")
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

func storeReplayer(base string, write func(string) string, rate float64) *replayer {
	r := newTestReplayer(base, BrowseScenario{Name: "s", Mean: 4, WriteRate: rate, Mix: MixStore})
	r.storeWrite = write
	return r
}

// traceOf is a trace that loads every page of a plan.
func traceOf(p Plan) Trace {
	var t Trace
	for _, s := range p.Steps {
		t.Steps = append(t.Steps, TraceStep{Kind: s.Kind, Page: s.Page, Reqs: []TraceReq{{Method: "GET", URL: s.Page, Class: "doc", End: 1}}})
	}
	return t
}

func TestStoreWritesGoToThePathOfTheAppWithTheCookiesOfTheVisit(t *testing.T) {
	for name, write := range map[string]func(string) string{"shop": SameStorePath, "nextjs": NextStorePath, "reactrouter": ReactRouterStorePath} {
		t.Run(name, func(t *testing.T) {
			f, srv := newFakeStore(t, write)
			r := storeReplayer(srv.URL, write, 1)
			var sent int
			for _, p := range GenerateStorePlans(1, 60, 4) {
				tr := traceOf(p)
				r.plans, r.traces = []Plan{p}, map[int]Trace{0: tr}
				if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, p); err != nil {
					t.Fatalf("plan %d: %v", p.ID, err)
				}
				sent += len(PlanStoreWrites(1, p, 1))
			}
			if len(f.log) != sent || sent == 0 {
				t.Fatalf("the shop saw %d writes, %d were planned: %v", len(f.log), sent, f.log)
			}
			for _, line := range f.log {
				if strings.HasPrefix(line, "refused") {
					t.Error(line)
				}
			}
			n := 0
			for _, c := range r.res.Writes {
				n += c
			}
			if n != sent || r.res.Writes[WriteAdd] == 0 || r.res.Writes[WriteCheckout] == 0 {
				t.Errorf("writes counted %v, sent %d", r.res.Writes, sent)
			}
			// 200, 204 and 303 are all answers to count by their status.
			for _, key := range []string{"write add 200 application/json", "write coupon 204 -", "write checkout 303 -"} {
				if r.res.Responses[key] == 0 {
					t.Errorf("no %q in %v", key, r.res.Responses)
				}
			}
			if len(r.res.lat["write checkout"]) == 0 || len(r.res.lat["write"]) != sent {
				t.Errorf("latencies %v", r.res.lat)
			}
		})
	}
}

func TestStoreWriteMustAnswerWithTheStatusOfThePlan(t *testing.T) {
	f, srv := newFakeStore(t, SameStorePath)
	f.answer = map[string]int{WriteCoupon: 200}
	r := storeReplayer(srv.URL, SameStorePath, 1)
	v := &visitState{client: &http.Client{}}
	err := r.doWrite(context.Background(), v, PlannedWrite{Kind: WriteCoupon, Store: &StoreRequest{Kind: WriteCoupon, Method: "POST", Path: "/store/cart", Body: []byte(`{"code":""}`), Status: 204}})
	if err == nil || !strings.Contains(err.Error(), "want 204") {
		t.Errorf("a coupon answered 200: %v", err)
	}
}

func TestReplaySendsTheBodyOfARecordedPostAndTakesA204(t *testing.T) {
	f, srv := newFakeStore(t, SameStorePath)
	r := storeReplayer(srv.URL, SameStorePath, 0)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/store/products/oak-tray", Reqs: []TraceReq{
		{Method: "GET", URL: "/store/products/oak-tray", Class: "doc", End: 5},
		{Method: "POST", URL: "/store/mine", Class: "nav", Start: 10, End: 15, Headers: map[string]string{"content-type": "application/json"}, Body: `{"slug":"oak-tray"}`},
	}}}}
	r.plans, r.traces = []Plan{{ID: 0, Steps: []PlanStep{{StepLoad, "/store/products/oak-tray"}}}}, map[int]Trace{0: tr}
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err != nil {
		t.Fatal(err)
	}
	if len(f.posted) != 1 || f.posted[0] != `{"slug":"oak-tray"}` {
		t.Errorf("the shop was sent %q", f.posted)
	}
}

func TestVerifyStoreChecksEachWriteShowsAndFailsOnAStalePage(t *testing.T) {
	for name, write := range map[string]func(string) string{"shop": SameStorePath, "nextjs": NextStorePath} {
		t.Run(name, func(t *testing.T) {
			f, srv := newFakeStore(t, write)
			r := storeReplayer(srv.URL, write, 0)
			if err := r.verifyStore(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(f.log, ","); got != "add,coupon,quantity,checkout" {
				t.Errorf("the shop saw %s", got)
			}
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
	r := storeReplayer(srv.URL, SameStorePath, 0)
	if err := r.verifyStore(context.Background()); err == nil || !strings.Contains(err.Error(), "check out") {
		t.Errorf("a checkout that answers 200 passed: %v", err)
	}
}

func TestStorefrontScenariosRunTifaTwiceAndTheOthersOnce(t *testing.T) {
	store, office := BrowseScenarios[len(BrowseScenarios)-2], BrowseScenarios[0]
	if store.Name != "store-w20" || office.Name != "office-w5" {
		t.Fatalf("scenarios %s and %s", store.Name, office.Name)
	}
	names := BrowseFrameworks
	if got := strings.Join(browseServers(store, names), ","); got != "tifa-nopage,tifa-nocache,nextjs,start,reactrouter" {
		t.Errorf("the storefront runs %s", got)
	}
	if got := strings.Join(browseServers(office, names), ","); got != "tifa,nextjs,start,reactrouter" {
		t.Errorf("the back office runs %s", got)
	}
	if got := strings.Join(browseServers(store, []string{"tifa", "tifa-nocache", "start"}), ","); got != "tifa-nopage,tifa-nocache,start" {
		t.Errorf("the storefront with tifa-nocache named runs %s", got)
	}
	for _, name := range strings.Split(DefaultScenarios, ",") {
		if strings.HasPrefix(name, "store-") && name != "store-w20" {
			t.Errorf("%s runs by default", name)
		}
	}
}

func TestTheStorefrontReplaysItsOwnFilesAndTifaVariantsReplayTifa(t *testing.T) {
	if got := SitePlansPath("d", SiteStore, 4); got != "d/plans-store-m4.json" {
		t.Error(got)
	}
	if got := SiteTracePath("d/", "tifa", SiteStore, 4); got != "d/tifa-store-m4.jsonl" {
		t.Error(got)
	}
	if PlansPath("d", 4) != "d/plans-m4.json" || TracePath("d", "tifa", 4) != "d/tifa-m4.jsonl" {
		t.Error("the files of the workbench changed their names")
	}
	env := Env{Shop: "/shop"}
	for name, want := range map[string]string{"tifa": "tifa", "tifa-nocache": "tifa", "tifa-nopage": "tifa", "nextjs": "nextjs", "start": "start", "reactrouter": "reactrouter"} {
		i := slices.IndexFunc(env.Specs(), func(s Spec) bool { return s.Name == name })
		if got := traceName(env.Specs()[i]); got != want {
			t.Errorf("%s replays the traces of %s, want %s", name, got, want)
		}
	}
	// Tifa with its whole-page cache off is the shop with one flag more.
	nopage, _ := env.Target("tifa-nopage")
	if !slices.Contains(nopage.Args, "--cache-whole-page=false") || slices.Contains(nopage.Args, "--cache-size") {
		t.Errorf("tifa-nopage runs with %v", nopage.Args)
	}
	nocache, _ := env.Target("tifa-nocache")
	if i := slices.Index(nocache.Args, "--cache-size"); i < 0 || nocache.Args[i+1] != "0" {
		t.Errorf("tifa-nocache runs with %v", nocache.Args)
	}
}

func TestOnlyStorefrontPlansWithATraceForEveryServerAreReplayed(t *testing.T) {
	dir := t.TempDir()
	var plans []Plan
	for i := range 150 {
		plans = append(plans, Plan{ID: i, Steps: []PlanStep{{StepLoad, "/store"}}})
	}
	data, _ := json.Marshal(plans)
	os.WriteFile(SitePlansPath(dir, SiteStore, 4), data, 0o644)
	write := func(framework string, skip int) {
		var b strings.Builder
		for i := range 150 {
			if i != skip {
				fmt.Fprintf(&b, `{"plan":%d,"steps":[{"kind":"load","page":"/store","reqs":[{"method":"GET","url":"/store","s":0,"e":1,"status":200,"class":"doc"}]}]}`+"\n", i)
			}
		}
		os.WriteFile(filepath.Join(dir, framework+"-store-m4.jsonl"), []byte(b.String()), 0o644)
	}
	write("tifa", 3)
	write("nextjs", 9)
	// Both Tifa servers read the traces of tifa.
	specs := []Spec{{Target: Target{Name: "tifa-nopage"}, Framework: "tifa"}, {Target: Target{Name: "tifa-nocache"}, Framework: "tifa"}, {Target: Target{Name: "nextjs"}, Framework: "nextjs"}}
	sets, common, err := loadSets(dir, SiteStore, specs, 4)
	if err != nil {
		t.Fatal(err)
	}
	if common != 148 || len(sets["tifa-nopage"].traces) != 148 || sets["tifa-nocache"].file != "tifa-store-m4.jsonl" {
		t.Errorf("common %d, %d, file %s", common, len(sets["tifa-nopage"].traces), sets["tifa-nocache"].file)
	}
	if _, err := os.Stat(PlansPath(dir, 4)); err == nil {
		t.Error("the workbench plans exist in a folder that only had the storefront's")
	}
}
