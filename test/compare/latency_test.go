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

// Every server, Next.js and a reference alike, plays one latency round of the blocks of the run
// (LatencyBlocks), at the rate taken from the closed rounds of Next.js, on a server of its own.
func TestEveryServerPlaysOneLatencyRoundOfItsBlocks(t *testing.T) {
	shortSpacing(t)
	_, nocache, node := warmSpecs()
	sc := mustScenario("shop")
	b, servers := warmBrowse(t, sc, node, nocache)
	b.o.LatencyBlocks = 3
	// 1500 visits in a second: 40% of it is 600 visits a second, so that a round of 3 blocks is short.
	b.res.Closed = []Round{nextClosed(sc.Name, b.o.Sessions, 1, 1500, 1)}
	b.res.LatencyRate, b.res.LatencyBase = map[string]float64{}, map[string]string{}
	if err := b.latency(t.Context(), sc); err != nil {
		t.Fatal(err)
	}
	if b.res.LatencyRate[sc.Name] != 600 || b.res.LatencyBase[sc.Name] != openBase {
		t.Errorf("the rate %v of %v, want 600 of %s", b.res.LatencyRate, b.res.LatencyBase, openBase)
	}
	for _, name := range []string{node.Name, nocache.Name} {
		var valid []Round
		for _, x := range b.res.Latency {
			if x.Framework == name && x.Valid {
				valid = append(valid, x)
			}
		}
		if len(valid) != 1 {
			t.Fatalf("%s: %d valid latency rounds, want 1", name, len(valid))
		}
		x := valid[0]
		if x.Round != 1 || x.Blocks != 3 || x.VisitsPlanned != 3*sc.blockVisits() || x.Rate != 600 || x.Model != "open" {
			t.Errorf("%s: round %d of %d blocks, %d visits planned at %v visits/s (%s), want round 1 of 3 blocks, %d visits at 600", name, x.Round, x.Blocks, x.VisitsPlanned, x.Rate, x.Model, 3*sc.blockVisits())
		}
	}
	// One server for each try (the servers of the check of the writes get nothing here: warmBrowse
	// stubs it).
	if n := len(busy(servers())); n != len(b.res.Latency) {
		t.Errorf("%d servers for %d latency rounds", n, len(b.res.Latency))
	}
	// A latency round is done once it has a valid try: the phase played again plays nothing.
	n := len(b.res.Latency)
	if err := b.latency(t.Context(), sc); err != nil || len(b.res.Latency) != n {
		t.Errorf("the phase again: %d rounds, %d before (%v)", len(b.res.Latency), n, err)
	}
}

// A latency round is split into three parts of the same length by the planned start of its visits,
// and the full loads (the pages answered whole, `document`) and the client navigations (the pages the router answered with data, `click nav`) of the three parts add up to those of the round.
func TestTheThreePartsOfALatencyRoundAddUpToTheRound(t *testing.T) {
	shortSpacing(t)
	_, _, node := warmSpecs()
	sc := mustScenario("shop")
	b, _ := warmBrowse(t, sc, node)
	out, err := b.openRound(t.Context(), node.Name, sc, 600, 3*sc.blockVisits())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Parts) != 3 {
		t.Fatalf("%d parts, want 3", len(out.Parts))
	}
	full, navs := 0, 0
	for i, p := range out.Parts {
		if p.FullLoads.N == 0 || p.ClientNavs.N == 0 || p.FullLoads.P99 < p.FullLoads.P50 || p.ClientNavs.P99 < p.ClientNavs.P50 {
			t.Errorf("part %d: %+v", i+1, p)
		}
		// The visits of a part start in its third of the round: about a third of the full loads each.
		if third := out.Types["document"].N / 3; p.FullLoads.N < third*9/10 || p.FullLoads.N > third*11/10 {
			t.Errorf("part %d has %d of the %d full loads", i+1, p.FullLoads.N, out.Types["document"].N)
		}
		full, navs = full+p.FullLoads.N, navs+p.ClientNavs.N
	}
	if full != out.Types["document"].N || navs != out.Types["click nav"].N || full == 0 || full+navs != out.PageViews {
		t.Errorf("the parts have %d full loads and %d client navigations, the round %d and %d of %d page views", full, navs, out.Types["document"].N, out.Types["click nav"].N, out.PageViews)
	}
	if out.WarmUp == nil || out.WarmUp.VisitsPlanned != 3*sc.blockVisits() {
		t.Errorf("the warm-up %+v, want as many visits as the round", out.WarmUp)
	}
}

// A latency round plays whole passes of the plans: a part of one has another page mix.
func TestALatencyRoundIsWholePassesOfThePlans(t *testing.T) {
	sc := mustScenario("shop")
	set := PlanSet{Blocks: 2}
	for _, blocks := range []int{0, 3} {
		b := &browse{o: BrowseOptions{Log: io.Discard, Sessions: 8, Rounds: 1, LatencyBlocks: blocks}, set: &set, servers: map[string][]string{sc.Name: {"nextjs"}}, res: &BrowseResult{
			Closed: []Round{nextClosed(sc.Name, 8, 1, 740, 100)}, LatencyRate: map[string]float64{}, LatencyBase: map[string]string{}}}
		if err := b.latency(t.Context(), sc); err == nil || !strings.Contains(err.Error(), "not whole passes of the 2 blocks") {
			t.Errorf("%d blocks: %v", blocks, err)
		}
	}
}
