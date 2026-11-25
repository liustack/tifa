package main

import (
	"cmp"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	tifadocs "github.com/liustack/tifa/docs"
)

// appDocsIndex lists the docs for writing an app that this tifa carries, for the managed block of
// AGENTS.md: one line per section folder, in the order of its index.md, with the file names of its
// docs in their order. An agent reads AGENTS.md at the start of every session, so what each doc
// covers is not here: tifa docs prints it.
func appDocsIndex() (string, error) {
	return docsIndexOf(tifadocs.FS)
}

// docsIndexOf builds the index of appDocsIndex from files, which holds section folders of docs.
// A doc without a summary or an order fails it: scripts/docs-list.sh --check and
// TestDocLayoutOfTheRepository keep every doc of a release from that.
func docsIndexOf(files fs.FS) (string, error) {
	type doc struct {
		name  string
		order int
	}
	type section struct {
		dir   string
		order int
		docs  []doc
	}
	sections := map[string]*section{}
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".zh-CN.md") {
			return err
		}
		text, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		dir, name := path.Split(p)
		s := sections[dir]
		if s == nil {
			s = &section{dir: dir}
			sections[dir] = s
		}
		meta, _ := frontMatter(string(text))
		if meta.order == 0 {
			return fmt.Errorf("docs/%s has no order in its front matter", p)
		}
		if isSectionIndex(p) {
			s.order = meta.order
			return nil
		}
		if meta.summary == "" {
			return fmt.Errorf("docs/%s has no summary in its front matter", p)
		}
		s.docs = append(s.docs, doc{name, meta.order})
		return nil
	})
	if err != nil {
		return "", err
	}
	list := make([]*section, 0, len(sections))
	for _, s := range sections {
		if s.order == 0 {
			return "", fmt.Errorf("docs/%s has no index.md with an order", s.dir)
		}
		list = append(list, s)
	}
	slices.SortFunc(list, func(a, b *section) int { return cmp.Compare(a.order, b.order) })
	var out strings.Builder
	for _, s := range list {
		slices.SortFunc(s.docs, func(a, b doc) int { return cmp.Compare(a.order, b.order) })
		names := make([]string, len(s.docs))
		for i, d := range s.docs {
			names[i] = "`" + d.name + "`"
		}
		fmt.Fprintf(&out, "- `docs/%s`: %s\n", s.dir, strings.Join(names, ", "))
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
