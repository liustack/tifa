package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Folders under docs/ that are not sections of the doc tree.
var notSections = []string{"studies", "research"}

// docLayoutProblems checks the layout of the docs of the module in dir. Every doc is in a section
// folder, docs/internals holds the docs for developing Tifa (audience dev) and the other sections
// hold the docs for apps, every doc has an order that is its own in the folder, and every section has
// an index.md with a title and an order of its own.
func docLayoutProblems(dir string) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	docs := filepath.Join(dir, "docs")
	entries, err := os.ReadDir(docs)
	if err != nil {
		return []string{err.Error()}
	}
	sectionOrders := map[int]string{}
	for _, e := range entries {
		if !e.IsDir() {
			if strings.HasSuffix(e.Name(), ".md") {
				add("docs/%s is not in a section folder", e.Name())
			}
			continue
		}
		if slices.Contains(notSections, e.Name()) {
			continue
		}
		section := "docs/" + e.Name()
		want := audienceApp
		if e.Name() == "internals" {
			want = audienceDev
		}
		index := readMeta(filepath.Join(docs, e.Name(), "index.md"))
		switch {
		case index == nil:
			add("%s has no index.md with front matter", section)
		default:
			if title, _ := index["title"].(string); title == "" {
				add("%s/index.md has no title", section)
			}
			n, ok := index["order"].(int)
			if !ok {
				add("%s/index.md has no order", section)
			} else if other, dup := sectionOrders[n]; dup {
				add("%s/index.md and %s/index.md have the same order %d", section, other, n)
			} else {
				sectionOrders[n] = section
			}
			if index["audience"] != want {
				add("%s/index.md has audience %v, want %s", section, index["audience"], want)
			}
		}
		orders := map[int]string{}
		err := fs.WalkDir(os.DirFS(filepath.Join(docs, e.Name())), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".zh-CN.md") || p == "index.md" {
				return err
			}
			path := section + "/" + p
			meta := readMeta(filepath.Join(docs, e.Name(), filepath.FromSlash(p)))
			if meta == nil {
				add("%s has no front matter", path)
				return nil
			}
			if meta["audience"] != want {
				add("%s has audience %v but is in %s, where the docs are for audience %s", path, meta["audience"], section, want)
			}
			n, ok := meta["order"].(int)
			if !ok {
				add("%s has no order", path)
			} else if other, dup := orders[n]; dup {
				add("%s and %s have the same order %d", path, other, n)
			} else {
				orders[n] = path
			}
			return nil
		})
		if err != nil {
			add("%s: %v", section, err)
		}
	}
	return problems
}

func readMeta(file string) map[string]any {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	rest, ok := strings.CutPrefix(string(data), "---\n")
	if !ok {
		return nil
	}
	head, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return nil
	}
	// read_when items may be invalid YAML (see TestDocFrontMatterIsValidYAML), cut it off.
	head, _, _ = strings.Cut(head, "\nread_when:")
	var m map[string]any
	if yaml.Unmarshal([]byte(head), &m) != nil {
		return nil
	}
	return m
}

func TestDocLayoutOfTheRepository(t *testing.T) {
	for _, p := range docLayoutProblems(filepath.Join("..", "..")) {
		t.Error(p)
	}
}

func TestDocLayoutProblemsAreFound(t *testing.T) {
	dir := t.TempDir()
	doc := func(rel, audience, order string) {
		text := "---\nsummary: S\naudience: " + audience + "\n"
		if order != "" {
			text += "order: " + order + "\n"
		}
		writeFile(t, filepath.Join(dir, "docs", rel), text+"---\n")
	}
	index := func(section, order, audience string) {
		writeFile(t, filepath.Join(dir, "docs", section, "index.md"), "---\ntitle: T\norder: "+order+"\naudience: "+audience+"\n---\n")
	}
	index("start", "1", "app")
	index("internals", "1", "dev") // the order of a section is its own
	index("deep", "2", "app")
	doc("start/a.md", "app", "1")
	doc("start/b.md", "app", "1")
	doc("start/c.md", "app", "")
	doc("start/sub/d.md", "app", "2")
	doc("internals/e.md", "app", "1")
	doc("deep/f.md", "dev", "1")
	doc("studies/g.md", "dev", "")
	doc("stray.md", "app", "1")
	got := strings.Join(docLayoutProblems(dir), "\n")
	for _, want := range []string{
		"docs/start/b.md and docs/start/a.md have the same order 1",
		"docs/start/c.md has no order",
		"docs/internals/e.md has audience app but is in docs/internals, where the docs are for audience dev",
		"docs/deep/f.md has audience dev but is in docs/deep, where the docs are for audience app",
		"docs/stray.md is not in a section folder",
		"docs/internals/index.md and docs/start/index.md have the same order 1",
	} {
		if !strings.Contains(got, want) && !strings.Contains(got, strings.Replace(want, "internals/index.md and docs/start/index.md", "start/index.md and docs/internals/index.md", 1)) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "studies") || strings.Contains(got, "sub/d.md") {
		t.Errorf("a doc that is fine or outside the sections is named:\n%s", got)
	}
}

func TestDocLayoutNeedsAnIndexForEverySection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docs", "start", "a.md"), "---\nsummary: S\naudience: app\norder: 1\n---\n")
	if got := strings.Join(docLayoutProblems(dir), "\n"); !strings.Contains(got, "docs/start has no index.md") {
		t.Errorf("problems:\n%s", got)
	}
}
