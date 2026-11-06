package langs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSiteReadsTheFilesAndTheDefault(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"go.mod":                              "module x\n",
		"tifa.yaml":                           "i18n:\n  default: zh-CN\n",
		"app/store/_locales/zh-CN.json":       "{}",
		"app/store/_locales/en-US.json":       "{}",
		"app/store/_locales/fr.draft.json":    "{}",
		"app/a/_locales/de.json":              "{}",
		"app/a/_locales/en_US.json":           "{}",
		"app/a/_locales/readme.md":            "",
		"node_modules/x/app/_locales/ja.json": "{}",
	})
	if got, want := Site(root), []string{"zh-CN", "de", "en-US"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Site = %v, want %v", got, want)
	}
	sub := filepath.Join(root, "app", "store")
	if got := Module(sub); got != root {
		t.Errorf("Module(%s) = %q, want %q", sub, got, root)
	}
}

func TestSiteWithoutLocalesIsNil(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"go.mod": "module x\n", "app/page.tsx": ""})
	if got := Site(root); got != nil {
		t.Errorf("Site = %v", got)
	}
}
