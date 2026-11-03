package scan

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeTree writes files (path to content, relative to dir) and symbolic links (path to target).
func writeTree(t *testing.T, dir string, files, links map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.FromSlash(target), p); err != nil {
			t.Fatal(err)
		}
	}
}

func errorLines(res Result) []string {
	var out []string
	for _, e := range res.Errors {
		out = append(out, e.Error())
	}
	return out
}

const linkEntry = "---\ntitle: x\n---\nsecret words\n"

func TestLinksOutOfTheCollectionFailTheScan(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"secret.md":                       linkEntry,
		"outside/a.md":                    linkEntry,
		"outside/bundle/index.md":         linkEntry,
		"app/content/blog/en/real.md":     linkEntry,
		"app/content/blog/en/b/index.md":  linkEntry,
		"app/content/docs/real/a.md":      linkEntry,
		"app/content/notes/a.md":          linkEntry,
		"app/content/other/kept/index.md": linkEntry,
	}, map[string]string{
		// Out of the collection: a file, a folder, a bundle's index.md, a language folder and a
		// whole collection folder.
		"app/content/blog/en/private.md": "../../../../secret.md",
		"app/content/blog/en/linked":     "../../../../outside",
		"app/content/blog/en/c/index.md": "../../../../../secret.md",
		"app/content/blog/zh-CN":         "../../../outside",
		"app/content/wiki":               "../../outside",
		"app/content/docs/sneaky":        "../notes",
		// Inside the collection, which stays allowed.
		"app/content/blog/en/alias.md": "real.md",
		"app/content/blog/en/same":     "b",
		"app/content/docs/real/loop":   "..",
	})
	root := filepath.Join(tmp, "app")
	res := Scan(root, []Collection{
		collection(t, "blog", note{}, []string{"en", "zh-CN"}, ""),
		collection(t, "docs", note{}, nil, ""),
		collection(t, "wiki", note{}, nil, ""),
		collection(t, "notes", note{}, nil, ""),
	}, []string{"blog", "docs", "wiki", "notes", "other"}, Options{})
	got := errorLines(res)
	want := []string{
		"content/blog/en/c/index.md: links to ../../../../../secret.md, which is outside content/blog: a collection holds only its own files",
		"content/blog/en/linked: links to ../../../../outside, which is outside content/blog: a collection holds only its own files",
		"content/blog/en/private.md: links to ../../../../secret.md, which is outside content/blog: a collection holds only its own files",
		"content/blog/zh-CN: links to ../../../outside, which is outside content/blog: a collection holds only its own files",
		"content/docs/real/loop: links to .., a folder that holds it",
		"content/docs/sneaky: links to ../notes, which is outside content/docs: a collection holds only its own files",
		"content/wiki: links to ../../outside, which is outside content/: a collection holds only its own files",
	}
	if !slices.Equal(got, want) {
		t.Errorf("errors:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if data, _ := res.File.Marshal(); strings.Contains(string(data), "secret") {
		t.Errorf("a file from outside is in content.json:\n%s", data)
	}
}

func TestLinksInsideTheCollectionAreEntries(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"content/notes/real.md":      linkEntry,
		"content/notes/b/index.md":   linkEntry,
		"content/notes/_drafts/x.md": linkEntry,
	}, map[string]string{
		"content/notes/alias.md":    "real.md",
		"content/notes/same":        "b",
		"content/notes/fromskip.md": "_drafts/x.md",
	})
	res := Scan(tmp, []Collection{collection(t, "notes", note{}, nil, "")}, []string{"notes"}, Options{})
	if len(res.Errors) > 0 {
		t.Fatal(errorLines(res))
	}
	var ids []string
	for _, r := range res.File["notes"] {
		ids = append(ids, r.ID)
	}
	if want := []string{"alias", "b", "fromskip", "real", "same"}; !slices.Equal(ids, want) {
		t.Errorf("IDs %v, want %v", ids, want)
	}
}
