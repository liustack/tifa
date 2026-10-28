package products

import (
	"fmt"
	"os"
	"testing"
)

// Every product has a photo of itself, a close detail and one in use, each in the three widths
// the pages pick from.
func TestEveryProductHasItsPhotos(t *testing.T) {
	for _, p := range catalogue {
		for _, view := range []string{"main", "detail", "scene"} {
			for _, width := range []int{160, 480, 1200} {
				if _, err := os.Stat(fmt.Sprintf("../../images/%s/%s-%d.avif", p.slug, view, width)); err != nil {
					t.Errorf("%s: %v", p.slug, err)
				}
			}
		}
	}
}
