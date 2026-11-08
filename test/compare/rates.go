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
	"slices"
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
var rateKeys = []string{"actionSpacing", "confirm", "cpu", "loadCpus", "method", "nextjs", "nextjsApp", "node", "plans", "rule", "scenario", "search", "serverCpus", "sources", "trace"}

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
// run: the method (the code that shapes the load and the verdicts) and the sources of the Next.js
// app, the scenario and the seed of its writes, the plans and the Next.js traces of its site, the
// sources the traces were recorded from (the stamp browse.sh writes: the shop, the apps, the shared
// domain code, the framework versions; "" when the traces have none), the cpu model and the pinning,
// the Node and Next.js versions, the spacing of actions, the rule, the search and the confirmation.
func (b *browse) rateConditions(sc BrowseScenario) map[string]string {
	site := sc.site()
	stamp := "STAMP"
	if site == SiteStore {
		stamp = "STAMP-store"
	}
	scenario, _ := json.Marshal(sc)
	// The sources of the traces are known only in a run that browse.sh checked against this checkout
	// (one with a stamp): a run by hand cannot tell what its traces were recorded from.
	sources := ""
	if b.o.Stamp != "" {
		sources = stampHash(filepath.Join(b.o.TraceDir, stamp), site)
	}
	sum := sha256.Sum256(fmt.Appendf(scenario, " seed %d", b.o.Seed))
	return map[string]string{
		"method":        methodHash(b.e.Root),
		"nextjsApp":     appHash(b.e.Root),
		"scenario":      hex.EncodeToString(sum[:8]),
		"plans":         fileHash(PlansPath(b.o.TraceDir, site)),
		"trace":         fileHash(TracePath(b.o.TraceDir, openBase, site)),
		"sources":       sources,
		"cpu":           b.res.Machine.CPU,
		"serverCpus":    b.o.ServerCPUs,
		"loadCpus":      b.o.LoadCPUs,
		"node":          b.res.Version["node"],
		"nextjs":        b.res.Version[openBase],
		"actionSpacing": ActionSpacing.String(),
		"rule":          LatencyRule,
		"search":        fmt.Sprintf("doubling from 4 visits/s up to %v, narrowed to 10%%, trials of %s after a warm-up of %s", b.maxRate(), b.o.TrialWindow, b.o.TrialWindow/3),
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

// methodFiles are the files of test/compare whose code shapes the load a run plays or the verdicts it
// gives: the plans, the replay and its writes, the pinning and the start of the servers, the rule, the
// search and the confirmation. The report is not among them: its wording measures nothing.
var methodFiles = []string{"browse.go", "load.go", "plan.go", "rates.go", "replay.go", "serve.go", "specs.go", "store_plan.go", "store_writes.go", "throughput.go", "topology.go", "trace.go", "verify_store.go", "writepool.go"}

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

// appHash is a short hash of the sources of the Next.js app and of the domain code it shares: the
// files git tracks there, as they are on disk (so a change not committed yet counts), without the
// installed packages and the build. It is "" when git cannot list them.
func appHash(root string) string {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--", "test/compare/nextjs", "test/compare/domain").Output()
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
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// stampHash is a short hash of the stamp of the traces of a site, or "" when it does not say what
// they were recorded from: no tree, another site, or sources with changes that were not committed.
func stampHash(path, site string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if !slices.Contains(lines, "site "+siteName(site)) || slices.Contains(lines, "dirty") ||
		!slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "tree ") }) {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}
