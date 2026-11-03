package tifatest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/content"
	internalcontent "github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/internal/content/scan"
)

// LoadContent gives every content collection of the app the entries tifa build would give it.
// It reads content/ of the app, whose root is the folder of the go.mod found from the current
// directory up, with the same rules and checks as tifa build: drafts are left out, an entry whose
// date is still to come is hidden, Words is counted and Validate runs on each entry. Any error
// fails the test. The collections get back what they had when the test ends, so do not call it
// from tests that run in parallel.
//
// The test must import the root package of the app, whose content.go declares the collections.
func LoadContent(t testing.TB) {
	t.Helper()
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
// date is still to come is hidden. Validate does not run.
func SetContent[T any](t testing.TB, c *content.Collection[T], entries ...T) {
	t.Helper()
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
