package bundle

import (
	"strings"
	"testing"
)

// reactI18nextFastPage calls useTranslation in each way the server renders it: every form of the
// namespaces, keyPrefix as text and as a function, nsMode fallback, a language and an instance in
// the props, the default namespace of the provider, an instance that is not initialized with and
// without suspense, and strings added between two renders. It writes what each call returns, the
// namespaces the instance reports, and the warnings, so a build with the original useTranslation
// and one with the plugin's give the same text when they do the same.
const reactI18nextFastPage = `import './host'
import { Suspense } from 'react'
import { renderToString } from 'react-dom/server.edge'
import i18next from 'i18next'
import { I18nextProvider, useTranslation } from 'react-i18next'

const warnings: string[] = []
console.warn = (...args: unknown[]) => { warnings.push(args.map(String).join(' ')) }

const i = i18next.createInstance()
i.init({
  lng: 'de',
  fallbackLng: false,
  ns: ['translation', 'shop', 'common'],
  defaultNS: 'translation',
  initAsync: false,
  interpolation: { escapeValue: false },
  resources: {
    de: {
      translation: { title: 'Laden', nested: { deep: { word: 'Wort' } } },
      shop: { cart: 'Warenkorb {{n}}', items_one: 'ein Artikel', items_other: '{{count}} Artikel' },
      common: { ok: 'OK', only: 'nur hier' },
    },
    en: { translation: { title: 'Shop' }, shop: { cart: 'Cart {{n}}' }, common: { ok: 'Okay' } },
  },
})
const other = i18next.createInstance()
other.init({ lng: 'fr', initAsync: false, resources: { fr: { translation: { title: 'Boutique' } } } })
const bare = i18next.createInstance()

function show(name: string, r: any, keys: Array<[any, any?]>) {
  const words = keys.map(([k, o]) => String(r.t(k, o))).join(',')
  return name + '=' + [words, r.ready, r.length, r[0] === r.t, r[1] === r.i18n, r[2] === r.ready, r.i18n && r.i18n.language, r.i18n && (r.i18n as any).__original === (r.i18n as any).__original, typeof r.t.lng === 'string' ? r.t.lng : JSON.stringify(r.t.lngs), JSON.stringify(r.t.ns), String(r.t.keyPrefix)].join('/')
}

function Default() { return <p>{show('default', useTranslation(), [['title'], ['shop:cart', { n: 2 }]])}</p> }
function Named() { return <p>{show('named', useTranslation('shop'), [['cart', { n: 3 }], ['items', { count: 1 }], ['items', { count: 5 }], ['missing']])}</p> }
function List() { return <p>{show('list', useTranslation(['shop', 'common']), [['cart', { n: 1 }], ['common:ok'], ['only']])}</p> }
function Fallback() { return <p>{show('fallback', useTranslation(['shop', 'common'], { nsMode: 'fallback' }), [['cart', { n: 4 }], ['only'], ['nothing']])}</p> }
function Prefix() { return <p>{show('prefix', useTranslation('translation', { keyPrefix: 'nested.deep' }), [['word'], [['none', 'word']]])}</p> }
function Selector() { return <p>{show('selector', useTranslation('translation', { keyPrefix: ($: any) => $.nested.deep } as any), [['word']])}</p> }
function Lng() { return <p>{show('lng', useTranslation('shop', { lng: 'en' }), [['cart', { n: 6 }]])}</p> }
function Other() { return <p>{show('other', useTranslation(undefined, { i18n: other }), [['title']])}</p> }
function Bare() { return <p>{show('bare', useTranslation('shop', { i18n: bare, useSuspense: false }), [['cart']])}</p> }
function BareSuspends() { return <p>{show('suspends', useTranslation('shop', { i18n: bare }), [['cart']])}</p> }
function Empty() { return <p>{show('empty', useTranslation(''), [['title']])}</p> }

function Page() {
  return (
    <I18nextProvider i18n={i}>
      <Default /><Named /><List /><Fallback /><Prefix /><Selector /><Lng /><Other /><Bare /><Empty />
      <I18nextProvider i18n={i} defaultNS="common"><Default /></I18nextProvider>
      <Suspense fallback={<i>waiting</i>}><BareSuspends /></Suspense>
    </I18nextProvider>
  )
}

const first = renderToString(<Page />)
i.addResourceBundle('de', 'shop', { cart: 'Korb {{n}}' }, true, true)
const second = renderToString(<Page />)
i.changeLanguage('en')
const third = renderToString(<Page />)
globalThis.result = [first, second, third, JSON.stringify(i.reportNamespaces.getUsedNamespaces()), JSON.stringify(warnings)].join('\n')
`

// The plugin's useTranslation gives what the original gives for every call a server makes: the
// page renders the same text, the same values come back, the instance reports the same namespaces
// and the same warnings are printed. A file the plugin does not know, here with one more line, keeps
// the original and prints the warning, which is how the two builds are compared.
func TestReactI18nextFastUseTranslationMatchesTheOriginal(t *testing.T) {
	render := func(root string) (string, string) {
		writeTree(t, root, map[string]string{"react-i18n.tsx": reactI18nextFastPage})
		code, printed := buildPrinting(t, root, "react-i18n.tsx")
		return evalWithIntl(t, code), printed
	}
	fast, printed := render(withReactI18next18(t, func(s string) string { return s }))
	if strings.Contains(printed, "react-i18next") {
		t.Fatalf("the build of the known file prints:\n%s", printed)
	}
	original, printed := render(withReactI18next18(t, func(s string) string { return s + "\n// changed\n" }))
	if strings.Count(printed, "react-i18next 18.0.0") != 1 || !strings.Contains(printed, "is no useTranslation the tifa plugin knows") {
		t.Fatalf("the build of a changed file prints:\n%s", printed)
	}
	if fast != original {
		t.Errorf("the plugin's useTranslation gives\n%s\n\nthe original gives\n%s", fast, original)
	}
	for _, want := range []string{"named=Warenkorb 3,ein Artikel,5 Artikel,missing/true", "Korb 3", "named=Cart 3", "Cart 6", "other=Boutique", "bare=", "<i>waiting</i>", "selector=Wort"} {
		if !strings.Contains(fast, want) {
			t.Errorf("the page has no %q:\n%s", want, fast)
		}
	}
}
