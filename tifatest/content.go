package tifatest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/content"
	internalcontent "github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/internal/content/scan"
)

// LoadContent gives every content collection of the app the entries tifa build would give it.
// It reads content/ of the app, whose root is the folder of the go.mod found from the current
// directory up, with the same rules and checks as tifa build: drafts are left out, an entry whose
// date is still to come is hidden (see SetContentTime), Words is counted and Validate runs on each
// entry. Any error fails the test. The collections get back what they had when the test ends. The
// collections are shared by the whole test binary, so a test that calls LoadContent cannot run in
// parallel: it fails when the test or one of its parents called t.Parallel, and t.Parallel panics
// after it.
//
// The test must import the root package of the app, whose content.go declares the collections.
func LoadContent(t testing.TB) {
	t.Helper()
	notParallel(t, "LoadContent")
	colls := internalcontent.Collections()
	if len(colls) == 0 {
		t.Fatal("tifatest.LoadContent: no content collection is declared: import the root package of the app, whose content.go declares them")
	}
	root, err := appRoot()
	if err != nil {
		t.Fatalf("tifatest.LoadContent: %v", err)
	}
	var problems []string
	var scanned []scan.Collection
	var declared []string
	for _, c := range colls {
		declared = append(declared, c.Source.Dir)
		tree, ps := scan.FromReflect(c.Schema)
		for _, p := range ps {
			problems = append(problems, p.Msg)
		}
		if c.Source.Match != "" {
			if err := scan.CheckMatch(c.Source.Match); err != nil {
				problems = append(problems, err.Error())
			}
		}
		scanned = append(scanned, scan.Collection{
			Name: c.Source.Dir, Languages: c.Source.Languages, Match: c.Source.Match,
			Schema: tree, Type: c.Schema.String(),
		})
	}
	if len(problems) > 0 {
		t.Fatalf("tifatest.LoadContent:\n%s", strings.Join(problems, "\n"))
	}
	res := scan.Scan(root, scanned, declared, scan.Options{})
	if len(res.Errors) > 0 {
		lines := make([]string, len(res.Errors))
		for i, e := range res.Errors {
			lines[i] = e.Error()
		}
		t.Fatalf("tifatest.LoadContent:\n%s", strings.Join(lines, "\n"))
	}
	for _, c := range colls {
		t.Cleanup(c.Save())
	}
	if err := internalcontent.LoadFile(res.File); err != nil {
		t.Fatalf("tifatest.LoadContent: %v", err)
	}
}

// SetContent makes entries the entries of collection c until the test ends, in place of what
// LoadContent reads. Entries with the same ID are translations of each other. Words and the other
// fields of content.Entry are those of the entries, apart from Translations, and an entry whose
// date is still to come is hidden. Validate does not run. Like LoadContent, it cannot be called
// from a test that runs in parallel.
func SetContent[T any](t testing.TB, c *content.Collection[T], entries ...T) {
	t.Helper()
	notParallel(t, "SetContent")
	coll := internalcontent.Lookup(c)
	if coll == nil {
		t.Fatal("tifatest.SetContent: the collection was not made by content.Define")
	}
	values := make([]any, len(entries))
	for i, e := range entries {
		values[i] = e
	}
	commit, err := coll.Set(values)
	if err != nil {
		t.Fatalf("tifatest.SetContent: %v", err)
	}
	t.Cleanup(coll.Save())
	commit()
}

// SetContentTime has the content collections judge which entries can be seen at tm instead of
// now, until the test ends: an entry is shown when its date is not after tm. It works with
// LoadContent and SetContent, called before or after them, and the time does not move from tm.
// Like them, it cannot be called from a test that runs in parallel.
func SetContentTime(t testing.TB, tm time.Time) {
	t.Helper()
	notParallel(t, "SetContentTime")
	t.Cleanup(internalcontent.SetClock(internalcontent.Clock{Now: func() time.Time { return tm }}))
}

// notParallel fails the test when it or a parent runs in parallel, and keeps it from calling
// t.Parallel later. t.Setenv does both, since an environment variable is shared by the process as
// the content of a collection is.
func notParallel(t testing.TB, fn string) {
	t.Helper()
	parallel := false
	func() {
		defer func() {
			if recover() != nil {
				parallel = true
			}
		}()
		t.Setenv("TIFA_CONTENT_TEST", "1")
	}()
	if parallel {
		t.Fatalf("tifatest.%s: the content of a collection is shared by the whole test binary, so a test that sets it cannot run in parallel: remove t.Parallel from this test and its parents", fn)
	}
}

// appRoot returns the folder of the go.mod that holds the current directory.
func appRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod in the current directory or above it")
		}
		dir = parent
	}
}
