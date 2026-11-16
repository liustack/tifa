package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// radixContextPage makes scopes with createContextScope of @radix-ui/react-context the way Radix
// packages do, a scope that composes others among them, and records what their hooks give and
// what the providers and consumers of the scoped contexts see. The scope objects are written with
// each context named by the order it first shows up in. Two scopes have names that are no strings,
// whose toString the page counts, and one scope that another composes is renamed before the
// composed scope reads its name.
const radixContextPage = `import './host'
import * as React from 'react'
import { renderToString } from 'react-dom/server.edge'
import { createContextScope } from '@radix-ui/react-context'

let names = 0
const objectName = { toString: () => (names++, 'Obj') }

const [createPopperContext, createPopperScope] = createContextScope('Popper')
const [createDialogContext, createDialogScope] = createContextScope('Dialog', [createPopperScope])
const [createMenuContext, createMenuScope] = createContextScope('Menu', [createDialogScope, createPopperScope])
const [createObjectContext, createObjectScope] = createContextScope(objectName as any, [createPopperScope])
const [, createNumberScope] = createContextScope(7 as any, [createPopperScope])
const [, createRenamedScope] = createContextScope('Renamed')
createRenamedScope.scopeName = 'Changed'
const [, createOuterScope] = createContextScope('Outer', [createRenamedScope, createPopperScope])
const [, createSymbolScope] = createContextScope(Symbol('s') as any)

const [PopperProvider, usePopper] = createPopperContext('Popper', { side: 'none' })
const [DialogProvider, useDialog] = createDialogContext('Dialog')
const [MenuProvider, useMenu] = createMenuContext('Menu')
const [ObjectProvider, useObject] = createObjectContext('Object')

const usePopperScope = createPopperScope()
const useDialogScope = createDialogScope()
const useMenuScope = createMenuScope()
const useObjectScope = createObjectScope()
const useNumberScope = createNumberScope()
const useOuterScope = createOuterScope()
const useSymbolScope = createSymbolScope()

const contexts = new Map<object, string>()
const describe = (v: any): any => {
  if (v === null || typeof v !== 'object') return v === undefined ? 'undefined' : v
  if (v.$$typeof !== undefined) {
    if (!contexts.has(v)) contexts.set(v, 'context ' + contexts.size + (v.displayName ? ' ' + v.displayName : ''))
    return contexts.get(v)
  }
  if (Array.isArray(v)) return v.map(describe)
  return Object.fromEntries(Reflect.ownKeys(v).map((k) => [String(k), describe(v[k])]))
}

const seen: any[] = []
function Scopes() {
  const popper = usePopperScope(undefined)
  const dialog = useDialogScope(undefined)
  const menu = useMenuScope(undefined)
  const inner = useMenuScope(menu.__scopeMenu)
  const fromDialog = useDialogScope(menu.__scopeMenu)
  seen.push({ popper, dialog, menu, inner, fromDialog, object: useObjectScope(undefined), number: useNumberScope(undefined), outer: useOuterScope(undefined) })
  return null
}
function Consumers({ scope }: any) {
  seen.push({ popper: usePopper('Popper consumer', scope), dialog: useDialog('Dialog consumer', scope), menu: useMenu('Menu consumer', scope, { optional: true }) })
  return <i>{usePopper('Text', scope).side}</i>
}
function Scoped() {
  const { __scopeMenu: scope } = useMenuScope(undefined)
  const object = useObjectScope(undefined)
  return (
    <MenuProvider scope={scope} name="menu">
      <DialogProvider scope={scope} open="yes">
        <PopperProvider scope={scope} side="top">
          <Consumers scope={scope} />
          <ObjectProvider scope={object.__scopeObj} kind="object">
            <b>{useObject.name}</b>
          </ObjectProvider>
        </PopperProvider>
      </DialogProvider>
      <Consumers scope={undefined} />
    </MenuProvider>
  )
}
function Symbols() {
  useSymbolScope(undefined)
  return null
}

const out: any[] = []
for (const [name, element] of [['scopes', <Scopes />], ['providers', <Scoped />], ['again', <Scopes />], ['symbol', <Symbols />]] as const) {
  seen.length = 0
  let html = ''
  let error = ''
  try {
    html = renderToString(element)
  } catch (e) {
    error = String((e as Error).message)
  }
  out.push({ name, html, error, seen: describe(seen) })
}
globalThis.result = JSON.stringify({ out, names })
`

type radixContextResult struct {
	Out   []json.RawMessage
	Names int
}

func radixContextRender(t *testing.T, root string) (radixContextResult, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"scopes.tsx": radixContextPage})
	code, printed := buildPrinting(t, root, "scopes.tsx")
	var r radixContextResult
	if err := json.Unmarshal([]byte(evalResult(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Out) != 4 {
		t.Fatalf("%d cases", len(r.Out))
	}
	return r, code, printed
}

// withRadixContext returns a copy of testdata/ssr whose react-context has its dist/index.mjs
// changed by change.
func withRadixContext(t *testing.T, change func(string) string) string {
	t.Helper()
	return withChangedPackage(t, "@radix-ui/react-context", filepath.Join("dist", "index.mjs"), change)
}

// radixContextOld turns dist/index.mjs of react-context 1.3 into the form of 1.1.0 to 1.2.0 where
// the plugin changes it: the same functions without __name around them.
func radixContextOld(source string) string {
	for _, r := range [][2]string{
		{"const createScope = /* @__PURE__ */ __name(() => {", "const createScope = () => {"},
		{"return /* @__PURE__ */ __name(function useScope(scope) {", "return function useScope(scope) {"},
		{"    }, \"useScope\");\n  }, \"createScope\");", "    };\n  };"},
		{"__name(createContextScope, \"createContextScope\");\n", ""},
		{"return /* @__PURE__ */ __name(function useComposedScopes(overrideScopes) {", "return function useComposedScopes(overrideScopes) {"},
		{"    }, \"useComposedScopes\");\n  }, \"createScope\");", "    };\n  };"},
	} {
		source = strings.ReplaceAll(source, r[0], r[1])
	}
	return source
}

// createContextScope of @radix-ui/react-context writes the key `__scope${scopeName}` in every call
// of the hooks of a scope. The tifa plugin has each place keep the key it wrote for the name it
// saw last, in the two forms the function has had since 1.1.0, and leaves a form it does not know
// alone with a warning. The hooks give scope objects with the same keys and contexts, the
// providers and consumers see the same values, a name that is no string is turned into a string as
// often as before, and a symbol for a name throws as before.
func TestSSRKeepsTheKeysOfRadixScopes(t *testing.T) {
	unknown := func(source string) string {
		return strings.Replace(source, "const currentScope = scopeProps[`__scope${scopeName}`];", "const currentScope = /* changed */ scopeProps[`__scope${scopeName}`];", 1)
	}
	for _, form := range []struct {
		name string
		old  bool
	}{{"1.3", false}, {"1.1", true}} {
		root := copyTestdata(t, "ssr", true)
		if form.old {
			root = withRadixContext(t, radixContextOld)
		}
		changed, code, printed := radixContextRender(t, root)
		if strings.Contains(printed, "@radix-ui/react-context") {
			t.Errorf("%s: the build prints:\n%s", form.name, printed)
		}
		if n := strings.Count(code, "`__scope${"); n != 2 {
			t.Errorf("%s: the bundle writes the key of a scope with a template in %d places, want the 2 of tifaScopeKey", form.name, n)
		}
		plain, code, printed := radixContextRender(t, withRadixContext(t, func(source string) string {
			if form.old {
				source = radixContextOld(source)
			}
			return unknown(source)
		}))
		if strings.Count(printed, "@radix-ui/react-context 1.3.0") != 1 || !strings.Contains(printed, "has no createContextScope the tifa plugin knows") {
			t.Errorf("%s: the build prints:\n%s", form.name, printed)
		}
		if n := strings.Count(code, "`__scope${"); n != 3 {
			t.Errorf("%s: a react-context the plugin does not know writes the key with a template in %d places, want 3", form.name, n)
		}
		if plain.Names == 0 || changed.Names != plain.Names {
			t.Errorf("%s: the name that is no string is turned into a string %d times, want %d", form.name, changed.Names, plain.Names)
		}
		for i, p := range plain.Out {
			if string(changed.Out[i]) != string(p) {
				t.Errorf("%s: case %d gives\n%s\nwant\n%s", form.name, i, changed.Out[i], p)
			}
		}
		if !strings.Contains(string(plain.Out[3]), "convert symbol to string") {
			t.Errorf("%s: a symbol for a name does not throw: %s", form.name, plain.Out[3])
		}
	}
}
