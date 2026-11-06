package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// i18nApp writes a module with the given files into a folder, with the repository as tifa, and
// returns its root. Files are by path.
func i18nApp(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join("testdata", "domains", "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join("testdata", "domains", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := strings.NewReplacer("example.com/domains", "example.com/i18n", "=> ../../../..", "=> "+repo).Replace(string(mod))
	all := map[string]string{
		"go.mod": gomod,
		"go.sum": string(sum),
	}
	for name, body := range files {
		all[name] = body
	}
	for name, body := range all {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const page = "export default function Page() { return null }\n"

func TestAreasAndLanguages(t *testing.T) {
	root := i18nApp(t, map[string]string{
		"tifa.yaml":                                 "i18n:\n  default: zh-CN\nsite:\n  url: https://shop.example.com\n",
		"app/layout.tsx":                            page,
		"app/store/_locales/zh-CN.json":             "{}",
		"app/store/_locales/en-US.json":             "{}",
		"app/store/_locales/fr.draft.json":          "{}",
		"app/store/_locales/notes.txt":              "",
		"app/store/page.tsx":                        page,
		"app/store/cart/page.tsx":                   page,
		"app/store/mine/route.go":                   "package mine\n\nimport \"github.com/liustack/tifa\"\n\nfunc GET(c *tifa.Ctx) error { return nil }\n",
		"app/projects/page.tsx":                     page,
		"app/projects/admin/_locales/zh-CN.json":    "{}",
		"app/projects/admin/_locales/de.json":       "{}",
		"app/projects/admin/_locales/de.draft.json": "{}",
		"app/projects/admin/page.tsx":               page,
	})
	m, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.I18n == nil {
		t.Fatal("no I18n")
	}
	if m.I18n.Default != "zh-CN" || m.I18n.Always || m.I18n.SiteURL != "https://shop.example.com" {
		t.Errorf("I18n = %+v", m.I18n)
	}
	if want := []string{"zh-CN", "de", "en-US"}; !reflect.DeepEqual(m.I18n.Languages, want) {
		t.Errorf("Languages = %v, want %v", m.I18n.Languages, want)
	}
	wantAreas := []Area{
		{Dir: "app/projects/admin", Languages: []string{"zh-CN", "de"}},
		{Dir: "app/store", Languages: []string{"zh-CN", "en-US"}},
	}
	if !reflect.DeepEqual(m.I18n.Areas, wantAreas) {
		t.Errorf("Areas = %+v, want %+v", m.I18n.Areas, wantAreas)
	}
	got := map[string]Route{}
	for _, r := range m.Routes {
		got[r.Dir] = r
	}
	for dir, want := range map[string]struct {
		area  string
		langs []string
	}{
		"app/store":          {"app/store", []string{"zh-CN", "en-US"}},
		"app/store/cart":     {"app/store", []string{"zh-CN", "en-US"}},
		"app/store/mine":     {"app/store", []string{"zh-CN", "en-US"}},
		"app/projects":       {"", nil},
		"app/projects/admin": {"app/projects/admin", []string{"zh-CN", "de"}},
	} {
		r, ok := got[dir]
		if !ok {
			t.Errorf("no route %s", dir)
			continue
		}
		if r.Area != want.area || !reflect.DeepEqual(r.Languages, want.langs) {
			t.Errorf("%s: area %q languages %v, want %q %v", dir, r.Area, r.Languages, want.area, want.langs)
		}
	}
}

func TestAppWideAreaAndOneLanguage(t *testing.T) {
	root := i18nApp(t, map[string]string{
		"app/_locales/en.json": "{}",
		"app/page.tsx":         page,
		"app/about/page.tsx":   page,
	})
	m, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	// With one language the default may be left out, and a site url is not needed.
	if m.I18n == nil || m.I18n.Default != "en" || !reflect.DeepEqual(m.I18n.Languages, []string{"en"}) {
		t.Fatalf("I18n = %+v", m.I18n)
	}
	for _, r := range m.Routes {
		if r.Area != "app" || !reflect.DeepEqual(r.Languages, []string{"en"}) {
			t.Errorf("%s: %q %v", r.Dir, r.Area, r.Languages)
		}
	}
}

func TestNoLocalesNoI18n(t *testing.T) {
	m, err := analyzeApp(t, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if m.I18n != nil {
		t.Errorf("I18n = %+v", m.I18n)
	}
	for _, r := range m.Routes {
		if r.Area != "" || r.Languages != nil {
			t.Errorf("%s: %q %v", r.Dir, r.Area, r.Languages)
		}
	}
}

func TestPrefixAlways(t *testing.T) {
	root := i18nApp(t, map[string]string{
		"tifa.yaml":                     "i18n:\n  default: en\n  prefix: always\nsite:\n  url: https://a.example.com\n",
		"app/store/_locales/en.json":    "{}",
		"app/store/_locales/zh-CN.json": "{}",
		"app/store/page.tsx":            page,
	})
	m, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if !m.I18n.Always {
		t.Errorf("I18n = %+v", m.I18n)
	}
}

func TestI18nBuildFailures(t *testing.T) {
	two := map[string]string{
		"app/store/_locales/zh-CN.json": "{}",
		"app/store/_locales/en-US.json": "{}",
		"app/store/page.tsx":            page,
	}
	with := func(extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range two {
			out[k] = v
		}
		for k, v := range extra {
			if v == "-" {
				delete(out, k)
				continue
			}
			out[k] = v
		}
		return out
	}
	const ok = "i18n:\n  default: zh-CN\nsite:\n  url: https://a.example.com\n"
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"no default with several languages": {with(map[string]string{"tifa.yaml": "site:\n  url: https://a.example.com\n"}), "i18n.default"},
		"default no area has":               {with(map[string]string{"tifa.yaml": "i18n:\n  default: fr\nsite:\n  url: https://a.example.com\n"}), `"fr"`},
		"area without the default":          {with(map[string]string{"tifa.yaml": ok, "app/admin/_locales/en-US.json": "{}", "app/admin/page.tsx": page}), "app/admin/_locales: the default language zh-CN"},
		"invalid tag":                       {with(map[string]string{"tifa.yaml": ok, "app/store/_locales/en_US.json": "{}"}), "app/store/_locales/en_US.json"},
		"not canonical":                     {with(map[string]string{"tifa.yaml": ok, "app/store/_locales/de-de.json": "{}"}), "de-DE"},
		"top level folder":                  {with(map[string]string{"tifa.yaml": ok, "app/en-US/page.tsx": page}), "app/en-US"},
		"no site url":                       {with(map[string]string{"tifa.yaml": "i18n:\n  default: zh-CN\n"}), "site.url"},
		"empty area":                        {with(map[string]string{"tifa.yaml": ok, "app/empty/_locales/readme.txt": "x", "app/empty/page.tsx": page}), "app/empty/_locales"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Analyze(i18nApp(t, c.files))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want an error with %q", err, c.want)
			}
		})
	}
}

const journalGo = `package site

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
)

var Journal = content.Define[Post](content.Source{Dir: "journal", Languages: tifa.Languages()})

type Post struct {
	content.Entry
	Title string ` + "`json:\"title\"`" + `
}
`

func TestContentTakesTheLanguagesOfTheSite(t *testing.T) {
	root := i18nApp(t, map[string]string{
		"tifa.yaml":                        "i18n:\n  default: zh-CN\nsite:\n  url: https://a.example.com\n",
		"content.go":                       journalGo,
		"app/store/_locales/zh-CN.json":    "{}",
		"app/store/_locales/en-US.json":    "{}",
		"app/store/page.tsx":               page,
		"content/journal/zh-CN/hello.md":   "---\ntitle: 你好\n---\n",
		"content/journal/en-US/hello.md":   "---\ntitle: Hello\n---\n",
		"content/journal/zh-CN/only-zh.md": "---\ntitle: 只有中文\n---\n",
	})
	m, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Content == nil || len(m.Content.Collections) != 1 {
		t.Fatalf("Content = %+v", m.Content)
	}
	if got, want := m.Content.Collections[0].Languages, []string{"zh-CN", "en-US"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Languages = %v, want %v", got, want)
	}
	var langs []string
	for _, r := range m.Content.File["journal"] {
		langs = append(langs, r.Lang+"/"+r.ID)
	}
	if want := []string{"zh-CN/hello", "en-US/hello", "zh-CN/only-zh"}; !reflect.DeepEqual(langs, want) {
		t.Errorf("entries = %v, want %v", langs, want)
	}
}

// Without languages the collection is one without them, as if the field were left out.
func TestContentWithoutLanguagesOnASiteWithout(t *testing.T) {
	root := i18nApp(t, map[string]string{
		"content.go":               journalGo,
		"app/page.tsx":             page,
		"content/journal/hello.md": "---\ntitle: Hello\n---\n",
	})
	m, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Content.Collections[0].Languages; len(got) != 0 {
		t.Errorf("Languages = %v", got)
	}
}
