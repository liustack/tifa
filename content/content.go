// Package content reads the Markdown files of an app as typed collections. A collection is
// declared in content.go in the root of the app:
//
//	var Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
//
//	type Post struct {
//		content.Entry
//		Title string   `json:"title"`
//		Tags  []string `json:"tags"`
//	}
//
// tifa build reads content/blog, checks the front matter of each entry against Post and embeds the
// entries in the program, which loads them before OnStart. Load functions and handlers read them
// with the methods of the collection. See docs/content.md.
package content

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"html"
	"io/fs"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/content"
)

// Source says where the entries of a collection are. Write it in place as the argument of Define,
// with string literals only: tifa build reads it from the source code.
type Source struct {
	// Dir is the folder under content/ that holds the collection, and the name of the collection:
	// letters, digits, - and _.
	Dir string
	// Languages lists the languages of the collection, the default one first. With languages,
	// each entry is in the folder of its language, such as content/blog/en/.
	Languages []string
	// Match keeps only the entries whose path matches it, from below the language folder (or the
	// collection folder without languages). Segments match as in path.Match, and ** matches any
	// number of folders, such as "*/*/index.md". Most collections need none.
	Match string
}

// Entry holds what tifa knows of an entry. Every schema type embeds it, and tifa fills it.
type Entry struct {
	// ID is the path of the file below the language folder, without .md and without the /index of
	// an index.md in a folder of its own, such as 202608/free-search.
	ID string `json:"id"`
	// Lang is the language of the entry, "" in a collection without languages.
	Lang string `json:"lang"`
	// Slug is the slug of the front matter, or else the last segment of ID.
	Slug string `json:"slug"`
	// Path is ID with its last segment replaced by Slug.
	Path string `json:"path"`
	// Date is the date of the front matter, zero without one: a date alone is midnight UTC. An
	// entry whose date is still to come is hidden until that moment.
	Date time.Time `json:"date,omitzero"`
	// Draft is true for an entry whose front matter has draft: true, which only tifa dev shows.
	Draft bool `json:"draft,omitzero"`
	// Translations lists the versions of the entry in each language that can be seen now, itself
	// included, in the order of Languages.
	Translations []Translation `json:"translations"`
	// Words is the number of words of the body: each Chinese, Japanese or Korean character is
	// one, and so is each run of other letters and digits.
	Words int `json:"words"`

	// body is where the body and the headings of the entry come from, nil for an entry that no
	// collection holds.
	body *body
}

// entry gives the content package the Entry inside any schema type.
func (e *Entry) entry() *Entry { return e }

// body is what an entry knows of its body.
type body struct {
	collection string
	// dist holds the files of the build, nil in tests.
	dist     fs.FS
	file     string
	headings []Heading
}

// Body returns the body of the entry, the HTML tifa build made from its Markdown. It reads the file
// of the build on each call (tifa dev reads it from .tifa/dist) and keeps no copy. A page shows it
// with <HTML value={...}> of tifa/client.
//
// Tests have no build: for an entry of tifatest.LoadContent or tifatest.SetContent, Body returns
// the placeholder <p>tifatest: body of <collection> <lang>/<ID></p>. Body panics for an entry that
// no collection holds, such as one made in place.
func (e Entry) Body() tifa.HTML {
	b := e.body
	switch {
	case b == nil:
		panic(fmt.Sprintf("content: Body of the entry %q, which no collection holds: read entries from a collection, or in a test from tifatest.SetContent", e.ID))
	case b.dist == nil:
		return tifa.HTML("<p>tifatest: body of " + html.EscapeString(b.collection+" "+path.Join(e.Lang, e.ID)) + "</p>")
	}
	data, err := fs.ReadFile(b.dist, b.file)
	if err != nil {
		panic(fmt.Sprintf("content: the body of %s %s: %v", b.collection, path.Join(e.Lang, e.ID), err))
	}
	return tifa.HTML(data)
}

// Headings returns the h1 to h6 of the body in the order they come, with the id each has in the
// HTML. In tests they are nil, see Body. The slice is new on each call.
func (e Entry) Headings() []Heading {
	if e.body == nil {
		return nil
	}
	return slices.Clone(e.body.headings)
}

// Heading is one h1 to h6 of a body: its level, its id and its text.
type Heading struct {
	Depth int    `json:"depth"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

// Translation is one version of an entry.
type Translation struct {
	Lang string `json:"lang"`
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Path string `json:"path"`
}

// Collection holds the entries of one collection. Its methods are safe to call from any goroutine.
type Collection[T any] struct {
	src  Source
	data atomic.Pointer[data[T]]
}

// item is one loaded entry with its translation key.
type item[T any] struct {
	v     T
	group string
}

// data is what a collection has loaded: every entry, and the ones that can be seen now.
type data[T any] struct {
	items []item[T] // sorted by ID, then by the order of the languages
	view  *view[T]
}

// view holds the entries that can be seen at one time, with their Translations.
type view[T any] struct {
	all    []T
	byLang map[string][]T
	index  map[[2]string]int // by language and ID, into all
}

var dirName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Define declares a collection of entries of type T, which must be a struct that embeds Entry. It
// is written only as the value of a package-level var in content.go in the root of the app, with a
// Source literal, such as
//
//	var Blog = content.Define[Post](content.Source{Dir: "blog"})
//
// tifa build reports every other use. Define panics on the mistakes it can see itself.
func Define[T any](src Source) *Collection[T] {
	if err := checkSchema(reflect.TypeFor[T]()); err != nil {
		panic(err.Error())
	}
	if !dirName.MatchString(src.Dir) {
		panic(fmt.Sprintf("content.Define: Dir %q must be the name of one folder under content/: letters, digits, - and _", src.Dir))
	}
	for i, l := range src.Languages {
		if !dirName.MatchString(l) {
			panic(fmt.Sprintf("content.Define: language %q must be the name of one folder: letters, digits, - and _", l))
		}
		if slices.Contains(src.Languages[:i], l) {
			panic(fmt.Sprintf("content.Define: Languages has %q twice", l))
		}
	}
	c := &Collection[T]{src: Source{Dir: src.Dir, Languages: slices.Clone(src.Languages), Match: src.Match}}
	err := content.Register(&content.Collection{
		Source: content.Source{Dir: src.Dir, Languages: slices.Clone(src.Languages), Match: src.Match},
		Value:  c,
		Schema: reflect.TypeFor[T](),
		Decode: c.decode,
		Set:    c.set,
		Save: func() func() {
			d := c.data.Load()
			return func() { c.data.Store(d) }
		},
		Show: c.show,
	})
	if err != nil {
		panic("content.Define: " + err.Error())
	}
	return c
}

var entryType = reflect.TypeFor[Entry]()

// checkSchema checks that t is a struct that embeds Entry, not a pointer to it.
func checkSchema(t reflect.Type) error {
	if t.Kind() != reflect.Struct || !slices.ContainsFunc(reflect.VisibleFields(t), func(f reflect.StructField) bool {
		return f.Anonymous && len(f.Index) == 1 && f.Type == entryType
	}) {
		return fmt.Errorf("content.Define: %s must be a struct type that embeds content.Entry, not a pointer to it", t)
	}
	for _, m := range []string{"UnmarshalJSON", "UnmarshalJSONFrom", "UnmarshalText"} {
		if _, ok := reflect.PointerTo(t).MethodByName(m); ok {
			return fmt.Errorf("content.Define: %s reads its own JSON, so front matter cannot be checked against it: remove its UnmarshalJSON, UnmarshalJSONFrom or UnmarshalText method", t)
		}
	}
	if embeddedEntries(t) > 1 {
		return fmt.Errorf("content.Define: %s embeds content.Entry more than once: a schema embeds it once, as its own field", t)
	}
	return nil
}

// embeddedEntries counts the content.Entry fields that t embeds, directly or through embedded
// structs. A struct cannot embed itself other than through a pointer, which is not followed.
func embeddedEntries(t reflect.Type) int {
	n := 0
	for i := range t.NumField() {
		f := t.Field(i)
		switch {
		case !f.Anonymous:
		case f.Type == entryType:
			n++
		case f.Type.Kind() == reflect.Struct:
			n += embeddedEntries(f.Type)
		}
	}
	return n
}

func entryOf[T any](v *T) *Entry {
	return any(v).(interface{ entry() *Entry }).entry()
}

// validator is a schema type with its own checks.
type validator interface{ Validate() error }

// decode turns the records of content.json into entries and checks each with Validate.
func (c *Collection[T]) decode(recs []content.Record, dist fs.FS) (func(), error) {
	items := make([]item[T], 0, len(recs))
	for _, r := range recs {
		var v T
		if err := json.Unmarshal(r.Data, &v); err != nil {
			return nil, fmt.Errorf("%s: %w", r.Source, err)
		}
		*entryOf(&v) = Entry{ID: r.ID, Lang: r.Lang, Slug: r.Slug, Path: r.Path, Date: r.Date, Draft: r.Draft, Words: r.Words, body: c.body(dist, r)}
		if val, ok := any(&v).(validator); ok {
			if err := val.Validate(); err != nil {
				return nil, fmt.Errorf("%s: Validate: %w", r.Source, err)
			}
		}
		items = append(items, item[T]{v: v, group: cmp.Or(r.Group, r.ID)})
	}
	return c.commit(items), nil
}

// body says where the body of the entry of r is, in dist when there is a build.
func (c *Collection[T]) body(dist fs.FS, r content.Record) *body {
	b := &body{collection: c.src.Dir}
	if dist != nil {
		b.dist, b.file = dist, r.File
		b.headings = make([]Heading, len(r.Headings))
		for i, h := range r.Headings {
			b.headings[i] = Heading(h)
		}
	}
	return b
}

// set makes values the entries of the collection, each its own translation key by ID.
func (c *Collection[T]) set(values []any) (func(), error) {
	items := make([]item[T], 0, len(values))
	seen := map[[2]string]bool{}
	for _, x := range values {
		v, ok := x.(T)
		if !ok {
			return nil, fmt.Errorf("an entry of collection %s is a %T, not a %s", c.src.Dir, x, reflect.TypeFor[T]())
		}
		e := entryOf(&v)
		switch {
		case len(c.src.Languages) == 0 && e.Lang != "":
			return nil, fmt.Errorf("entry %s has the language %q, and collection %s has no languages: leave Lang empty", e.ID, e.Lang, c.src.Dir)
		case len(c.src.Languages) > 0 && !slices.Contains(c.src.Languages, e.Lang):
			return nil, fmt.Errorf("entry %s has the language %q, which is not a language of collection %s (%s)", e.ID, e.Lang, c.src.Dir, joinLangs(c.src.Languages))
		case seen[[2]string{e.Lang, e.ID}]:
			return nil, fmt.Errorf("collection %s has the entry %q of language %q twice", c.src.Dir, e.ID, e.Lang)
		}
		e.body = &body{collection: c.src.Dir}
		seen[[2]string{e.Lang, e.ID}] = true
		items = append(items, item[T]{v: v, group: e.ID})
	}
	return c.commit(items), nil
}

// commit sorts items and returns the function that makes them the data of the collection.
func (c *Collection[T]) commit(items []item[T]) func() {
	slices.SortStableFunc(items, func(a, b item[T]) int {
		x, y := entryOf(&a.v), entryOf(&b.v)
		return cmp.Or(cmp.Compare(x.ID, y.ID), cmp.Compare(c.langIndex(x.Lang), c.langIndex(y.Lang)))
	})
	return func() {
		c.data.Store(&data[T]{items: items, view: c.makeView(items, content.Now())})
	}
}

// show makes the entries that can be seen at t the ones the collection shows, see
// content.Collection.Show. The data may be replaced meanwhile, and then show judges the new data.
func (c *Collection[T]) show(t time.Time) ([]content.Shown, time.Time) {
	for {
		d := c.data.Load()
		if d == nil {
			return nil, time.Time{}
		}
		v := c.makeView(d.items, t)
		if !c.data.CompareAndSwap(d, &data[T]{items: d.items, view: v}) {
			continue
		}
		var shown []content.Shown
		for _, x := range v.all {
			e := entryOf(&x)
			if _, ok := d.view.index[[2]string{e.Lang, e.ID}]; !ok {
				shown = append(shown, content.Shown{Collection: c.src.Dir, Lang: e.Lang, ID: e.ID, Date: e.Date})
			}
		}
		var next time.Time
		for _, it := range d.items {
			if date := entryOf(&it.v).Date; date.After(t) && (next.IsZero() || date.Before(next)) {
				next = date
			}
		}
		return shown, next
	}
}

func (c *Collection[T]) langIndex(lang string) int { return slices.Index(c.src.Languages, lang) }

// makeView returns the entries that can be seen at t, with their Translations: an entry whose
// date comes after t is left out.
func (c *Collection[T]) makeView(items []item[T], t time.Time) *view[T] {
	visible := make([]item[T], 0, len(items))
	groups := map[string][]Translation{}
	for _, it := range items {
		e := entryOf(&it.v)
		if e.Date.After(t) {
			continue
		}
		visible = append(visible, it)
		groups[it.group] = append(groups[it.group], Translation{Lang: e.Lang, ID: e.ID, Slug: e.Slug, Path: e.Path})
	}
	for _, g := range groups {
		slices.SortStableFunc(g, func(a, b Translation) int { return cmp.Compare(c.langIndex(a.Lang), c.langIndex(b.Lang)) })
	}
	v := &view[T]{all: make([]T, 0, len(visible)), byLang: map[string][]T{}, index: map[[2]string]int{}}
	for _, it := range visible {
		x := it.v
		e := entryOf(&x)
		e.Translations = slices.Clone(groups[it.group])
		v.index[[2]string{e.Lang, e.ID}] = len(v.all)
		v.all = append(v.all, x)
		v.byLang[e.Lang] = append(v.byLang[e.Lang], x)
	}
	return v
}

// current returns the entries that can be seen now, and panics when the collection has no data.
func (c *Collection[T]) current() *view[T] {
	d := c.data.Load()
	if d == nil {
		if testing.Testing() {
			panic(fmt.Sprintf("content: collection %s has no data: in a test call tifatest.LoadContent(t) or tifatest.SetContent", c.src.Dir))
		}
		panic(fmt.Sprintf("content: collection %s has no data yet: read it from OnStart on, not while packages are initialized", c.src.Dir))
	}
	return d.view
}

// All returns the entries of every language that can be seen now, sorted by ID and, for one ID,
// in the order of the languages. Drafts and entries whose date is still to come are left out.
// The slice is new on each call, so it can be sorted or changed.
func (c *Collection[T]) All() []T { return slices.Clone(c.current().all) }

// In returns the entries of one language, sorted by ID, as All does. It returns nil for a language
// the collection does not have. In a collection without languages, In("") is All().
func (c *Collection[T]) In(lang string) []T {
	v := c.current()
	if len(c.src.Languages) == 0 {
		if lang != "" {
			return nil
		}
		return slices.Clone(v.all)
	}
	if !slices.Contains(c.src.Languages, lang) {
		return nil
	}
	return slices.Clone(v.byLang[lang])
}

// Get returns the entry of a language by ID. It returns tifa.ErrNotFound when there is none, or it
// cannot be seen yet, so a Load can return the error as it is. In a collection without languages
// lang must be "", and Get panics on another value, which is a mistake in the code.
func (c *Collection[T]) Get(lang, id string) (T, error) {
	v := c.current()
	if len(c.src.Languages) == 0 && lang != "" {
		panic(fmt.Sprintf("content: Get on collection %s, which has no languages, with lang %q: pass \"\"", c.src.Dir, lang))
	}
	i, ok := v.index[[2]string{lang, id}]
	if !ok {
		var zero T
		return zero, tifa.ErrNotFound
	}
	return v.all[i], nil
}

// Languages returns the languages of the collection, the default one first, nil without any.
func (c *Collection[T]) Languages() []string { return slices.Clone(c.src.Languages) }

func joinLangs(langs []string) string { return strings.Join(langs, ", ") }
