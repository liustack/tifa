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

// browserTrace is the trace of a plan as a browser with a client router leaves it: the landing is a
// document and a click gets navigation data, but for the page views of docClicks, which the page does
// as a document load (a plain link, a form with no router).
func browserTrace(p Plan, docClicks ...int) Trace {
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

// browserTraces are the traces of plans, each as browserTrace makes it.
func browserTraces(plans []Plan) map[int]Trace {
	out := map[int]Trace{}
	for _, p := range plans {
		out[p.ID] = browserTrace(p)
	}
	return out
}

// The pages are the same source in every framework, so a page view that is a document in one is a
// document in all four. The plans count the documents from the traces, and a set of traces that does
// not agree on them is refused, with the plan, the page view and the framework that differs.
func TestTheFrameworksAgreeOnWhichPageViewsAreDocuments(t *testing.T) {
	dir := t.TempDir()
	set := GenerateSitePlanSet(1, 1)
	data, _ := json.Marshal(set)
	os.WriteFile(PlansPath(dir), data, 0o644)
	// A plan with a click: in the browser its second page view was a document load.
	var with Plan
	for _, p := range set.Plans {
		if len(p.Steps) > 2 {
			with = p
			break
		}
	}
	names := []string{"tifa", "nextjs", "start", "reactrouter"}
	write := func(name string, docClicks ...int) {
		var b strings.Builder
		for _, p := range set.Plans {
			tr := browserTrace(p)
			if p.ID == with.ID {
				tr = browserTrace(p, docClicks...)
			}
			line, _ := json.Marshal(tr)
			b.Write(line)
			b.WriteString("\n")
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
		{ID: 0, Role: RoleShopper, Steps: []PlanStep{{StepLoad, "/store"}, {StepClick, list}}}, // the home is new, and so is the list, which the click loads as a document
		landing(1, list), // a hit: the click of visit 0 made the page seen
		{ID: 2, Role: RoleShopper, Steps: []PlanStep{{StepLoad, "/store/products/oak-tray"}, {StepClick, pen + "?reviews=2"}}}, // the click gets data: no document, and the page is not seen
		landing(3, pen+"?reviews=2"), // new
		{ID: 4, Role: RoleShopper, Steps: []PlanStep{{StepLoad, pen}, {StepClick, "/store/checkout"}, {StepClick, "/store"}}}, // the pen is new, the visit checks out, and then loads the home as a document: the kettle of its cart changed
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
	plans := []Plan{{ID: 7, Role: RoleShopper, Steps: []PlanStep{{StepLoad, "/store"}, {StepClick, "/store/products?sort=newest"}}}}
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
	plan := Plan{Steps: []PlanStep{{StepLoad, "/store"}, {StepClick, "/store/products?sort=newest"}, {StepClick, "/store"}, {StepClick, "/store/products/oak-tray"}}}
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

// writeTraces writes the plans of set and, for each name, the trace of every plan as trace makes it for
// that name, to a folder that LoadTraces reads.
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
			line, _ := json.Marshal(trace(name, p))
			b.Write(line)
			b.WriteString("\n")
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
