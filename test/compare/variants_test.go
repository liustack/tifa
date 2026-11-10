package compare

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// formWorkload is a workload of a scenario over the plans of the site with the traces of every form.
func formWorkload(t *testing.T, seed uint64, blocks int, name string) *Workload {
	t.Helper()
	set := GenerateSitePlanSet(seed, blocks)
	w, err := NewWorkload(&set, formTraces(set.Plans, func(p Plan) Trace { return browserTrace(p) }), scenario(t, name), seed)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A played visit of a plan that searches for a word of the list takes one of the forms of the plan,
// the visit or one of its variants, by the seed and the number of the visit: every form about as
// often, another one in another cycle and under another seed, and the same one whatever the traces,
// so every server plays the same. A plan with no variant is played as it is.
func TestAPlayedVisitTakesOneFormOfItsPlanByTheSeedAndTheVisit(t *testing.T) {
	w := formWorkload(t, 4, 1, "shop")
	other := formWorkload(t, 4, 1, "shop")
	for id, tr := range other.Traces {
		// The traces of another framework: more requests, the same page views.
		for i := range tr.Steps {
			tr.Steps[i].Reqs = append(tr.Steps[i].Reqs, TraceReq{Method: "GET", URL: "/x", Class: "prefetch", End: 1, Status: 200})
		}
		other.Traces[id] = tr
	}
	seeded := formWorkload(t, 5, 1, "shop")
	n := w.cycleLen()
	count := map[int]int{}
	moved, reseeded, withVariants := 0, 0, 0
	for seq := range 20 * n {
		p := w.planFor(seq)
		base := w.Plans[slices.IndexFunc(w.Plans, func(q Plan) bool { return q.ID == p.ID })]
		if len(p.Variants) != 0 {
			t.Fatalf("visit %d plays a plan that still has variants", seq)
		}
		if len(base.Variants) == 0 {
			if p.form != 0 || !slices.Equal(p.Steps, base.Steps) {
				t.Fatalf("visit %d plays form %d of plan %d, which has no variant", seq, p.form, p.ID)
			}
			continue
		}
		withVariants++
		count[p.form]++
		if !slices.Equal(p.Steps, planForm(base, p.form).Steps) || p.form < 0 || p.form >= PlanVariants {
			t.Fatalf("visit %d plays form %d of plan %d with other page views than that form", seq, p.form, p.ID)
		}
		if q := other.planFor(seq); q.form != p.form || !slices.Equal(q.Steps, p.Steps) {
			t.Fatalf("visit %d plays form %d on one server and form %d on another", seq, p.form, q.form)
		}
		if seq >= n && w.planFor(seq-n).form != p.form {
			moved++
		}
		if seq < n && seeded.planFor(seq).ID == p.ID && seeded.planFor(seq).form != p.form {
			reseeded++
		}
	}
	for v := range PlanVariants {
		if share := float64(count[v]) / float64(withVariants); share < 0.17 || share > 0.23 {
			t.Errorf("form %d is played %.3f of the time, want about a fifth: %v", v, share, count)
		}
	}
	if moved < withVariants/2 {
		t.Errorf("%d of %d visits play another form than the same plan a cycle before: too few", moved, withVariants)
	}
	if w.planFor(7).form != w.planFor(7).form {
		t.Error("the form of a visit is not a function of the visit")
	}
}

// The replay sends the requests of the form a visit takes, and everything else of the visit follows
// that form: the writes go after its page views and buy the product of its page, the documents
// counted are its documents, and the mix counts what it does.
func TestTheReplayPlaysTheFormAVisitTakes(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			mu.Lock()
			got = append(got, r.URL.RequestURI())
			mu.Unlock()
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	w := formWorkload(t, 6, 1, "shop")
	w.Scenario.ReadOnly = true
	r := &runner{base: srv.URL, write: nodePath, create: nodeCreate, storeWrite: samePath, products: nodeProducts, editProduct: nodeEditProduct, wl: w, seed: 6,
		cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }}
	played := map[int]bool{}
	for seq := 0; seq < 3*w.cycleLen() && len(played) < PlanVariants; seq++ {
		p := w.planFor(seq)
		if p.Role != RoleShopper || len(w.Plans[slices.IndexFunc(w.Plans, func(q Plan) bool { return q.ID == p.ID })].Variants) == 0 || played[p.form] {
			continue
		}
		played[p.form] = true
		mu.Lock()
		got = nil
		mu.Unlock()
		if err := visitOnce(r, newAcc(), seq); err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, s := range p.Steps {
			want = append(want, subsFor(r.seed, seq, p).apply(s.Page))
		}
		mu.Lock()
		if !slices.Equal(got, want) {
			t.Errorf("visit %d takes form %d of plan %d and asked for %v, want the pages of the form %v", seq, p.form, p.ID, got, want)
		}
		mu.Unlock()
		docs, err := w.docSteps(p)
		if err != nil {
			t.Fatal(err)
		}
		at := slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Act == ActSearch })
		if !docs[0] || !docs[at] {
			t.Errorf("visit %d: the documents of its form are %v, and its search is page view %d", seq, docs, at)
		}
	}
	if len(played) != PlanVariants {
		t.Fatalf("the visits of three cycles played the forms %v, want all %d", played, PlanVariants)
	}
	// A visit that adds to the cart buys the product of the page of the form it takes.
	writes := formWorkload(t, 6, 2, "shop")
	bought := 0
	for seq := range 6 * writes.cycleLen() {
		p := writes.planFor(seq)
		if p.form == 0 {
			continue
		}
		for _, x := range writes.writesOf(seq) {
			if x.Kind == WriteAdd {
				if page := p.Steps[x.After].Page; !strings.HasSuffix(x.Store.Path, "/products/"+productOf(page)) {
					t.Fatalf("visit %d takes form %d and adds %s after the page %s", seq, p.form, x.Store.Path, page)
				}
				bought++
			}
		}
	}
	if bought == 0 {
		t.Error("no visit that takes a variant adds to the cart in six cycles")
	}
	// The mix of a block counts the forms its visits take, and the forms of every cycle keep the quotas:
	// a variant searches where the visit does, for a word of the same class, and does the same actions.
	n := writes.Scenario.blockVisits()
	for b := range 20 * writes.cycleLen() / n {
		var round Round
		writes.plan(&round, b*n, n)
		if why := round.Mix.why(writes.Scenario); why != "" || round.Mix.Searches != 200 || round.Mix.SearchesUnique != 10 {
			t.Errorf("the mix of block %d of the run: %d searches, %d for a word of their own, %s", b, round.Mix.Searches, round.Mix.SearchesUnique, why)
		}
	}
}

// The crawler requests of a block are laid out once for all cycles, from the documents of the visits of
// its plans, while a played visit takes any form of its plan. So every variant must load a document
// at the page views where the visit of its plan does, and a set of traces in which one does not is
// refused.
func TestAVariantThatLoadsOtherDocumentsThanItsVisitIsRefused(t *testing.T) {
	set := GenerateSitePlanSet(2, 1)
	var with Plan
	for _, p := range set.Plans {
		if len(p.Variants) > 0 && p.Steps[len(p.Steps)-1].Act == ActLink {
			with = p
			break
		}
	}
	traces := formTraces(set.Plans, func(p Plan) Trace { return browserTrace(p) })
	if _, err := NewWorkload(&set, traces, mustScenario("shop"), 2); err != nil {
		t.Fatal(err)
	}
	tr := traces[with.ID]
	tr.Variants = slices.Clone(tr.Variants)
	tr.Variants[2] = browserTrace(planForm(with, 3), len(with.Steps)-1)
	traces[with.ID] = tr
	_, err := NewWorkload(&set, traces, mustScenario("shop"), 2)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("plan %d", with.ID)) || !strings.Contains(err.Error(), "variant 3") {
		t.Errorf("error %v, want one that names variant 3 of plan %d", err, with.ID)
	}
	// And a plan with variants needs the trace of each.
	tr.Variants = tr.Variants[:2]
	traces[with.ID] = tr
	if _, err := NewWorkload(&set, traces, mustScenario("shop"), 2); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("plan %d", with.ID)) {
		t.Errorf("error %v, want one that names plan %d, which lacks the traces of two variants", err, with.ID)
	}
}

// The expected hit rate follows the form each visit takes: its pages, and the page views its trace
// says are documents.
func TestExpectCountsTheDocumentsOfTheFormAVisitTakes(t *testing.T) {
	list, tray := "/store/products?sort=newest", "/store/products/oak-tray"
	plan := Plan{ID: 0, Role: RoleShopper,
		Steps:    []PlanStep{{Kind: StepLoad, Page: "/store"}, {Kind: StepClick, Page: list, Act: ActLink}},
		Variants: [][]PlanStep{{{Kind: StepLoad, Page: "/store"}, {Kind: StepClick, Page: tray, Act: ActLink}}},
	}
	w := handWorkload([]Plan{plan}, nil)
	// The click is a document load in both forms.
	w.Traces = formTraces([]Plan{plan}, func(p Plan) Trace { return browserTrace(p, 1) })
	const visits = 40
	want := map[string]int{}
	for seq := range visits {
		p := w.planFor(seq)
		want[pageClass(p.Steps[1].Page)]++
	}
	if want["list"] < 5 || want["product"] < 5 {
		t.Fatalf("the %d visits take the forms %v: too few of one", visits, want)
	}
	got, err := w.Expect(handDeps(), nil, expectSpan{0, visits, true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Humans["home"].Docs != visits || got.Humans["list"].Docs != want["list"] || got.Humans["product"].Docs != want["product"] {
		t.Errorf("the plans count %+v, want %d homes and the lists and products of the forms the visits take: %v", got.Humans, visits, want)
	}
	// Each page is new once, and a hit after that: nothing writes.
	if got.Humans["list"].New != 1 || got.Humans["product"].New != 1 || got.Humans["home"].New != 1 {
		t.Errorf("the plans count %+v, want each page new once", got.Humans)
	}
}
