package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldExes lists the programs moveAside left in dir.
func oldExes(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tifa-dev.old-") {
			names = append(names, e.Name())
		}
	}
	return names
}

// moveAside frees the name of the program for go build, and deletes what an earlier call moved aside.
func TestMoveAsideFreesTheNameAndDeletesOldOnes(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tifa-dev"+exeSuffix)
	if err := moveAside(exe); err != nil {
		t.Fatalf("no program yet: %v", err)
	}
	if got := oldExes(t, dir); len(got) != 0 {
		t.Fatalf("moved aside without a program: %q", got)
	}
	writeFile(t, exe, "first")
	if err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exe); !os.IsNotExist(err) {
		t.Fatalf("the program is still at its name: %v", err)
	}
	first := oldExes(t, dir)
	if len(first) != 1 || !strings.HasSuffix(first[0], exeSuffix) {
		t.Fatalf("moved aside: %q", first)
	}
	writeFile(t, exe, "second")
	if err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	// The content tells the two programs apart, not the name: on Windows time.Now may return the same
	// time for both calls, as its clock moves on only every 0.5 to 15.6 ms, and the second program
	// then gets the name of the first, which is deleted before.
	second := oldExes(t, dir)
	if len(second) != 1 {
		t.Fatalf("after the second build: %q, want only the second program", second)
	}
	data, err := os.ReadFile(filepath.Join(dir, second[0]))
	if err != nil || string(data) != "second" {
		t.Fatalf("the program moved aside holds %q, %v", data, err)
	}
}
