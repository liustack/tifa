package scan

import (
	"slices"
	"testing"
)

// TestExplicitYAMLTagsAreChecked writes values with an explicit tag such as !!bool, which must
// not get past the checks of their type.
func TestExplicitYAMLTagsAreChecked(t *testing.T) {
	const head = "title: x\ndescription: d\n"
	for _, c := range []struct{ front, want string }{
		{head + "draft: !!bool garbage\n", `a.md:4:8: draft: want true or false, got "garbage"`},
		{"title: !!str [a, b]\ndescription: d\n", "a.md:2:8: title: want a string, got a list"},
		{head + "count: !!int abc\n", `a.md:4:8: count: want an integer, got "abc"`},
		{head + "meta: {stars: !!int 1.5}\n", `a.md:4:15: meta.stars: want an integer, got "1.5"`},
		{head + "ratio: !!float [1]\n", "a.md:4:8: ratio: want a number, got a list"},
		{head + "updated: !!str [2026-08-09]\n", "a.md:4:10: updated: want a date, got a list"},
		{head + "slug: !!str [a]\n", "a.md:4:7: slug: want a string, got a list"},
		{head + "translationKey: !!str {a: b}\n", "a.md:4:17: translationKey: want a string, got a map"},
		{head + "date: !!str [2026-08-09]\n", "a.md:4:7: date: want a date, got a list"},
		{head + "image: {src: !!str {a: b}}\n", "a.md:4:14: image.src: want a string, got a map"},
	} {
		got := matterErrors(t, richPost{}, c.front)
		if !slices.Equal(got, []string{c.want}) {
			t.Errorf("%q:\nerrors %q\nwant   %q", c.front, got, c.want)
		}
	}
	// Explicit tags that agree with the value are fine.
	if got := matterErrors(t, richPost{}, "title: !!str 12\ndescription: d\ndraft: !!bool true\ncount: !!int 3\n"); len(got) > 0 {
		t.Errorf("errors %q", got)
	}
}
