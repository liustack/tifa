package compare

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"strings"
)

// TraceReq is one request the browser sent to the server. Start and End are milliseconds from the
// start of its step. Class is "doc" (a document), "nav" (the data a client navigation fetches, and
// route discovery) or "prefetch" (what the framework fetches ahead of a click). INM says that the
// browser sent If-None-Match, whose value the replay takes from the answers of its own visit.
type TraceReq struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	INM     bool              `json:"inm"`
	Start   float64           `json:"s"`
	End     float64           `json:"e"`
	Status  int               `json:"status"`
	Class   string            `json:"class"`
	// Body is what the browser sent with a POST or PATCH (the storefront remembers the product a visitor
	// looked at with a POST of its own), empty for a request with none.
	Body string `json:"body,omitempty"`
}

// TraceStep is one page view of a visit and the requests it made.
type TraceStep struct {
	Kind string     `json:"kind"`
	Page string     `json:"page"`
	Reqs []TraceReq `json:"reqs"`
}

// Trace is what a browser sent in one visit of a plan. Error is set when the browser could not play
// the plan, and such a trace is left out.
type Trace struct {
	Plan  int         `json:"plan"`
	Steps []TraceStep `json:"steps"`
	Error string      `json:"error,omitempty"`
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
			for _, r := range s.Reqs {
				if err := checkRouterHeaders(r); err != nil {
					return nil, 0, fmt.Errorf("%s: plan %d: %w", path, t.Plan, err)
				}
				if r.Status >= 400 { // status 0 is a request the browser cancelled before it had an answer: it was sent, so it is replayed
					return nil, 0, fmt.Errorf("%s: plan %d: %s %s answered %d", path, t.Plan, r.Method, r.URL, r.Status)
				}
			}
		}
		traces = append(traces, t)
	}
	return traces, dropped, sc.Err()
}

// The sites that visits are made on. The workbench is the pages under /projects, the store the
// storefront. The files of the workbench have no site in their names.
const (
	SiteWorkbench = ""
	SiteStore     = "store"
)

// siteTag is what a site adds to the names of its files: "-store", or nothing for the workbench.
func siteTag(site string) string {
	if site == SiteWorkbench {
		return ""
	}
	return "-" + site
}

// TracePath is the file of the traces of one framework on the workbench for plans of a mean length.
func TracePath(dir, framework string, mean int) string {
	return SiteTracePath(dir, framework, SiteWorkbench, mean)
}

// PlansPath is the file of the plans of the workbench of a mean length.
func PlansPath(dir string, mean int) string { return SitePlansPath(dir, SiteWorkbench, mean) }

// SiteTracePath is the file of the traces of one framework on a site.
func SiteTracePath(dir, framework, site string, mean int) string {
	return fmt.Sprintf("%s/%s%s-m%d.jsonl", strings.TrimRight(dir, "/"), framework, siteTag(site), mean)
}

// SitePlansPath is the file of the plans of a site.
func SitePlansPath(dir, site string, mean int) string {
	return fmt.Sprintf("%s/plans%s-m%d.json", strings.TrimRight(dir, "/"), siteTag(site), mean)
}

// checkRouterHeaders fails a request that the router of a framework made with a header that the
// recorded request lacks. Without it the server answers with a whole page, which a browser never
// gets: Next.js asks for RSC with `rsc: 1` on a URL with `_rsc=`, Start calls a server function with
// `x-tsr-serverfn`.
func checkRouterHeaders(r TraceReq) error {
	switch {
	case strings.Contains(r.URL, "_rsc=") && r.Headers["rsc"] == "":
		return fmt.Errorf("%s %s lost its rsc header", r.Method, r.URL)
	case strings.Contains(r.URL, "/_serverFn/") && r.Headers["x-tsr-serverfn"] == "":
		return fmt.Errorf("%s %s lost its x-tsr-serverfn header", r.Method, r.URL)
	}
	return nil
}
