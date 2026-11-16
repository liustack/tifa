package compare

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
)

// A continued run that measures a server again sets its new rounds beside rounds it kept from
// before, so the machine must be as fast as it was then. Before it measures anything it plays as many
// closed rounds of a server it keeps as its cell has, as normal rounds of that cell, and compares the
// median of their page views a second with the median of the rounds it kept there (checkMachine). The
// rounds are no result. Medians, not one round against the range: the rounds of a cell differ by 7 to
// 8% on a machine that did not change.

// machineMargin is how far, as a share, the median of the check may be from the median of the kept
// rounds of its cell: further, the machine is another one now.
const machineMargin = 0.03

// MachineCheck is a check of the machine that passed (checkMachine): the server and the cell it
// played, the page views a second of each of its rounds, their median, the median of the kept rounds
// and the difference in percent, when it ran, and the stamps of the servers of the run it passed for.
type MachineCheck struct {
	Server     string                 `json:"server"`
	Scenario   string                 `json:"scenario"`
	Sessions   int                    `json:"sessions"`
	Values     []float64              `json:"pageViewsPerSecond"`
	Median     float64                `json:"median"`
	KeptMedian float64                `json:"keptMedian"`
	Diff       float64                `json:"diffPercent"`
	Date       string                 `json:"date"`
	Stamps     map[string]ServerStamp `json:"stamps"`
}

// checkStamps are the stamps of every server of a result, without the dates they were measured on.
func checkStamps(all map[string]ServerStamp) map[string]ServerStamp {
	out := map[string]ServerStamp{}
	for name, s := range all {
		s.Date = ""
		out[name] = s
	}
	return out
}

// checkServer is the server a check of the machine plays on a scenario, and its kept rounds at the
// base sessions: TanStack Start when it is kept with complete closed rounds there, else the first
// other main server (not a reference) that is. "" when none is.
func (b *browse) checkServer(sc BrowseScenario, stale []string) (string, []Round) {
	base := b.o.Sessions
	ok := func(f string) bool {
		_, reference := browseReferences[f]
		return !reference && !slices.Contains(stale, f) && len(b.res.cellRounds(f, sc.Name, base)) >= b.o.Rounds
	}
	names := b.servers[sc.Name]
	if i := slices.Index(names, "start"); i >= 0 && ok("start") {
		return "start", b.res.cellRounds("start", sc.Name, base)
	}
	for _, f := range names {
		if ok(f) {
			return f, b.res.cellRounds(f, sc.Name, base)
		}
	}
	return "", nil
}

// checkMachine plays the check of the machine when a continued run measures a server again (stale)
// and keeps a main server with complete closed rounds at the base sessions: as many closed rounds of
// that server (checkServer) as its cell has, each played as a normal round of the cell (closedTry: its
// size, its warm-up, its checks, a try that is not valid played again), and none added to the
// results. The median of their page views a second must be within machineMargin of the median of the
// kept rounds of the cell. Then the check is kept in the result with the stamps of the servers, and a
// run continued later under the same stamps does not play it again. Otherwise the run stops: the
// rounds it keeps were measured on a machine that is faster or slower now.
func (b *browse) checkMachine(ctx context.Context, stale []string) error {
	if len(stale) == 0 {
		return nil
	}
	stamps := checkStamps(b.res.ServerStamps)
	if n := len(b.res.MachineChecks); n > 0 && maps.Equal(b.res.MachineChecks[n-1].Stamps, stamps) {
		return nil
	}
	for _, sc := range b.o.Scenarios {
		if !sc.Throughput {
			continue
		}
		fw, kept := b.checkServer(sc, stale)
		if fw == "" {
			continue
		}
		var got []float64
		blocks := b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, fw)]
		for round := 1; round <= closedRounds(fw, b.o.Rounds); round++ {
			out, next, err := b.checkRound(ctx, fw, sc, round, blocks)
			if err != nil {
				return err
			}
			blocks = next
			got = append(got, pageViewsPerSecond(out))
		}
		views := each(kept, pageViewsPerSecond)
		median, keptMedian := Median(got), Median(views)
		diff := 100 * (median - keptMedian) / keptMedian
		b.logf("check  %-10s %2d sessions %-12s %s page views/s, median %.1f, the kept rounds %s, median %.1f: %+.1f%%, within %.0f%%\n", sc.Name, b.o.Sessions, fw, fmtList(got), median, fmtList(views), keptMedian, diff, 100*machineMargin)
		if math.Abs(diff) > 100*machineMargin {
			return fmt.Errorf("the check of the machine: %d closed rounds of %s on %s at %d sessions did %s page views a second, a median of %.1f page views a second, and the median of the %d kept rounds is %.1f (%s): %+.1f%%, more than %.0f%% off. The kept results were measured on a machine that is faster or slower now: the run must start again, remove %s",
				len(got), BrowseLabel(fw), sc.Name, b.o.Sessions, fmtList(got), median, len(views), keptMedian, fmtList(views), diff, 100*machineMargin, b.o.OutJSON)
		}
		b.res.MachineChecks = append(b.res.MachineChecks, MachineCheck{Server: fw, Scenario: sc.Name, Sessions: b.o.Sessions, Values: got, Median: median, KeptMedian: keptMedian, Diff: diff,
			Date: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), Stamps: stamps})
		return b.commit()
	}
	return nil
}

// fmtList writes page views a second one after the other, as "81.9, 76.3 and 76.5".
func fmtList(values []float64) string {
	var out []string
	for _, v := range values {
		out = append(out, fmt.Sprintf("%.1f", v))
	}
	if len(out) < 2 {
		return strings.Join(out, "")
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

// checkRound plays a round of the check of the machine on a server of a scenario at the base sessions,
// as a round of its cell is played from blocks, again while it is not valid (up to maxTries), logs
// every try, and returns it with the size the next round plays. It keeps nothing in the result: the round, and the
// size of the cell it may correct, are thrown away. A round still not valid after its tries stops the
// run. The tests set playCheck.
func (b *browse) checkRound(ctx context.Context, fw string, sc BrowseScenario, round, blocks int) (*Round, int, error) {
	out, err := retryInvalid(func(try int) (*Round, error) {
		if b.playCheck != nil {
			return b.playCheck(fw, sc, round, try)
		}
		out, next, err := b.closedTry(ctx, fw, sc, b.o.Sessions, blocks, b.res.Prepared[sc.Name], b.res.WarmVisits[sc.Name])
		if err != nil {
			return nil, err
		}
		out.Round, out.Attempt = round, try
		// Every try is logged, as a try of a round of the cell is: one that is not valid (most often a
		// round outside the window of its length, resized) is played again on a server of its own.
		b.logf("check  %-10s %2d sessions %-12s round %d (try %d): %d blocks, %.1f page views/s, valid %v %s\n",
			sc.Name, b.o.Sessions, fw, round, try, out.Blocks, pageViewsPerSecond(out), out.Valid, out.WhyInvalid)
		if next > 0 {
			blocks = next
		}
		return out, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if !out.Valid {
		return nil, 0, fmt.Errorf("the check of the machine: round %d of %s on %s at %d sessions was not valid in %d tries (%s)", round, BrowseLabel(fw), sc.Name, b.o.Sessions, maxTries, out.WhyInvalid)
	}
	return out, blocks, nil
}
