package compare

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// TraceReq is one request the browser sent to the server. Start and End are milliseconds from the
// start of its step. Class is "doc" (a document), "nav" (the data a client navigation fetches, and
// route discovery) or "prefetch" (what the framework fetches ahead of a click). INM says that the
// browser sent If-None-Match, whose value the replay takes from the answers of its own visit to the
// same URL.
//
// A request that the browser did not let finish has Cancel set to why (the network error the
// browser reported, such as net::ERR_ABORTED, or "open-at-end" for one still open when the page
// view was over), and End is when it was cancelled. AfterNav says that a navigation of the page
// was committed within 200 ms before. Responded is when the headers of the answer arrived, if they
// did. The replay cancels such a request at that offset from the start of its step, releasing the
// connection without reading the rest of the answer.
type TraceReq struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	INM       bool              `json:"inm"`
	Start     float64           `json:"s"`
	End       float64           `json:"e"`
	Status    int               `json:"status"`
	Class     string            `json:"class"`
	Cancel    string            `json:"cancel,omitempty"`
	AfterNav  bool              `json:"afterNav,omitempty"`
	Responded float64           `json:"responded,omitempty"`
	// Body is what the browser sent with a POST or PATCH (the storefront remembers the product a visitor
	// looked at with a POST of its own), empty for a request with none.
	Body string `json:"body,omitempty"`
}

// TraceStep is one page view of a visit and the requests it made. Page is the page of the plan. Shown
// is the URL the browser showed when that is the same page written otherwise (another order of the
// parameters, another escape), and is empty when it is the URL of the plan to the letter.
type TraceStep struct {
	Kind  string     `json:"kind"`
	Page  string     `json:"page"`
	Shown string     `json:"shown,omitempty"`
	Reqs  []TraceReq `json:"reqs"`
}

// Trace is what a browser sent in one visit of a plan. Error is set when the browser could not play
// the plan, and such a trace is left out. A plan that searches for a word of the list is recorded in
// every form (Plan.Variants): Variant says which form a recorded visit is, 0 for the visit of the plan
// and n for its n-th variant, each a line of the file. Variants are the traces of the variants of the
// plan, in order, which LoadTraces puts with the trace of the visit of the plan.
type Trace struct {
	Plan     int         `json:"plan"`
	Variant  int         `json:"variant,omitempty"`
	Steps    []TraceStep `json:"steps"`
	Error    string      `json:"error,omitempty"`
	Variants []Trace     `json:"-"`
}

// form is the trace of form v of the plan: the visit of the plan for 0, its v-th variant otherwise.
func (t Trace) form(v int) Trace {
	if v == 0 {
		return t
	}
	return t.Variants[v-1]
}

// respelled is how many page views of the visit the browser showed under a URL that is the page of the
// plan and is not written as the plan writes it.
func (t Trace) respelled() int {
	n := 0
	for _, s := range t.Steps {
		if s.Shown != "" {
			n++
		}
	}
	return n
}

// ReadTraces reads the file the recorder wrote (one JSON line a visit) and drops the visits that
// failed. It also returns how many it dropped.
func ReadTraces(path string) (traces []Trace, dropped int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var t Trace
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", path, err)
		}
		if t.Error != "" {
			dropped++
			continue
		}
		for _, s := range t.Steps {
			if err := checkStep(s); err != nil {
				return nil, 0, fmt.Errorf("%s: plan %d: %w", path, t.Plan, err)
			}
		}
		traces = append(traces, t)
	}
	return traces, dropped, sc.Err()
}

func checkStep(s TraceStep) error {
	foreground := 0
	for _, r := range s.Reqs {
		if err := checkRouterHeaders(r); err != nil {
			return err
		}
		if tifaPreload.MatchString(r.URL) && r.Class != "prefetch" {
			return fmt.Errorf("%s %s is a preload of Tifa's router and has the class %s, not prefetch: record the traces again", r.Method, r.URL, r.Class)
		}
		if r.Status >= 400 {
			return fmt.Errorf("%s %s answered %d", r.Method, r.URL, r.Status)
		}
		if r.Status == 0 && r.Cancel == "" {
			return fmt.Errorf("%s %s has no status and was not cancelled", r.Method, r.URL)
		}
		if r.End < r.Start {
			return fmt.Errorf("%s %s ends before it starts", r.Method, r.URL)
		}
		if r.Class != "prefetch" && (r.Cancel == "" || r.Responded > 0 && r.Responded <= r.End) {
			foreground++ // finished, or cancelled after its answer had arrived
		}
	}
	if foreground == 0 {
		return fmt.Errorf("the page view of %s has no foreground request that finished", s.Page)
	}
	return nil
}

// checkRouterHeaders fails a request that the router of a framework made with a header that the
// recorded request lacks. Without it the server answers with a whole page, which a browser never
// gets: Next.js asks for RSC with `rsc: 1` on a URL with `_rsc=`, Start calls a server function
// with `x-tsr-serverfn`.
func checkRouterHeaders(r TraceReq) error {
	switch {
	case strings.Contains(r.URL, "_rsc=") && r.Headers["rsc"] == "":
		return fmt.Errorf("%s %s lost its rsc header", r.Method, r.URL)
	case strings.Contains(r.URL, "/_serverFn/") && r.Headers["x-tsr-serverfn"] == "":
		return fmt.Errorf("%s %s lost its x-tsr-serverfn header", r.Method, r.URL)
	}
	return nil
}

// tifaPreload is a data request of Tifa's router that only preloads: .p after the digits of its _tifa
// parameter (js/runtime/data.ts). The router fetches it ahead of a click, for the link under a pointer,
// so it is a prefetch: the recorder gives it that class (preloadOfTifa in record.mjs), as it does a
// prefetch of Next.js, and the replay sends it in the background of its page view. TanStack Start
// marks its preloads in no way, so one of its preloads cannot be told from a call of the page.
var tifaPreload = regexp.MustCompile(`[?&]_tifa=[^&]*\.p(?:&|$)`)

// TracePath is the file of the traces of one framework.
func TracePath(dir, framework string) string {
	return fmt.Sprintf("%s/%s.jsonl", strings.TrimRight(dir, "/"), framework)
}

// PlansPath is the file of the plans of the site.
func PlansPath(dir string) string {
	return fmt.Sprintf("%s/plans.json", strings.TrimRight(dir, "/"))
}

// document says whether the page view was a document load: the browser asked for a document, as it
// does for a landing and for an action the page does with no router (a plain link, a form).
func (s TraceStep) document() bool {
	for _, r := range s.Reqs {
		if r.Class == "doc" {
			return true
		}
	}
	return false
}

// documents says, for each page view of the visit, whether it was a document load.
func (t Trace) documents() []bool {
	out := make([]bool, len(t.Steps))
	for i, s := range t.Steps {
		out[i] = s.document()
	}
	return out
}
