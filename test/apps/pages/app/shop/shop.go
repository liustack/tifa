// Package shop holds the shop domain and its Layout, which applies to /shops/{shopId} and below.
package shop

type Shop struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Owner *Owner `json:"owner,omitempty"`
}

type Owner struct {
	Name string `json:"name"`
}

type Item struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Price float64  `json:"price"`
	Tags  []string `json:"tags"`
}

var items = map[string]Item{
	"apple": {ID: "apple", Title: "Apple", Price: 1.5, Tags: []string{"fruit"}},
}

// FindItem returns the item with id, false when there is none.
func FindItem(id string) (Item, bool) {
	item, ok := items[id]
	return item, ok
}
