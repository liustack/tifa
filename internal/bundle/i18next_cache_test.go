package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/js"
)

// i18nextCachePage translates through every kind of t that getFixedT returns and through the
// t of the instance, with options of every kind of value, and records the translations. It
// does so three times, then after each change an app can make through i18next's API, which
// must show in the translations that follow. missing and missingEvents count the calls of
// missingKeyHandler and of the missingKey listener, logs what an instance with debug on logs,
// formats the calls of a formatter: a translation the cache gives again does not call it.
const i18nextCachePage = `import i18next from 'i18next'

const messages = {
  en: {
    translation: {
      title: 'Shop',
      greet: 'Hello {{name}}',
      pair: '{{a}} and {{b}}',
      nested: { deep: { key: 'Deep {{name}}' }, other: 'Other', title: 'Nested title' },
      item_one: '{{count}} item',
      item_other: '{{count}} items',
      item_zero: 'No items',
      friend_male: 'A boyfriend',
      friend: 'A friend',
      nest: 'Nested: $t(nested.other)',
      price: 'Costs {{val, number(minimumFractionDigits: 2)}}',
      counted: 'Counted {{val, counted}}',
      upper: 'Up {{x, upper}}',
      list: ['a', 'b'],
      pp: 'abc',
      'Hello, world?': 'Hi there',
    },
    common: { hello: 'Hello from common', title: 'Common title' },
  },
  de: { translation: { title: 'Laden', greet: 'Hallo {{name}}' } },
}
const copy = () => JSON.parse(JSON.stringify(messages))

let formats = 0
let missing = 0
let missingEvents = 0
let logs = 0
let dynamic = 0

// loud logs what it does to a logger that counts, as an app with debug on does.
const count = () => logs++
const loud = i18next.createInstance()
loud.use({ type: 'logger', log: count, warn: count, error: count }).init({ lng: 'en', resources: copy(), initAsync: false, debug: true })

// shop and twin share one resources object, as instances an app makes from one import do.
const shared = copy()
const shop = i18next.createInstance()
shop.init({ lng: 'de', fallbackLng: 'en', resources: shared, initAsync: false, ns: ['translation', 'common'], interpolation: { escapeValue: false } })
shop.services.formatter.add('counted', (value) => (formats++, '#' + value))
const twin = i18next.createInstance()
twin.init({ lng: 'en', resources: shared, initAsync: false, interpolation: { escapeValue: false } })

const flat = i18next.createInstance()
flat.init({ lng: 'en', resources: { en: { translation: { 'Hello, world?': 'Hi', 'a.b': 'dotted' } } }, nsSeparator: false, keySeparator: false, initAsync: false })

const custom = i18next.createInstance()
custom.init({
  lng: 'en',
  resources: copy(),
  initAsync: false,
  nsSeparator: '::',
  keySeparator: '>',
  overloadTranslationOptionHandler: (args) => ({ defaultValue: 'overloaded', name: args.slice(1).join(' ') }),
})

const saving = i18next.createInstance()
saving.init({ lng: 'en', resources: copy(), initAsync: false, saveMissing: true, missingKeyHandler: () => missing++ })
saving.on('missingKey', () => missingEvents++)

class Options {
  constructor() {
    this.name = 'class'
  }
}

const keptEn = shop.getFixedT('en')
const keptCommon = shop.getFixedT('en', 'common')
const keptNested = shop.getFixedT('en', undefined, 'nested')
keptNested.keyPrefix = undefined
let clone

const show = (v) => (typeof v === 'string' ? v : JSON.stringify(v))

function calls() {
  const ts = [
    shop.getFixedT('en'),
    shop.getFixedT(['de', 'en']),
    shop.getFixedT('en', 'common'),
    shop.getFixedT('en', null, 'nested'),
    shop.getFixedT(null),
    shop.getFixedT('en', ['translation', 'common'], undefined, { scopeNs: ['translation', 'common'] }),
    shop.getFixedT('en', null, ($) => $.nested),
    keptEn,
    keptCommon,
    keptNested,
    twin.getFixedT(null),
    flat.getFixedT('en'),
    custom.getFixedT('en'),
    saving.getFixedT('en'),
    loud.getFixedT('en'),
    shop.t.bind(shop),
  ]
  if (clone) ts.push(clone.getFixedT(null))
  return ts.map((t) => {
    const r = []
    r.push(t('title'), t('common:hello'), t('hello'), t('Hello, world?'))
    r.push(t('greet', { name: 'N' }), t('pair', { a: 'N', b: '!' }), t('pair', { b: '!', a: 'N' }))
    r.push(t('pair', { a: 'a1:sb', b: '' }), t('pair', { a: 'a', b: '1:sb' }), t('greet', { 'name:': 'x', name: 'y' }))
    r.push(t('greet', 'a'), t('greet', 'a', 'b'))
    for (const name of ['1', 1, true, null, undefined, 10n, ['a', 'b'], [1, 2], 'x;n1;']) r.push(t('greet', { name }))
    r.push(t('greet', { name: { toString: () => 'dynamic ' + ++dynamic } }))
    r.push(t('greet', Object.assign(Object.create(null), { name: 'no prototype' })), t('greet', new Options()))
    r.push(t('greet', { name: 'S', replace: { name: 'R' } }))
    r.push(t('nested.deep.key', { name: 'N' }), t('deep.key', { name: 'P' }))
    r.push(t('missing.key'), t('missing.key', { defaultValue: 'fallback' }), t('missing.key', 'default as string'), t('missing.key', 'a', 'b'))
    r.push(t(['missing', 'title']), t(['title']))
    for (const count of [0, 1, 2, 5, '1', -0, 0.5]) r.push(t('item', { count }))
    r.push(t('friend', { context: 'male' }), t('friend', { context: 'other' }))
    r.push(t('nest'), t('price', { val: 1234.5 }), t('price', { val: -0 }), t('price', { val: 0 }), t('counted', { val: 7 }), t('upper', { x: 'abc' }))
    const list = t('list', { returnObjects: true })
    if (Array.isArray(list)) list.push('pushed')
    r.push(list, t('list', { returnObjects: true }), t('list', { joinArrays: '+' }), t('list'))
    r.push(t('pp', { postProcess: 'reverse' }), t('pp', { postProcess: ['reverse'] }))
    r.push(t(($) => $.nested.other))
    r.push(t('title', { lng: 'en' }), t('title', { lng: 'de' }), t('hello', { ns: 'common' }), t('title', { keyPrefix: '' }), t('title', {}), t('title', null))
    r.push(t('title', { returnDetails: true }))
    return r.map(show)
  })
}

const runs = []
const step = (name, change) => {
  if (change) change()
  runs.push([name, calls()])
}
step('first')
step('again')
step('once more')
step('addResource', () => shop.addResource('en', 'translation', 'title', 'Shop 2'))
step('silent addResource', () => shop.addResource('en', 'translation', 'title', 'Shop 3', { silent: true }))
step('silent addResource of the store', () => shop.store.addResource('de', 'translation', 'title', 'Laden 2', { silent: true }))
step('addResources', () => shop.addResources('en', 'common', { hello: 'Hello 2' }))
step('addResourceBundle', () => shop.addResourceBundle('en', 'translation', { nested: { other: 'Other 2' } }, true, true))
step('removeResourceBundle', () => shop.removeResourceBundle('de', 'translation'))
step('addResourceBundle again', () => shop.addResourceBundle('de', 'translation', { title: 'Laden 3' }))
step('changeLanguage to en', () => shop.changeLanguage('en'))
step('changeLanguage to de', () => shop.changeLanguage('de'))
step('setDefaultNamespace', () => shop.setDefaultNamespace('common'))
step('setDefaultNamespace back', () => shop.setDefaultNamespace('translation'))
step('formatter add', () => shop.services.formatter.add('upper', (value) => String(value).toUpperCase()))
step('formatter addCached', () => shop.services.formatter.addCached('upper', () => (value) => String(value) + '!'))
step('use a post-processor', () => i18next.use({ type: 'postProcessor', name: 'reverse', process: (value) => [...value].reverse().join('') }))
step('fixedT.lng', () => (keptEn.lng = 'de'))
step('fixedT.keyPrefix', () => (keptEn.keyPrefix = 'nested'))
step('fixedT.ns', () => (keptCommon.ns = 'translation'))
step('cloneInstance', () => (clone = shop.cloneInstance({ lng: 'en' })))
step('addResource of the clone', () => clone.addResource('en', 'translation', 'title', 'From the clone'))
step('changeLanguage of the clone', () => clone.changeLanguage('de'))
step('init again', () => custom.init({ lng: 'de', nsSeparator: ':', keySeparator: '.' }))
step('loadNamespaces', () => shop.loadNamespaces('extra'))
step('loadLanguages', () => shop.loadLanguages('fr'))
step('reloadResources', () => shop.reloadResources())
globalThis.result = JSON.stringify({ runs, missing, missingEvents, logs, formats })
`

// i18nextCacheFill translates one message with a different long name each call, as many times
// as fill is told, which would keep a translation of about 2 KB for each call in a cache
// without a bound.
const i18nextCacheFill = `import i18next from 'i18next'

const i18n = i18next.createInstance()
i18n.init({ lng: 'en', resources: { en: { translation: { greet: 'Hello {{name}}' } } }, initAsync: false })
const t = i18n.getFixedT('en')
const long = 'x'.repeat(1000)
globalThis.fill = (from, n) => {
  let last
  for (let i = from; i < from + n; i++) last = t('greet', { name: long + i })
  return last.length
}
`

// i18nextApp builds page as the SSR bundle of an application in root whose .tifa/vite.js
// passes settings to the tifa plugin, and returns the bundle and what the build printed.
func i18nextApp(t *testing.T, root, page, settings string) (string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"package.json":         `{ "type": "module" }` + "\n",
		"vite.config.ts":       "import tifa from './.tifa/vite.js'\nexport default { plugins: [tifa()] }\n",
		".tifa/vite/plugin.js": js.VitePlugin,
		".tifa/vite.js":        "import { tifa } from './vite/plugin.js'\nexport default () => tifa(" + settings + ")\n",
		".tifa/client.ts":      "export const client = 1\n",
		".tifa/server.ts":      page,
	})
	var output strings.Builder
	n := NewNode(root, &output)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("App: %v\n%s", err, output.String())
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	return string(code), output.String()
}

type i18nextCacheResult struct {
	Runs          [][2]any
	Missing       int
	MissingEvents int
	Logs          int
	Formats       int
}

func i18nextCacheRender(t *testing.T, settings string) (i18nextCacheResult, string) {
	t.Helper()
	code, printed := i18nextApp(t, copyTestdata(t, "ssr", true), i18nextCachePage, settings)
	var r i18nextCacheResult
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	return r, printed
}

// With build.i18next_cache the t of getFixedT gives the translation it gave before for the same
// call, and translates again after any change made through i18next's API. Translations stay
// those of i18next without the cache, a missing key still reaches missingKeyHandler and the
// listeners of missingKey every time, an instance with debug on logs as much, and a formatter of
// the app runs on every call, since the cache keeps no translation that ran one.
func TestSSRCachesI18nextTranslationsTheSameWay(t *testing.T) {
	plain, _ := i18nextCacheRender(t, `{ target: ["es2022"] }`)
	cached, printed := i18nextCacheRender(t, `{ target: ["es2022"], i18nextCache: true }`)
	if strings.Contains(printed, "i18next 26.5.3") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if len(plain.Runs) != len(cached.Runs) {
		t.Fatalf("%d runs, want %d", len(cached.Runs), len(plain.Runs))
	}
	for i := range plain.Runs {
		want, got := mustJSON(t, plain.Runs[i]), mustJSON(t, cached.Runs[i])
		if string(got) != string(want) {
			t.Errorf("run %d differs:\n%s\nwant\n%s", i, got, want)
		}
	}
	if string(mustJSON(t, plain.Runs[0])) == string(mustJSON(t, plain.Runs[len(plain.Runs)-1])) {
		t.Error("the changes do not change the translations")
	}
	if cached.Missing != plain.Missing || cached.MissingEvents != plain.MissingEvents || plain.Missing == 0 || plain.MissingEvents == 0 {
		t.Errorf("missingKeyHandler called %d times and missingKey %d, want %d and %d", cached.Missing, cached.MissingEvents, plain.Missing, plain.MissingEvents)
	}
	if cached.Logs != plain.Logs || plain.Logs == 0 {
		t.Errorf("i18next logged %d times, want %d", cached.Logs, plain.Logs)
	}
	if cached.Formats != plain.Formats || plain.Formats == 0 {
		t.Errorf("the formatter of the app ran %d times with the cache, want %d", cached.Formats, plain.Formats)
	}
}

// The cache of build.i18next_cache holds at most about 1 MB of translations in a VM, and
// starts over when the next one would not fit.
func TestSSRBoundsTheI18nextCache(t *testing.T) {
	code, _ := i18nextApp(t, copyTestdata(t, "ssr", true), i18nextCacheFill, `{ target: ["es2022"], i18nextCache: true }`)
	bytecode, err := ssr.Compile(code, 256<<20, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := ssr.Load(bytecode, 256<<20, 0, time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if _, err := vm.Eval("fill(0, 10)", slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	vm.RunGC()
	before := vm.MemorySize()
	if _, err := vm.Eval("fill(10, 10000)", slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	vm.RunGC()
	if grown := vm.MemorySize() - before; grown > 2<<20 {
		t.Errorf("10000 translations of about 1 KB each grew the VM by %d bytes", grown)
	}
}

// For an i18next whose code the plugin does not know, build.i18next_cache prints a warning and
// the bundle is the one without the setting.
func TestSSRLeavesAnUnknownI18nextUncached(t *testing.T) {
	cases := []struct{ name, old, new string }{
		{"fixedT", "const fixedT = (key, opts, ...rest) => {", "const fixedT = /* changed */ (key, opts, ...rest) => {"},
		{"its end", "      return this.t(resultKey, o);", "      return /* changed */ this.t(resultKey, o);"},
		{"the instance", "const instance = I18n.createInstance();", "const instance = /* changed */ I18n.createInstance();"},
		{"a method", "  setDefaultNamespace(ns) {", "  setDefaultNamespaces(ns) {"},
	}
	page := "import i18next from 'i18next'\n(globalThis as any).result = i18next.getFixedT('en')('title')\n"
	for _, c := range cases {
		change := func(source string) string {
			if strings.Count(source, c.old) != 1 {
				t.Fatalf("%s: %q is not in i18next once", c.name, c.old)
			}
			return strings.Replace(source, c.old, c.new, 1)
		}
		root := withChangedPackage(t, "i18next", filepath.Join("dist", "esm", "i18next.js"), change)
		off, _ := i18nextApp(t, root, page, `{ target: ["es2022"] }`)
		on, printed := i18nextApp(t, root, page, `{ target: ["es2022"], i18nextCache: true }`)
		if strings.Count(printed, "i18next 26.5.3: ") != strings.Count(printed, "[plugin tifa:ssr] ") || strings.Count(printed, "does not cache its translations") != 1 {
			t.Errorf("%s changed: the build prints:\n%s", c.name, printed)
		}
		if on != off {
			t.Errorf("%s changed: the bundle differs from the one without the cache", c.name)
		}
	}
}

// i18nextReusePage renders requests the ways apps use i18next on a server, and counts the
// translations i18next makes for each instance, which a call the cache answers does not make.
// shared is one instance that changes its language for each request, as an app with one
// instance does. base is cloned for each request, as i18next-http-middleware does, and kept
// translates next to an instance made for each request, as the examples of react-i18next for
// servers do. Each request takes a new t from getFixedT, as useTranslation does on a server.
const i18nextReusePage = `import i18next from 'i18next'

const resources = {
  en: { translation: { title: 'Shop', greet: 'Hello {{name}}', count_one: '{{count}} item', count_other: '{{count}} items' } },
  de: { translation: { title: 'Laden', greet: 'Hallo {{name}}', count_one: '{{count}} Ding', count_other: '{{count}} Dinge' } },
}
const make = (lng) => {
  const i18n = i18next.createInstance()
  i18n.init({ lng, resources, initAsync: false, interpolation: { escapeValue: false } })
  return i18n
}
const shared = make('en')
const base = make('en')
const kept = make('de')

const counts = new Map()
const proto = Object.getPrototypeOf(shared.translator)
const translate = proto.translate
proto.translate = function (...args) {
  counts.set(this, (counts.get(this) ?? 0) + 1)
  return translate.apply(this, args)
}

const render = (t) => [t('title'), t('greet', { name: 'N' }), t('count', { count: 2 })]
const requests = []
const made = { shared: 0, base: 0, kept: 0 }
for (let i = 0; i < 8; i++) {
  const lng = i % 2 ? 'de' : 'en'
  if (i === 4) for (const name in made) made[name] = counts.get(name === 'shared' ? shared.translator : name === 'base' ? base.translator : kept.translator) ?? 0
  shared.changeLanguage(lng)
  const clone = base.cloneInstance({ initAsync: false })
  clone.changeLanguage(lng)
  const fresh = make(lng)
  requests.push([
    render(shared.getFixedT(shared.language)),
    render(shared.getFixedT(null)),
    render(clone.getFixedT(clone.language)),
    render(base.getFixedT(null)),
    render(fresh.getFixedT(fresh.language)),
    render(kept.getFixedT(kept.language)),
  ])
}
const after = (i18n) => (counts.get(i18n.translator) ?? 0)
globalThis.result = JSON.stringify({
  requests,
  later: { shared: after(shared) - made.shared, base: after(base) - made.base, kept: after(kept) - made.kept },
})
`

// The cache keeps its translations across what apps do for each request: a shared instance
// changing its language, cloneInstance, and an instance made for each request. The translations
// stay those without the cache.
func TestSSRI18nextCacheKeepsAcrossRequests(t *testing.T) {
	type result struct {
		Requests [][][]string
		Later    map[string]int
	}
	run := func(settings string) result {
		code, _ := i18nextApp(t, copyTestdata(t, "ssr", true), i18nextReusePage, settings)
		var r result
		if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	plain := run(`{ target: ["es2022"] }`)
	cached := run(`{ target: ["es2022"], i18nextCache: true }`)
	if string(mustJSON(t, cached.Requests)) != string(mustJSON(t, plain.Requests)) {
		t.Errorf("translations differ:\n%s\nwant\n%s", mustJSON(t, cached.Requests), mustJSON(t, plain.Requests))
	}
	if plain.Requests[0][0][0] != "Shop" || plain.Requests[1][0][0] != "Laden" || plain.Requests[1][2][0] != "Laden" {
		t.Errorf("the languages do not change: %s", mustJSON(t, plain.Requests[:2]))
	}
	for name, n := range plain.Later {
		if n == 0 {
			t.Errorf("%s: no translations counted without the cache", name)
		}
		if cached.Later[name] != 0 {
			t.Errorf("%s translated %d times in the last four requests with the cache, want none", name, cached.Later[name])
		}
	}
}

// i18nextAppCodePage translates through code of the app that reads a variable, zone, which
// changes between requests: formatters added with add and addCached, a post-processor, a
// missingInterpolationHandler, a formatter module, and defaultVariables the app changes in the
// options. Each translation must show the zone of its request. An i18nFormat module, as
// i18next-icu is, gets new formats from addUserDefinedFormats, which replaces its formats object.
const i18nextAppCodePage = `import i18next from 'i18next'

let zone = 'A'
const messages = {
  en: {
    translation: {
      plain: 'At {{t, zoned}}',
      cached: 'At {{t, zonedCached}}',
      pp: 'Post',
      user: 'Hi {{user}}',
      missing: 'Hi {{nobody}}',
    },
  },
}
const init = (i18n, options = {}) => i18n.init({ lng: 'en', resources: messages, initAsync: false, interpolation: { escapeValue: false }, ...options })

const app = i18next.createInstance()
init(app, { interpolation: { escapeValue: false, defaultVariables: { user: 'A' } } })
app.services.formatter.add('zoned', (value) => value + '@' + zone)
app.services.formatter.addCached('zonedCached', () => (value) => value + '@' + zone)
app.use({ type: 'postProcessor', name: 'zone', process: (value) => value + '@' + zone })

const handler = i18next.createInstance()
init(handler, { missingInterpolationHandler: () => zone })

const module = i18next.createInstance()
module.use({ type: 'formatter', init() {}, add() {}, addCached() {}, format: (value) => value + '@' + zone })
init(module)

const icu = {
  type: 'i18nFormat',
  formats: { zone: 'A' },
  parse(res) { return res + '@' + this.formats.zone },
  addUserDefinedFormats(formats) { this.formats = { ...this.formats, ...formats } },
}
const message = i18next.createInstance()
message.use(icu)
init(message)

const runs = []
for (const z of ['A', 'B', 'A', 'C', 'C', 'B']) {
  zone = z
  app.options.interpolation.defaultVariables.user = z
  icu.addUserDefinedFormats({ zone: z })
  const t = app.getFixedT('en')
  runs.push([
    t('plain', { t: 1 }),
    t('cached', { t: 1 }),
    t('pp', { postProcess: 'zone' }),
    t('user'),
    handler.getFixedT('en')('missing'),
    module.getFixedT('en')('plain', { t: 1 }),
    message.getFixedT('en')('pp'),
  ])
}
globalThis.result = JSON.stringify(runs)
`

// The cache keeps no translation that ran code of the app, so code that reads something of the
// request gives each request its own translation. Nor does it keep one across a change of
// defaultVariables.
func TestSSRI18nextCacheLeavesAppCodeUncached(t *testing.T) {
	code, _ := i18nextApp(t, copyTestdata(t, "ssr", true), i18nextAppCodePage, `{ target: ["es2022"] }`)
	plain := evalWithIntl(t, code)
	code, _ = i18nextApp(t, copyTestdata(t, "ssr", true), i18nextAppCodePage, `{ target: ["es2022"], i18nextCache: true }`)
	cached := evalWithIntl(t, code)
	if cached != plain {
		t.Errorf("translations differ:\n%s\nwant\n%s", cached, plain)
	}
	if !strings.Contains(plain, `["At 1@B","At 1@B","Post@B","Hi B","Hi B","At 1@B","Post@B"]`) {
		t.Errorf("the zone does not reach the translations: %s", plain)
	}
}

// i18nextMissPage translates one key with a new name on every call, as a page that writes the
// name of each user does, then counts the translations i18next makes for calls that repeat.
const i18nextMissPage = `import i18next from 'i18next'

const i18n = i18next.createInstance()
i18n.init({ lng: 'en', resources: { en: { translation: { greet: 'Hello {{name}}', title: 'Shop' } } }, initAsync: false })
let translations = 0
const translate = i18n.translator.translate
i18n.translator.translate = function (...args) {
  translations++
  return translate.apply(this, args)
}
const counted = (f) => {
  const before = translations
  f()
  return translations - before
}
const t = () => i18n.getFixedT('en')
for (let i = 0; i < 3000; i++) t()('greet', { name: 'user ' + i })
const off = counted(() => { for (let i = 0; i < 10; i++) t()('greet', { name: 'same' }) })
const title = counted(() => { for (let i = 0; i < 10; i++) t()('title') })
for (let i = 0; i < 300; i++) t()('greet', { name: 'same' })
const back = counted(() => { for (let i = 0; i < 10; i++) t()('greet', { name: 'same' }) })
globalThis.result = JSON.stringify({ off, title, back })
`

// A key whose calls keep missing stops being looked up and kept, so they cost about what they
// cost without the cache, and other keys stay cached. Once its calls repeat, it is cached again.
func TestSSRI18nextCacheStopsForKeysThatMiss(t *testing.T) {
	code, _ := i18nextApp(t, copyTestdata(t, "ssr", true), i18nextMissPage, `{ target: ["es2022"], i18nextCache: true }`)
	var r struct{ Off, Title, Back int }
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if r.Off < 9 {
		t.Errorf("after 3000 different names, %d of 10 calls with the same name translated, want at least 9", r.Off)
	}
	if r.Title > 1 {
		t.Errorf("another key translated %d times in 10 calls", r.Title)
	}
	if r.Back != 0 {
		t.Errorf("after 300 calls with the same name, %d of the next 10 translated, want none", r.Back)
	}
}

// i18nextFormatsPage formats a value that differs on every call with a formatter added with
// addCached, whose formatters i18next keeps for each set of options, as many times as grow is
// told.
const i18nextFormatsPage = `import i18next from 'i18next'

const i18n = i18next.createInstance()
i18n.init({ lng: 'en', resources: { en: { translation: { paid: 'Paid {{amount, money}}' } } }, initAsync: false })
i18n.services.formatter.addCached('money', (lng, options) => (value) => '$' + value)
let n = 0
globalThis.grow = (count) => {
  for (let i = 0; i < count; i++, n++) i18n.getFixedT('en')('paid', { amount: n, order: 'order ' + n })
}
`

// The cache counts the calls of a formatter of the app without adding to each formatter i18next
// keeps, so a VM grows about as much with the cache as without it.
func TestSSRI18nextCacheAddsNothingToTheFormattersOfI18next(t *testing.T) {
	grown := func(settings string) int64 {
		code, _ := i18nextApp(t, copyTestdata(t, "ssr", true), i18nextFormatsPage, settings)
		bytecode, err := ssr.Compile(code, 256<<20, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer bytecode.Close()
		vm, err := ssr.Load(bytecode, 256<<20, 0, time.Minute, false)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()
		if _, err := vm.Eval("grow(100)", slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
		vm.RunGC()
		before := vm.MemorySize()
		if _, err := vm.Eval("grow(5000)", slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
		vm.RunGC()
		return int64(vm.MemorySize()) - int64(before)
	}
	plain, cached := grown(`{ target: ["es2022"] }`), grown(`{ target: ["es2022"], i18nextCache: true }`)
	if cached-plain > 100<<10 {
		t.Errorf("5000 formats grew the VM by %d bytes with the cache and %d without", cached, plain)
	}
}
