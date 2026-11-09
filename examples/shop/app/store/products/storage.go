package products

import (
	"slices"
	"sync"
)

// The catalogue is kept in memory in place of a product table, and staff change it from the back
// office. catalogueMu guards it: a read takes a copy under the read lock, so a page works on one
// state of the catalogue from start to end, and a write changes it under the write lock.
//
// A write changes only fields that are values (the price, the names, the descriptions and the
// stock), never the slices of a record, so a copy of the records shares nothing a write changes.
var catalogueMu sync.RWMutex

// records returns a copy of the catalogue, in its order, which the caller may sort and keep.
func records() []productRecord {
	catalogueMu.RLock()
	defer catalogueMu.RUnlock()
	return slices.Clone(catalogue)
}

// record returns a copy of the product slug, and whether the catalogue has it.
func record(slug string) (productRecord, bool) {
	catalogueMu.RLock()
	defer catalogueMu.RUnlock()
	for _, p := range catalogue {
		if p.slug == slug {
			return p, true
		}
	}
	return productRecord{}, false
}
