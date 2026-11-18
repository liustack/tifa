package analyze

import (
	"strings"
	"testing"
)

// A retired key in tifa.yaml is a warning that names the file, and the build goes on.
func TestRetiredKeyInTifaYamlWarns(t *testing.T) {
	m, err := analyzeApp(t, "retiredkey")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(m.Warnings) != 1 {
		t.Fatalf("warnings: %v", m.Warnings)
	}
	got := m.Warnings[0].String()
	for _, part := range []string{"tifa.yaml", "line 2", "cache_control.s_maxage", "no longer used", "cache.s_maxage"} {
		if !strings.Contains(got, part) {
			t.Errorf("%q lacks %q", got, part)
		}
	}
}
