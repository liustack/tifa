package tifa

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadLanguages(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                           "module x\n",
		"tifa.yaml":                        "i18n:\n  default: zh-CN\n",
		"app/store/_locales/zh-CN.json":    "{}",
		"app/store/_locales/en-US.json":    "{}",
		"app/store/_locales/fr.draft.json": "{}",
		"app/store/pkg/x.go":               "package pkg\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sub := filepath.Join(root, "app", "store", "pkg")
	want := []string{"zh-CN", "en-US"}
	// Not set, as in go test: the files of the module of the current folder.
	if got := readLanguages("", sub); !reflect.DeepEqual(got, want) {
		t.Errorf("not set: %v, want %v", got, want)
	}
	// Set by tifa build: what it says, whatever the files say.
	if got := readLanguages("list:de,en", sub); !reflect.DeepEqual(got, []string{"de", "en"}) {
		t.Errorf("set: %v", got)
	}
	// Set and empty is an app without languages, not a reason to read the files.
	if got := readLanguages("list:", sub); got != nil {
		t.Errorf("set and empty: %v", got)
	}
	// No module and no folder.
	if got := readLanguages("", t.TempDir()); got != nil {
		t.Errorf("no module: %v", got)
	}
}

func TestLanguagesIsACopy(t *testing.T) {
	languagesOnce.Do(func() {})
	languages = []string{"a", "b"}
	Languages()[0] = "x"
	if got := Languages(); got[0] != "a" {
		t.Errorf("Languages gave out its own slice: %v", got)
	}
}
