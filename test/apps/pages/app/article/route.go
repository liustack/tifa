package article

import "github.com/liustack/tifa"

// Shelf is the data of the Layout around the article pages.
type Shelf struct {
	Note tifa.HTML `json:"note"`
}

func Layout(c *tifa.Ctx) (Shelf, error) {
	return Shelf{Note: `<p class=shelf>SHELF_NOTE<br/>of the shelf</p>`}, nil
}
