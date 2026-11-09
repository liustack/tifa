package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// An answer that a cache may keep says so in its headers. The Node apps must send none of them, so
// that every request of the comparison does the work again in the three of them.
func TestCacheSignsFindsWhatLetsAnAnswerBeKept(t *testing.T) {
	for _, tc := range []struct {
		header http.Header
		want   []string
	}{
		{http.Header{}, nil},
		{http.Header{"Cache-Control": {"private, no-cache, no-store, max-age=0, must-revalidate"}}, nil},
		{http.Header{"Vary": {"Accept-Encoding"}}, nil},
		{http.Header{"Etag": {`"abc"`}}, []string{"ETag"}},
		{http.Header{"Last-Modified": {"Mon, 09 Nov 2026 00:00:00 GMT"}}, []string{"Last-Modified"}},
		{http.Header{"Cache-Control": {"public, max-age=60"}}, []string{"Cache-Control max-age=60"}},
		{http.Header{"Cache-Control": {"s-maxage=31536000, stale-while-revalidate"}}, []string{"Cache-Control s-maxage=31536000"}},
		{http.Header{"X-Nextjs-Cache": {"HIT"}}, []string{"x-nextjs-cache"}},
		{http.Header{"X-Nextjs-Prerender": {"1"}, "Etag": {"x"}}, []string{"ETag", "x-nextjs-prerender"}},
	} {
		if got := CacheSigns(tc.header); !slices.Equal(got, tc.want) {
			t.Errorf("%v: %q, want %q", tc.header, got, tc.want)
		}
	}
}

// Before a run, every Node app is asked twice for the documents and the navigation data its traces
// replay (one of each path, with the headers the browser sent), and a run stops when one answer has
// a sign of a cache. Assets, writes, requests the browser canceled and the route manifest of React
// Router (which holds no data of a page, made once a build like an asset) are not asked.
func TestARunStopsOnANodeAppThatSendsWhatACacheKeeps(t *testing.T) {
	wl := &Workload{Plans: []Plan{{ID: 1}, {ID: 2}}, Traces: map[int]Trace{
		1: {Plan: 1, Steps: []TraceStep{
			{Kind: StepLoad, Page: "/store", Reqs: []TraceReq{{Method: "GET", URL: "/store", Class: "doc"}, {Method: "GET", URL: "/assets/a.js", Class: "asset"}}},
			{Kind: StepClick, Page: "/store/p/1", Reqs: []TraceReq{
				{Method: "GET", URL: "/store/p/1.data?_rsc=a", Class: "nav", Headers: map[string]string{"RSC": "1"}},
				{Method: "POST", URL: "/store/p/1", Class: "nav"},
				{Method: "GET", URL: "/store/p/2.data", Class: "nav", Cancel: "aborted"},
				{Method: "GET", URL: "/__manifest?paths=%2Fstore", Class: "nav"},
			}},
		}},
		2: {Plan: 2, Steps: []TraceStep{{Kind: StepClick, Page: "/store/p/1", Reqs: []TraceReq{{Method: "GET", URL: "/store/p/1.data?_rsc=b", Class: "nav", Headers: map[string]string{"RSC": "1"}}}}}},
	}}
	got := replayedGets(wl)
	if urls := slices.Collect(func(yield func(string) bool) {
		for _, q := range got {
			yield(q.URL)
		}
	}); !slices.Equal(urls, []string{"/store", "/store/p/1.data?_rsc=a"}) {
		t.Fatalf("the requests asked are %v", urls)
	}
	var mu sync.Mutex
	asked := map[string]int{}
	etag := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.URL.Path+" "+r.Header.Get("RSC")]++
		n := asked[r.URL.Path+" "+r.Header.Get("RSC")]
		mu.Unlock()
		w.Header().Set("Cache-Control", "private, no-store")
		if etag && r.URL.Path == "/store/p/1.data" && n == 2 {
			w.Header().Set("ETag", `"x"`)
		}
		if r.URL.Path == "/store" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}
	}))
	defer srv.Close()
	if err := CheckNoCache(context.Background(), srv.URL, got); err != nil {
		t.Fatal(err)
	}
	if asked["/store "] != 2 || asked["/store/p/1.data 1"] != 2 || asked["/elsewhere "] != 0 {
		t.Errorf("asked %v", asked)
	}
	etag = true
	clear(asked)
	err := CheckNoCache(context.Background(), srv.URL, got)
	if err == nil || !strings.Contains(err.Error(), "/store/p/1.data?_rsc=a") || !strings.Contains(err.Error(), "ETag") {
		t.Errorf("an answer with an ETag gives %v", err)
	}
}
