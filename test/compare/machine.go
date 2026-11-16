package compare

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"
)

// A continued run that measures a server again sets its new rounds beside rounds it kept from
// before, so the machine must be as fast as it was then. Before it measures anything it plays one
// closed round of a server it keeps, as a normal round of that cell, and compares its page views a
// second with the rounds it kept there (checkMachine). The round is no result.

// machineMargin is how far outside the kept rounds of its cell the check round may land: below the
// slowest of them less 2%, or above the fastest of them and 2%, the machine is another one now.
const machineMargin = 0.02

// MachineCheck is a check of the machine that passed (checkMachine): the server and the cell it
// played, its page views a second and the range of the kept rounds it fell in, widened by
// machineMargin, when it ran, and the stamps of the servers of the run it passed for.
type MachineCheck struct {
	Server   string                 `json:"server"`
	Scenario string                 `json:"scenario"`
	Sessions int                    `json:"sessions"`
	Value    float64                `json:"pageViewsPerSecond"`
	Min      float64                `json:"min"`
	Max      float64                `json:"max"`
	Date     string                 `json:"date"`
	Stamps   map[string]ServerStamp `json:"stamps"`
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
// and keeps a main server with complete closed rounds at the base sessions: one closed round of that
// server (checkServer), played as a normal round of the cell (closedTry: its size, its warm-up, its
// checks, a try that is not valid played again), and not added to the results. Its page views a
// second must lie within the slowest kept round of the cell less machineMargin and the fastest and
// machineMargin. Then the check is kept in the result with the stamps of the servers, and a run
// continued later under the same stamps does not play it again. Outside that range the run stops:
// the rounds it keeps were measured on a machine that is faster or slower now.
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
		out, err := b.checkRound(ctx, fw, sc)
		if err != nil {
			return err
		}
		views := each(kept, pageViewsPerSecond)
		lo, hi := slices.Min(views)*(1-machineMargin), slices.Max(views)*(1+machineMargin)
		got := pageViewsPerSecond(out)
		b.logf("check  %-10s %2d sessions %-12s %.1f page views/s, the kept rounds %s within %.0f%%: %.1f to %.1f\n", sc.Name, b.o.Sessions, fw, got, fmtCell(views, "%.1f"), 100*machineMargin, lo, hi)
		if got < lo || got > hi {
			return fmt.Errorf("the check of the machine: a closed round of %s on %s at %d sessions did %.1f page views a second, and the rounds of it this result keeps did %s, so the range is %.1f to %.1f (%.0f%% beyond them). The kept results were measured on a machine that is faster or slower now: the run must start again, remove %s", BrowseLabel(fw), sc.Name, b.o.Sessions, got, fmtCell(views, "%.1f"), lo, hi, 100*machineMargin, b.o.OutJSON)
		}
		b.res.MachineChecks = append(b.res.MachineChecks, MachineCheck{Server: fw, Scenario: sc.Name, Sessions: b.o.Sessions, Value: got, Min: lo, Max: hi,
			Date: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), Stamps: stamps})
		return b.commit()
	}
	return nil
}

// checkRound plays the round of the check of the machine on a server of a scenario at the base
// sessions, as a round of its cell is played, again while it is not valid (up to maxTries). It keeps
// nothing in the result: the round, and the size of the cell it may correct, are thrown away. A
// round still not valid after its tries stops the run. The tests set playCheck.
func (b *browse) checkRound(ctx context.Context, fw string, sc BrowseScenario) (*Round, error) {
	blocks := b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, fw)]
	out, err := retryInvalid(func(try int) (*Round, error) {
		if b.playCheck != nil {
			return b.playCheck(fw, sc, try)
		}
		out, next, err := b.closedTry(ctx, fw, sc, b.o.Sessions, blocks, b.res.Prepared[sc.Name], b.res.WarmVisits[sc.Name])
		if err != nil {
			return nil, err
		}
		out.Attempt = try
		if next > 0 {
			blocks = next
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	if !out.Valid {
		return nil, fmt.Errorf("the check of the machine: no closed round of %s on %s at %d sessions was valid in %d tries (%s)", BrowseLabel(fw), sc.Name, b.o.Sessions, maxTries, out.WhyInvalid)
	}
	return out, nil
}
