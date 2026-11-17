package bundle

import (
	"strings"
	"testing"
)

// i18nextPrefixPage reads what a t gives, calls it again, then changes one of the things the key of
// the cache is made from, and reads again. Each change is one the cache must see: a t whose
// properties are set after getFixedT, a language changed while a t has none, an array changed in
// place, default variables set, changed and removed in place, and another formats object.
const i18nextPrefixPage = `import i18next from 'i18next'

const i = i18next.createInstance()
i.init({
  lng: 'en',
  initAsync: false,
  ns: ['translation', 'common'],
  interpolation: { escapeValue: false },
  resources: {
    en: { translation: { title: 'Shop', greet: 'Hello {{name}} {{x}}', nested: { title: 'Nested' } }, common: { title: 'Common', greet: 'Common {{name}} {{x}}' } },
    de: { translation: { title: 'Laden', greet: 'Hallo {{name}} {{x}}', nested: { title: 'Verschachtelt' } }, common: { title: 'Gemeinsam', greet: 'Gemeinsam {{name}} {{x}}' } },
  },
})
const out = []
const read = (t) => out.push(t('title'), t('title'), t('greet', { name: 'N' }), t('nested.title'))
const run = (name, t, change) => {
  read(t)
  change()
  read(t)
  read(t)
}

// A t with no language follows the language of the instance, however it changes.
const free = i.getFixedT(null)
run('free', free, () => i.changeLanguage('de'))
run('free back', free, () => i.changeLanguage('en'))
run('free in place', free, () => (i.translator.language = 'de'))
run('free in place back', free, () => (i.translator.language = 'en'))
run('free set', free, () => (free.lng = 'de'))
run('free unset', free, () => (free.lng = undefined))

// Properties of a t set after getFixedT, with values that differ in type or sign.
const t = i.getFixedT('en')
for (const [k, v] of [['lng', 'de'], ['lng', ''], ['lng', 'en'], ['keyPrefix', 'nested'], ['keyPrefix', undefined], ['keyPrefix', null], ['keyPrefix', 0], ['keyPrefix', -0], ['keyPrefix', NaN], ['keyPrefix', undefined], ['ns', 'common'], ['ns', ['common']], ['lngs', ['de', 'en']], ['lngs', undefined]]) {
  run(k, t, () => (t[k] = v))
}
const arrays = i.getFixedT(['de', 'en'], ['translation', 'common'])
run('array ns in place', arrays, () => (arrays.ns[0] = 'common'))
run('array ns in place back', arrays, () => (arrays.ns[0] = 'translation'))
run('array ns grows', arrays, () => arrays.ns.reverse())
run('array lngs in place', arrays, () => (arrays.lngs[0] = 'en'))
run('array lngs in place back', arrays, () => arrays.lngs.reverse())

// Two t that differ in one thing, used in turn.
const en = i.getFixedT('en', 'translation')
const de = i.getFixedT('de', 'translation')
const common = i.getFixedT('en', 'common')
const prefixed = i.getFixedT('en', 'translation', 'nested')
for (let n = 0; n < 3; n++) out.push(en('title'), de('title'), common('title'), prefixed('title'), en('title'), de('title'), en('greet', { name: 'x' }), de('greet', { name: 'x' }))

// Default variables, set, changed in place, swapped and removed.
const v = i.getFixedT('en')
run('defaults none', v, () => (i.options.interpolation.defaultVariables = { x: 'A' }))
run('defaults in place', v, () => (i.options.interpolation.defaultVariables.x = 'B'))
run('defaults grow', v, () => (i.options.interpolation.defaultVariables.y = 'C'))
run('defaults swapped', v, () => (i.options.interpolation.defaultVariables = { x: 'D' }))
run('defaults of another type', v, () => (i.options.interpolation.defaultVariables = { x: 1 }))
run('defaults removed', v, () => (i.options.interpolation.defaultVariables = undefined))
run('defaults empty', v, () => (i.options.interpolation.defaultVariables = {}))
run('defaults gone', v, () => delete i.options.interpolation.defaultVariables)

// A format service whose formats object is swapped between calls.
const j = i18next.createInstance()
j.use({ type: 'i18nFormat', formats: { tag: '1' }, parse(res) { return this.formats ? res + '/' + this.formats.tag : res } })
j.init({ lng: 'en', initAsync: false, resources: { en: { translation: { title: 'Shop' } } } })
const f = j.getFixedT('en')
run('formats', f, () => (j.services.i18nFormat.formats = { tag: '2' }))
run('formats swapped again', f, () => (j.services.i18nFormat.formats = { tag: '3' }))
run('formats removed', f, () => (j.services.i18nFormat.formats = undefined))
run('formats back', f, () => (j.services.i18nFormat.formats = { tag: '4' }))
globalThis.result = JSON.stringify(out)
`

func TestSSRI18nextCacheSeesEveryChangeOfTheKey(t *testing.T) {
	forI18nextVersions(t, func(t *testing.T, ver i18nextVersion) {
		plain, cached := i18nextCompare(t, ver, i18nextPrefixPage)
		if plain != cached {
			p, c := strings.Split(plain, ","), strings.Split(cached, ",")
			for i := range p {
				if i >= len(c) || p[i] != c[i] {
					t.Fatalf("call %d: without the cache %s, with it %s", i, p[i], c[min(i, len(c)-1)])
				}
			}
			t.Fatalf("without the cache %s, with it %s", plain, cached)
		}
		for _, want := range []string{"Laden", "Common", "Hello N A", "Hello N B", "Hello N D", "/2", "/3", "/4", "Verschachtelt"} {
			if !strings.Contains(plain, want) {
				t.Errorf("the page never gives %q, so it does not test that change", want)
			}
		}
	})
}
