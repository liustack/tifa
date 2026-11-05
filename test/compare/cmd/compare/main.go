// Command compare builds, measures and reports the comparison of Tifa with Next.js, TanStack
// Start, on the six workbench pages of examples/shop. Run it from anywhere in the repository:
//
//	go run ./test/compare/cmd/compare build
//	go run ./test/compare/cmd/compare measure -all -server-cpus 1 -load-cpus 2 -json out.json
//	go run ./test/compare/cmd/compare report out.json
//	go run ./test/compare/cmd/compare diff old.json new.json
//
// build installs and builds the shop and the Node apps. measure builds first unless -skip-build is
// given, then times every server from the start of its process and prints the tables. report prints
// the tables of a saved run again. diff exits with status 1 when a Tifa metric got worse by more
// than max(2%, the A/A spread). docs/testing.md explains the method and what the numbers mean.
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

const usage = `usage:
  compare build
  compare measure [flags]
  compare report out.json
  compare diff old.json new.json`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build", "measure":
		measure(os.Args[1], os.Args[2:])
	case "report":
		if len(os.Args) != 3 {
			fatal(fmt.Errorf("%s", usage))
		}
		fmt.Println(read(os.Args[2]).Markdown())
	case "diff":
		if len(os.Args) != 4 {
			fatal(fmt.Errorf("%s", usage))
		}
		lines, flagged := compare.DiffRecords(read(os.Args[2]), read(os.Args[3]))
		if flagged == 0 {
			fmt.Println("no Tifa metric is worse than its limit")
			return
		}
		fmt.Printf("%d Tifa metrics are worse than their limit:\n%s\n", flagged, strings.Join(lines, "\n"))
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func read(path string) *compare.Record {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	rec, err := compare.ReadRecord(data)
	if err != nil {
		fatal(fmt.Errorf("%s: %w", path, err))
	}
	return rec
}

func measure(cmd string, args []string) {
	flags := flag.NewFlagSet(cmd, flag.ExitOnError)
	var (
		skipBuild  = flags.Bool("skip-build", false, "measure: use the last build")
		all        = flags.Bool("all", false, "measure: every configuration (default and engine) and the A/A check")
		config     = flags.String("config", "", "measure: one configuration, default or engine (default: both with -all, else default)")
		serverCPUs = flags.String("server-cpus", "", "measure: cpus for the server, as taskset -c takes them (Linux only), empty for no pinning")
		loadCPUs   = flags.String("load-cpus", "", "measure: cpus for the client, as above. Must be on other physical cores than the server's")
		rounds     = flags.Int("rounds", 3, "measure: rounds, each with fresh servers, in a rotating order")
		steady     = flags.Duration("steady", 30*time.Second, "measure: length of the steady phase after the cold requests")
		cold       = flags.Int("cold", 1000, "measure: requests of the cold series, from process start")
		servers    = flags.String("servers", "", "measure: comma separated spec ids, such as tifa,tifa-nocache,nextjs (default all of the configuration)")
		aa         = flags.Bool("aa", false, "measure: also run Tifa against Tifa in each round and print the spread (-all does it too)")
		render     = flags.Bool("render", false, "measure: also run the render benchmark of test/ssr in a warm VM, to split Tifa's request into the render and the rest")
		jsonPath   = flags.String("json", "", "measure: write the record as JSON to this file")
	)
	flags.Parse(args)

	if why := compare.MissingTools(); why != "" {
		fatal(fmt.Errorf("%s", why))
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
		ServerCPUs: *serverCPUs, LoadCPUs: *loadCPUs, Rounds: *rounds, Steady: *steady, Cold: *cold,
		AA: *aa || *all, Render: *render, Log: os.Stderr,
	}
	switch {
	case *config != "" && *all:
		fatal(fmt.Errorf("give -all or -config, not both"))
	case *config != "":
		o.Configs = []string{*config}
	case !*all:
		o.Configs = []string{compare.ConfigDefault}
	}
	if *servers != "" {
		o.Servers = strings.Split(*servers, ",")
	}
	rec, err := env.Measure(ctx, o)
	if err != nil {
		fatal(err)
	}
	if *jsonPath != "" {
		data, err := rec.JSON()
		if err == nil {
			err = os.WriteFile(*jsonPath, data, 0o644)
		}
		if err != nil {
			fatal(err)
		}
	}
	fmt.Println(rec.Markdown())
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
