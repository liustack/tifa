// Command compare builds and measures the comparison of tifa with Next.js and TanStack Start, on
// the six workbench pages of examples/shop. Run it from anywhere in the repository:
//
//	go run ./test/compare/cmd/compare build
//	go run ./test/compare/cmd/compare measure -server-cpus 0 -load-cpus 4 -json out.json
//
// build installs and builds the shop and the two Node apps. measure builds first unless
// -skip-build is given, then runs oha against each server and prints the tables as Markdown.
// docs/testing.md explains the method and what the numbers mean.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/liustack/tifa/test/compare"
)

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "build" && os.Args[1] != "measure") {
		fmt.Fprintln(os.Stderr, "usage: compare build | compare measure [flags]")
		os.Exit(2)
	}
	cmd := os.Args[1]
	flags := flag.NewFlagSet(cmd, flag.ExitOnError)
	var (
		skipBuild    = flags.Bool("skip-build", false, "measure: use the last build")
		serverCPUs   = flags.String("server-cpus", "", "measure: cpus for the server, as taskset -c takes them (Linux only), empty for no pinning")
		loadCPUs     = flags.String("load-cpus", "", "measure: cpus for oha, as above")
		rounds       = flags.Int("rounds", 2, "measure: rounds, each with a fresh server")
		warmup       = flags.Duration("warmup", 10*time.Second, "measure: warm-up before the six pages in turn")
		duration     = flags.Duration("duration", 15*time.Second, "measure: length of the measured run")
		servers      = flags.String("servers", "", "measure: comma separated servers out of tifa, nextjs, nextjs-jitless, start (default all)")
		perPage      = flags.Bool("per-page", false, "measure: also run each page alone")
		pageWarmup   = flags.Duration("page-warmup", 3*time.Second, "measure: warm-up of a page alone")
		pageDuration = flags.Duration("page-duration", 10*time.Second, "measure: length of the run of a page alone")
		render       = flags.Bool("render", false, "measure: also run the render benchmark of test/ssr, to split tifa's request (needs -per-page)")
		jsonPath     = flags.String("json", "", "measure: write the results as JSON to this file")
	)
	flags.Parse(os.Args[2:])

	if why := compare.MissingTools(); why != "" {
		fatal(fmt.Errorf("%s", why))
	}
	if cmd == "measure" {
		if why := compare.MissingTools("oha"); why != "" {
			fatal(fmt.Errorf("%s", why))
		}
		if *render && !*perPage {
			fatal(fmt.Errorf("-render needs -per-page"))
		}
	}
	env, err := compare.NewEnv()
	if err != nil {
		fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if cmd == "build" || !*skipBuild {
		if err := env.Prepare(ctx, os.Stderr); err != nil {
			fatal(err)
		}
	}
	if cmd == "build" {
		return
	}
	o := compare.Options{
		ServerCPUs: *serverCPUs, LoadCPUs: *loadCPUs, Rounds: *rounds, Warmup: *warmup, Duration: *duration,
		PerPage: *perPage, PageWarmup: *pageWarmup, PageDuration: *pageDuration, Render: *render, Log: os.Stderr,
	}
	if *servers != "" {
		o.Servers = strings.Split(*servers, ",")
	}
	report, err := env.Measure(ctx, o)
	if err != nil {
		fatal(err)
	}
	fmt.Println(report.Markdown())
	if *jsonPath != "" {
		data, err := report.JSON()
		if err == nil {
			err = os.WriteFile(*jsonPath, data, 0o644)
		}
		if err != nil {
			fatal(err)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
