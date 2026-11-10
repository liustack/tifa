package compare

import (
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

// Every played visit that lands with a parameter has a value of its own: the same plan played in
// another cycle, or in the warm-up after the visits of the round, gets another one, no two visits of
// the visits played have the same, and a value has the length and the characters of the real ones.
func TestEachPlayedVisitHasItsOwnClickId(t *testing.T) {
	w := siteWorkload(t, 2, 2, "shop")
	cycle := w.cycleLen()
	type key struct{ name, value string }
	seen := map[key]int{}
	withParam := 0
	// Three cycles of the round, and as many of the warm-up after the visits prepared for it.
	for _, from := range []int{0, 4 * cycle} {
		for seq := from; seq < from+3*cycle; seq++ {
			p := w.planFor(seq)
			sub := landingFor(w.Seed, seq, p)
			name := landingParam(p.Steps[0].Page)
			if name == "" {
				if sub.placeholder != "" || sub.value != "" {
					t.Fatalf("visit %d lands with no parameter and has the value %q", seq, sub.value)
				}
				continue
			}
			withParam++
			v, _ := landingValueOf(name)
			if sub.placeholder != v.Placeholder || len(sub.value) != LandingValueLengths[name] || sub.value == sub.placeholder {
				t.Fatalf("visit %d: %s has the value %q for the placeholder %q", seq, name, sub.value, sub.placeholder)
			}
			if strings.Trim(sub.value, v.Alphabet) != "" {
				t.Fatalf("visit %d: the value %q of %s has characters outside its alphabet", seq, sub.value, name)
			}
			k := key{name, sub.value}
			if other, ok := seen[k]; ok {
				t.Fatalf("visits %d and %d both have %s=%s", other, seq, name, sub.value)
			}
			seen[k] = seq
			if again := landingFor(w.Seed, seq, p); again != sub {
				t.Fatalf("visit %d has another value when asked again", seq)
			}
			if other := landingFor(w.Seed+1, seq, p); other.value == sub.value {
				t.Fatalf("visit %d has the same value under another seed", seq)
			}
		}
	}
	if withParam < 6*200 {
		t.Fatalf("%d visits of the six cycles carry a parameter", withParam)
	}
}

// The replay puts the value of the visit in place of the placeholder in every request of the visit
// (the URL, the body and the headers, where a router carries the URL of the page), and no placeholder
// reaches the server. Another visit of the same plan sends another value.
func TestTheReplayPutsTheValueOfTheVisitInEveryRequestOfItsVisit(t *testing.T) {
	ph := placeholderOf("tifa-gclid-", 95)
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.String()+"|"+r.Header.Get("next-router-state-tree"))
		mu.Unlock()
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	land := "/store/products?sort=newest&gclid=" + ph
	load := TraceStep{Kind: StepLoad, Page: land, Reqs: []TraceReq{
		{Method: "GET", URL: land, Class: "doc", End: 1, Status: 200},
	}}
	click := TraceStep{Kind: StepClick, Page: "/store", Reqs: []TraceReq{
		{Method: "GET", URL: "/store?_rsc=abc", Class: "nav", Start: 0, End: 1, Status: 200, Headers: map[string]string{"rsc": "1", "next-router-state-tree": "%5B%22__PAGE__%3F%7B%22gclid%22%3A%22" + ph + "%22%7D"}},
		{Method: "GET", URL: "/_serverFn/x?payload=%7B%22gclid%22%3A%22" + ph + "%22%7D", Class: "nav", Start: 2, End: 3, Status: 200, Headers: map[string]string{"x-tsr-serverfn": "true"}},
	}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, Trace{Steps: []TraceStep{load, click}})
	var all [2][]string
	for seq := range 2 {
		mu.Lock()
		got = nil
		mu.Unlock()
		if err := visitOnce(r, newAcc(), seq); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		all[seq] = append([]string(nil), got...)
		mu.Unlock()
		if len(all[seq]) != 3 {
			t.Fatalf("visit %d sent %d requests: %v", seq, len(all[seq]), all[seq])
		}
		sub := landingFor(r.seed, seq, r.wl.planFor(seq))
		if sub.value == "" {
			t.Fatalf("visit %d has no value", seq)
		}
		for _, line := range all[seq] {
			if strings.Contains(line, "tifa-gclid") {
				t.Errorf("visit %d sent a placeholder: %s", seq, line)
			}
			if !strings.Contains(line, sub.value) {
				t.Errorf("visit %d: %s lacks its value %s", seq, line, sub.value)
			}
		}
		if !strings.HasPrefix(all[seq][0], "/store/products?sort=newest&gclid="+sub.value+"|") {
			t.Errorf("visit %d: the document is %s", seq, all[seq][0])
		}
	}
	if strings.Contains(all[1][0], strings.Split(all[0][0], "gclid=")[1][:20]) {
		t.Errorf("two visits of the same plan sent the same value: %s and %s", all[0][0], all[1][0])
	}
}

// A set of traces in which a plan lands with a parameter and its document request lacks it is
// refused, as one that lost a header of its router is: the visit would not be the one planned.
func TestATraceThatLostItsLandingParameterIsRefused(t *testing.T) {
	dir := t.TempDir()
	set := GenerateSitePlanSet(1, 1)
	data, _ := json.Marshal(set)
	os.WriteFile(PlansPath(dir), data, 0o644)
	lost := -1
	write := func(drop bool) {
		var b strings.Builder
		for _, p := range set.Plans {
			if drop && lost < 0 && landingParam(p.Steps[0].Page) != "" {
				lost = p.ID
			}
			for _, tr := range browserForms(p, func(form Plan) Trace { return browserTrace(form) }) {
				if drop && p.ID == lost {
					tr.Steps[0].Reqs[0].URL = stripLanding(tr.Steps[0].Reqs[0].URL)
				}
				line, _ := json.Marshal(tr)
				b.Write(line)
				b.WriteString("\n")
			}
		}
		os.WriteFile(filepath.Join(dir, "tifa.jsonl"), []byte(b.String()), 0o644)
	}
	write(false)
	if _, _, err := LoadTraces(dir, []string{"tifa"}); err != nil {
		t.Fatalf("a whole set: %v", err)
	}
	write(true)
	if _, _, err := LoadTraces(dir, []string{"tifa"}); err == nil || !strings.Contains(err.Error(), "landing parameter") || !strings.Contains(err.Error(), "tifa.jsonl") {
		t.Errorf("a set with a document that lost its parameter: %v", err)
	}
}

// A landing parameter is no part of the page a server answers: the split of the renders of Tifa by
// why counts a URL as answered before whatever click id its visit carried, since the visits of a
// page all ask for the URL of their own.
func TestALandingParameterIsNoPartOfTheURLTheRendersAreSplitBy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tifa-Render", "vm")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	page := func(name string) Trace {
		land := "/store/products?sort=newest"
		if name != "" {
			land = withLanding(land, name)
		}
		return Trace{Steps: []TraceStep{{Kind: StepLoad, Page: land, Reqs: []TraceReq{{Method: "GET", URL: land, Class: "doc", End: 1, Status: 200}}}}}
	}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, page(""), page("gclid"), page("fbclid"))
	a := newAcc()
	for seq := range 3 {
		if err := visitOnce(r, a, seq); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.vmWhy[StoreKindList]; got[RenderFirst] != 1 || got[RenderAgain] != 2 {
		t.Errorf("the renders of the list by why: %v, want one first and two again", got)
	}
}

// The landing parameter is checked as the server reads it, by its whole key and its whole value: a
// key with another name, a value with something before or after the placeholder, a key given twice
// and another parameter beside it are all refused, as is a document of another page.
func TestTheLandingParameterIsCheckedByItsWholeKeyAndValue(t *testing.T) {
	ph := placeholderOf("tifa-gclid-", LandingValueLengths["gclid"])
	page := "/store/products?sort=newest&gclid=" + ph
	plan := Plan{ID: 7, Steps: []PlanStep{{Kind: StepLoad, Page: page}}}
	trace := func(url string) Trace {
		return Trace{Steps: []TraceStep{{Kind: StepLoad, Page: page, Reqs: []TraceReq{{Method: "GET", URL: url, Class: "doc"}}}}}
	}
	for url, ok := range map[string]bool{
		page: true,
		"/store/products?gclid=" + ph + "&sort=newest":             true,
		"/store/products?sort=newest&not_gclid=" + ph:              false,
		"/store/products?sort=newest&xgclid=" + ph:                 false,
		"/store/products?sort=newest&gclid=" + ph + "x":            false,
		"/store/products?sort=newest&gclid=x" + ph:                 false,
		"/store/products?sort=newest&gclid=other":                  false,
		"/store/products?sort=newest&gclid=" + ph + "&gclid=" + ph: false,
		"/store/products?sort=newest&gclid=other&gclid=" + ph:      false,
		"/store/products?sort=newest&gclid=" + ph + "&q=cup":       false,
		"/store/products?gclid=" + ph:                              false,
		"/store/products/oak-tray?sort=newest&gclid=" + ph:         false,
		"/store/products?sort=newest":                              false,
	} {
		err := checkLanding(plan, trace(url))
		switch {
		case ok && err != nil:
			t.Errorf("%s was refused: %v", url, err)
		case !ok && (err == nil || !strings.Contains(err.Error(), "landing parameter")):
			t.Errorf("%s was accepted or refused with the wrong error: %v", url, err)
		}
	}
	// Only a document counts: a data request with the parameter does not stand for it.
	data := trace(page)
	data.Steps[0].Reqs[0].Class = "nav"
	if checkLanding(plan, data) == nil {
		t.Error("a request that is not a document was taken for the landing document")
	}
}

// Plans made before the landing parameters, or with the wrong number of them in a block, are refused
// where they are loaded (by `compare browse` and by the recorder too), whatever the plans say of
// themselves: each block must have the quota of the mix, and the error says to make the plans again
// and record the traces again.
func TestPlansWithoutTheLandingParametersOfTheMixAreRefusedWhenLoaded(t *testing.T) {
	good := GenerateSitePlanSet(1, 2)
	write := func(set PlanSet) error {
		dir := t.TempDir()
		data, _ := json.Marshal(set)
		os.WriteFile(PlansPath(dir), data, 0o644)
		_, err := ReadPlans(dir)
		return err
	}
	if err := write(good); err != nil {
		t.Fatalf("plans with the mix: %v", err)
	}
	strip := func(keep func(blk int, p Plan) bool) PlanSet {
		set := good
		set.Plans = make([]Plan, len(good.Plans))
		for i, p := range good.Plans {
			p.Steps = slices.Clone(p.Steps)
			if !keep(p.Block, p) {
				p.Steps[0].Page = stripLanding(p.Steps[0].Page)
			}
			set.Plans[i] = p
		}
		return set
	}
	for name, set := range map[string]PlanSet{
		"plans of before the parameters": strip(func(int, Plan) bool { return false }),
		"no email parameter in block 1": strip(func(blk int, p Plan) bool {
			name := landingParam(p.Steps[0].Page)
			return blk == 0 || !slices.ContainsFunc(EmailParamNames, func(e struct {
				Name  string
				Share float64
			}) bool {
				return e.Name == name
			})
		}),
	} {
		err := write(set)
		if err == nil || !strings.Contains(err.Error(), "make the plans again") || !strings.Contains(err.Error(), "record") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A visit whose search is for a word of its own is recorded with the placeholder of the plans, and the
// replay puts the word of the visit in its place in every request of the visit, as it does for a
// landing parameter: in the URL of the search and of what follows, in the headers where a router
// carries the URL of the page, and in a body. The word is letters and digits, written the same in
// every encoding, so it is put in as text. No placeholder reaches the server, the word is the one of
// the seed and the visit, and a visit that also lands with a click id sends both its values.
func TestTheReplayPutsTheWordOfTheVisitInEveryRequestOfItsVisit(t *testing.T) {
	click := placeholderOf("tifa-gclid-", 95)
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.URL.String()+"|"+r.Header.Get("next-router-state-tree")+"|"+string(body))
		mu.Unlock()
		w.Header().Set(RenderHeader, RenderVM)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	land, list := "/store?gclid="+click, "/store/products?q="+SearchPlaceholder
	load := TraceStep{Kind: StepLoad, Page: land, Reqs: []TraceReq{{Method: "GET", URL: land, Class: "doc", End: 1, Status: 200}}}
	search := TraceStep{Kind: StepClick, Page: list, Reqs: []TraceReq{
		{Method: "GET", URL: list, Class: "doc", End: 1, Status: 200},
		{Method: "GET", URL: "/store/products.data?q=" + SearchPlaceholder, Class: "nav", Start: 2, End: 3, Status: 200},
	}}
	after := TraceStep{Kind: StepClick, Page: "/store/products/oak-tray", Reqs: []TraceReq{
		{Method: "GET", URL: "/store/products/oak-tray?_rsc=abc", Class: "nav", End: 1, Status: 200, Headers: map[string]string{"rsc": "1", "next-router-state-tree": "%5B%22products%22%2C%7B%22children%22%3A%5B%22__PAGE__%3F%7B%5C%22q%5C%22%3A%5C%22" + SearchPlaceholder + "%5C%22%7D%22"}},
		{Method: "GET", URL: "/_serverFn/x?payload=%7B%22q%22%3A%7B%22t%22%3A1%2C%22s%22%3A%22" + SearchPlaceholder + "%22%7D%7D", Class: "nav", Start: 2, End: 3, Status: 200, Headers: map[string]string{"x-tsr-serverfn": "true"}},
		{Method: "POST", URL: "/store/mine", Class: "nav", Start: 4, End: 5, Status: 200, Body: `{"from":"/store/products?q=` + SearchPlaceholder + `"}`},
	}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, Trace{Steps: []TraceStep{load, search, after}})
	r.wl.Plans[0].Steps[1].Act, r.wl.Plans[0].Steps[1].Term = ActSearch, SearchPlaceholder
	words := map[string]bool{}
	a := newAcc()
	for seq := range 3 {
		mu.Lock()
		got = nil
		mu.Unlock()
		if err := visitOnce(r, a, seq); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		sent := append([]string(nil), got...)
		mu.Unlock()
		word := uniqueTerm(r.seed, seq)
		words[word] = true
		if len(sent) != 6 {
			t.Fatalf("visit %d sent %d requests: %v", seq, len(sent), sent)
		}
		for i, line := range sent {
			if strings.Contains(line, SearchPlaceholder) || strings.Contains(line, "tifa-gclid") {
				t.Errorf("visit %d sent a placeholder: %s", seq, line)
			}
			if i > 0 && !strings.Contains(line, word) {
				t.Errorf("visit %d: %s lacks its word %s", seq, line, word)
			}
		}
		if !slices.ContainsFunc(sent, func(line string) bool { return strings.HasPrefix(line, "/store/products?q="+word+"|") }) {
			t.Errorf("visit %d did not ask for the list of its word %s: %v", seq, word, sent)
		}
		if value := landingFor(r.seed, seq, r.wl.planFor(seq)).value; !strings.HasPrefix(sent[0], "/store?gclid="+value+"|") {
			t.Errorf("visit %d landed on %s, want its click id %s", seq, sent[0], value)
		}
	}
	// The list of the word of a visit is a page of its own: the client's count of the pages Tifa had
	// answered before takes the two requests of each of the three visits for pages it had not.
	if got := a.vmWhy[StoreKindList]; got[RenderFirst] != 6 || len(got) != 1 {
		t.Errorf("the lists of the words of three visits were rendered for %v, want six pages not answered before", got)
	}
	if len(words) != 3 {
		t.Errorf("three visits searched for %d words", len(words))
	}
}

// A set of traces in which a visit that searches for a word of its own was recorded with another word
// than the placeholder is refused: the replay would have nothing to put the word of the visit in
// place of.
func TestATraceThatLostItsSearchPlaceholderIsRefused(t *testing.T) {
	set := GenerateSitePlanSet(1, 1)
	var with Plan
	at := -1
	for _, p := range set.Plans {
		if at = slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Term == SearchPlaceholder }); at >= 0 {
			with = p
			break
		}
	}
	dir := writeTraces(t, set, []string{"reactrouter"}, func(_ string, form Plan) Trace {
		tr := browserTrace(form)
		if form.ID == with.ID {
			tr.Steps[at].Reqs[0].URL = strings.Replace(tr.Steps[at].Reqs[0].URL, SearchPlaceholder, "kettle", 1)
		}
		return tr
	})
	_, _, err := LoadTraces(dir, []string{"reactrouter"})
	if err == nil {
		t.Fatal("a trace recorded with another word than the placeholder was loaded")
	}
	for _, want := range []string{fmt.Sprintf("plan %d", with.ID), fmt.Sprintf("page view %d", at), SearchPlaceholder, "reactrouter.jsonl"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
}
