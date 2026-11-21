package analyze

import (
	"strings"
	"testing"
)

// A retired key in tifa.yaml is a warning at its line, and the build goes on.
func TestRetiredKeyInTifaYamlWarns(t *testing.T) {
	m, err := analyzeApp(t, "retiredkey")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(m.Warnings) != 1 {
		t.Fatalf("warnings: %v", m.Warnings)
	}
	if pos := m.Warnings[0].Pos; pos.Filename != "tifa.yaml" || pos.Line != 2 {
		t.Errorf("at %v, want tifa.yaml line 2", pos)
	}
	got := m.Warnings[0].String()
	if !strings.HasPrefix(got, "tifa.yaml:2: ") {
		t.Errorf("%q does not start with the file and line", got)
	}
	for _, part := range []string{"cache_control.s_maxage", "no longer used", "cache.s_maxage"} {
		if !strings.Contains(got, part) {
			t.Errorf("%q lacks %q", got, part)
		}
	}
}
