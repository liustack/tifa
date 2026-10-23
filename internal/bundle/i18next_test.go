package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/tifa/internal/ssr"
)

// i18nextPage translates with every kind of key, key prefix, namespace and separator that the
// fixedT and looksLikeObjectPath of i18next tell apart, through the instances an app makes and the
// functions getFixedT returns. It runs the calls once to fill the caches, twice more, then once
// each with a built-in looksLikeObjectPath calls replaced by an equal one and by ones that give
// other results, which change whether a key such as common:hello? names a namespace, and with
// Array.isArray, which fixedT calls, counted. probeReads counts the reads
// of an option of the instance, which fixedT copies on every call as i18next has it, and only
// for a key or key prefix that is a function with the plugin's change.
const i18nextPage = `import './host'
import i18next from 'i18next'

const resources = {
  en: {
    translation: {
      title: 'Shop',
      nested: { deep: { key: 'Deep {{name}}' }, other: 'Other' },
      item_one: '{{count}} item',
      item_other: '{{count}} items',
      item_zero: 'No items',
      friend_male: 'A boyfriend',
      friend_female: 'A girlfriend',
      friend: 'A friend',
      nest: 'Nested: $t(nested.other)',
      price: 'Costs {{val, number(minimumFractionDigits: 2)}}',
      list: ['a', 'b'],
      'Hello, world?': 'Hi there',
      'a|b': 'pipe',
      'x/y': 'slash',
      'p!q': 'bang',
      'x;y': 'semicolon',
    },
    common: { hello: 'Hello from common', title: 'Common title' },
  },
  de: { translation: { title: 'Laden' } },
}

const shop = i18next.createInstance()
shop.init({ lng: 'de', fallbackLng: 'en', resources, initAsync: false, ns: ['translation', 'common'], interpolation: { escapeValue: false } })
let probeReads = 0
Object.defineProperty(shop.options, 'tifaProbe', { enumerable: true, configurable: true, get: () => (probeReads++, 1) })

const flat = i18next.createInstance()
flat.init({ lng: 'en', resources: { en: { translation: { 'Hello, world?': 'Hi', 'a.b': 'dotted' } } }, nsSeparator: false, keySeparator: false, initAsync: false })

const custom = i18next.createInstance()
custom.init({
  lng: 'en',
  resources,
  initAsync: false,
  nsSeparator: '::',
  keySeparator: '>',
  overloadTranslationOptionHandler: (args) => ({ defaultValue: 'overloaded ' + String(args[1]) }),
})

function calls() {
  const out = []
  const ts = [
    shop.t.bind(shop),
    shop.getFixedT('en'),
    shop.getFixedT(['de', 'en']),
    shop.getFixedT('en', 'common'),
    shop.getFixedT('en', null, 'nested'),
    shop.getFixedT('en', null, ($) => $.nested),
    shop.getFixedT('en', ['translation', 'common'], undefined, { scopeNs: ['translation', 'common'] }),
    flat.getFixedT('en'),
    custom.getFixedT('en'),
  ]
  for (const t of ts) {
    const r = []
    r.push(t('common:hello?'), t('title'))
    r.push(t('nested.deep.key', { name: 'N' }))
    r.push(t('deep.key', { name: 'P' }))
    r.push(t('common:hello'))
    r.push(t('Hello, world?'), t('Hello, common:hello'), t('common:hello.x'))
    r.push(t('a|b', { nsSeparator: '|' }))
    r.push(t('x/y', { keySeparator: '/' }))
    r.push(t('p!q'), t('x;y'), t('p!q', { nsSeparator: '!' }), t('x;y', { keySeparator: ';', nsSeparator: false }))
    r.push(t('missing.key', { defaultValue: 'fallback' }))
    r.push(t('missing.key', 'default as string'))
    r.push(t(['missing', 'title']))
    for (const count of [0, 1, 2, 5]) r.push(t('item', { count }))
    r.push(t('friend', { context: 'male' }), t('friend', { context: 'other' }))
    r.push(t('nest'), t('price', { val: 1234.5 }))
    r.push(JSON.stringify(t('list', { returnObjects: true })), t('list', { joinArrays: '+' }))
    r.push(t(($) => $.nested.other), t(($) => $.common.hello), t(($) => $.deep.key, { name: 'S' }))
    r.push(t([($) => $.missing, ($) => $.title]))
    r.push(t('hello', { ns: 'common' }), t('title', { lng: 'en' }), t('title', { keyPrefix: '' }))
    for (let i = 0; i < 24; i++) r.push(t('nested.other', { nsSeparator: String.fromCharCode(65 + i) }))
    for (let i = 0; i < 24; i++) r.push(t('Hello, world?', { keySeparator: String.fromCharCode(97 + i), nsSeparator: ':' }))
    out.push(r)
  }
  return out
}

// last makes the last call of looksLikeObjectPath one with the separators of the shop, the ones the
// first call after a built-in is replaced uses, with a key whose namespace depends on the regexp.
const last = () => shop.t('title')
const runs = []
for (let i = 0; i < 3; i++) runs.push(calls())
const includes = String.prototype.includes
last()
String.prototype.includes = function (s, p) {
  return includes.call(this, s, p)
}
runs.push(calls())
last()
String.prototype.includes = function (s, p) {
  return this.length === 1 && s.length === 1 ? true : includes.call(this, s, p)
}
runs.push(calls())
String.prototype.includes = includes
const filter = Array.prototype.filter
last()
Array.prototype.filter = function (f, self) {
  return filter.call(this, (v, i, a) => v !== '?' && f.call(self, v, i, a))
}
runs.push(calls())
Array.prototype.filter = filter
const isArray = Array.isArray
let isArrayCalls = 0
Array.isArray = function (v) {
  isArrayCalls++
  return isArray(v)
}
runs.push(calls())
Array.isArray = isArray
runs.push(calls())
const before = probeReads
const fixedT = shop.getFixedT('en')
for (let i = 0; i < 10; i++) fixedT('title')
globalThis.result = JSON.stringify({ runs, isArrayCalls, probeReads: probeReads - before })
`

type i18nextResult struct {
	Runs         [][][]any
	IsArrayCalls int
	ProbeReads   int
}

func i18nextRender(t *testing.T, root string) (i18nextResult, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"i18n.tsx": i18nextPage})
	code, printed := buildPrinting(t, root, "i18n.tsx")
	var r i18nextResult
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Runs) != 8 {
		t.Fatalf("%d runs, want 8", len(r.Runs))
	}
	return r, printed
}

// The tifa plugin makes the functions getFixedT returns copy the options of i18next only when a
// key or a key prefix is a function, and looksLikeObjectPath keep the regexp of the last
// separators. Translations stay the same as with an i18next whose code the plugin does not know,
// which it leaves alone with a warning.
func TestSSRSpeedsUpI18nextTheSameWay(t *testing.T) {
	changed, printed := i18nextRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "i18next 26.5.3") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if changed.ProbeReads != 0 {
		t.Errorf("fixedT copied the options for %d of 10 plain keys", changed.ProbeReads)
	}
	cases := []struct{ name, old, new string }{
		{"fixedT", "const selectorOpts = {", "const selectorOpts = /* changed */ {"},
		{"looksLikeObjectPath", "const possibleChars = chars.filter(", "const possibleChars = /* changed */ chars.filter("},
	}
	for _, c := range cases {
		root := withChangedPackage(t, "i18next", filepath.Join("dist", "esm", "i18next.js"), func(source string) string {
			if strings.Count(source, c.old) != 1 {
				t.Fatalf("%s: %q is not in i18next once", c.name, c.old)
			}
			return strings.Replace(source, c.old, c.new, 1)
		})
		plain, printed := i18nextRender(t, root)
		if strings.Count(printed, "i18next 26.5.3") != 1 || !strings.Contains(printed, "has no "+c.name+" the tifa plugin knows") {
			t.Errorf("%s changed: the build prints:\n%s", c.name, printed)
		}
		if c.name == "fixedT" && plain.ProbeReads != 10 {
			t.Errorf("%s changed: fixedT copied the options for %d of 10 plain keys, want all", c.name, plain.ProbeReads)
		}
		if plain.IsArrayCalls != changed.IsArrayCalls {
			t.Errorf("%s changed: %d calls of Array.isArray, the plugin's change made %d", c.name, plain.IsArrayCalls, changed.IsArrayCalls)
		}
		want, _ := json.Marshal(plain.Runs)
		got, _ := json.Marshal(changed.Runs)
		if string(got) != string(want) {
			for i := range plain.Runs {
				a, _ := json.Marshal(changed.Runs[i])
				b, _ := json.Marshal(plain.Runs[i])
				if string(a) != string(b) {
					t.Errorf("%s changed: run %d differs:\n%s\nwant\n%s", c.name, i, a, b)
				}
			}
		}
	}
	if !strings.Contains(string(mustJSON(t, changed.Runs[0])), "Hi there") || !strings.Contains(string(mustJSON(t, changed.Runs[0])), "Laden") {
		t.Errorf("translations missing: %s", mustJSON(t, changed.Runs[0]))
	}
}

// evalWithIntl runs a bundle in a VM made as the SSR pool makes it, with Intl, which i18next's
// plurals need, and returns globalThis.result.
func evalWithIntl(t *testing.T, code string) string {
	t.Helper()
	bytecode, err := ssr.Compile(code, 128<<20, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := ssr.Load(bytecode, 128<<20, 0, 5*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if _, err := vm.ExecutePendingJobs(); err != nil {
		t.Fatal(err)
	}
	value, err := vm.Eval("String(globalThis.result)", slowjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	return value.(string)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
