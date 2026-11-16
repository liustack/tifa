package compare

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"
)

// serverKey names what a result holds of one spec in one scenario.
func serverKey(sc, spec string) string { return sc + "/" + spec }

// latencyShare is the share of the closed throughput of Next.js (openBase) at which every server of a
// scenario plays its latency round. Next.js sustained, in the open model, about 0.8 of the visits a
// second its closed rounds completed, and latency is played at half of what it sustains: 0.4.
const latencyShare = 0.4

// latencyRate is the rate of the latency rounds of a scenario: latencyShare of the visits a second
// that Next.js completes in the median of its closed rounds at the base sessions, to the nearest
// quarter of a visit a second. It needs those rounds complete, rounds valid rounds that all passed
// the rule: a round that failed it is no throughput, and a rate of 0 plays nothing.
func latencyRate(res *BrowseResult, sc string, sessions, rounds int) (float64, error) {
	cell := res.cellRounds(openBase, sc, sessions)
	if res.cellFailed(openBase, sc, sessions) {
		i := slices.IndexFunc(cell, func(x Round) bool { return !x.Pass })
		return 0, fmt.Errorf("%s: %s failed the rule in a closed round at %d sessions (%s), so its throughput sets no latency rate", sc, BrowseLabel(openBase), sessions, cell[i].Why)
	}
	if len(cell) < rounds {
		return 0, fmt.Errorf("%s: the latency rate is %.0f%% of the closed throughput of %s at %d sessions, and it has %d of the %d valid closed rounds there: run the throughput phase first", sc, 100*latencyShare, BrowseLabel(openBase), sessions, len(cell), rounds)
	}
	median := Median(each(cell, func(x *Round) float64 { return float64(x.Visits) / x.Seconds }))
	rate := math.Round(latencyShare*median*4) / 4
	if rate <= 0 {
		return 0, fmt.Errorf("%s: %.0f%% of the %.3f visits a second %s completes in its closed rounds at %d sessions rounds to 0", sc, 100*latencyShare, median, BrowseLabel(openBase), sessions)
	}
	return rate, nil
}

// openRound runs the open model on a fresh server: visits at rate a second, after a warm-up of as
// many visits with their writes, the visits after those of the round, apart.
func (b *browse) openRound(ctx context.Context, fw string, sc BrowseScenario, rate float64, visits int) (*Round, error) {
	out, err := b.with(ctx, fw, sc, visits, visits, visits, func(r *runner) *Round {
		return r.RunOpen(ctx, rate, visits, 30*time.Second, 0)
	})
	if err != nil {
		return nil, err
	}
	// A generator that cannot keep up makes the round invalid, so that it is never reported as a limit
	// of the server.
	if why := generatorFault(out, b.o.LoadCPUCount); why != "" {
		out.GeneratorLimited, out.Valid, out.WhyInvalid = true, false, why
	}
	return out, nil
}

// latency is the open run of a scenario: every server, a reference too, plays one round of
// LatencyBlocks blocks at the same arrival rate (latencyRate), on a fresh server. A round that is not
// valid (the load generator fell behind) is played again, up to maxTries tries. A server that cannot
// hold the rate fails at it, and its round is kept as it is: the rate is never lowered for it.
func (b *browse) latency(ctx context.Context, sc BrowseScenario) error {
	if !sc.Latency {
		return nil
	}
	if !slices.Contains(b.servers[sc.Name], openBase) {
		return fmt.Errorf("%s: the latency rate is set by %s, which is not among the servers", sc.Name, BrowseLabel(openBase))
	}
	if b.res.LatencyRate[sc.Name] == 0 {
		rate, err := latencyRate(b.res, sc.Name, b.o.Sessions, b.o.Rounds)
		if err != nil {
			return err
		}
		b.res.LatencyRate[sc.Name], b.res.LatencyBase[sc.Name] = rate, openBase
		if err := b.commit(); err != nil {
			return err
		}
	}
	if b.o.LatencyBlocks <= 0 || b.o.LatencyBlocks%b.set.Blocks != 0 {
		return fmt.Errorf("%s: a latency round of %d blocks is not whole passes of the %d blocks of the plans", sc.Name, b.o.LatencyBlocks, b.set.Blocks)
	}
	rate, visits := b.res.LatencyRate[sc.Name], b.o.LatencyBlocks*sc.blockVisits()
	b.logf("latency %s: %d visits at %.2f visits/s, %.0f%% of the closed throughput of %s\n", sc.Name, visits, rate, 100*latencyShare, b.res.LatencyBase[sc.Name])
	for _, fw := range b.servers[sc.Name] {
		// The tries it played before, in a run that stopped: a valid one ends its tries.
		tries := 0
		for _, x := range b.res.Latency {
			if x.Framework == fw && x.Scenario == sc.Name {
				tries++
				if x.Valid {
					tries = maxTries
				}
			}
		}
		for try := tries + 1; try <= maxTries; try++ {
			r, err := b.openRound(ctx, fw, sc, rate, visits)
			if err != nil {
				return err
			}
			r.Round, r.Attempt = 1, try
			b.res.Latency = append(b.res.Latency, *r)
			b.logf("latency %-10s %-12s try %d: %d of %d visits succeeded, %d failed, %d unfinished, load P50 %.1f P99 %.1f, click P50 %.1f P99 %.1f ms, pass %v %s valid %v %s\n", sc.Name, fw, r.Attempt, r.Visits, r.VisitsPlanned, r.VisitsFailed, r.VisitsUnfinished, r.Types["load"].P50, r.Types["load"].P99, r.Types["click"].P50, r.Types["click"].P99, r.Pass, r.Why, r.Valid, r.WhyInvalid)
			if err := b.commit(); err != nil {
				return err
			}
			if r.Valid {
				break
			}
		}
	}
	return nil
}
