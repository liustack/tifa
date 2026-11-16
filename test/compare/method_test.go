package compare

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The method is a hash of the code that shapes the load and the verdicts: a change to one of its
// files is another method, and a file that is missing makes it unknown.
func TestTheMethodIsAHashOfItsFiles(t *testing.T) {
	root := t.TempDir()
	write := func(name, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755)
		os.WriteFile(filepath.Join(root, name), []byte(s), 0o644)
	}
	for _, f := range methodFiles {
		write(filepath.Join("test", "compare", f), f)
	}
	method := methodHash(root)
	if method == "" {
		t.Fatal("no method")
	}
	write("test/compare/replay.go", "changed")
	if methodHash(root) == method {
		t.Error("a change to the replay is the same method")
	}
	changed := methodHash(root)
	write("test/compare/stats.go", "another percentile")
	if methodHash(root) == changed {
		t.Error("a change to the percentiles is the same method")
	}
	os.Remove(filepath.Join(root, "test", "compare", "load.go"))
	if methodHash(root) != "" {
		t.Error("a missing file of the method gives a method")
	}
	for _, f := range []string{"load.go", "replay.go", "stats.go", "store_writes.go", "writepool.go", "browse.go", "latency.go"} {
		if !slices.Contains(methodFiles, f) {
			t.Errorf("the method leaves out %s", f)
		}
	}
}

// Every source file of test/compare is either part of the method or left out of it, by name: a new
// file that is neither fails here until it is put in one list. The report and the table of the
// servers of every framework are out, what prepares, replays, judges and starts a run is in. The
// folders are test/compare, its command and the package that starts the servers (methodDirs).
func TestEverySourceOfCompareIsInOrOutOfTheMethod(t *testing.T) {
	var files []string
	for _, dir := range methodDirs {
		fs, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(fs) == 0 {
			t.Fatalf("%s: %v %v", dir, fs, err)
		}
		files = append(files, fs...)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		in, out := slices.Contains(methodFiles, f), slices.Contains(methodExcluded, f)
		if in == out {
			t.Errorf("%s is in the method %v and left out %v: put it in methodFiles or in methodExcluded", f, in, out)
		}
	}
	for _, f := range append(slices.Clone(methodFiles), methodExcluded...) {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s is listed and is not there", f)
		}
	}
	for _, f := range []string{"browse.go", "latency.go", "load.go", "replay.go", "restock.go", "setup.go", "serve.go", "spec.go", "store_writes.go", "writepool.go", "store_paths.go", "store_links.json", "nocache.go", "cmd/compare/browse_cmd.go", "../../internal/testproc/testproc.go"} {
		if !slices.Contains(methodFiles, f) {
			t.Errorf("the method leaves out %s", f)
		}
	}
	for _, f := range []string{"report.go", "report_browse.go", "specs.go"} {
		if slices.Contains(methodFiles, f) {
			t.Errorf("the method holds %s", f)
		}
	}
	data, _ := os.ReadFile("specs.go")
	if !strings.Contains(string(data), "var BrowseFrameworks") {
		t.Error("the servers of browse are not in specs.go")
	}
}

// A server starts with the environment it inherits and then its own, and of two values of a name
// the last is the one it sees.
func TestAServerStartsWithTheEnvironmentItInheritsAndItsOwn(t *testing.T) {
	tg := Target{Env: []string{"NODE_ENV=production", "PORT=%PORT%"}}
	got := tg.environ([]string{"HOME=/a", "NODE_ENV=development", "PORT=1"})
	want := []string{"HOME=/a", "NODE_ENV=production", "PORT=%PORT%"}
	if !slices.Equal(got, want) {
		t.Errorf("environ = %q, want %q", got, want)
	}
}
