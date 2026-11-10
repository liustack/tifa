package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A render in a VM of the landing document of a visit is split by what the plans say of it: a page not
// seen before, one seen with other data, and one seen with the same data, which the memo should have
// held.
func TestTheRendersInAVMAreSplitByTheClassOfTheirDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(RenderHeader, RenderVM)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	doc := func(page string) Trace {
		return Trace{Steps: []TraceStep{{Kind: StepLoad, Page: page, Reqs: []TraceReq{{Method: "GET", URL: page, Class: "doc", End: 1, Status: 200}}}}}
	}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, doc("/store"), doc("/store"), doc("/store/products/oak-tray"), doc("/store"))
	r.expect = &Expectation{human: map[int]docClass{0: docNew, 1: docChanged, 2: docSame}}
	a := newAcc()
	for seq := range 4 {
		if err := visitOnce(r, a, seq); err != nil {
			t.Fatal(err)
		}
	}
	// Visit 3 is not in the expectation (it was not counted, as a warm-up's): the client's own split.
	want := map[string]map[string]int{
		"home":    {RenderFirst: 1, RenderChanged: 1, RenderAgain: 1},
		"product": {RenderSame: 1},
	}
	if !reflect.DeepEqual(a.vmWhy, want) {
		t.Errorf("renders by why %v, want %v", a.vmWhy, want)
	}
}

// A crawler request rendered in a VM is split the same way, by its number in the stream of crawler
// requests.
func TestACrawlerRenderInAVMIsSplitByTheClassOfItsDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(RenderHeader, RenderVM)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	wl := siteWorkload(t, 2, 1, "shop")
	r := &runner{base: srv.URL, wl: wl, seed: 2, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }}
	n := 0
	for wl.crawlOf(n).Old {
		n++
	}
	r.expect = &Expectation{crawl: map[int]docClass{n: docChanged}}
	a := newAcc()
	r.runCrawl(context.Background(), http.DefaultTransport.(*http.Transport), a, n, time.Now())
	key := crawlKey(wl.crawlOf(n))
	if want := map[string]map[string]int{key: {RenderChanged: 1}}; !reflect.DeepEqual(a.crawl.why, want) {
		t.Errorf("crawler renders by why %v, want %v", a.crawl.why, want)
	}
}

// A round of Tifa carries the hit rate its plans allow: the warm-up shows a page, the round asks for it.
func TestARoundOfTifaCarriesTheHitRateItsPlansAllow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.Write([]byte("tifa_memo_entries 0\ntifa_memo_hits_total 0\ntifa_memo_misses_total 0\ntifa_memo_shared_total 0\ntifa_memo_bytes 0\ntifa_memo_evictions_total 0\n"))
			return
		}
		w.Header().Set(RenderHeader, RenderHit)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	doc := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/store", Reqs: []TraceReq{{Method: "GET", URL: "/store", Class: "doc", End: 1, Status: 200}}}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, doc, doc)
	r.metrics, r.deps = "/metrics", handDeps()
	r.prepared, r.warm = 1, 1
	round := r.RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.ExpectedError != "" || round.Expected == nil {
		t.Fatalf("no expected hit rate: %q", round.ExpectedError)
	}
	if c := round.Expected.Humans["home"]; c != (ExpectedCell{Docs: 1, Hits: 1}) {
		t.Errorf("expected %+v, want the one document of the round, a hit of the page of the warm-up", c)
	}
	// A server with no metrics route (not Tifa) has none.
	r2 := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, doc, doc)
	r2.deps = handDeps()
	if round := r2.RunClosed(context.Background(), 1, 1, 10*time.Second); round.Expected != nil {
		t.Error("a server that is not Tifa has an expected hit rate")
	}
}

// expectedRound is a round of Tifa with the documents of a kind of page counted by the plans and by the
// server.
func expectedRound(framework string, kind string, docs, hits int, vm, memo, shared int, why map[string]int) Round {
	sc := mustScenario("shop")
	x := closedRound(framework, sc.Name, 8, 1, 4000, 10, true)
	x.PageRenders = map[string]map[string]int{kind: {"vm": vm, "memo": memo, "shared": shared}}
	x.VMWhy = map[string]map[string]int{kind: why}
	x.Expected = &Expected{Humans: map[string]ExpectedCell{kind: {Docs: docs, Hits: hits, Changed: docs - hits}}}
	x.Memo = &MemoRound{}
	return x
}

// The appendix sets, for each kind of page, what the plans allow the memo to answer beside what it
// answered, and splits the renders in a VM into a URL not seen, one seen with other data, and one seen
// with the same data, which the memo should have held.
func TestTheAppendixSetsTheExpectedHitRateBesideTifas(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "tifa-nocache"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"}}
	for i := range 2 {
		a := expectedRound("tifa", "product", 100, 70, 30, 60, 10, map[string]int{RenderFirst: 20, RenderChanged: 6, RenderSame: 4})
		a.Memo.End.Evictions = int64(3 * (i + 1))
		b := expectedRound("tifa", "home", 120, 108, 60, 40, 20, map[string]int{RenderFirst: 5, RenderSame: 55})
		b.PageRenders["product"] = a.PageRenders["product"]
		b.VMWhy["product"] = a.VMWhy["product"]
		b.Expected.Humans["product"] = a.Expected.Humans["product"]
		b.Memo = a.Memo
		c := expectedRound("tifa-nocache", "product", 100, 70, 100, 0, 0, map[string]int{RenderFirst: 30, RenderChanged: 20, RenderSame: 50})
		r.Closed = append(r.Closed, b, c)
	}
	body, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	for _, want := range []string{
		"| | kind of page | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo | memo and shared, measured | VM: URL not seen before | VM: URL seen, data changed | VM: URL seen, data the same | private page, no cart cookie | private page, cart cookie |",
		// Over two rounds: the plans allow 140 of 200 documents, the memo and shared answered 140.
		"| Tifa | product | 200 | 120 | 20 | 60 | 70.0% of 200 (the memo dropped 9 pages) | 70.0% of 200 | 40 | 12 | 8 | - | - |",
		// The memo answered a third of the documents of the home that the plans allow it to answer.
		"| Tifa | home | 240 | 80 | 40 | 120 | 90.0% of 240 (the memo dropped 9 pages) | 50.0% of 240 | 10 | 0 | 110 | - | - |",
		// With the memo off nothing is answered from it, and the plans allow what they allow.
		"| Tifa, memo off (reference) | product | 200 | 0 | 0 | 200 | 70.0% of 200 | 0.0% of 200 | 60 | 40 | 100 | - | - |",
		// The home is 40 points under what is allowed with 240 documents: a red flag. The product is not.
		"**Red flag**: Tifa, home: the memo and shared renders answered 50.0% of 240 documents, the plans allow 90.0%.",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
		if strings.Contains(body, want) {
			t.Errorf("the body has %q", want)
		}
	}
	if strings.Contains(appendix, "Red flag**: Tifa, product") || strings.Contains(appendix, "Red flag**: Tifa, memo off") {
		t.Errorf("a red flag where the memo is within 5 points or off:\n%s", appendix)
	}
	// A kind with 100 documents or fewer is no red flag, however far under.
	for i := range r.Closed {
		if r.Closed[i].Framework == "tifa" {
			x := r.Closed[i].Expected.Humans["home"]
			x.Docs, x.Hits = 40, 40
			r.Closed[i].Expected.Humans["home"] = x
			r.Closed[i].PageRenders["home"] = map[string]int{"vm": 40}
		}
	}
	if md := r.Markdown(); strings.Contains(md, "Red flag**: Tifa, home") {
		t.Error("a red flag for a kind of 80 documents")
	}
}

// The landing groups carry the expected rate too, since a landing parameter is no part of the page.
func TestTheLandingTableSetsTheExpectedHitRateBesideTifas(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"}}
	x := closedRound("tifa", sc.Name, 8, 1, 4000, 10, true)
	x.PageRenders = map[string]map[string]int{"product": {"vm": 30, "memo": 60, "shared": 10}}
	x.Landing = map[string]map[string]int{LandingClickID: {"memo": 30, "vm": 10}}
	x.Expected = &Expected{Groups: map[string]ExpectedCell{LandingClickID: {Docs: 40, Hits: 36}}}
	r.Closed = append(r.Closed, x)
	_, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	if want := "| Tifa | with a click id | 40 | 30 (75.0%) | 0 | 10 | 90.0% of 40 |"; !strings.Contains(appendix, want) {
		t.Errorf("the appendix lacks %q:\n%s", want, appendix)
	}
}

// The crawler table sets what the plans allow beside how Tifa answered the crawler requests, by kind of
// URL and for U as a whole, and splits the renders in a VM by the class of the document.
func TestTheCrawlerTableSetsTheExpectedHitRateBesideTifas(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"}}
	for round := 1; round <= 2; round++ {
		x := crawled(closedRound("tifa", sc.Name, 8, round, 4000, 10, true))
		x.Crawler.Why = map[string]map[string]int{"U|en-US|product": {RenderFirst: 2, RenderChanged: 1, RenderSame: 5}}
		x.Expected = &Expected{Crawlers: map[string]ExpectedCell{"product": {Docs: 40, Hits: 35, Changed: 3, New: 2}, "list": {Docs: 8, Hits: 2, New: 6}}, Old: 5}
		r.Closed = append(r.Closed, x)
	}
	_, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	for _, want := range []string{
		"| | answers by | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo | VM: URL not seen before | VM: URL seen, data changed | VM: URL seen, data the same |",
		"| Tifa | product | 90 | 60 (66.7%) | 4 | 26 | 87.5% of 80 | 4 | 2 | 10 |",
		"| Tifa | list | 16 | 12 (75.0%) | 0 | 4 | 25.0% of 16 | 0 | 0 | 0 |",
		// U as a whole: the kinds the plans count, 96 documents of the crawlers in all.
		"| Tifa | U | 184 | 120 (65.2%) | 4 | 60 | 77.1% of 96 | 4 | 2 | 10 |",
		"| Tifa | zh-CN | 98 | 48 (49.0%) | 0 | 50 | - | 0 | 0 | 0 |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
	}
}
