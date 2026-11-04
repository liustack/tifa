// Package content connects the content collections an app declares with the package
// github.com/liustack/tifa/content to the server, the build and tifatest. It holds the records
// of dist/content.json and the list of declared collections. It reads no Markdown: the package
// content/scan does, at build time and in tests.
package content

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"sync"
	"time"
)

// FileName is the name of the file in dist/ that holds the entries of every collection.
const FileName = "content.json"

// Record is one entry of a collection as dist/content.json holds it.
type Record struct {
	ID   string `json:"id"`
	Lang string `json:"lang,omitempty"`
	Slug string `json:"slug"`
	Path string `json:"path"`
	// Group is the translation key: the translationKey of the front matter, or the ID.
	Group string    `json:"group"`
	Date  time.Time `json:"date,omitzero"`
	// Draft is true only for the drafts tifa dev shows. tifa build leaves drafts out.
	Draft bool `json:"draft,omitzero"`
	Words int  `json:"words"`
	// Source is the Markdown file relative to the app root, such as content/blog/en/a/index.md.
	Source string `json:"source"`
	// File is the body of the entry in dist, such as content/blog/en/a.html, see BodyFile. Under
	// tifa dev it is in the folder of the conversion that wrote it, such as content/3/blog/en/a.html.
	File string `json:"file"`
	// Headings are the h1 to h6 of the body in the order they come, which the Vite plugin
	// collects. tifa build adds them, a scan has none.
	Headings []Heading `json:"headings,omitempty"`
	// Data is the front matter turned into the JSON of the schema type.
	Data jsontext.Value `json:"data"`
}

// Heading is one h1 to h6 of a body.
type Heading struct {
	Depth int    `json:"depth"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

// BodyFile is where the body of an entry goes in dist: content/<collection>/<lang>/<ID>.html,
// without the language folder for a collection without languages.
func BodyFile(collection, lang, id string) string {
	return path.Join("content", collection, lang, id+".html")
}

// Markdown is one entry that the tifa Vite plugin turns into HTML, as EntriesFile lists it.
type Markdown struct {
	Collection string `json:"collection"`
	Lang       string `json:"lang"`
	ID         string `json:"id"`
	// Source is the Markdown file relative to the app root, which relative URLs start from.
	Source string `json:"source"`
	// Fallback is the folder of the version in the default language, relative to the app root,
	// where a relative URL is looked for when the file is not next to Source. "" for an entry in
	// the default language or without one.
	Fallback string `json:"fallback,omitempty"`
	// Out is the file of the HTML in dist, see BodyFile.
	Out string `json:"out"`
	// Text is the Markdown of Source with each line of the front matter left empty, so that the
	// lines of the body keep their numbers.
	Text string `json:"text"`
	// Frontmatter is the front matter as the JSON of the schema type.
	Frontmatter jsontext.Value `json:"frontmatter"`
}

// EntriesFile and HeadingsFile are where the build and the Vite plugin meet, relative to .tifa:
// the build writes the entries to convert into EntriesFile ({"entries": [Markdown...]}), and the
// plugin writes the HTML of each into dist and its headings into HeadingsFile, by Out.
const (
	EntriesFile  = "content/entries.json"
	HeadingsFile = "content/headings.json"
)

// Entries is the content of EntriesFile.
type Entries struct {
	Entries []Markdown `json:"entries"`
}

// AddHeadings puts the headings the Vite plugin found, by the Out of each entry, into the records
// of f. Every record must have its headings: a record the plugin did not convert is an error.
func (f File) AddHeadings(headings map[string][]Heading) error {
	for name, recs := range f {
		for i := range recs {
			h, ok := headings[recs[i].File]
			if !ok {
				return fmt.Errorf("the Vite plugin did not convert %s of collection %s", recs[i].Source, name)
			}
			recs[i].Headings = h
		}
	}
	return nil
}

// File is the content of dist/content.json: the records of each collection by its name, sorted
// by ID and then by the order of the languages. A collection without entries may be missing.
type File map[string][]Record

// Marshal writes f with its keys sorted, so that the same entries give the same bytes.
func (f File) Marshal() ([]byte, error) {
	return json.Marshal(f, json.Deterministic(true))
}

// Source is the content.Source of a collection.
type Source struct {
	Dir       string
	Languages []string
	Match     string
}

// Collection is one collection declared with content.Define. The functions come from the generic
// code of the content package, which knows the schema type.
type Collection struct {
	Source Source
	// Value is the *content.Collection[T] that Define returned.
	Value any
	// Schema is T.
	Schema reflect.Type
	// Decode turns records into entries of T and runs Validate on each. commit makes them the
	// data of the collection.
	// dist holds the bodies of the records, nil in tests, which have no build.
	Decode func(recs []Record, dist fs.FS) (commit func(), err error)
	// Set makes values, each a T, the data of the collection, see tifatest.SetContent.
	Set func(values []any) (commit func(), err error)
	// Save returns a function that gives the collection back the data it has now.
	Save func() (restore func())
	// Show makes the entries whose date is not after t the ones the collection shows, and returns
	// those it did not show before, sorted by ID, and the first date after t, zero without one.
	// It does nothing for a collection without data.
	Show func(t time.Time) (shown []Shown, next time.Time)
}

var (
	mu          sync.Mutex
	collections []*Collection
)

// Register adds a collection. It fails when another collection reads the same folder.
func Register(c *Collection) error {
	mu.Lock()
	defer mu.Unlock()
	for _, other := range collections {
		if other.Source.Dir == c.Source.Dir {
			return fmt.Errorf("two collections read content/%s", c.Source.Dir)
		}
	}
	collections = append(collections, c)
	return nil
}

// Unregister removes the collection whose Value is v. Only tests use it, for a collection that
// must not outlive the test that defines it.
func Unregister(v any) {
	mu.Lock()
	defer mu.Unlock()
	collections = slices.DeleteFunc(collections, func(c *Collection) bool { return c.Value == v })
}

// Collections returns the declared collections in the order they were declared.
func Collections() []*Collection {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(collections)
}

// Lookup returns the collection whose Value is v, nil when v was not declared with Define.
func Lookup(v any) *Collection {
	mu.Lock()
	defer mu.Unlock()
	for _, c := range collections {
		if c.Value == v {
			return c
		}
	}
	return nil
}

// Load reads content.json from dist and makes it the data of every declared collection with
// Install. Either every collection gets its data, or none does. dist is nil only for an App
// without the files of tifa build, as in the tests of the server, and then Load does nothing.
func Load(dist fs.FS) error {
	if dist == nil || len(Collections()) == 0 {
		return nil
	}
	data, err := fs.ReadFile(dist, FileName)
	if err != nil {
		return fmt.Errorf("tifa: content: reading %s of the build: %w", FileName, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("tifa: content: reading %s of the build: %w", FileName, err)
	}
	return Install(f, dist)
}

// LoadFile makes the records of f the data of every declared collection, as Install does without
// the files of a build: the body of each entry is a placeholder, as tifatest gives it.
func LoadFile(f File) error { return Install(f, nil) }
