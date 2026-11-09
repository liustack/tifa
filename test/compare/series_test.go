package compare

import (
	"context"
	"math"
	"testing"
	"time"
)

// A round records page views a second and the P99 of their latency in 10 second steps from the first
// request, so that a cache filling up or a JIT warming shows as a change over the round.
func TestSeriesCountsPageViewsInTenSecondSteps(t *testing.T) {
	start := time.Unix(1000, 0)
	at := func(s float64) time.Time { return start.Add(time.Duration(s * float64(time.Second))) }
	views := []viewSample{
		{at(0.5), 10}, {at(3), 20}, {at(9.9), 90}, // step 0: 3 page views
		{at(10), 5}, // step 1: 1 page view
		// step 2: none (a stall)
		{at(31), 7}, {at(34), 9}, // step 3, 4 seconds long: 2 page views
	}
	got := viewSeries(views, start, at(34.5))
	if len(got) != 4 {
		t.Fatalf("%d steps, want 4: %+v", len(got), got)
	}
	for i, want := range []SeriesStep{
		{From: 0, PageViews: 3, PerSecond: 0.3, P99Ms: 90},
		{From: 10, PageViews: 1, PerSecond: 0.1, P99Ms: 5},
		{From: 20, PageViews: 0, PerSecond: 0, P99Ms: 0},
		{From: 30, PageViews: 2, PerSecond: 2 / 4.5, P99Ms: 9},
	} {
		g := got[i]
		if g.From != want.From || g.PageViews != want.PageViews || math.Abs(g.PerSecond-want.PerSecond) > 1e-9 || g.P99Ms != want.P99Ms {
			t.Errorf("step %d: %+v, want %+v", i, g, want)
		}
	}
}

// The steps are in the round, in the JSON, and add up to its page views.
func TestARoundHoldsItsSeries(t *testing.T) {
	srv, _ := fakeServer(t)
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}})
	r := testRunner(srv, plans, traces, nil)
	closed := r.RunClosed(context.Background(), 2, 20, 10*time.Second)
	open := r.RunOpen(context.Background(), 50, 10, time.Second, 0)
	for name, round := range map[string]*Round{"closed": closed, "open": open} {
		sum := 0
		for _, s := range round.Series {
			sum += s.PageViews
		}
		if len(round.Series) == 0 || sum != round.PageViews || round.PageViews == 0 {
			t.Errorf("%s: %d page views, series %+v", name, round.PageViews, round.Series)
		}
	}
	data, err := (&BrowseResult{Closed: []Round{*closed}, Sustain: map[string]*Sustain{}}).json()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ReadBrowseResult(data)
	if err != nil || len(back.Closed[0].Series) != len(closed.Series) {
		t.Errorf("the series did not survive the JSON: %v", err)
	}
}
