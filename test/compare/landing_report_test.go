package compare

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The documents of the landings of the storefront are counted by the parameter they carry (a click
// id, an email parameter or none) and by how Tifa answered them, and only the landings of the
// storefront: the documents of the back office are not landings of shoppers.
func TestTheLandingDocumentsAreCountedByTheParameterTheyCarry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		how := "memo"
		if strings.Contains(r.URL.RawQuery, "fbclid") {
			how = "vm"
		}
		if strings.HasPrefix(r.URL.Path, "/projects") {
			how = "vm"
		}
		w.Header().Set("X-Tifa-Render", how)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	doc := func(page string) Trace {
		return Trace{Steps: []TraceStep{{Kind: StepLoad, Page: page, Reqs: []TraceReq{
			{Method: "GET", URL: page, Class: "doc", End: 1, Status: 200},
			{Method: "GET", URL: "/data", Class: "nav", Start: 2, End: 3, Status: 200},
		}}}}
	}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"},
		doc(withLanding("/store/products?sort=newest", "gclid")), doc(withLanding("/store", "fbclid")), doc(withLanding("/zh-CN/store", "_kx")),
		doc("/store/products/oak-tray?x=1"), doc("/projects/studio?x=1"))
	a := newAcc()
	for seq := range 5 {
		if err := visitOnce(r, a, seq); err != nil {
			t.Fatal(err)
		}
	}
	var round Round
	a.round(&round, 0, 0, 0, 0, time.Now(), time.Now(), 0)
	if got := fmt.Sprint(round.Landing); got != "map[click id:map[memo:1 vm:1] email parameter:map[memo:1] no parameter:map[memo:1]]" {
		t.Errorf("the landing documents %s", got)
	}
}

// The appendix says, for each Tifa server, how its landing documents with a click id, with an email
// parameter and with none were answered: they all go to the same page, so the three should be
// answered alike.
func TestTheAppendixSplitsTheLandingHitsOfTifaByTheirParameter(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs", "tifa-nocache"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"}}
	for round := 1; round <= 2; round++ {
		x := closedRound("tifa", sc.Name, 8, round, 4000, 10, true)
		x.PageRenders = map[string]map[string]int{"product": {"vm": 30, "memo": 60, "shared": 10}}
		x.Landing = map[string]map[string]int{"click id": {"memo": 30, "vm": 6, "shared": 4}, "email parameter": {"memo": 9, "vm": 1}, "no parameter": {"memo": 100, "vm": 20}}
		r.Closed = append(r.Closed, x, closedRound("nextjs", sc.Name, 8, round, 400, 10, true))
	}
	body, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	for _, want := range []string{
		"| | landing documents | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo | memo and shared, measured |",
		// The measured share is the answers the memo gave or shared, as in the table by kind of page.
		"| Tifa | with a click id | 80 | 60 | 8 | 12 | - | 85.0% of 80 |",
		"| Tifa | with an email parameter | 20 | 18 | 0 | 2 | - | 90.0% of 20 |",
		"| Tifa | with no parameter | 240 | 200 | 0 | 40 | - | 83.3% of 240 |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
		if strings.Contains(body, want) {
			t.Errorf("the body has %q", want)
		}
	}
	if strings.Contains(appendix, "| Next.js | with") {
		t.Error("the split has a row for Next.js, which says nothing of its renders")
	}
}
