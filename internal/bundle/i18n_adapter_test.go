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
import { I18nProvider, i18nFor, setupI18n, useTranslation } from './runtime/i18n'

const setups: string[] = []
setupI18n((i18n, lang) => {
  setups.push(lang + ':' + i18n.language)
  i18n.services.formatter?.addCached('shout', () => (value: string) => value.toUpperCase())
})

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
let late = ''
try {
  setupI18n(() => {})
} catch (e) {
  late = String(e)
}
globalThis.result = JSON.stringify({
  renders,
  setups,
  late,
  same: i18nFor('en-US') === i18nFor('en-US'),
  differ: i18nFor('en-US') !== i18nFor('zh-CN'),
  languages: [i18nFor('en-US').language, i18nFor('zh-CN').language],
  loads: [i18nFor('zh-CN').options.load, i18nFor('zh-CN').options.fallbackLng],
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
	Renders   []string
	Setups    []string
	Late      string
	Same      bool
	Differ    bool
	Languages []string
	Loads     []any
}

func TestI18nAdapterTranslatesEachLanguageFromTheLocalesOfItsFolders(t *testing.T) {
	root := i18nAdapterApp(t, i18nAdapterLocales)
	code, printed := buildPrinting(t, root, "i18n.tsx")
	var r i18nAdapterResult
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatalf("%v\n%s", err, printed)
	}
	// The key of a missing string is the string: a draft file is not published, and a namespace no
	// folder has gives its keys back.
	en := "<p>Cart|Total AB|Admin store|My site|draftOnly|key</p>"
	zh := "<p>购物车|合计 AB|后台商店|我的站点|draftOnly|key</p>"
	want := []string{en, zh, en, zh, zh}
	for i, got := range r.Renders {
		if got != want[i] {
			t.Errorf("render %d = %s, want %s", i, got, want[i])
		}
	}
	if !r.Same || !r.Differ {
		t.Errorf("an instance is made for each language and kept: same %v, differ %v", r.Same, r.Differ)
	}
	if strings.Join(r.Setups, ",") != "en-US:en-US,zh-CN:zh-CN" {
		t.Errorf("setup ran %v, want once for each language, in order of first use", r.Setups)
	}
	if !strings.Contains(r.Late, "setupI18n") {
		t.Errorf("setupI18n after the first instance gave %q, want an error that names it", r.Late)
	}
	if r.Languages[0] != "en-US" || r.Languages[1] != "zh-CN" {
		t.Errorf("languages %v", r.Languages)
	}
	if r.Loads[0] != "currentOnly" || r.Loads[1] != false {
		t.Errorf("load and fallbackLng are %v", r.Loads)
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
