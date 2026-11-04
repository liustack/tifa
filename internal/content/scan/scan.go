// Package scan reads the content/ folder of an app as tifa build does: which files are entries,
// their IDs, slugs and paths, the front matter checked against the schema and turned into JSON,
// translations, drafts and word counts. The analysis runs it with schemas made by go/types, and
// tifatest with schemas made by reflect.
package scan

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/content"
)

// Collection is a declared collection as the scan needs it.
type Collection struct {
	// Name is Dir of the Source, the folder under content/.
	Name      string
	Languages []string
	Match     string
	Schema    *Node
	// Type names the schema type in messages, such as myblog.Post.
	Type string
	// LanguagesAt is where Languages is written, such as content.go:9:60, "" when not known.
	LanguagesAt string
}

// Options change what a scan keeps.
type Options struct {
	// Drafts keeps the entries whose front matter has draft: true, as tifa dev shows them.
	Drafts bool
}

// Error is a problem of a file of content/. Line and Col are 0 when it belongs to the whole file,
// and File is "" when it belongs to no file.
type Error struct {
	File      string
	Line, Col int
	Msg       string
}

func (e Error) Error() string {
	switch {
	case e.File == "":
		return e.Msg
	case e.Line == 0:
		return e.File + ": " + e.Msg
	case e.Col == 0:
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
}

// Result is what a scan found.
type Result struct {
	// File holds the entries of every collection that has no errors.
	File content.File
	// Errors is sorted by file and position.
	Errors []Error
	// Warnings are problems that do not stop a build, such as missing translations.
	Warnings []string
	// Markdown lists the entries of File for the Vite plugin to turn into HTML.
	Markdown []content.Markdown
}

// Dir is the folder of an app that holds the collections.
const Dir = "content"

// Scan reads the collections colls from the content folder of the app at root. declared lists the
// name of every declared collection, those left out of colls for errors of their declaration
// included: a folder of content/ that none of them names is an error.
func Scan(root string, colls []Collection, declared []string, opts Options) Result {
	s := &scanner{root: root, opts: opts, res: Result{File: content.File{}}}
	s.undeclared(declared)
	for _, c := range colls {
		s.collection(c)
	}
	slices.SortStableFunc(s.res.Errors, func(a, b Error) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col), cmp.Compare(a.Msg, b.Msg))
	})
	return s.res
}

type scanner struct {
	root string
	opts Options
	res  Result
	// limit is the real path of the folder of the collection being read, which its links must
	// not lead out of, and limitName its path in messages, such as content/blog.
	limit, limitName string
}

func (s *scanner) fail(file string, format string, args ...any) {
	s.res.Errors = append(s.res.Errors, Error{File: file, Msg: fmt.Sprintf(format, args...)})
}

// skipped reports whether a file or folder named name makes no entry.
func skipped(name string) bool { return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") }

// undeclared reports the folders of content/ that no collection reads.
func (s *scanner) undeclared(declared []string) {
	entries, err := os.ReadDir(filepath.Join(s.root, Dir))
	if err != nil {
		return // each collection reports its missing folder
	}
	for _, e := range entries {
		if skipped(e.Name()) || !isDir(filepath.Join(s.root, Dir, e.Name())) || slices.Contains(declared, e.Name()) {
			continue
		}
		s.fail(Dir+"/"+e.Name(), "no collection reads this folder: declare one in content.go, or move the folder out of content/")
	}
}

// candidate is a Markdown file that may be an entry.
type candidate struct {
	lang   string
	rel    string // below the language folder, or the collection folder without languages
	file   string // relative to the app root, such as content/blog/en/a/index.md
	bundle bool   // index.md of a folder of its own
}

// abs returns the file of p, a path relative to the app root with forward slashes.
func (s *scanner) abs(p string) string { return filepath.Join(s.root, filepath.FromSlash(p)) }

// link resolves the symbolic link at p, a path relative to the app root, and checks that it leads
// to a file or folder inside limit, a real path whose name in messages is limitName. ok is false
// when the link is broken or leads elsewhere, which is reported for a link that leads outside. The
// files of a collection go into the program, so a link must not bring in files from elsewhere.
func (s *scanner) link(p, limit, limitName string) (real string, ok bool) {
	target, err := os.Readlink(s.abs(p))
	if err != nil {
		s.fail(p, "%v", err)
		return "", false
	}
	real, err = filepath.EvalSymlinks(s.abs(p))
	if err != nil {
		s.fail(p, "links to %s, which does not exist", filepath.ToSlash(target))
		return "", false
	}
	if !within(limit, real) {
		s.fail(p, "links to %s, which is outside %s: a collection holds only its own files", filepath.ToSlash(target), limitName)
		return "", false
	}
	return real, true
}

// within reports whether the real path p is dir or below it.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// realDir returns the real path of the folder at p, a path relative to the app root.
func (s *scanner) realDir(p string) (string, bool) {
	real, err := filepath.EvalSymlinks(s.abs(p))
	if err != nil {
		s.fail(p, "%v", err)
		return "", false
	}
	return real, true
}

func (s *scanner) collection(c Collection) {
	dir := path.Join(Dir, c.Name)
	info, err := os.Lstat(s.abs(dir))
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		top, ok := s.realDir(Dir)
		if !ok {
			return
		}
		if _, ok := s.link(dir, top, Dir+"/"); !ok {
			return
		}
		info, err = os.Stat(s.abs(dir))
	}
	if err != nil || !info.IsDir() {
		s.fail("", "%s does not exist: create it, or fix Dir", dir)
		return
	}
	s.limit, err = filepath.EvalSymlinks(s.abs(dir))
	if err != nil {
		s.fail(dir, "%v", err)
		return
	}
	s.limitName = dir
	before := len(s.res.Errors)
	var cands []candidate
	if len(c.Languages) == 0 {
		cands = s.walk(dir, "", "", []string{s.limit})
	} else {
		entries, err := os.ReadDir(filepath.Join(s.root, filepath.FromSlash(dir)))
		if err != nil {
			s.fail(dir, "%v", err)
			return
		}
		at := "content.go"
		if c.LanguagesAt != "" {
			at = c.LanguagesAt
		}
		byLang := map[string][]candidate{}
		for _, e := range entries {
			name, p := e.Name(), dir+"/"+e.Name()
			if skipped(name) {
				continue
			}
			real, ok := s.follow(p, e, s.limit)
			if !ok {
				continue
			}
			switch {
			case isDir(real):
				if !slices.Contains(c.Languages, name) {
					s.fail(p, "not a language of the collection %s (%s), set in %s", c.Name, strings.Join(c.Languages, ", "), at)
					continue
				}
				byLang[name] = s.walk(p, name, "", []string{s.limit, real})
			case strings.HasSuffix(name, ".mdx"):
				s.mdx(p)
			case strings.HasSuffix(name, ".md"):
				s.fail(p, "the collection %s has languages, so its files go in a language folder, such as %s/%s/%s", c.Name, dir, c.Languages[0], name)
			}
		}
		for _, l := range c.Languages {
			cands = append(cands, byLang[l]...)
		}
	}
	if c.Match != "" {
		cands = slices.DeleteFunc(cands, func(x candidate) bool { return !match(c.Match, x.rel) })
	}
	// In path order, so that guide.md comes before guide/index.md, and of two entries that
	// clash the later one is reported.
	slices.SortStableFunc(cands, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(slices.Index(c.Languages, a.lang), slices.Index(c.Languages, b.lang)), cmp.Compare(a.rel, b.rel))
	})
	recs, md := s.entries(c, cands)
	if len(s.res.Errors) > before {
		return
	}
	s.res.Markdown = append(s.res.Markdown, md...)
	slices.SortStableFunc(recs, func(a, b content.Record) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(slices.Index(c.Languages, a.Lang), slices.Index(c.Languages, b.Lang)))
	})
	s.res.File[c.Name] = recs
	s.missing(c, recs)
}

// follow returns the real path of e, at p in the folder whose real path is parent. A symbolic link
// must lead inside the collection, see link.
func (s *scanner) follow(p string, e os.DirEntry, parent string) (string, bool) {
	if e.Type()&os.ModeSymlink == 0 {
		return filepath.Join(parent, e.Name()), true
	}
	return s.link(p, s.limit, s.limitName)
}

// inside reports whether the file at p, a path relative to the app root, is a file of the
// collection: a file that is not a symbolic link, or a link that leads inside the collection.
func (s *scanner) inside(p string) bool {
	info, err := os.Lstat(s.abs(p))
	if err != nil {
		s.fail(p, "%v", err)
		return false
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return true
	}
	_, ok := s.link(p, s.limit, s.limitName)
	return ok
}

// isDir reports whether the real path p is a folder.
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// walk lists the candidates below the folder base (relative to the app root), whose path below
// the language folder starts with rel. A folder holding index.md is one entry, and nothing below
// it is another. base itself is never such a folder. folders holds the real paths of the folders
// from the collection folder down to this one, which a link must not lead back to.
func (s *scanner) walk(base, lang, rel string, folders []string) []candidate {
	dir := path.Join(base, rel)
	here := folders[len(folders)-1]
	entries, err := os.ReadDir(here)
	if err != nil {
		s.fail(dir, "%v", err)
		return nil
	}
	var out []candidate
	for _, e := range entries {
		name := e.Name()
		if skipped(name) {
			continue
		}
		r := path.Join(rel, name)
		p := path.Join(base, r)
		real, ok := s.follow(p, e, here)
		if !ok {
			continue
		}
		if isDir(real) {
			if slices.Contains(folders, real) {
				target, _ := os.Readlink(s.abs(p))
				s.fail(p, "links to %s, a folder that holds it", filepath.ToSlash(target))
				continue
			}
			switch {
			case fileExists(filepath.Join(real, "index.md")):
				if s.inside(p + "/index.md") {
					out = append(out, candidate{lang: lang, rel: r + "/index.md", file: p + "/index.md", bundle: true})
				}
			case fileExists(filepath.Join(real, "index.mdx")):
				s.mdx(p + "/index.mdx")
			default:
				out = append(out, s.walk(base, lang, r, append(slices.Clip(folders), real))...)
			}
			continue
		}
		switch {
		case strings.HasSuffix(name, ".mdx"):
			s.mdx(p)
		case strings.HasSuffix(name, ".md"):
			out = append(out, candidate{lang: lang, rel: r, file: p})
		}
	}
	return out
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func (s *scanner) mdx(p string) {
	md := strings.TrimSuffix(path.Base(p), "x")
	s.fail(p, "MDX is not supported: a body becomes HTML at build time and cannot hold React components. Rename it to %s and put the components in the page", md)
}

// seenAt is an entry that holds an ID, a path or a translation key of one language, with where
// its front matter set it, if it did.
type seenAt struct {
	file string
	at   pos
}

// entries reads the candidates of c and checks that IDs, paths and translation keys are unique in
// each language. Drafts take part in the checks and are left out of the result unless Options.Drafts.
func (s *scanner) entries(c Collection, cands []candidate) ([]content.Record, []content.Markdown) {
	var recs []content.Record
	var md []content.Markdown
	ids, paths, groups := map[[2]string]seenAt{}, map[[2]string]seenAt{}, map[[2]string]seenAt{}
	for _, x := range cands {
		src, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(x.file)))
		if err != nil {
			s.fail(x.file, "%v", err)
			continue
		}
		m := parseMatter(src, c.Schema, c.Type)
		for _, e := range m.errs {
			s.res.Errors = append(s.res.Errors, Error{File: x.file, Line: e.at.line, Col: e.at.col, Msg: e.msg})
		}
		if len(m.errs) > 0 {
			continue
		}
		id := strings.TrimSuffix(x.rel, ".md")
		if x.bundle {
			id = strings.TrimSuffix(x.rel, "/index.md")
		}
		slug := m.slug
		if slug == "" {
			slug = path.Base(id)
			if why := segmentProblem(slug); why != "" {
				s.fail(x.file, "the slug %q, the last segment of the ID, is not one URL segment: %s. Rename the file, or set slug in the front matter", slug, why)
				continue
			}
		}
		entryPath := slug
		if d := path.Dir(id); d != "." {
			entryPath = d + "/" + slug
		}
		group := cmp.Or(m.group, id)
		here := func(at pos) seenAt { return seenAt{x.file, at} }
		if other, ok := ids[[2]string{x.lang, id}]; ok {
			s.fail(x.file, "the ID %s is also the ID of %s", id, other.file)
			continue
		}
		ids[[2]string{x.lang, id}] = here(pos{})
		if other, ok := paths[[2]string{x.lang, entryPath}]; ok {
			s.clash(here(m.slugAt), other, "the path %s is also the path of %s", entryPath)
			continue
		}
		paths[[2]string{x.lang, entryPath}] = here(m.slugAt)
		if other, ok := groups[[2]string{x.lang, group}]; ok {
			s.clash(here(m.groupAt), other, "translationKey %q is also used by %s: a language has one entry per translation", group)
			continue
		}
		groups[[2]string{x.lang, group}] = here(m.groupAt)
		if m.draft && !s.opts.Drafts {
			continue
		}
		out := content.BodyFile(c.Name, x.lang, id)
		recs = append(recs, content.Record{
			ID: id, Lang: x.lang, Slug: slug, Path: entryPath, Group: group,
			Date: m.date, Draft: m.draft, Words: countWords(m.body), Source: x.file, File: out, Data: m.data,
		})
		md = append(md, content.Markdown{
			Collection: c.Name, Lang: x.lang, ID: id, Source: x.file, Out: out,
			Text: strings.Repeat("\n", m.lines) + string(m.body), Frontmatter: m.data,
		})
	}
	// A relative URL of a translation is looked for next to the version in the default language
	// too, a draft or one still to come included.
	if len(c.Languages) > 0 {
		for i := range md {
			if md[i].Lang == c.Languages[0] {
				continue
			}
			if other, ok := groups[[2]string{c.Languages[0], recs[i].Group}]; ok {
				md[i].Fallback = path.Dir(other.file)
			}
		}
	}
	return recs, md
}

// clash reports that two entries share a path or a translation key. It goes to the one whose
// front matter set it, the later one when both did: the message names the other.
func (s *scanner) clash(cur, other seenAt, format, value string) {
	at, name := cur, other.file
	if cur.at == (pos{}) && other.at != (pos{}) {
		at, name = other, cur.file
	}
	s.res.Errors = append(s.res.Errors, Error{File: at.file, Line: at.at.line, Col: at.at.col, Msg: fmt.Sprintf(format, value, name)})
}

// missing warns about the entries that have no version in a language of c.
func (s *scanner) missing(c Collection, recs []content.Record) {
	if len(c.Languages) < 2 {
		return
	}
	langs := map[string]map[string]bool{} // by group
	first := map[string]string{}          // the ID of the first version of each group
	var order []string
	for _, r := range recs {
		if langs[r.Group] == nil {
			langs[r.Group] = map[string]bool{}
			order = append(order, r.Group)
		}
		langs[r.Group][r.Lang] = true
		if _, ok := first[r.Group]; !ok || r.Lang == c.Languages[0] {
			first[r.Group] = r.ID
		}
	}
	for _, l := range c.Languages {
		var ids []string
		for _, g := range order {
			if !langs[g][l] {
				ids = append(ids, first[g])
			}
		}
		if len(ids) == 0 {
			continue
		}
		slices.Sort(ids)
		list := ids
		if len(list) > 3 {
			list = append(slices.Clone(list[:3]), "...")
		}
		noun := "entries of %s have"
		if len(ids) == 1 {
			noun = "entry of %s has"
		}
		s.res.Warnings = append(s.res.Warnings, fmt.Sprintf("%d "+noun+" no %s version: %s", len(ids), c.Name, l, strings.Join(list, ", ")))
	}
}
