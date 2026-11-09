package products

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

// catalogueFile is the products the shop starts with, one record each, in the order of the catalogue.
// The comparison of test/compare reads the same file for its Node ports and its plans.
//
//go:embed catalogue.json
var catalogueFile []byte

// fileProduct is a record of catalogue.json. A member left out is the zero value: no sale price, no
// badge, no sizes. added is an RFC 3339 instant.
type fileProduct struct {
	Slug        string    `json:"slug"`
	Name        T         `json:"name"`
	Category    string    `json:"category"`
	PriceFen    int       `json:"priceFen"`
	CompareFen  int       `json:"compareFen,omitzero"`
	Rating      float64   `json:"rating"`
	Stock       int       `json:"stock"`
	Badge       string    `json:"badge,omitzero"`
	Colors      []string  `json:"colors"`
	Sizes       []string  `json:"sizes,omitzero"`
	Tags        []string  `json:"tags"`
	Summary     T         `json:"summary"`
	Description T         `json:"description"`
	Materials   []T       `json:"materials"`
	Specs       [][2]T    `json:"specs"`
	Added       time.Time `json:"added"`
}

// demoCatalogue returns the products the shop starts with, read from catalogue.json. Each call
// returns records of its own, which edits can change. The file is part of the program, so a file it
// cannot read is a bug of the shop, and it panics.
func demoCatalogue() []productRecord {
	list, err := readCatalogue(catalogueFile)
	if err != nil {
		panic("products: catalogue.json: " + err.Error())
	}
	return list
}

// readCatalogue decodes the records of a catalogue file. It refuses a member it does not know, a
// text without both languages, and a category, colour or badge the shop does not have.
func readCatalogue(data []byte) ([]productRecord, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var file []fileProduct
	if err := d.Decode(&file); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, fmt.Errorf("more than one value")
	}
	list := make([]productRecord, len(file))
	for i, f := range file {
		if err := f.check(); err != nil {
			return nil, fmt.Errorf("product %d (%q): %w", i+1, f.Slug, err)
		}
		list[i] = productRecord{
			slug: f.Slug, name: f.Name, category: f.Category, priceFen: f.PriceFen, compareFen: f.CompareFen,
			rating: f.Rating, stock: f.Stock, badge: f.Badge, colors: f.Colors, sizes: f.Sizes, tags: f.Tags,
			summary: f.Summary, description: f.Description, materials: f.Materials, specs: f.Specs,
			added: f.Added, number: i + 1,
		}
	}
	return list, nil
}

// check fails on a record the pages could not show.
func (f fileProduct) check() error {
	if f.Slug == "" || f.Added.IsZero() || len(f.Colors) == 0 {
		return fmt.Errorf("no slug, date added or colour")
	}
	texts := append([]T{f.Name, f.Summary, f.Description}, f.Materials...)
	for _, s := range f.Specs {
		texts = append(texts, s[0], s[1])
	}
	for _, t := range texts {
		if t[0] == "" || t[1] == "" {
			return fmt.Errorf("a text without both languages: %q", t)
		}
	}
	known := false
	for _, c := range categories {
		known = known || c.id == f.Category
	}
	if !known {
		return fmt.Errorf("no category %q", f.Category)
	}
	for _, id := range f.Colors {
		known = false
		for _, c := range colors {
			known = known || c.id == id
		}
		if !known {
			return fmt.Errorf("no colour %q", id)
		}
	}
	known = f.Badge == ""
	for _, b := range badges {
		known = known || b.id == f.Badge
	}
	if !known {
		return fmt.Errorf("no badge %q", f.Badge)
	}
	return nil
}
