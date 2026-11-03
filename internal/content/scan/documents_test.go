package scan

import (
	"slices"
	"testing"
)

func TestASecondYAMLDocumentIsAnError(t *testing.T) {
	for _, c := range []struct{ front, want string }{
		{"title: x\ndescription: d\n--- # more\ndraft: true\n", "a.md:5:1: the front matter holds a second YAML document: write all its keys in one map, without ... or --- between them"},
		{"title: x\ndescription: d\n...\ndraft: true\n", "a.md:4: the front matter is not YAML: did not find expected <document start>"},
		{"title: x\ndescription: d\n--- more text\n", "a.md:4:5: the front matter holds a second YAML document: write all its keys in one map, without ... or --- between them"},
	} {
		got := matterErrors(t, richPost{}, c.front)
		if !slices.Equal(got, []string{c.want}) {
			t.Errorf("%q:\nerrors %q\nwant   %q", c.front, got, c.want)
		}
	}
	// A document end marker with nothing after it is one document.
	if got := matterErrors(t, richPost{}, "title: x\ndescription: d\n...\n"); len(got) > 0 {
		t.Errorf("errors %q", got)
	}
}
