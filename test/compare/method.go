package compare

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

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

// methodDirs are the folders of the code a run is made of, from test/compare: the package, its
// command (which reads the settings and counts the cpus) and the package that starts the servers.
// Every source file in them is in methodFiles or methodExcluded.
var methodDirs = []string{".", "cmd/compare", "../../internal/testproc"}

// methodFiles are the files of methodDirs a result is bound to (the setting "method" of a result,
// browseConfig), whole, by their path from test/compare: what builds and prepares the apps and the
// data, the plans and the traces, the replay and its writes, the start, pinning and control of the
// servers (spec.go, and how a process starts, internal/testproc), the command that sets a run up, the
// rule, the latency rounds and their rate, and every file they call into. A change to any of them is
// another method, even when the code a run plays is the same: the hash may take in more than runs,
// never less.
var methodFiles = []string{"cmd/compare/browse_cmd.go", "cmd/compare/main.go", "../../internal/testproc/bind_darwin.go", "../../internal/testproc/bind_linux.go", "../../internal/testproc/bind_other.go", "../../internal/testproc/files_other.go", "../../internal/testproc/files_unix.go", "../../internal/testproc/testproc.go", "browse.go", "build.go", "build_store_next.go", "calibrate.go", "calibration.go", "compare.go", "crawl.go", "crawler.go", "domain.go", "landing.go", "latency.go", "load.go", "machine.go", "measure.go", "method.go", "mix.go", "nocache.go", "plan.go", "record.go", "recorder.go", "render.go", "replay.go", "restock.go", "scenario.go", "serve.go", "setup.go", "series.go", "spec.go", "stats.go", "store.go", "store_actions.go", "store_links.json", "store_list.go", "store_paths.go", "store_plan.go", "store_terms.go", "store_writes.go", "topology.go", "trace.go", "verify_store.go", "writepool.go"}

// methodExcluded are the files of test/compare the method is not: the report, which only writes what
// a run decided, the table of the servers of every framework (specs.go) and the sources of the other
// apps, the closed-loop steps of the throughput, and what only the other comparisons and the tests
// use. Every source file is in methodFiles or here (TestEverySourceOfCompareIsInOrOutOfTheMethod).
var methodExcluded = []string{"announce.go", "attrs.go", "conns.go", "counted.go", "deps.go", "expected.go", "filters.go", "hold.go", "locks.go", "locks_other.go", "parity.go", "report.go", "report_browse.go", "specs.go", "status.go", "store_deps.json", "store_start.go", "throughput.go", "tracecheck.go", "versions.go"}

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
