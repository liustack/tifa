package languages

import (
	"slices"
	"testing"

	"github.com/liustack/tifa"
)

// A plain go test, which tifa build did not make, reads the _locales files and tifa.yaml of the
// module of the current folder, at init and later.
func TestLanguagesOfPlainGoTest(t *testing.T) {
	want := []string{"zh-CN", "en-US"}
	if !slices.Equal(atInit, want) || !slices.Equal(tifa.Languages(), want) {
		t.Errorf("at init %v, now %v, want %v", atInit, tifa.Languages(), want)
	}
}
