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
	// Data is the front matter turned into the JSON of the schema type.
	Data jsontext.Value `json:"data"`
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
	Decode func(recs []Record) (commit func(), err error)
	// Set makes values, each a T, the data of the collection, see tifatest.SetContent.
	Set func(values []any) (commit func(), err error)
	// Save returns a function that gives the collection back the data it has now.
	Save func() (restore func())
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

// Load reads content.json from dist and makes it the data of every declared collection. Either
// every collection gets its data, or none does. dist is nil only for an App without the files of
// tifa build, as in the tests of the server, and then Load does nothing.
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
	return LoadFile(f)
}

// LoadFile makes the records of f the data of every declared collection. It runs Validate on every
// entry first, and changes nothing when one fails.
func LoadFile(f File) error {
	var commits []func()
	for _, c := range Collections() {
		commit, err := c.Decode(f[c.Source.Dir])
		if err != nil {
			return fmt.Errorf("tifa: content: %w", err)
		}
		commits = append(commits, commit)
	}
	for _, commit := range commits {
		commit()
	}
	return nil
}
