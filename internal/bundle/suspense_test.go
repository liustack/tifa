package bundle

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/slowjs"
)

// lazyTree renders, with Render, a tree whose components load the ways a page may wait for them:
// a React.lazy of a module inside the bundle, a lazy component that is itself made of lazy ones, a
// lazy component outside any Suspense boundary, and use of a promise settled in a job. Its
// components without the waits are the tree StaticTree renders.
const lazyTree = "import { Suspense, lazy, use, type ComponentType } from 'react'\n" +
	"import Panel from './panel'\n" +
	"const LazyPanel = lazy(() => import('./panel'))\n" +
	"const Nested = lazy(() => Promise.resolve().then(() => ({ default: () => <Suspense fallback=\"inner\"><LazyPanel /></Suspense> })))\n" +
	"const ready = Promise.resolve().then(() => 'ready')\n" +
	"function Uses() { return <b>{use(ready)}</b> }\n" +
	"function Tree({ P, N, U }: { P: ComponentType; N: ComponentType; U: ComponentType }) {\n" +
	"  return <main><Suspense fallback=\"wait\"><P /><N /><U /></Suspense><P /></main>\n" +
	"}\n" +
	"export const Render = () => <Tree P={LazyPanel} N={Nested} U={Uses} />\n" +
	"export const StaticTree = () => <Tree P={Panel} N={() => <Suspense fallback=\"inner\"><Panel /></Suspense>} U={() => <b>ready</b>} />\n"

// prerenderToString, which the tifa plugin adds to React DOM's server entry, renders what
// renderToString would and waits for the components that suspend on work the jobs of the VM
// settle. Its HTML is what renderToString writes for the same tree without the waits: every
// boundary complete in place, no fallback and no script that swaps one in.
func TestSSRPrerenderToStringWaitsForLazyComponents(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{
		"panel.tsx": "export default function Panel() {\n  return <p>panel</p>\n}\n",
		"tree.tsx":  lazyTree,
		"lazy.tsx": "import './host'\n" +
			"import { renderToString, prerenderToString } from 'react-dom/server.edge'\n" +
			"import { Render, StaticTree } from './tree'\n" +
			"const errors: unknown[] = []\n" +
			"const want = renderToString(<StaticTree />)\n" +
			"prerenderToString(<Render />, { onError: (e: unknown) => errors.push(e) } as any).then((html: string) => {\n" +
			"  globalThis.result = [html === want, errors.length, html].join(' ')\n" +
			"}, (e: Error) => { globalThis.result = 'rejected ' + e.message })\n",
	})
	code, _ := buildScript(t, root, "lazy.tsx")
	got := evalResult(t, code)
	if !strings.HasPrefix(got, "true 0 <main>") || strings.Contains(got, "wait") || strings.Count(got, "<p>panel</p>") != 3 {
		t.Errorf("result = %q, want the HTML of the tree without waits and no error", got)
	}
}

// A page renders the same with prerenderToString as with renderToString when nothing suspends.
func TestSSRPrerenderToStringWritesWhatRenderToStringWrites(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	page := strings.Replace(segmentsPage, "<Suspense fallback=\"e\"><Never /></Suspense>", "", 1)
	page = strings.Replace(page, "globalThis.result = [0, 1, 2].map(() => renderToString(<Page />)).join('\\n')\n",
		"import { prerenderToString } from 'react-dom/server.edge'\n"+
			"function Ready() { return <main><Page /><Suspense fallback=\"g\"><p>ready</p></Suspense></main> }\n"+
			"const want = renderToString(<Ready />)\n"+
			"prerenderToString(<Ready />).then((html: string) => { globalThis.result = String(html === want) })\n", 1)
	if page == segmentsPage {
		t.Fatal("segmentsPage changed")
	}
	writeTree(t, root, map[string]string{"same.tsx": page})
	code, _ := buildScript(t, root, "same.tsx")
	if got := evalResult(t, code); got != "true" {
		t.Errorf("result = %q, want the HTML renderToString writes", got)
	}
}

// Work that never settles leaves prerenderToString waiting, since the VM has no timers and does no
// I/O. Its signal then ends the render as renderToString ends it: the boundaries still waiting are
// left to the browser, and onError gets the reason renderToString gives.
func TestSSRPrerenderToStringStopsWhenItsSignalAborts(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"never.tsx": "import './host'\n" +
		"import { renderToString, prerenderToString } from 'react-dom/server.edge'\n" +
		"import { Suspense, lazy } from 'react'\n" +
		"const Never = lazy(() => new Promise<never>(() => {}))\n" +
		"const tree = <p><Suspense fallback=\"wait\"><Never /></Suspense></p>\n" +
		"const want: unknown[] = [], got: unknown[] = []\n" +
		"const html = renderToString(tree, { onError: (e: unknown) => want.push(e) } as any)\n" +
		"const controller = new AbortController()\n" +
		"globalThis.stop = () => controller.abort()\n" +
		"prerenderToString(tree, { onError: (e: unknown) => got.push(e), signal: controller.signal } as any).then((result: string) => {\n" +
		"  globalThis.result = [result === html, got.length, got[0] === want[0], html].join(' ')\n" +
		"})\n",
	})
	code, _ := buildScript(t, root, "never.tsx")
	vm, err := slowjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if err := evalModule(vm, code); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.ExecutePendingJobs(); err != nil {
		t.Fatal(err)
	}
	if got, err := vm.Eval("String(globalThis.result)", slowjs.EvalGlobal); err != nil || got != "undefined" {
		t.Fatalf("result before stop = %v, %v, want none", got, err)
	}
	if _, err := vm.Eval("globalThis.stop()", slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.ExecutePendingJobs(); err != nil {
		t.Fatal(err)
	}
	got, err := vm.Eval("String(globalThis.result)", slowjs.EvalGlobal)
	if err != nil || !strings.HasPrefix(got.(string), "true 1 true <p><!--$!-->") {
		t.Errorf("result = %v, %v, want what renderToString writes and its error", got, err)
	}
}

// A React DOM whose renderToStringImpl the plugin does not know gets a warning, and its
// prerenderToString renders with renderToString, which does not wait: the lazy component is left
// to the browser, as before the plugin waited.
func TestSSRPrerenderToStringOfAnUnknownReactDOMDoesNotWait(t *testing.T) {
	root := withChangedPackage(t, "react-dom", filepath.Join("cjs", "react-dom-server-legacy.browser.production.js"), func(source string) string {
		const ready = "    readyToStream = !1;\n"
		if strings.Count(source, ready) != 1 {
			t.Fatalf("react-dom does not have %q once", ready)
		}
		return strings.Replace(source, ready, "    readyToStream = false;\n", 1)
	})
	writeTree(t, root, map[string]string{
		"panel.tsx": "export default function Panel() {\n  return <p>panel</p>\n}\n",
		"old.tsx": "import './host'\n" +
			"import { prerenderToString } from 'react-dom/server.edge'\n" +
			"import { Suspense, lazy } from 'react'\n" +
			"const Panel = lazy(() => import('./panel'))\n" +
			"prerenderToString(<Suspense fallback=\"wait\"><Panel /></Suspense>).then((html: string) => { globalThis.result = html })\n",
	})
	code, printed := buildPrinting(t, root, "old.tsx")
	if !strings.Contains(printed, "has no renderToStringImpl the tifa plugin knows") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got := evalResult(t, code); !strings.HasPrefix(got, "<!--$!-->") || !strings.Contains(got, "wait") {
		t.Errorf("result = %q, want the boundary left to the browser", got)
	}
}
