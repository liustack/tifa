package compare

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// browserTrace is the trace of a plan as a browser leaves it: the landing is a document, and so is a
// page view whose act loads one (a search, a plain link that filters, the next page of a list) and
// the page views of docClicks, links that happen to be plain ones. Every other page view gets
// navigation data. It is the trace of the visit of the plan alone: browserForms has its variants.
func browserTrace(p Plan, docClicks ...int) Trace {
	for i, s := range p.Steps {
		if doc, known := actLoads(s); i > 0 && known && doc {
			docClicks = append(docClicks, i)
		}
	}
	return landingTrace(p, docClicks...)
}

// browserForms are the traces of every form of a plan, the visit and then its variants, each as trace
// makes it, as the recorder writes them: one line each, with the number of the variant.
func browserForms(p Plan, trace func(Plan) Trace) []Trace {
	out := make([]Trace, 1+len(p.Variants))
	for v := range out {
		out[v] = trace(planForm(p, v))
		out[v].Variant = v
	}
	return out
}

// landingTrace is the trace of a plan in which the landing is a document and every other page view gets
// navigation data, whatever its act, but for the page views of docClicks. It is for the tests that
// count the documents of the plans themselves.
func landingTrace(p Plan, docClicks ...int) Trace {
	t := Trace{Plan: p.ID}
	for i, s := range p.Steps {
		class := "nav"
		if s.Kind == StepLoad {
			class = "doc"
		}
		for _, d := range docClicks {
			if d == i {
				class = "doc"
			}
		}
		t.Steps = append(t.Steps, TraceStep{Kind: s.Kind, Page: s.Page, Reqs: []TraceReq{{Method: "GET", URL: s.Page, Class: class, End: 1, Status: 200}}})
	}
	return t
}

// formTraces are the traces of plans with their variants, each form as trace makes it, as LoadTraces
// gives them: the trace of a plan is the one of its visit, with those of its variants.
func formTraces(plans []Plan, trace func(Plan) Trace) map[int]Trace {
	out := map[int]Trace{}
	for _, p := range plans {
		forms := browserForms(p, trace)
		forms[0].Variants = forms[1:]
		out[p.ID] = forms[0]
	}
	return out
}

// browserTraces are the traces of plans, every form as browserTrace makes it.
func browserTraces(plans []Plan) map[int]Trace {
	return formTraces(plans, func(p Plan) Trace { return browserTrace(p) })
}

// The pages are the same source in every framework, so a page view that is a document in one is a
// document in all four. The plans count the documents from the traces, and a set of traces that does
// not agree on them is refused, with the plan, the page view and the framework that differs.
func TestTheFrameworksAgreeOnWhichPageViewsAreDocuments(t *testing.T) {
	dir := t.TempDir()
	set := GenerateSitePlanSet(1, 1)
	data, _ := json.Marshal(set)
	os.WriteFile(PlansPath(dir), data, 0o644)
	// A plan with two clicks on links, which the plan does not say the kind of: in the browser the first
	// was a document load, as a click on a plain link is.
	var with Plan
	for _, p := range set.Plans {
		if len(p.Steps) > 2 && len(p.Variants) == 0 && p.Steps[1].Act == ActLink && p.Steps[2].Act == ActLink {
			with = p
			break
		}
	}
	names := []string{"tifa", "nextjs", "start", "reactrouter"}
	write := func(name string, docClicks ...int) {
		var b strings.Builder
		for _, p := range set.Plans {
			for _, tr := range browserForms(p, func(form Plan) Trace {
				if form.ID == with.ID {
					return browserTrace(form, docClicks...)
				}
				return browserTrace(form)
			}) {
				line, _ := json.Marshal(tr)
				b.Write(line)
				b.WriteString("\n")
			}
		}
		os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(b.String()), 0o644)
	}
	for _, n := range names {
		write(n, 1)
	}
	_, traces, err := LoadTraces(dir, names)
	if err != nil {
		t.Fatalf("four traces that agree: %v", err)
	}
	for _, n := range names {
		if got := traces[n][with.ID].documents(); len(got) != len(with.Steps) || !got[0] || !got[1] || got[2] {
			t.Errorf("%s: the documents of plan %d are %v, want the landing and the first click", n, with.ID, got)
		}
	}
	// TanStack Start got navigation data where the other three loaded a document.
	write("start")
	_, _, err = LoadTraces(dir, names)
	if err == nil {
		t.Fatal("traces that do not agree on the documents were loaded")
	}
	for _, want := range []string{fmt.Sprintf("plan %d", with.ID), "page view 1", "start", with.Steps[1].Page, "record"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
	// One framework alone has nothing to disagree with.
	if _, _, err := LoadTraces(dir, []string{"start"}); err != nil {
		t.Errorf("the trace of one framework: %v", err)
	}
}

// A click the page does as a document load asks for a document as a landing does: it is counted, it
// hits or not as a landing would, and it makes its page seen. A click the router takes gets data and
// no HTML, and counts for nothing. Which is which comes from the traces.
func TestExpectCountsADocumentThatAClickAsks(t *testing.T) {
	list, pen := "/store/products?sort=newest", "/store/products/walnut-pen"
	plans := []Plan{
		{ID: 0, Role: RoleShopper, Steps: []PlanStep{{Kind: StepLoad, Page: "/store"}, {Kind: StepClick, Page: list}}}, // the home is new, and so is the list, which the click loads as a document
		landing(1, list), // a hit: the click of visit 0 made the page seen
		{ID: 2, Role: RoleShopper, Steps: []PlanStep{{Kind: StepLoad, Page: "/store/products/oak-tray"}, {Kind: StepClick, Page: pen + "?reviews=2"}}}, // the click gets data: no document, and the page is not seen
		landing(3, pen+"?reviews=2"), // new
		{ID: 4, Role: RoleShopper, Steps: []PlanStep{{Kind: StepLoad, Page: pen}, {Kind: StepClick, Page: "/store/checkout"}, {Kind: StepClick, Page: "/store"}}}, // the pen is new, the visit checks out, and then loads the home as a document: the kettle of its cart changed
		landing(5, "/store"), // a hit: nothing changed since the last document of visit 4
	}
	w := handWorkload(plans, nil)
	w.Traces[0] = browserTrace(plans[0], 1)
	w.Traces[4] = browserTrace(plans[4], 2)
	add, co := checkout(4)
	w.Writes = map[[2]int]PlannedWrite{{4, 0}: add, {4, 1}: co}
	got, err := w.Expect(handDeps(), nil, expectSpan{0, len(plans), true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ExpectedCell{
		"home":    {Docs: 3, Hits: 1, Changed: 1, New: 1},
		"list":    {Docs: 2, Hits: 1, New: 1},
		"product": {Docs: 3, New: 3},
	}
	if len(got.Humans) != len(want) {
		t.Errorf("kinds %v, want %v", got.Humans, want)
	}
	for kind, c := range want {
		if g := got.Humans[kind]; g != c {
			t.Errorf("%s: %+v, want %+v", kind, g, c)
		}
	}
	// The class of each document goes by the visit and the page view, and a page view that is no document
	// has none.
	for at, class := range map[[2]int]docClass{{0, 0}: docNew, {0, 1}: docNew, {1, 0}: docSame, {2, 0}: docNew, {3, 0}: docNew, {4, 0}: docNew, {4, 2}: docChanged, {5, 0}: docSame} {
		if g, ok := got.humanClass(at[0], at[1]); !ok || g != class {
			t.Errorf("page view %d of visit %d is %v, want %v", at[1], at[0], g, class)
		}
	}
	for _, at := range [][2]int{{2, 1}, {4, 1}} {
		if g, ok := got.humanClass(at[0], at[1]); ok {
			t.Errorf("page view %d of visit %d got no document and has the class %v", at[1], at[0], g)
		}
	}
	// The landings by the parameter they carry are the landings alone: 6 of the 8 documents.
	if g := Total(got.Groups); g.Docs != 6 {
		t.Errorf("%d landings counted by their parameter, want 6", g.Docs)
	}
}

// The plans cannot tell which page views are documents without the trace of a plan, so the count fails
// for a plan whose trace has not its page views, and does not take the landing alone.
func TestExpectFailsForAPlanWithoutTheTraceOfItsPageViews(t *testing.T) {
	plans := []Plan{{ID: 7, Role: RoleShopper, Steps: []PlanStep{{Kind: StepLoad, Page: "/store"}, {Kind: StepClick, Page: "/store/products?sort=newest"}}}}
	w := handWorkload(plans, nil)
	w.Traces = map[int]Trace{7: {Plan: 7, Steps: browserTrace(plans[0]).Steps[:1]}}
	if _, err := w.Expect(handDeps(), nil, expectSpan{0, 1, true}); err == nil || !strings.Contains(err.Error(), "plan 7") {
		t.Errorf("error %v, want one that names the plan", err)
	}
}

// A render in a VM of any document of a visit is split by what the plans say of that document, by the
// visit and the page view: a click that loads a document has a class of its own, and a page view that
// shows the page of the landing again does not take the class of the landing.
func TestARenderInAVMOfADocumentAClickAsksIsSplitByItsClass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(RenderHeader, RenderVM)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	plan := Plan{Steps: []PlanStep{{Kind: StepLoad, Page: "/store"}, {Kind: StepClick, Page: "/store/products?sort=newest"}, {Kind: StepClick, Page: "/store"}, {Kind: StepClick, Page: "/store/products/oak-tray"}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, browserTrace(plan, 1, 2))
	r.expect = &Expectation{human: map[[2]int]docClass{{0, 0}: docNew, {0, 1}: docChanged, {0, 2}: docSame}}
	a := newAcc()
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	// The last click got navigation data, which the plans do not count: the client's own split.
	want := map[string]map[string]int{
		"home":    {RenderFirst: 1, RenderSame: 1},
		"list":    {RenderChanged: 1},
		"product": {RenderFirst: 1},
	}
	if !reflect.DeepEqual(a.vmWhy, want) {
		t.Errorf("renders by why %v, want %v", a.vmWhy, want)
	}
	// The table of the landings counts the landing alone, not the documents the clicks asked for.
	if want := map[string]map[string]int{LandingNone: {RenderVM: 1}}; !reflect.DeepEqual(a.landing, want) {
		t.Errorf("landing documents %v, want %v", a.landing, want)
	}
}

// writeTraces writes the plans of set and, for each name, the trace of every form of every plan as
// trace makes it for that name (the visit of a plan and its variants, which trace gets as plans of
// their own), to a folder that LoadTraces reads.
func writeTraces(t *testing.T, set PlanSet, names []string, trace func(name string, p Plan) Trace) string {
	t.Helper()
	dir := t.TempDir()
	data, _ := json.Marshal(set)
	if err := os.WriteFile(PlansPath(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		var b strings.Builder
		for _, p := range set.Plans {
			for _, tr := range browserForms(p, func(form Plan) Trace { return trace(name, form) }) {
				line, _ := json.Marshal(tr)
				b.Write(line)
				b.WriteString("\n")
			}
		}
		if err := os.WriteFile(TracePath(dir, name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// planWithClicks is the first plan of set with at least n page views.
func planWithClicks(t *testing.T, set PlanSet, role string, n int) Plan {
	t.Helper()
	for _, p := range set.Plans {
		if p.Role == role && len(p.Steps) >= n {
			return p
		}
	}
	t.Fatalf("no %s plan of %d page views", role, n)
	return Plan{}
}

// The plans count a document the browser asked for, and the replay counts an answer it read whole. A
// document the browser cancelled is one and not the other, so a round that plays it could not be set
// beside the plans. No recording of the shop has one, and a set of traces with one is refused where
// it is loaded, with the plan, the page view and the framework. Only documents: Next.js cancels its
// RSC requests and its prefetches, and those load as before.
func TestATraceWithACancelledDocumentIsRefused(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	with := planWithClicks(t, set, RoleShopper, 3)
	names := []string{"tifa", "nextjs"}
	for _, tc := range []struct {
		name   string
		cancel func(tr *Trace)
		ok     bool
	}{
		{"navigation data and a prefetch cancelled", func(tr *Trace) {
			nav := cancelledAfterAnswer(with.Steps[1].Page+"?_rsc=1", "nav", 0, 1, 2)
			pre := cancelled("/store?_rsc=2", "prefetch", 0, 3)
			nav.Headers, pre.Headers = map[string]string{"rsc": "1"}, map[string]string{"rsc": "1"}
			tr.Steps[1].Reqs = append(tr.Steps[1].Reqs, nav, pre)
		}, true},
		{"the document of a click cancelled after its answer began", func(tr *Trace) {
			tr.Steps[1].Reqs = []TraceReq{cancelledAfterAnswer(with.Steps[1].Page, "doc", 0, 1, 2)}
		}, false},
		{"the document of a click cancelled before any answer, beside data that arrived", func(tr *Trace) {
			tr.Steps[1].Reqs = []TraceReq{cancelled(with.Steps[1].Page, "doc", 0, 1), req(with.Steps[1].Page+"?_rsc=1", "nav", 1, 2)}
		}, false},
	} {
		// The second page view of the plan is a document load in both frameworks.
		dir := writeTraces(t, set, names, func(name string, p Plan) Trace {
			if p.ID != with.ID {
				return browserTrace(p)
			}
			tr := browserTrace(p, 1)
			if name == "nextjs" {
				tc.cancel(&tr)
			}
			return tr
		})
		_, _, err := LoadTraces(dir, names)
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case !tc.ok && err == nil:
			t.Errorf("%s: loaded", tc.name)
		case !tc.ok:
			for _, want := range []string{"cancelled", fmt.Sprintf("plan %d", with.ID), "page view 1", "nextjs.jsonl", "record"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s: the error lacks %q: %v", tc.name, want, err)
				}
			}
		}
	}
}

// The documents of a plan are counted from its trace, so a trace is checked against its plan where it
// is loaded, one framework alone too, with nothing of another framework to set it beside: every page
// view is the one of the plan, the landing has one document, a GET of the page of the plan, and no
// page view has more than one document or a document of another page. A trace that is not so would
// change the documents counted, and with them the crawler requests and the expected hit rate, and
// nothing later would tell.
func TestATraceIsCheckedAgainstItsPlanWhenItIsLoaded(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	var with Plan
	for _, p := range set.Plans {
		// A plan that lands with no parameter, which the check of the landing parameters does not look at.
		if p.Role == RoleShopper && len(p.Steps) >= 3 && landingParam(p.Steps[0].Page) == "" {
			with = p
			break
		}
	}
	doc := func(method, url string) TraceReq {
		return TraceReq{Method: method, URL: url, Class: "doc", End: 1, Status: 200}
	}
	for _, tc := range []struct {
		name   string
		change func(tr *Trace)
		want   string // in the error, "" for a trace that loads
	}{
		{"the trace as a browser leaves it", func(tr *Trace) {}, ""},
		{"a click that loads a document", func(tr *Trace) { tr.Steps[1].Reqs = []TraceReq{doc("GET", with.Steps[1].Page)} }, ""},
		{"a click recorded as a load", func(tr *Trace) { tr.Steps[1].Kind = StepLoad }, "page view 1"},
		{"a page view of another page", func(tr *Trace) { tr.Steps[2].Page = "/store/journal" }, "page view 2"},
		{"a landing with no document", func(tr *Trace) { tr.Steps[0].Reqs[0].Class = "nav" }, "landing"},
		{"a landing whose document is another page", func(tr *Trace) { tr.Steps[0].Reqs[0].URL = "/store/journal" }, "page view 0"},
		{"a landing whose document is a POST", func(tr *Trace) { tr.Steps[0].Reqs[0].Method = "POST" }, "page view 0"},
		{"a landing with two documents", func(tr *Trace) { tr.Steps[0].Reqs = append(tr.Steps[0].Reqs, doc("GET", with.Steps[0].Page)) }, "page view 0"},
		{"a click with a document of another page", func(tr *Trace) {
			tr.Steps[1].Reqs = append(tr.Steps[1].Reqs, doc("GET", "/store/journal"))
		}, "page view 1"},
		{"a click with two documents", func(tr *Trace) {
			tr.Steps[2].Reqs = []TraceReq{doc("GET", with.Steps[2].Page), doc("GET", with.Steps[2].Page)}
		}, "page view 2"},
	} {
		dir := writeTraces(t, set, []string{"tifa"}, func(_ string, p Plan) Trace {
			tr := browserTrace(p)
			if p.ID == with.ID {
				tc.change(&tr)
			}
			return tr
		})
		_, _, err := LoadTraces(dir, []string{"tifa"})
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case tc.want != "" && err == nil:
			t.Errorf("%s: loaded", tc.name)
		case tc.want != "":
			for _, want := range []string{tc.want, fmt.Sprintf("plan %d", with.ID), "tifa.jsonl", "record"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s: the error lacks %q: %v", tc.name, want, err)
				}
			}
		}
	}
}

// A run that loaded the traces of one framework has nothing that shows that the frameworks agree on
// which page views are documents. It says so where it starts, and its report says so among the ways it
// is not a formal run.
func TestARunOnTheTracesOfOneFrameworkSaysThatNothingShowsTheyAgree(t *testing.T) {
	if note := oneTraceNote([]string{"tifa", "nextjs", "start", "reactrouter"}); note != "" {
		t.Errorf("four traces: %q", note)
	}
	note := oneTraceNote([]string{"tifa"})
	if !strings.Contains(note, "tifa alone") || !strings.Contains(note, "agree") {
		t.Fatalf("the traces of one framework: %q", note)
	}
	sc := mustScenario("shop")
	o := FormalBrowse
	o.ServerCPUs, o.LoadCPUs = "0", "1,5"
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: slices.Clone(BrowseFrameworks)}, Version: map[string]string{}, Stamp: "from browse.sh",
		Config: browseConfig(o, "AMD EPYC 7B13", "t1", "m1"), Sustain: map[string]*Sustain{}, Traces: []string{"tifa", "nextjs", "start", "reactrouter"}}
	if md := r.Markdown(); strings.Contains(md, "Not a formal run") {
		t.Errorf("a formal run on four traces says it is not one")
	}
	r.Servers[sc.Name], r.Traces = []string{"tifa", "tifa-nocache"}, []string{"tifa"}
	top, _, _ := strings.Cut(r.Markdown(), "### ")
	if !strings.Contains(top, "**Not a formal run.**") || !strings.Contains(top, note) {
		t.Errorf("the top of the report lacks %q:\n%s", note, top)
	}
}

// A plan that searches for a word of the list is recorded in every form, the visit and its variants,
// one line each with the number of the variant. Loading puts the variants with the trace of their
// plan, and refuses a set that lacks one, or in which one is not the recording of its own page views.
func TestTheTracesHoldEveryVariantOfAPlan(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	var with Plan
	for _, p := range set.Plans {
		if len(p.Variants) > 0 && len(p.Steps) > 2 {
			with = p
			break
		}
	}
	names := []string{"tifa", "nextjs"}
	load := func(change func(name string, form Plan, tr *Trace)) error {
		dir := writeTraces(t, set, names, func(name string, form Plan) Trace {
			tr := browserTrace(form)
			if form.ID == with.ID {
				change(name, form, &tr)
			}
			return tr
		})
		_, traces, err := LoadTraces(dir, names)
		if err == nil {
			for _, n := range names {
				tr := traces[n][with.ID]
				if len(tr.Variants) != PlanVariants-1 || tr.Variant != 0 {
					t.Errorf("%s: the trace of plan %d is variant %d and has %d variants", n, with.ID, tr.Variant, len(tr.Variants))
				}
				for v, form := range tr.Variants {
					if form.Variant != v+1 || form.Steps[len(form.Steps)-1].Page != with.Variants[v][len(with.Steps)-1].Page {
						t.Errorf("%s: variant %d of plan %d is not the trace of its page views", n, v+1, with.ID)
					}
				}
			}
		}
		return err
	}
	if err := load(func(string, Plan, *Trace) {}); err != nil {
		t.Fatalf("a set with every variant: %v", err)
	}
	// The variants the cases change, told by their page views. The first one has other pages than the
	// visit (a variant can draw the word of the visit again and be the same visit).
	other := 1 + slices.IndexFunc(with.Variants, func(v []PlanStep) bool { return !slices.Equal(v, with.Steps) })
	if other == 0 {
		t.Fatalf("every variant of plan %d is the visit itself", with.ID)
	}
	isVariant := func(form Plan, v int) bool { return slices.Equal(form.Steps, with.Variants[v-1]) }
	for name, tc := range map[string]struct {
		change func(name string, form Plan, tr *Trace)
		want   []string
	}{
		"a variant recorded with the pages of the visit": {func(name string, form Plan, tr *Trace) {
			if name == "nextjs" && isVariant(form, other) {
				*tr = browserTrace(planForm(with, 0))
			}
		}, []string{"nextjs.jsonl", fmt.Sprintf("plan %d", with.ID), fmt.Sprintf("variant %d", other)}},
		"a variant whose search got navigation data": {func(name string, form Plan, tr *Trace) {
			if name == "tifa" && isVariant(form, 3) {
				at := slices.IndexFunc(form.Steps, func(s PlanStep) bool { return s.Act == ActSearch })
				tr.Steps[at].Reqs[0].Class = "nav"
			}
		}, []string{"tifa.jsonl", fmt.Sprintf("plan %d", with.ID), "variant 3", "search"}},
	} {
		err := load(tc.change)
		if err == nil {
			t.Errorf("%s: loaded", name)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error lacks %q: %v", name, want, err)
			}
		}
	}
	// A set with a variant missing is refused, with what is missing.
	dir := writeTraces(t, set, names, func(_ string, form Plan) Trace { return browserTrace(form) })
	data, _ := os.ReadFile(TracePath(dir, "nextjs"))
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var tr Trace
		json.Unmarshal([]byte(line), &tr)
		if tr.Plan != with.ID || tr.Variant != 4 {
			kept = append(kept, line)
		}
	}
	os.WriteFile(TracePath(dir, "nextjs"), []byte(strings.Join(kept, "\n")+"\n"), 0o644)
	if _, _, err := LoadTraces(dir, names); err == nil || !strings.Contains(err.Error(), "nextjs.jsonl") || !strings.Contains(err.Error(), "record") {
		t.Errorf("a set without a variant of plan %d: %v", with.ID, err)
	}
}

// The act of a page view says what the browser does for it, where the page leaves no choice: a search
// is a plain form and a filter by a link and the next page are plain links, so they load a document,
// and the form of the sidebar and the sort box go through the router, so they load none. A trace in
// which one of them did otherwise is not a recording of these pages, and is refused. A click on a
// link is the page's to decide, and the trace tells.
func TestATraceIsCheckedAgainstTheActsOfItsPlan(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	for _, tc := range []struct {
		act  string
		docs bool // the act loads a document
	}{{ActSearch, true}, {ActFilter, true}, {ActPage, true}, {ActForm, false}, {ActSort, false}} {
		var with Plan
		at := -1
		for _, p := range set.Plans {
			if at = slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Act == tc.act }); at >= 0 && len(p.Variants) == 0 {
				with = p
				break
			}
		}
		if at < 0 {
			t.Fatalf("no plan with a %s and no variant", tc.act)
		}
		dir := writeTraces(t, set, []string{"start"}, func(_ string, form Plan) Trace {
			tr := browserTrace(form)
			if form.ID == with.ID {
				// The other way round: navigation data for an act that loads a document, a document for one
				// the router takes.
				tr.Steps[at].Reqs[0].Class = map[bool]string{true: "nav", false: "doc"}[tc.docs]
			}
			return tr
		})
		_, _, err := LoadTraces(dir, []string{"start"})
		if err == nil {
			t.Errorf("a %s recorded the other way round was loaded", tc.act)
			continue
		}
		for _, want := range []string{fmt.Sprintf("plan %d", with.ID), fmt.Sprintf("page view %d", at), tc.act, "start.jsonl"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error lacks %q: %v", tc.act, want, err)
			}
		}
	}
	// A click on a link may be either.
	link := planWithClicks(t, set, RoleStaff, 3)
	dir := writeTraces(t, set, []string{"start"}, func(_ string, form Plan) Trace {
		if form.ID == link.ID {
			return browserTrace(form, 1)
		}
		return browserTrace(form)
	})
	if _, _, err := LoadTraces(dir, []string{"start"}); err != nil {
		t.Errorf("a click on a link that loads a document: %v", err)
	}
}

// A page the browser shows under a URL that is the same page and is not written as the plan writes it
// is kept by the recorder with the page view, and the traces say how many there are.
func TestTheTracesCountThePagesShownUnderAnotherSpelling(t *testing.T) {
	var tr Trace
	line := `{"plan":3,"variant":2,"steps":[{"kind":"load","page":"/store","reqs":[]},{"kind":"click","page":"/store/products?q=a&sort=newest","shown":"/store/products?sort=newest&q=a","reqs":[]}]}`
	if err := json.Unmarshal([]byte(line), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Variant != 2 || tr.Steps[1].Shown != "/store/products?sort=newest&q=a" || tr.respelled() != 1 {
		t.Errorf("the trace %+v counts %d pages shown under another spelling", tr, tr.respelled())
	}
}
