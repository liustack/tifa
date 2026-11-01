package bundle

import (
	"strings"
	"testing"
)

// recognizedPage loads, in one SSR bundle, the code of every library whose hot spots the plugin
// rewrites: React DOM's string renderer, React's createElement and cloneElement, its JSX runtime,
// seroval, @emotion/hash, lucide-react, i18next, react-i18next, @radix-ui/react-slot, clsx and
// core-js.
const recognizedPage = `import 'core-js/internals/internal-state.js'
import { cloneElement, createElement } from 'react'
import { jsx } from 'react/jsx-runtime'
import { prerenderToString, renderToString } from 'react-dom/server.edge'
import { serialize } from 'seroval'
import hash from '@emotion/hash'
import { House } from 'lucide-react'
import i18next from 'i18next'
import { I18nextProvider, useTranslation } from 'react-i18next'
import { Slot } from '@radix-ui/react-slot'
import clsx from 'clsx'

function Page() {
  const { t } = useTranslation()
  return jsx(Slot, { className: clsx('a', 'b'), children: cloneElement(createElement(House, { title: t('title') + hash('x') }), { id: 'icon' }) })
}
const element = jsx(I18nextProvider, { i18n: i18next, children: jsx(Page, {}) })
;(globalThis as any).result = [renderToString(element), prerenderToString(element), serialize(['a', 'b']), i18next.getFixedT('en')('title')]
`

// Each rewrite of the plugin recognizes the library it changes in the versions the tests pin. A
// build that prints the text every warning of an unrecognized rewrite starts with fails here, so
// a dependency that moves on and silently turns a rewrite off is found by this test.
func TestSSRBuildRecognizesEveryRewrite(t *testing.T) {
	code, printed := i18nextApp(t, copyTestdata(t, "ssr", true), recognizedPage, `{ target: ["es2022"], i18nextCache: true }`)
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	// The page reached the code of the libraries, or the check proves nothing.
	for _, want := range []string{"__tifa_escape", "__tifa_copy_props", "__tifa_murmur2__", "tifaFormActions"} {
		if !strings.Contains(code, want) {
			t.Errorf("the bundle has no %s: the rewrite it belongs to did not run", want)
		}
	}
}

// A library the plugin does not recognize makes the same build print the text, so the test above
// has something to find.
func TestSSRBuildOfAnUnrecognizedLibraryPrintsTheText(t *testing.T) {
	root := withChangedPackage(t, "@emotion/hash", "dist/emotion-hash.esm.js", func(s string) string {
		return strings.ReplaceAll(s, "murmur2", "murmur3")
	})
	_, printed := i18nextApp(t, root, recognizedPage, `{ target: ["es2022"], i18nextCache: true }`)
	if !strings.Contains(printed, notRecognized+"@emotion/hash 0.9.2: ") {
		t.Errorf("the build prints:\n%s", printed)
	}
}
