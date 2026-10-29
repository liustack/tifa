package compare_test

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

const sample = `<!DOCTYPE html><html><head><title>Order SH-1042</title><style>.x{}</style></head>
<body><div id="root"><h1>Studio</h1><p>Total <!-- -->¥428.00</p><script>var hidden = "no"</script>
<template id="B:0"><p>behind a boundary</p></template><table><tr><td>a</td></tr><tr><td>b</td></tr></table></div></body></html>`

func TestParseViewKeepsWhatAReaderSees(t *testing.T) {
	v := compare.ParseView([]byte(sample))
	if v.Title != "Order SH-1042" {
		t.Errorf("title %q", v.Title)
	}
	want := []string{"Studio", "Total", "¥428.00", "a", "b"}
	if strings.Join(v.Text, "|") != strings.Join(want, "|") {
		t.Errorf("text %q, want %q", v.Text, want)
	}
	if v.Count["tr"] != 2 || v.Count["h1"] != 1 || v.Count["a"] != 0 {
		t.Errorf("counts %v", v.Count)
	}
}

func TestDiffSaysWhatDiffers(t *testing.T) {
	a := compare.ParseView([]byte(sample))
	if d := compare.Diff(a, compare.ParseView([]byte(sample))); d != "" {
		t.Errorf("the same page differs: %s", d)
	}
	b := compare.ParseView([]byte(strings.Replace(sample, "¥428.00", "¥429.00", 1)))
	if d := compare.Diff(a, b); !strings.Contains(d, "¥428.00") || !strings.Contains(d, "¥429.00") {
		t.Errorf("diff %q does not name the text that changed", d)
	}
	c := compare.ParseView([]byte(strings.Replace(sample, "<tr><td>b</td></tr>", "", 1)))
	if d := compare.Diff(a, c); !strings.Contains(d, "<tr> 2 against 1") {
		t.Errorf("diff %q does not name the rows", d)
	}
}
