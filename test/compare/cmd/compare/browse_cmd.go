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
	out, phases, stamp, serverCPUs, loadCPUs, locks string
	sessions, rounds                                int
	probe                                           bool
	maxRate                                         float64
	sizing, target, trial, final                    time.Duration
}

func browseFlags(flags *flag.FlagSet) {
	o := &browseOpts
	flags.StringVar(&o.out, "out", "browse.json", "browse: the result, written after every round and continued if it exists")
	flags.StringVar(&o.phases, "phases", "throughput,control,sustain,latency", "browse: the parts to run: throughput (every server at -sessions concurrent sessions, then each stepped up while it does not use its cpus, or down when it fails), control (Tifa with no writes), sustain (the arrival rate Next.js sustains) and latency (every server at half of it)")
	flags.StringVar(&o.stamp, "stamp", "", "browse: what the traces and builds belong to, kept in the result and checked when a result is continued")
	flags.StringVar(&o.serverCPUs, "server-cpus", "", "browse: cpus for the server, as taskset -c takes them (Linux only)")
	flags.StringVar(&o.loadCPUs, "load-cpus", "", "browse: cpus for the load generator")
	flags.StringVar(&o.locks, "locks", "", "browse: files to flock while a server of the run runs, comma separated, taken in this order and given back between the rounds")
	flags.Float64Var(&o.maxRate, "max-rate", compare.FormalBrowse.MaxRate, "browse: the search for the sustainable rate stops here, visits a second")
	flags.BoolVar(&o.probe, "probe", false, "browse: search the rate Next.js sustains again, even where test/compare/rates.json has it, and write it into the file")
	flags.IntVar(&o.sessions, "sessions", compare.FormalBrowse.Sessions, "browse: the concurrent sessions (no think time) every server starts the throughput at")
	flags.IntVar(&o.rounds, "rounds", compare.FormalBrowse.Rounds, "browse: rounds of every cell, in a rotating order with fresh servers")
	flags.DurationVar(&o.sizing, "sizing", compare.FormalBrowse.Sizing, "browse: the length of the sizing run that sizes the first round of a cell, on a server of its own")
	flags.DurationVar(&o.target, "target", compare.FormalBrowse.Target, "browse: about how long a closed round should take")
	flags.DurationVar(&o.trial, "trial-window", compare.FormalBrowse.TrialWindow, "browse: an open trial of the search for the sustainable rate")
	flags.DurationVar(&o.final, "final-window", compare.FormalBrowse.FinalWindow, "browse: an open round of the latency table, at least")
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
		Phases: strings.Split(o.phases, ","), Sessions: o.sessions, Rounds: o.rounds, Sizing: o.sizing, Target: o.target, TrialWindow: o.trial, FinalWindow: o.final, Stamp: o.stamp, MaxRate: o.maxRate, Log: os.Stderr}
	for _, lock := range strings.Split(o.locks, ",") {
		if lock = strings.TrimSpace(lock); lock != "" {
			opts.Locks = append(opts.Locks, lock)
		}
	}
	res, err := env.Browse(ctx, opts)
	if err != nil {
		fatal(err)
	}
	fmt.Println(res.Markdown())
}
