package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa/test/compare"
)

var browseOpts struct {
	out, scenarios, phases, extra, stamp, serverCPUs, loadCPUs string
	users, rounds                                              int
	probe                                                      bool
	maxRate                                                    float64
	warm, target, trial, final                                 time.Duration
}

func browseFlags(flags *flag.FlagSet) {
	o := &browseOpts
	flags.StringVar(&o.out, "out", "browse.json", "browse: the result, written after every round and continued if it exists")
	flags.StringVar(&o.scenarios, "scenarios", compare.DefaultScenarios, "browse: comma separated scenarios: office-w5, office-w20, office-w50 (back office writes), r95w5, r80w20, r50w50 (every write renames a customer) and store-w5, store-w20, store-w50 (the storefront)")
	flags.StringVar(&o.phases, "phases", "closed,saturation,sustain,latency", "browse: the parts to run: closed (throughput at equal users), saturation (the users at which each server that did not use its cpus saturates, read from test/compare/saturation.json or probed), sustain (the arrival rate Next.js sustains) and latency (every server at half of it), the last two for the scenarios with 5% writes only")
	flags.StringVar(&o.extra, "extra-users", "8,16", "browse: more users for the scenarios with 5% writes, besides -users")
	flags.StringVar(&o.stamp, "stamp", "", "browse: what the traces and builds belong to, kept in the result and checked when a result is continued")
	flags.StringVar(&o.serverCPUs, "server-cpus", "", "browse: cpus for the server, as taskset -c takes them (Linux only)")
	flags.StringVar(&o.loadCPUs, "load-cpus", "", "browse: cpus for the load generator")
	flags.Float64Var(&o.maxRate, "max-rate", 16384, "browse: the search for the sustainable rate stops here, visits a second")
	flags.BoolVar(&o.probe, "probe", false, "browse: probe again the users at which each server saturates, and rewrite test/compare/saturation.json")
	flags.IntVar(&o.users, "users", 32, "browse: users of every scenario")
	flags.IntVar(&o.rounds, "rounds", 3, "browse: rounds of every cell, in a rotating order with fresh servers")
	flags.DurationVar(&o.warm, "warm", 15*time.Second, "browse: warm-up of a closed round, apart from it")
	flags.DurationVar(&o.target, "target", 75*time.Second, "browse: about how long a closed round should take")
	flags.DurationVar(&o.trial, "trial-window", 45*time.Second, "browse: an open trial of the search for the sustainable rate")
	flags.DurationVar(&o.final, "final-window", 240*time.Second, "browse: an open round of the latency table, at least")
}

func countCPUs(list string) int {
	n := 0
	for _, part := range strings.Split(list, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			a, _ := strconv.Atoi(lo)
			b, _ := strconv.Atoi(hi)
			n += b - a + 1
		} else {
			n++
		}
	}
	return n
}

func browseRun(ctx context.Context, env compare.Env, dir string, seed uint64, servers []string) {
	o := &browseOpts
	opts := compare.BrowseOptions{TraceDir: dir, OutJSON: o.out, Seed: seed, Servers: servers, ServerCPUs: o.serverCPUs, LoadCPUs: o.loadCPUs, LoadCPUCount: countCPUs(o.loadCPUs), ServerCPUCount: countCPUs(o.serverCPUs), Probe: o.probe,
		Phases: strings.Split(o.phases, ","), Users: o.users, Rounds: o.rounds, Warm: o.warm, Target: o.target, TrialWindow: o.trial, FinalWindow: o.final, Stamp: o.stamp, MaxRate: o.maxRate, Log: os.Stderr}
	for _, name := range strings.Split(o.scenarios, ",") {
		sc, ok := compare.BrowseScenarioByName(strings.TrimSpace(name))
		if !ok {
			fatal(fmt.Errorf("no scenario %q", name))
		}
		opts.Scenarios = append(opts.Scenarios, sc)
	}
	for _, f := range strings.Split(o.extra, ",") {
		if f = strings.TrimSpace(f); f != "" {
			n, err := strconv.Atoi(f)
			if err != nil {
				fatal(fmt.Errorf("-extra-users: %w", err))
			}
			opts.ExtraUsers = append(opts.ExtraUsers, n)
		}
	}
	res, err := env.Browse(ctx, opts)
	if err != nil {
		fatal(err)
	}
	fmt.Println(res.Markdown())
}
