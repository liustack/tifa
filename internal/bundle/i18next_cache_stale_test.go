package bundle

import "testing"

// i18nextCompare builds page twice for the i18next version ver, without the cache and with it,
// and returns what each build gave in globalThis.result.
func i18nextCompare(t *testing.T, ver i18nextVersion, page string) (plain, cached string) {
	t.Helper()
	build := func(settings string) string {
		code, _ := i18nextApp(t, i18nextRoot(t, ver), page, settings)
		return evalWithIntl(t, code)
	}
	return build(`{ target: ["es2022"] }`), build(`{ target: ["es2022"], i18nextCache: true }`)
}

// i18nextHydratePage has two instances that share one ns array, as instances made from one
// options object do. Where the separators of namespace and key are the same, the array decides
// whether the first part of extra.title is a namespace. b holds the string, a has none, and
// a.hydrate adds the namespace to the array they share.
const i18nextHydratePage = `import i18next from 'i18next'

const ns = ['translation']
const options = {
  lng: 'en',
  initAsync: false,
  ns,
  keySeparator: '.',
  nsSeparator: '.',
  resources: { en: { translation: { extra: { title: 'OLD' } }, extra: { title: 'NEW' } } },
}
const a = i18next.createInstance()
const b = i18next.createInstance()
a.init(options)
b.init(options)
const t = b.getFixedT('en')
const before = [t('extra.title'), t('extra.title')]
a.hydrate({ en: { extra: { title: 'hydrated' } } })
globalThis.result = JSON.stringify({ before, after: [t('extra.title'), t('extra.title')] })
`

// Hydrating an instance that holds no strings still empties the cache of the other instances, which
// share its ns array.
func TestSSRI18nextCacheForgetsAfterHydrateOfAnotherInstance(t *testing.T) {
	forI18nextVersions(t, func(t *testing.T, ver i18nextVersion) {
		if ver.hydrateLine == "" {
			t.Skip("this i18next has no hydrate")
		}
		plain, cached := i18nextCompare(t, ver, i18nextHydratePage)
		const want = `{"before":["OLD","OLD"],"after":["NEW","NEW"]}`
		if plain != want || cached != want {
			t.Errorf("without the cache %s, with it %s, want %s", plain, cached, want)
		}
	})
}

// i18nextBackendPage reads a string twice, so the cache has it, asks a backend that answers later
// to reload, reads again while it has not answered, and reads once more after it did.
const i18nextBackendPage = `import i18next from 'i18next'

const i = i18next.createInstance()
i.use({ type: 'backend', read: (lng, ns) => Promise.resolve({ title: 'NEW' }) })
i.init({ lng: 'en', initAsync: false, ns: ['translation'], partialBundledLanguages: true, resources: { en: { translation: { title: 'OLD' } } } })
const t = i.getFixedT('en')
const before = [t('title'), t('title')]
const reloaded = i.reloadResources()
const during = [t('title'), t('title')]
reloaded.then(() => {
  globalThis.result = JSON.stringify({ before, during, after: [t('title'), t('title')] })
})
`

func TestSSRI18nextCacheForgetsWhenABackendAnswers(t *testing.T) {
	forI18nextVersions(t, func(t *testing.T, ver i18nextVersion) {
		plain, cached := i18nextCompare(t, ver, i18nextBackendPage)
		const want = `{"before":["OLD","OLD"],"during":["OLD","OLD"],"after":["NEW","NEW"]}`
		if plain != want || cached != want {
			t.Errorf("without the cache %s, with it %s, want %s", plain, cached, want)
		}
	})
}
