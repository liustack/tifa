package products

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The records the shop starts with are catalogue.json, member for member, in its order.
func TestTheCatalogueFileIsTheCatalogue(t *testing.T) {
	var file []map[string]any
	if err := json.Unmarshal(catalogueFile, &file); err != nil {
		t.Fatal(err)
	}
	list := demoCatalogue()
	back := make([]fileProduct, len(list))
	for i, p := range list {
		if p.number != i+1 {
			t.Errorf("%s is number %d, at place %d", p.slug, p.number, i+1)
		}
		back[i] = fileProduct{
			Slug: p.slug, Name: p.name, Category: p.category, PriceFen: p.priceFen, CompareFen: p.compareFen,
			Rating: p.rating, Stock: p.stock, Badge: p.badge, Colors: p.colors, Sizes: p.sizes, Tags: p.tags,
			Summary: p.summary, Description: p.description, Materials: p.materials, Specs: p.specs, Added: p.added,
		}
	}
	data, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records, file) {
		for i := range min(len(file), len(records)) {
			if !reflect.DeepEqual(records[i], file[i]) {
				t.Fatalf("product %d: the record is\n%v\nthe file has\n%v", i+1, records[i], file[i])
			}
		}
		t.Fatalf("%d records of the %d products of the file", len(records), len(file))
	}
}

// A record the pages could not show stops the shop before it serves a page.
func TestTheCatalogueRefusesARecordItCannotShow(t *testing.T) {
	good := `{"slug":"cup","name":["杯","Cup"],"category":"drinkware","priceFen":100,"rating":4,"stock":1,"colors":["mist"],"tags":[],"summary":["好","Good"],"description":["好","Good"],"materials":[],"specs":[],"added":"2026-09-02T10:00:00Z"}`
	if list, err := readCatalogue([]byte("[" + good + "]")); err != nil || len(list) != 1 || list[0].number != 1 {
		t.Fatalf("a good record: %v, %v", list, err)
	}
	for name, bad := range map[string]string{
		"an unknown member":           strings.Replace(good, `"stock":1`, `"stock":1,"stok":2`, 1),
		"a text in one language":      strings.Replace(good, `["杯","Cup"]`, `["杯"]`, 1),
		"a colour the shop lacks":     strings.Replace(good, `["mist"]`, `["mauve"]`, 1),
		"a category the shop lacks":   strings.Replace(good, `"drinkware"`, `"garden"`, 1),
		"a badge the shop lacks":      strings.Replace(good, `"stock":1`, `"stock":1,"badge":"hot"`, 1),
		"a date that is not RFC 3339": strings.Replace(good, `2026-09-02T10:00:00Z`, `2026-09-02`, 1),
		"no colour":                   strings.Replace(good, `["mist"]`, `[]`, 1),
	} {
		if _, err := readCatalogue([]byte("[" + bad + "]")); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
