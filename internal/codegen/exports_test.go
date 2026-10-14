package codegen

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestExportsOf(t *testing.T) {
	for file, want := range map[string][]string{
		"app/layout.tsx":                     {"NotFound", "default"},
		"app/route.tsx":                      {"default", "head"},
		"app/auth/register/route.tsx":        {"default"},
		"app/projects/projectId+/layout.tsx": {"Error", "default"},
	} {
		got, err := exportsOf(filepath.Join("testdata", "shop", file))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}

func TestExportsOfReportsSyntaxErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "route.tsx")
	writeFile(t, file, "export default function () { return <div> }\n")
	if _, err := exportsOf(file); err == nil {
		t.Fatal("exportsOf accepted a broken file")
	}
}
