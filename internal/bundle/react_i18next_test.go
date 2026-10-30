package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// reactI18nextPage renders components that call useTranslation, under the states an i18n object
// can be in, and describes what each call gave: the own keys of i18n in order, how each property
// compares with the one of the object it copies, its attributes, its prototype, and whether the
// objects the calls gave are different. It counts the calls of Object.getOwnPropertyDescriptors,
// which useTranslation makes for every i18n object it copies.
const reactI18nextPage = `import './host'
import { renderToString } from 'react-dom/server.edge'
import i18next from 'i18next'
import { I18nextProvider, useTranslation } from 'react-i18next'

const resources = { en: { translation: { title: 'Shop' } }, de: { translation: { title: 'Laden' } } }
function make(lng: string) {
  const i = i18next.createInstance()
  i.init({ lng, resources, initAsync: false })
  return i
}

let descriptorCalls = 0
const getOwnPropertyDescriptors = Object.getOwnPropertyDescriptors
Object.getOwnPropertyDescriptors = function (o: object) {
  descriptorCalls++
  return getOwnPropertyDescriptors(o)
} as typeof getOwnPropertyDescriptors

const seen: any[] = []
function Probe() {
  const { t, i18n } = useTranslation()
  seen.push(i18n)
  return <span>{t('title')}</span>
}
function Nested({ outer }: { outer: any }) {
  const { i18n } = useTranslation(undefined, { i18n: outer })
  seen.push(i18n)
  return null
}

function view(w: any, original: any) {
  const keys = Reflect.ownKeys(w)
  const own = keys.map((k) => Object.getOwnPropertyDescriptor(w, k)!)
  return {
    keys: keys.map(String),
    kinds: own.map((d, i) =>
      'value' in d ? (keys[i] === '__original' ? (d.value === original ? 'original' : 'other') : Object.is(d.value, original[keys[i]]) ? 'same' : 'value') : 'accessor',
    ),
    flags: own.map((d) => [d.writable, d.enumerable, d.configurable].map(Number).join('')),
    proto: Object.getPrototypeOf(w) === Object.getPrototypeOf(original),
    language: w.language,
  }
}

const scenarios: any[] = []
function run(name: string, original: any, count = 4, Component: any = Probe) {
  seen.length = 0
  const before = descriptorCalls
  const html = renderToString(
    <I18nextProvider i18n={original}>
      {Array.from({ length: count }, (_, i) => <Component key={i} />)}
    </I18nextProvider>,
  )
  scenarios.push({
    name,
    html,
    calls: descriptorCalls - before,
    distinct: new Set(seen).size,
    seen: seen.length,
    views: seen.map((w) => view(w, original)),
  })
}

const shop = make('en')
const other = make('de')
run('first', shop, 40)
run('again', shop)
run('other instance', other)
run('both', shop)
shop.extra = { a: 1 }
run('new key', shop)
shop.options = { ...shop.options, tifaMark: 1 }
run('replaced values', shop)
delete shop.extra
run('deleted key', shop)
shop.changeLanguage('de')
run('language changed', shop)
let reads = 0
Object.defineProperty(shop, 'live', { get: () => (reads++, 1), enumerable: true, configurable: true })
run('accessor', shop)
run('accessor again', shop)
Object.defineProperty(shop, 'live', { value: 2, enumerable: false, writable: true, configurable: true })
run('hidden', shop)
const marked = Symbol('marked')
const sym = make('en')
sym[marked] = 1
run('symbol key', sym)
run('symbol key again', sym)
const frozen = make('en')
run('frozen copy', frozen)
Object.freeze(frozen)
run('frozen', frozen)
run('frozen again', frozen)
const bare = make('en')
Object.setPrototypeOf(bare, Object.prototype)
run('plain prototype', bare, 3)
const proto = make('en')
run('prototype', proto)
Object.setPrototypeOf(proto, { inherited: 1, __proto__: Object.getPrototypeOf(proto) })
run('prototype changed', proto)
const outer = make('en')
let wrapper: any
function Capture() {
  wrapper = useTranslation().i18n
  return null
}
renderToString(<I18nextProvider i18n={outer}><Capture /></I18nextProvider>)
run('wrapper of a wrapper', outer, 3, () => <Nested outer={wrapper} />)
run('wrapper of a wrapper again', outer, 3, () => <Nested outer={wrapper} />)
const swap = make('en')
run('swap', swap)
swap.a1 = 1
run('swap added', swap)
delete swap.a1
swap.a2 = 1
run('swap renamed', swap)
const readonly = make('en')
Object.defineProperty(readonly, 'ro', { value: 1, writable: false, enumerable: true, configurable: true })
run('read only', readonly)
run('read only again', readonly)
const locked = make('en')
Object.defineProperty(locked, 'lk', { value: 1, writable: true, enumerable: true, configurable: false })
run('locked', locked)
run('locked again', locked)
const crowd = Array.from({ length: 12 }, () => make('en'))
for (let round = 0; round < 2; round++) for (const i of crowd) run('crowd', i, 2)
run('last', other)
globalThis.result = JSON.stringify({ scenarios, reads, descriptorCalls })
`

type i18nextView struct {
	Keys     []string
	Kinds    []string
	Flags    []string
	Proto    bool
	Language string
}

type i18nextScenario struct {
	Name     string
	HTML     string
	Calls    int
	Distinct int
	Seen     int
	Views    []i18nextView
}

type reactI18nextResult struct {
	Scenarios []i18nextScenario
	Reads     int
}

func reactI18nextRender(t *testing.T, root string) (reactI18nextResult, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"react-i18n.tsx": reactI18nextPage})
	code, printed := buildPrinting(t, root, "react-i18n.tsx")
	var r reactI18nextResult
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Scenarios) < 20 {
		t.Fatalf("%d scenarios", len(r.Scenarios))
	}
	return r, printed
}

// useTranslation copies the whole i18n object into a new object on every call. The tifa plugin
// makes the copy from a list of the keys it found plain on the object before, and checks the list
// against the object's keys on every call, so the copy has what the object has now. Calls give
// objects, keys, attributes, prototypes and values as the plugin's change leaves them out, for
// every state an object gets into after it was copied, and for objects the list does not fit:
// accessors, symbols, frozen, hidden. A react-i18next whose code the plugin does not know is left
// alone with a warning.
func TestSSRCopiesTheI18nObjectOfReactI18nextFromAListOfItsKeys(t *testing.T) {
	changed, printed := reactI18nextRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "react-i18next 17.1.0") {
		t.Errorf("the build prints:\n%s", printed)
	}
	root := withChangedPackage(t, "react-i18next", filepath.Join("dist", "es", "useTranslation.js"), func(source string) string {
		const old = "const createI18nWrapper = original => {"
		if strings.Count(source, old) != 1 {
			t.Fatalf("react-i18next does not have %q once", old)
		}
		return strings.Replace(source, old, "const createI18nWrapper = /* changed */ original => {", 1)
	})
	plain, printed := reactI18nextRender(t, root)
	if strings.Count(printed, "react-i18next 17.1.0") != 1 || !strings.Contains(printed, "has no createI18nWrapper the tifa plugin knows") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if plain.Reads != changed.Reads {
		t.Errorf("the getter of an i18n object ran %d times, the plugin's change made it run %d", plain.Reads, changed.Reads)
	}
	for i, p := range plain.Scenarios {
		c := changed.Scenarios[i]
		if c.Name != p.Name {
			t.Fatalf("scenario %d is %q, want %q", i, c.Name, p.Name)
		}
		if c.HTML != p.HTML || c.Distinct != p.Distinct || c.Seen != p.Seen {
			t.Errorf("%s: calls give %d different objects of %d and %q, want %d of %d and %q", p.Name, c.Distinct, c.Seen, c.HTML, p.Distinct, p.Seen, p.HTML)
		}
		for j := range p.Views {
			want, _ := json.Marshal(p.Views[j])
			got, _ := json.Marshal(c.Views[j])
			if string(got) != string(want) {
				t.Errorf("%s: call %d gives\n%s\nwant\n%s", p.Name, j, got, want)
				break
			}
		}
		if p.Distinct != p.Seen {
			t.Errorf("%s: %d calls gave %d different objects, want all different", p.Name, p.Seen, p.Distinct)
		}
		if p.Calls != p.Seen {
			t.Errorf("%s: Object.getOwnPropertyDescriptors ran %d times for %d calls", p.Name, p.Calls, p.Seen)
		}
	}
	// The scenarios the list fits run Object.getOwnPropertyDescriptors once, when the object has
	// no list or differs from it. In the others it runs for every call, as before: objects the
	// list may not describe, which have accessors, symbols, no room for new keys, a prototype of
	// Object or a key of the copy.
	descriptors := map[string]int{
		"first": 1, "again": 0, "other instance": 1, "both": 0, "new key": 1, "replaced values": 0, "deleted key": 1,
		"language changed": 0, "prototype": 1, "prototype changed": 1, "frozen copy": 1, "swap": 1, "swap added": 1, "swap renamed": 1,
	}
	every := map[string]bool{
		"accessor": true, "accessor again": true, "hidden": true, "symbol key": true, "symbol key again": true,
		"frozen": true, "frozen again": true, "read only": true, "read only again": true, "locked": true, "locked again": true, "plain prototype": true, "wrapper of a wrapper": true,
		"wrapper of a wrapper again": true,
	}
	for _, s := range changed.Scenarios {
		switch want, ok := descriptors[s.Name]; {
		case ok:
			if s.Calls != want {
				t.Errorf("%s: Object.getOwnPropertyDescriptors ran %d times for %d calls, want %d", s.Name, s.Calls, s.Seen, want)
			}
		case every[s.Name]:
			if s.Calls != s.Seen {
				t.Errorf("%s: Object.getOwnPropertyDescriptors ran %d times for %d calls, want one for each", s.Name, s.Calls, s.Seen)
			}
		case s.Name == "crowd" || s.Name == "last":
			if s.Calls > 1 {
				t.Errorf("%s: Object.getOwnPropertyDescriptors ran %d times for %d calls, want at most 1", s.Name, s.Calls, s.Seen)
			}
		default:
			t.Errorf("scenario %q has no expectation", s.Name)
		}
	}
}
