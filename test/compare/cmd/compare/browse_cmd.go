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
	out, phases, stamp, serverStamps, serverCPUs, loadCPUs, locks, announce, hold string
	sessions, rounds                                                              int
	sizing, target                                                                time.Duration
	latencyBlocks                                                                 int
	calibrate                                                                     bool
}

func browseFlags(flags *flag.FlagSet) {
	o := &browseOpts
	flags.StringVar(&o.out, "out", "browse.json", "browse: the result, written after every round and continued if it exists")
	flags.StringVar(&o.phases, "phases", "throughput,control,latency", "browse: the parts to run: throughput (every server at -sessions concurrent sessions, then each stepped up while it does not use its cpus, or down when it fails), control (Tifa with no writes) and latency (every server at 40% of the visits a second Next.js completes in its closed rounds)")
	flags.StringVar(&o.stamp, "stamp", "", "browse: what every server of the run shares (the plans, the replayer, the toolchain, the settings), kept in the result: a result of another stamp is not continued")
	flags.StringVar(&o.serverStamps, "server-stamps", "", "browse: what the results of each server belong to besides -stamp (its trace and its build), as spec=stamp, comma separated: a result continued under another stamp of a server measures that server again")
	flags.StringVar(&o.serverCPUs, "server-cpus", "", "browse: cpus for the server, as taskset -c takes them (Linux only)")
	flags.StringVar(&o.loadCPUs, "load-cpus", "", "browse: cpus for the load generator")
	flags.StringVar(&o.locks, "locks", "", "browse: files to flock while a server of the run runs, comma separated, taken in this order and given back between the rounds")
	flags.StringVar(&o.hold, "hold", "", "browse: the command of the machine that keeps it quiet for a timing round, run around each server of the run")
	flags.StringVar(&o.announce, "announce", "", "browse: a folder in which the run leaves a file named by its pid while it waits for or runs a round")
	flags.IntVar(&o.sessions, "sessions", compare.FormalBrowse.Sessions, "browse: the concurrent sessions (no think time) every server starts the throughput at")
	flags.IntVar(&o.rounds, "rounds", compare.FormalBrowse.Rounds, "browse: rounds of every cell, in a rotating order with fresh servers")
	flags.DurationVar(&o.sizing, "sizing", compare.FormalBrowse.Sizing, "browse: the length of the sizing run that sizes the first round of a cell, on a server of its own")
	flags.DurationVar(&o.target, "target", compare.FormalBrowse.Target, "browse: about how long a closed round should take")
	flags.IntVar(&o.latencyBlocks, "latency-blocks", compare.FormalBrowse.LatencyBlocks, "browse: the blocks of the one latency round of every server, a multiple of the blocks of the plans")
	flags.BoolVar(&o.calibrate, "calibrate", true, "browse: time the machine with compare calibrate, pinned to the server cpus, before the warm-up of every round, before its clock and after it, against the speed at the start of the run (a run without it is not formal)")
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

// serverStamps reads -server-stamps, spec=stamp pairs, comma separated.
func serverStamps() map[string]string {
	list := browseOpts.serverStamps
	var out map[string]string
	for _, pair := range strings.Split(list, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		name, stamp, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			fatal(fmt.Errorf("-server-stamps %s: want spec=stamp, comma separated", list))
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = stamp
	}
	return out
}

// browseStatus prints, for each server, what a run with these stamps does with the result of -out
// (compare.BrowseStatus). It reads and changes nothing else.
func browseStatus(dir string, servers []string) {
	calib, err := compare.BrowseCalibrationStatus(browseOpts.out)
	if err != nil {
		fatal(err)
	}
	fmt.Print(calib)
	for _, server := range servers {
		status, err := compare.BrowseStatus(browseOpts.out, dir, browseOpts.stamp, serverStamps(), server)
		if err != nil {
			fatal(err)
		}
		fmt.Print(status)
	}
}

func browseRun(ctx context.Context, env compare.Env, dir string, seed uint64, servers []string) {
	o := &browseOpts
	opts := compare.BrowseOptions{TraceDir: dir, OutJSON: o.out, Seed: seed, Servers: servers, ServerCPUs: o.serverCPUs, LoadCPUs: o.loadCPUs, LoadCPUCount: countCPUs(o.loadCPUs), ServerCPUCount: countCPUs(o.serverCPUs),
		Phases: strings.Split(o.phases, ","), Sessions: o.sessions, Rounds: o.rounds, Sizing: o.sizing, Target: o.target, LatencyBlocks: o.latencyBlocks, Stamp: o.stamp, ServerStamps: serverStamps(), Log: os.Stderr}
	for _, lock := range strings.Split(o.locks, ",") {
		if lock = strings.TrimSpace(lock); lock != "" {
			opts.Locks = append(opts.Locks, lock)
		}
	}
	opts.Announce = o.announce
	opts.Hold = strings.Fields(o.hold)
	if o.calibrate {
		opts.Calibrate = compare.CalibrateChild(o.serverCPUs)
	}
	res, err := env.Browse(ctx, opts)
	if err != nil {
		fatal(err)
	}
	fmt.Println(res.Markdown())
}
