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
// AGENTS.md: a line with each section folder, in the order of its index.md, then one line per doc
// in its order with the file name and the first clause of the summary, as tifa docs prints it.
func appDocsIndex() (string, error) {
	return docsIndexOf(tifadocs.FS)
}

// docsIndexOf builds the index of appDocsIndex from files, which holds section folders of docs.
// A doc without a summary or an order fails it: scripts/docs-list.sh --check and
// TestDocLayoutOfTheRepository keep every doc of a release from that.
func docsIndexOf(files fs.FS) (string, error) {
	type doc struct {
		name, summary string
		order         int
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
		s.docs = append(s.docs, doc{name, firstSentence(meta.summary), meta.order})
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
	for i, s := range list {
		if i > 0 {
			out.WriteString("\n")
		}
		fmt.Fprintf(&out, "`docs/%s`\n", s.dir)
		slices.SortFunc(s.docs, func(a, b doc) int { return cmp.Compare(a.order, b.order) })
		for _, d := range s.docs {
			fmt.Fprintf(&out, "- `%s`: %s\n", d.name, d.summary)
		}
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
