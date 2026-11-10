package compare

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// The latency of a scenario runs every server at half of the arrival rate Next.js was confirmed to
// sustain on it. The search for that rate takes about an hour, so the confirmed rate is kept in a
// checked-in file (test/compare/rates.json), with everything it was measured under. A run uses it
// only when all of that is the run's too, and otherwise refuses and asks for -probe, which searches
// it again and writes the file: a rate is never reused silently.

// RateFile is the checked-in file: per scenario, the rate Next.js was confirmed to sustain.
type RateFile map[string]RateEntry

// RateEntry is one confirmed rate, in Unit, with the conditions it was measured under (Under, each
// of which must match a run that uses it) and the run it came from.
type RateEntry struct {
	Rate   float64           `json:"rate"`
	Unit   string            `json:"unit"`
	Under  map[string]string `json:"under"`
	Source RateSource        `json:"source"`
}

// RateSource is the run a rate came from: its date, the commit of Tifa, the result file and every
// confirmation round it played.
type RateSource struct {
	Date    string      `json:"date"`
	Tifa    string      `json:"tifa"`
	Result  string      `json:"result"`
	Confirm []RateRound `json:"confirm"`
}

// RateRound is one confirmation round of a rate.
type RateRound struct {
	Rate     float64 `json:"rate"`
	Attempt  int     `json:"attempt"`
	Round    int     `json:"round"`
	Pass     bool    `json:"pass"`
	Valid    bool    `json:"valid"`
	LoadP99  float64 `json:"loadP99Ms"`
	ClickP99 float64 `json:"clickP99Ms"`
	WriteP99 float64 `json:"writeP99Ms"`
	Why      string  `json:"why,omitempty"`
}

// LatencyRule is the rule a rate is sustained under, as it is written into the file.
var LatencyRule = fmt.Sprintf("P99 of full loads, clicks and writes at most %.0f ms; no failure; no growing backlog; no visit started over 250 ms late; the load generator under 0.8 of its cpus", LatencyLimit)

// rateUnit is the unit of the rates.
const rateUnit = "visits/s"

// rateKeys are the conditions every entry of the file has (see rateConditions).
var rateKeys = []string{"actionSpacing", "confirm", "cpu", "go", "loadCpuCount", "loadCpus", "method", "nextjs", "nextjsApp", "nextjsServer", "node", "plans", "rule", "scenario", "search", "serverCpus", "sources", "trace"}

// readRates reads the file, which is empty when it does not exist yet. A file of another format is
// refused, and so is an entry in another unit than visits/s, with a rate of 0 or less, or without
// one of the conditions.
func readRates(path string) (RateFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return RateFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	f := RateFile{}
	if err := json.Unmarshal(data, &f, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, sc := range slices.Sorted(maps.Keys(f)) {
		e := f[sc]
		if e.Unit != rateUnit {
			return nil, fmt.Errorf("%s: the rate of %s is in %q, not in %s", path, sc, e.Unit, rateUnit)
		}
		if e.Rate <= 0 {
			return nil, fmt.Errorf("%s: the rate of %s is %v", path, sc, e.Rate)
		}
		for _, k := range rateKeys {
			if e.Under[k] == "" {
				return nil, fmt.Errorf("%s: the rate of %s has no condition %s", path, sc, k)
			}
		}
	}
	return f, nil
}

// writeRate sets the entry of one scenario and writes the file again, whole or not at all: to a file
// next to it that it then renames. Runs must not write it at the same time (one would lose the entry
// of the other): one run at a time searches rates.
func writeRate(path, sc string, e RateEntry) error {
	f, err := readRates(path)
	if err != nil {
		return err
	}
	f[sc] = e
	data, err := json.Marshal(f, jsonIndent)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fileRate is the entry of the file a run uses for a scenario: nil when the rate must be searched
// (no entry, or -probe), and an error when the entry was measured under other conditions than the
// run's, or under one the run cannot tell (an empty one).
func fileRate(f RateFile, sc string, under map[string]string, probe bool) (*RateEntry, error) {
	e, ok := f[sc]
	if !ok || probe {
		return nil, nil
	}
	if diff := conditionDiff(e.Under, under); len(diff) > 0 {
		return nil, fmt.Errorf("%s: the rate of %s in test/compare/rates.json was measured under other conditions than this run (%s): run with -probe (PROBE=1 for browse.sh) to search it again and write the file (a condition that is \"\" here is one this run cannot tell: the sources of the traces are known only in a run of browse.sh)", sc, BrowseLabel(openBase), strings.Join(diff, "; "))
	}
	return &e, nil
}

// conditionDiff says how two sets of conditions differ, a condition by line: one that is in one and
// not the other, that differs, or that is empty in either (a condition nobody can tell never matches).
func conditionDiff(was, here map[string]string) []string {
	var diff []string
	for _, k := range slices.Sorted(mapKeys(here, was)) {
		if here[k] == "" || was[k] != here[k] {
			diff = append(diff, fmt.Sprintf("%s %q then, %q here", k, was[k], here[k]))
		}
	}
	return diff
}

// mapKeys is the keys of both maps.
func mapKeys(a, b map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range a {
			if !yield(k) {
				return
			}
		}
		for k := range b {
			if _, ok := a[k]; !ok && !yield(k) {
				return
			}
		}
	}
}

// fileHash is a short hash of a file, or "" when it cannot be read.
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// rateConditions are the conditions the rate of Next.js on a scenario is measured under in this
// run, what that rate depends on and nothing else: the method (the files of test/compare that
// prepare, replay, judge and start a run, methodHash), the server of Next.js (nextjsServer) and the
// sources of its app, the Go the load generator is built with and the settings of its runtime
// (loadGo), the scenario and
// the seed of its writes, the plans and the Next.js trace of the site, the sources that trace was
// recorded from (the lines of the stamp browse.sh writes that bear on it, stampHash; "" when the
// traces have none), the cpu model and the pinning, with the number of cpus of the load the run
// counted (which judges the generator),
// the Node and Next.js versions, the spacing of actions, the rule, the search and the confirmation.
func (b *browse) rateConditions(sc BrowseScenario) map[string]string {
	scenario, _ := json.Marshal(sc)
	// The sources of the traces are known only in a run that browse.sh checked against this checkout
	// (one with a stamp): a run by hand cannot tell what its traces were recorded from.
	sources := ""
	if b.o.Stamp != "" {
		sources = stampHash(filepath.Join(b.o.TraceDir, "STAMP"))
	}
	sum := sha256.Sum256(fmt.Appendf(scenario, " seed %d", b.o.Seed))
	return map[string]string{
		"method":        methodHash(b.e.Root),
		"nextjsServer":  nextjsServer(b.e.Specs(), os.Environ()),
		"go":            loadGo(os.Environ()),
		"nextjsApp":     appHash(b.e.Root),
		"scenario":      hex.EncodeToString(sum[:8]),
		"plans":         fileHash(PlansPath(b.o.TraceDir)),
		"trace":         fileHash(TracePath(b.o.TraceDir, openBase)),
		"sources":       sources,
		"cpu":           b.res.Machine.CPU,
		"serverCpus":    b.o.ServerCPUs,
		"loadCpus":      b.o.LoadCPUs,
		"loadCpuCount":  strconv.Itoa(b.o.LoadCPUCount),
		"node":          b.res.Version["node"],
		"nextjs":        b.res.Version[openBase],
		"actionSpacing": ActionSpacing.String(),
		"rule":          LatencyRule,
		"search":        fmt.Sprintf("doubling from 4 visits/s up to %v, narrowed to 10%%, trials of %s, each after a warm-up of as many visits with their writes", b.maxRate(), b.o.TrialWindow),
		"confirm":       fmt.Sprintf("%d rounds of %s at the rate found, then up to 3 rates 10%% lower, and one 10%% above when the first passes", b.o.Rounds, 2*b.o.TrialWindow),
	}
}

// jsonIndent writes the file for people to read and diff.
var jsonIndent = jsontext.WithIndent("  ")

// keepRate writes a rate the search confirmed into the file of rates, with what it was measured
// under and its confirmation rounds. A rate that the load generator limited, or one measured under
// a condition the run cannot tell, is not written: it could not be reused.
func (b *browse) keepRate(sc BrowseScenario, s *Sustain) error {
	if s.Rate == 0 || s.Limited {
		return nil
	}
	for k, v := range s.Under {
		if v == "" {
			b.logf("sustain %-10s the rate is not written into %s: the run does not know its %s\n", sc.Name, b.o.RateFile, k)
			return nil
		}
	}
	src := RateSource{Date: b.res.Date, Tifa: b.res.Version["tifa"], Result: filepath.Base(b.o.OutJSON)}
	for _, x := range s.Confirm {
		src.Confirm = append(src.Confirm, RateRound{Rate: x.Rate, Attempt: x.Attempt, Round: x.Round, Pass: x.Pass, Valid: x.Valid,
			LoadP99: x.Types["load"].P99, ClickP99: x.Types["click"].P99, WriteP99: x.Types["write"].P99, Why: cmp.Or(x.Why, x.WhyInvalid)})
	}
	b.logf("sustain %-10s %.2f visits/s written into %s\n", sc.Name, s.Rate, b.o.RateFile)
	return writeRate(b.o.RateFile, sc.Name, RateEntry{Rate: s.Rate, Unit: rateUnit, Under: s.Under, Source: src})
}

// methodDirs are the folders of the code a run is made of, from test/compare: the package, its
// command (which reads the settings and counts the cpus) and the package that starts the servers.
// Every source file in them is in methodFiles or rateExcluded.
var methodDirs = []string{".", "cmd/compare", "../../internal/testproc"}

// methodFiles are the files of methodDirs a rate of Next.js is bound to, whole, by their path from
// test/compare: what builds and prepares the apps and the data, the plans and the traces, the replay
// and its writes, the start, pinning and control of the servers (the server of Next.js among them,
// spec.go, and how a process starts, internal/testproc), the command that sets a run up, the rule,
// the search and the confirmation, and every file they call into. A change to any of them is
// another method, even when the code a search runs is the same: the hash may take in more than
// runs, never less.
var methodFiles = []string{"cmd/compare/browse_cmd.go", "cmd/compare/main.go", "../../internal/testproc/bind_darwin.go", "../../internal/testproc/bind_linux.go", "../../internal/testproc/bind_other.go", "../../internal/testproc/files_other.go", "../../internal/testproc/files_unix.go", "../../internal/testproc/testproc.go", "browse.go", "build.go", "build_store_next.go", "compare.go", "crawl.go", "crawler.go", "domain.go", "landing.go", "load.go", "measure.go", "mix.go", "nocache.go", "plan.go", "rates.go", "record.go", "recorder.go", "render.go", "replay.go", "restock.go", "scenario.go", "serve.go", "setup.go", "series.go", "spec.go", "stats.go", "store.go", "store_links.json", "store_paths.go", "store_plan.go", "store_writes.go", "topology.go", "trace.go", "verify_store.go", "writepool.go"}

// rateExcluded are the files of test/compare the rate of Next.js is not bound to: the report, which
// only writes what a run decided, the table of the servers of every framework (specs.go) and the
// sources of the other apps, the closed-loop steps of the throughput, and what only the other
// comparisons and the tests use. Every source file is in methodFiles or here
// (TestEverySourceOfCompareIsInOrOutOfTheMethod).
var rateExcluded = []string{"attrs.go", "conns.go", "counted.go", "deps.go", "expected.go", "filters.go", "parity.go", "report.go", "report_browse.go", "specs.go", "store_deps.json", "store_list.go", "store_reactrouter.go", "store_start.go", "store_terms.go", "throughput.go", "tracecheck.go", "versions.go"}

// methodHash is a short hash of the files of the method, or "" when one cannot be read.
func methodHash(root string) string {
	h := sha256.New()
	for _, f := range methodFiles {
		data, err := os.ReadFile(filepath.Join(root, "test", "compare", f))
		if err != nil {
			return ""
		}
		fmt.Fprintf(h, "%s %d\n", f, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// appHash is a short hash of the sources of the Next.js app, of the domain code it shares and of the
// catalogue file of the shop that the domain code imports (ShopCatalogue): the files git tracks
// there, as they are on disk (so a change not committed yet counts), and the pages that Sync copied
// from the shop into the app (shop/, which git ignores), without the installed packages and the
// build. It is "" when git cannot list them.
func appHash(root string) string {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--", "test/compare/nextjs", "test/compare/domain", ShopCatalogue).Output()
	if err != nil {
		return ""
	}
	files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(files) == 0 || files[0] == "" {
		return ""
	}
	slices.Sort(files)
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return ""
		}
		fmt.Fprintf(h, "%s %d\n", f, len(data))
		h.Write(data)
	}
	copied := filepath.Join(root, "test", "compare", "nextjs", "shop")
	err = filepath.WalkDir(copied, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == copied {
			return filepath.SkipDir
		}
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(h, "%s %d\n", filepath.ToSlash(rel), len(data))
		h.Write(data)
		return nil
	})
	if err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// nextTraceSources are the lines of the stamp of the traces that the trace of Next.js was recorded
// from: the trees of the shop (whose pages are copied into the app), the Next.js app, the domain code
// and the overlays it shares, the recorder and the plan generators, and the version of Next.js, the
// browser, the site and the plans. The trees of Tifa, Start and React Router are not among them.
var nextTraceSources = []string{"tree examples/shop ", "tree test/compare/nextjs ", "tree test/compare/domain ", "tree test/compare/overlay ", "tree test/compare/recorder ", "tree test/compare/plan.go ", "tree test/compare/landing.go ", "tree test/compare/mix.go ", "tree test/compare/store_plan.go ", "tree test/compare/store_links.json ", "next ", "playwright ", "site ", "plans "}

// stampHash is a short hash of the lines of the stamp of the traces that the trace of Next.js was
// recorded from (nextTraceSources), or "" when the stamp does not say what they were recorded from: no
// tree, another site, or sources with changes that were not committed.
func stampHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if !slices.Contains(lines, "site "+SiteShop) || slices.Contains(lines, "dirty") ||
		!slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "tree ") }) {
		return ""
	}
	h := sha256.New()
	for _, l := range lines {
		if slices.ContainsFunc(nextTraceSources, func(p string) bool { return strings.HasPrefix(l, p) }) {
			fmt.Fprintln(h, l)
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// nextSourceFile is where the build of the Next.js app records the sources it was made from (appHash),
// in its output. The cache Next.js writes into .next while it serves is not part of it.
const nextSourceFile = "compare-source"

// writeNextSource records in the output of the Next.js app at dir the sources it was just built from.
func writeNextSource(root, dir string) error {
	h := appHash(root)
	if h == "" {
		return fmt.Errorf("%s: cannot tell the sources of the Next.js app (git ls-files failed)", dir)
	}
	return os.WriteFile(filepath.Join(dir, ".next", nextSourceFile), []byte(h+"\n"), 0o644)
}

// checkNextBuild says whether the Next.js app that runs was built from the sources of the checkout,
// which its rate is bound to: a build that does not say what it was built from, or one of other
// sources, is refused.
func (b *browse) checkNextBuild() error {
	path := filepath.Join(b.e.Next, ".next", nextSourceFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("the Next.js build does not say what it was built from (%v): build it again (compare build, or browse.sh build)", err)
	}
	if built, now := strings.TrimSpace(string(data)), appHash(b.e.Root); built != now {
		return fmt.Errorf("the Next.js build was made from other sources than the checkout (built from %q, the sources are %q): build it again (compare build, or browse.sh build)", built, now)
	}
	return nil
}

// nodeReads are the variables of the environment that change what a Node server does, by name
// (nodeNames) or by the start of a name (nodePrefixes): the options of Node and V8, the pool of
// libuv, the settings of Next.js, the allocator of libc, the time zone and the locale.
var (
	nodeNames    = []string{"GLIBC_TUNABLES", "LD_PRELOAD", "TZ", "LANG"}
	nodePrefixes = []string{"NODE_", "UV_", "V8_", "NEXT_", "__NEXT", "MALLOC_", "LC_"}
)

// goReads are the variables of the environment that change what the Go runtime of the load
// generator does. GOMAXPROCS is not among them: a pinned run sets it to its cpus.
var goReads = []string{"GODEBUG", "GOGC", "GOMEMLIMIT"}

// reads is the variables of env with one of names, or a name that starts with one of prefixes, sorted.
func reads(env, names, prefixes []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(names, name) || slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			out = append(out, kv)
		}
	}
	slices.Sort(out)
	return out
}

// loadGo is the Go of the load generator: its version and the settings of its runtime that env
// gives (goReads), as written.
func loadGo(env []string) string {
	return strings.Join(append([]string{runtime.Version()}, reads(env, goReads, nil)...), " ")
}

// nextjsServer is the server of Next.js a rate is bound to: the program, its arguments, and of the
// environment it starts with (Target.environ, the one it inherits and its own) every variable Node
// reads (nodeNames, nodePrefixes). The folder of the app is where it is on that machine and its sources are
// nextjsApp. It is "" when no spec is Next.js as it ships.
func nextjsServer(specs []Spec, inherited []string) string {
	i := slices.IndexFunc(specs, func(s Spec) bool { return s.Name == openBase })
	if i < 0 {
		return ""
	}
	t := specs[i].Target
	env := reads(t.environ(inherited), nodeNames, nodePrefixes)
	data, err := json.Marshal(struct {
		Prog string   `json:"prog"`
		Args []string `json:"args"`
		Env  []string `json:"env"`
	}{t.Prog, t.Args, env})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}
