// Package schemas holds schema types that go vet would refuse, for the test that compares the
// field trees reflect and go/types give. go vet ./... leaves testdata out.
package schemas

import "github.com/liustack/tifa/content"

// All lists a value of each schema type by its name.
var All = map[string]any{
	"UnexportedTag": UnexportedTag{},
}

type UnexportedTag struct {
	content.Entry
	Title string `json:"title"`
	note  string `json:"note"`
	skip  string `json:"-"`
}

var _ = UnexportedTag{}.note + UnexportedTag{}.skip
