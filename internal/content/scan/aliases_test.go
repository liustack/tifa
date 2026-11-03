package scan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liustack/tifa/content"
)

type chain struct {
	Next *chain  `json:"next"`
	Kids []chain `json:"kids,omitempty"`
}

type chained struct {
	content.Entry
	Root  *chain `json:"root"`
	Other *chain `json:"other"`
}

func matterErrors(t *testing.T, schema any, front string) []string {
	t.Helper()
	tree, problems := FromReflect(reflect.TypeOf(schema))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	m := parseMatter([]byte("---\n"+front+"---\n"), tree, "scan.chained")
	var out []string
	for _, e := range m.errs {
		out = append(out, Error{File: "a.md", Line: e.at.line, Col: e.at.col, Msg: e.msg}.Error())
	}
	return out
}

func TestAliasesThatHoldThemselvesAreErrors(t *testing.T) {
	for _, c := range []struct{ front, want string }{
		{"root: &x {next: *x}\n", "a.md:2:17: root.next: the alias *x names a value that holds it"},
		{"root: &x\n  kids: [*x]\n", "a.md:3:10: root.kids[0]: the alias *x names a value that holds it"},
		{"root: &x {kids: [{next: *x}]}\n", "a.md:2:25: root.kids[0].next: the alias *x names a value that holds it"},
	} {
		got := matterErrors(t, chained{}, c.front)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%q: errors %q, want %q", c.front, got, c.want)
		}
	}
	// An alias of a value that does not hold it is fine, even twice.
	if got := matterErrors(t, chained{}, "root: &x {next: {}}\nother: {next: *x, kids: [*x, *x]}\n"); len(got) > 0 {
		t.Errorf("errors %q", got)
	}
}

func TestAliasesThatExpandTooMuchAreErrors(t *testing.T) {
	// Nine levels of ten aliases each, which would be 10^8 values.
	var b strings.Builder
	b.WriteString("root:\n  kids:\n    - &a {}\n")
	for level := 'b'; level <= 'i'; level++ {
		b.WriteString("    - &" + string(level) + " {kids: [")
		for i := range 10 {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + string(level-1))
		}
		b.WriteString("]}\n")
	}
	got := matterErrors(t, chained{}, b.String())
	if len(got) != 1 || !strings.HasSuffix(got[0], ": the front matter expands its aliases into too many values") {
		t.Errorf("errors %q", got)
	}
}
