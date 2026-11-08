package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reactI18next18Page renders a component that calls useTranslation, and gives the HTML, the
// language read through the i18n object it returns, and whether that object knows the one it wraps.
const reactI18next18Page = `import './host'
import { renderToString } from 'react-dom/server.edge'
import i18next from 'i18next'
import { I18nextProvider, useTranslation } from 'react-i18next'

const i = i18next.createInstance()
i.init({ lng: 'de', resources: { en: { translation: { title: 'Shop' } }, de: { translation: { title: 'Laden' } } }, initAsync: false })
let seen: any
function Probe() {
  const { t, i18n } = useTranslation()
  seen = i18n
  return <span>{t('title')}</span>
}
const html = renderToString(<I18nextProvider i18n={i}><Probe /></I18nextProvider>)
globalThis.result = [html, seen.language, seen.__original === i].join('|')
`

// withReactI18next18 returns a copy of testdata/ssr with i18next 27.0.1 and react-i18next 18.0.0,
// the files of the second changed by change.
func withReactI18next18(t *testing.T, change func(string) string) string {
	t.Helper()
	root := withI18next(t, "i18next-27-0-1", func(s string) string { return s })
	target := filepath.Join(root, "node_modules", "react-i18next")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(testModules(t), "react-i18next-18-0-0"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(target, os.DirFS(real)); err != nil {
		t.Fatal(err)
	}
	// The packages next to the real folder are its dependencies.
	siblings, err := os.ReadDir(filepath.Dir(real))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range siblings {
		link := filepath.Join(root, "node_modules", e.Name())
		if _, err := os.Lstat(link); err == nil || e.Name() == "react-i18next" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dep, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(real), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dep, link); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(target, "dist", "es", "useTranslation.js")
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(change(string(source))), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// react-i18next 18 wraps the i18n object in a Proxy instead of copying it, so the plugin leaves
// useTranslation as it is and prints nothing. A form of it that the plugin does not know prints
// the warning, and the page renders the same.
func TestSSRLeavesReactI18next18AsItIs(t *testing.T) {
	render := func(root string) (string, string) {
		writeTree(t, root, map[string]string{"react-i18n.tsx": reactI18next18Page})
		code, printed := buildPrinting(t, root, "react-i18n.tsx")
		return evalWithIntl(t, code), printed
	}
	const want = "<span>Laden</span>|de|true"
	got, printed := render(withReactI18next18(t, func(s string) string { return s }))
	if got != want || strings.Contains(printed, "react-i18next") || strings.Contains(printed, "i18next 27") {
		t.Errorf("result %q, want %q, the build prints:\n%s", got, want, printed)
	}
	got, printed = render(withReactI18next18(t, func(source string) string {
		const old = "const createI18nWrapper = original => new Proxy(original, {"
		if strings.Count(source, old) != 1 {
			t.Fatalf("react-i18next does not have %q once", old)
		}
		return strings.Replace(source, old, "const createI18nWrapper = original => new Proxy(/* changed */ original, {", 1)
	}))
	if got != want || strings.Count(printed, "react-i18next 18.0.0") != 1 || !strings.Contains(printed, "has no createI18nWrapper the tifa plugin knows") {
		t.Errorf("result %q, want %q, the build prints:\n%s", got, want, printed)
	}
}
