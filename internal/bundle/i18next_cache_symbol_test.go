package bundle

import (
	"encoding/json"
	"testing"
)

// i18nextSymbolPage gives a post-processor an option under a Symbol key. The key of the cache has
// to tell the two calls apart, or skip them.
const i18nextSymbolPage = `import i18next from 'i18next'

const tag = Symbol('tag')
const i = i18next.createInstance()
i.use({ type: 'postProcessor', name: 'tag', process: (value, key, options) => value + options[tag] })
i.init({ lng: 'en', initAsync: false, resources: { en: { translation: { title: 'Title' } } } })
const t = i.getFixedT('en')
const calls = []
for (const v of ['A', 'B', 'A', 'B']) calls.push(t('title', { postProcess: 'tag', [tag]: v }))
globalThis.result = JSON.stringify(calls)
`

func TestSSRI18nextCacheSeesSymbolOptions(t *testing.T) {
	forI18nextVersions(t, func(t *testing.T, ver i18nextVersion) {
		plain, cached := i18nextCompare(t, ver, i18nextSymbolPage)
		want, _ := json.Marshal([]string{"TitleA", "TitleB", "TitleA", "TitleB"})
		if plain != string(want) || cached != string(want) {
			t.Errorf("without the cache %s, with it %s, want %s", plain, cached, want)
		}
	})
}
