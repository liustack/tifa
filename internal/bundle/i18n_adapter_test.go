package bundle

import (
	"encoding/json"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// i18nAdapterPage renders pages with the adapter (js/runtime/i18n.tsx) in several languages, one
// after the other in one VM, as the server does.
const i18nAdapterPage = `import './host'
import { renderToString } from 'react-dom/server.edge'
import { I18nProvider, i18n, useTranslation } from './runtime/i18n'

// As onStart of hooks.ts adds a formatter: once, for every language, which it gets as lng.
i18n.services.formatter?.addCached('shout', (lng) => (value: string) => value.toUpperCase() + (lng === 'zh-CN' ? '！' : '!'))

function Page() {
  const store = useTranslation('store')
  const nested = useTranslation('admin/store')
  const site = useTranslation()
  return (
    <p>
      {[
        store.t('cart.title'),
        store.t('total', { name: 'ab' }),
        nested.t('x'),
        site.t('site'),
        store.t('draftOnly'),
        store.t('other:key'),
      ].join('|')}
    </p>
  )
}

function render(lang: string) {
  ;(globalThis as any).lang = lang
  return renderToString(<I18nProvider><Page /></I18nProvider>)
}

const renders = [render('en-US'), render('zh-CN'), render('en-US'), render('zh-CN'), render('zh-CN')]
globalThis.result = JSON.stringify({
  renders,
  // A head function translates outside the components, in the language of its page.
  titles: [i18n.getFixedT('zh-CN', 'store')('cart.title'), i18n.getFixedT('en-US', 'store')('cart.title')],
  language: i18n.language,
  fallback: i18n.options.fallbackLng,
})
`

// i18nAdapterApp makes an application in a fresh directory: the ssr test app, the runtime the
// adapter needs with a stand-in for tifa/client, and the locale files.
func i18nAdapterApp(t *testing.T, locales map[string]string) string {
	t.Helper()
	root := copyTestdata(t, "ssr", true)
	files := map[string]string{
		"i18n.tsx":          i18nAdapterPage,
		"runtime/client.ts": "export function useLanguage(): string { return (globalThis as any).lang }\n",
		"runtime/i18n.tsx":  readRuntime(t, "i18n.tsx"),
	}
	for name, text := range locales {
		files[name] = text
	}
	writeTree(t, root, files)
	return root
}

func readRuntime(t *testing.T, name string) string {
	t.Helper()
	data, err := fs.ReadFile(js.Runtime, "runtime/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var i18nAdapterLocales = map[string]string{
	"app/_locales/en-US.json":                `{"site": "My site"}`,
	"app/_locales/zh-CN.json":                `{"site": "我的站点"}`,
	"app/store/_locales/en-US.json":          `{"cart": {"title": "Cart"}, "total": "Total {{name, shout}}"}`,
	"app/store/_locales/zh-CN.json":          `{"cart": {"title": "购物车"}, "total": "合计 {{name, shout}}"}`,
	"app/store/_locales/zh-CN.draft.json":    `{"draftOnly": "草稿文字"}`,
	"app/store/_locales/en-US.draft.json":    `{"draftOnly": "draft text"}`,
	"app/admin/store/_locales/en-US.json":    `{"x": "Admin store"}`,
	"app/admin/store/_locales/zh-CN.json":    `{"x": "后台商店"}`,
	"app/admin/other/_locales/zh-CN.json":    `{"onlyChinese": "有"}`,
	"app/admin/other/_locales/ja.draft.json": `{"onlyJapanese": "ja"}`,
}

type i18nAdapterResult struct {
	Renders  []string
	Titles   []string
	Language string
	Fallback any
}

func TestI18nAdapterTranslatesEachLanguageFromTheLocalesOfItsFolders(t *testing.T) {
	root := i18nAdapterApp(t, i18nAdapterLocales)
	code, printed := buildPrinting(t, root, "i18n.tsx")
	var r i18nAdapterResult
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatalf("%v\n%s", err, printed)
	}
	// The key of a missing string is the string: a draft file is not published, and a namespace no
	// folder has gives its keys back. One instance renders each page in its own language.
	en := "<p>Cart|Total AB!|Admin store|My site|draftOnly|key</p>"
	zh := "<p>购物车|合计 AB！|后台商店|我的站点|draftOnly|key</p>"
	want := []string{en, zh, en, zh, zh}
	for i, got := range r.Renders {
		if got != want[i] {
			t.Errorf("render %d = %s, want %s", i, got, want[i])
		}
	}
	if strings.Join(r.Titles, "|") != "购物车|Cart" {
		t.Errorf("getFixedT gave %v", r.Titles)
	}
	if r.Language != "zh-CN" {
		t.Errorf("after a page in zh-CN the instance is in %q", r.Language)
	}
	if r.Fallback != false {
		t.Errorf("fallbackLng is %v, want false: a missing string gives its key", r.Fallback)
	}
	for _, text := range []string{"草稿文字", "draft text", "onlyJapanese"} {
		if strings.Contains(code, text) {
			t.Errorf("the bundle holds %q of a draft file", text)
		}
	}
}

// An application without _locales folders gets an adapter with no strings, and a bundle that does
// not import the adapter holds nothing of the locale files that lie around.
func TestI18nAdapterWithoutLocalesBundlesNoStrings(t *testing.T) {
	root := i18nAdapterApp(t, nil)
	code, printed := buildPrinting(t, root, "i18n.tsx")
	var r i18nAdapterResult
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatalf("%v\n%s", err, printed)
	}
	if want := "<p>cart.title|total|x|site|draftOnly|key</p>"; r.Renders[0] != want {
		t.Errorf("render = %s, want %s", r.Renders[0], want)
	}
}

func TestI18nAdapterLeavesBundlesThatDoNotImportItAlone(t *testing.T) {
	plain := copyTestdata(t, "ssr", true)
	before, _ := buildScript(t, plain, "render.tsx")
	root := i18nAdapterApp(t, i18nAdapterLocales)
	after, _ := buildScript(t, root, "render.tsx")
	if before != after {
		t.Error("the bundle of a page that does not import the adapter changed with locale files and adapter files around")
	}
}

// i18next splits a namespace from a key at ":", so a folder whose path has one cannot be a
// namespace. The build says which folder.
func TestI18nAdapterRefusesAFolderNameWithAColon(t *testing.T) {
	root := i18nAdapterApp(t, map[string]string{"app/shop:eu/_locales/en-US.json": `{"a": "b"}`})
	_, _, err := script(testdata(t), root, "i18n.tsx", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "app/shop:eu/_locales") || !strings.Contains(err.Error(), `":"`) {
		t.Fatalf("got %v, want an error that names the folder and the colon", err)
	}
}
