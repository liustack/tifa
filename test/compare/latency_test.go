package compare

import (
	"io"
	"strings"
	"testing"
)

// nextClosed is a closed round of Next.js at sessions that completed visits in seconds.
func nextClosed(sc string, sessions, round, visits int, seconds float64) Round {
	x := closedRound(openBase, sc, sessions, round, 4*visits, seconds, true)
	x.Visits = visits
	return x
}

// The latency rate is 40% of the visits a second Next.js completes in the median of its closed rounds
// at the base sessions, to the nearest quarter of a visit a second. The rounds at other sessions, the
// rounds of other servers and the invalid ones do not count.
func TestTheLatencyRateIsFortyPercentOfTheClosedThroughputOfNextJS(t *testing.T) {
	sc := mustScenario("shop")
	res := &BrowseResult{Closed: []Round{
		nextClosed(sc.Name, 8, 1, 740, 100),  // 7.4 visits/s
		nextClosed(sc.Name, 8, 2, 700, 100),  // 7.0
		nextClosed(sc.Name, 8, 3, 1000, 100), // 10.0
		nextClosed(sc.Name, 10, 1, 2000, 100),
		closedRound("tifa", sc.Name, 8, 1, 40000, 100, true),
	}}
	invalid := nextClosed(sc.Name, 8, 2, 9000, 100)
	invalid.Valid = false
	res.Closed = append(res.Closed, invalid)
	// The median is 7.4 visits/s, and 40% of it 2.96, which rounds to 3.
	rate, err := latencyRate(res, sc.Name, 8, 3)
	if err != nil || rate != 3 {
		t.Errorf("the rate is %v (%v), want 3", rate, err)
	}
	for _, tc := range []struct {
		visits int
		want   float64
	}{{740, 3}, {700, 2.75}, {660, 2.75}, {650, 2.5}, {2000, 8}} {
		res := &BrowseResult{Closed: []Round{nextClosed(sc.Name, 8, 1, tc.visits, 100)}}
		if rate, err := latencyRate(res, sc.Name, 8, 1); err != nil || rate != tc.want {
			t.Errorf("%d visits in 100 s: the rate is %v (%v), want %v", tc.visits, rate, err, tc.want)
		}
	}
}

// The latency rounds need the closed rounds of Next.js at the base sessions, all of them valid and
// passing: without them the rate is an error that says so.
func TestTheLatencyRateNeedsTheClosedRoundsOfNextJS(t *testing.T) {
	sc := mustScenario("shop")
	failed := nextClosed(sc.Name, 8, 2, 700, 100)
	failed.Pass, failed.Why = false, "click P99 1200 ms over 1000 ms"
	for _, tc := range []struct {
		name   string
		closed []Round
		want   string
	}{
		{"none", nil, "0 of the 3"},
		{"two of three", []Round{nextClosed(sc.Name, 8, 1, 740, 100), nextClosed(sc.Name, 8, 2, 740, 100)}, "2 of the 3"},
		{"other sessions", []Round{nextClosed(sc.Name, 6, 1, 740, 100), nextClosed(sc.Name, 6, 2, 740, 100), nextClosed(sc.Name, 6, 3, 740, 100)}, "0 of the 3"},
		{"a failed round", []Round{nextClosed(sc.Name, 8, 1, 740, 100), failed, nextClosed(sc.Name, 8, 3, 740, 100)}, "failed the rule"},
		{"too slow", []Round{nextClosed(sc.Name, 8, 1, 1, 100), nextClosed(sc.Name, 8, 2, 1, 100), nextClosed(sc.Name, 8, 3, 1, 100)}, "rounds to 0"},
	} {
		_, err := latencyRate(&BrowseResult{Closed: tc.closed}, sc.Name, 8, 3)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Next.js") {
			t.Errorf("%s: %v, want an error that says %q", tc.name, err, tc.want)
		}
	}
}

// The latency phase plays nothing without the closed rounds of Next.js at the base sessions: it
// stops with the error of the rate, and keeps no rate.
func TestTheLatencyPhaseRefusesWithoutTheClosedRoundsOfNextJS(t *testing.T) {
	sc := mustScenario("shop")
	b := &browse{o: BrowseOptions{Log: io.Discard, Sessions: 8, Rounds: 3}, servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, res: &BrowseResult{
		Closed:      []Round{nextClosed(sc.Name, 8, 1, 740, 100)},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{},
	}}
	err := b.latency(t.Context(), sc)
	if err == nil || !strings.Contains(err.Error(), "1 of the 3 valid closed rounds") || !strings.Contains(err.Error(), "throughput phase") {
		t.Errorf("the latency phase without the closed rounds of Next.js: %v", err)
	}
	if len(b.res.Latency) != 0 || b.res.LatencyRate[sc.Name] != 0 {
		t.Errorf("it played %d rounds, at the rate %v", len(b.res.Latency), b.res.LatencyRate)
	}
	b.servers[sc.Name] = []string{"tifa"}
	if err := b.latency(t.Context(), sc); err == nil || !strings.Contains(err.Error(), "not among the servers") {
		t.Errorf("a run without Next.js: %v", err)
	}
}
