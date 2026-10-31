package bundle

import (
	"regexp"
	"strings"
	"testing"
)

// attributeOrderPage renders attributes of every kind of name with values of every kind, each
// value for all the names in turn, twice and the second time in the opposite order of values, so
// that a name is seen first with a value that is written and then with one that is not, and the
// other way round, and 5,000 names of data attributes and of event handlers, more than the module keeps. A
// render that throws gives its message.
const attributeOrderPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"const names = ['className', 'tabIndex', 'dir', 'role', 'viewBox', 'width', 'height', 'src', 'href', 'style', 'value', 'hidden', 'xlinkHref',\n" +
	"  'data-slot', 'data-x', 'aria-hidden', 'aria-x', 'id', 'type', 'name', 'title', 'htmlFor', 'httpEquiv', 'acceptCharset', 'strokeWidth', 'xmlns',\n" +
	"  'x:y', 'bad name', '9lives', 'onClick', 'onclick', 'on', 'ON', 'one', 'foo', 'constructor', '__proto__', 'hasOwnProperty', 'ref', 'key']\n" +
	"const values: unknown[] = ['text', '', 'a & b < \"c\" d\\'e', 0, 5, -1.5, NaN, true, false, null, undefined, Symbol('s'), () => 1,\n" +
	"  { toString: () => 'obj<' }, 10n]\n" +
	"const out: string[] = []\n" +
	"const many: any = {}\n" +
	"for (let i = 0; i < 5000; i++) { many['data-n' + i] = i; many['onN' + i] = () => 1 }\n" +
	"for (let pass = 0; pass < 2; pass++) {\n" +
	"  out.push(renderToString(<p {...many} />))\n" +
	"  for (const value of pass ? [...values].reverse() : values) {\n" +
	"    for (const name of names) {\n" +
	"      const props = { [name]: value } as any\n" +
	"      try {\n" +
	"        out.push(renderToString(<div><svg {...props}><use {...props} /></svg><p {...props} /></div>))\n" +
	"      } catch (e) {\n" +
	"        out.push(name + ' ' + String(typeof value) + ' threw ' + (e as Error).message)\n" +
	"      }\n" +
	"    }\n" +
	"  }\n" +
	"}\n" +
	"globalThis.result = out.join('\\n')\n"

// attributeLookup finds, in a minified bundle, the lookup of the chunk that pushAttribute writes
// before the value of an attribute whose name it has seen.
var attributeLookup = regexp.MustCompile("\\([\\w$]+=[\\w$]+\\.get\\([\\w$]+\\)\\)!==void 0\\)\\{if\\([\\w$]+===``\\)return;if\\(typeof [\\w$]+==`string`\\|\\|typeof [\\w$]+==`number`\\)")

// pushAttribute writes a string or a number that is the value of an attribute whose name it has
// seen with the chunk it made for the name, with one lookup of the name in place of the checks of
// the name, its alias and its validity. Pages render the same as in a React DOM the plugin does not
// change, in whichever order names and values come.
func TestSSRWritesAttributesByTheirNames(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"attributes.tsx": attributeOrderPage})
	code, printed := buildPrinting(t, root, "attributes.tsx")
	if !attributeLookup.MatchString(code) {
		t.Error("the bundle does not look up the names of attributes")
	}
	if strings.Contains(printed, "react-dom") {
		t.Errorf("the build prints:\n%s", printed)
	}
	written := evalResult(t, code)
	for _, want := range []string{
		`<p data-slot="text">`, `<p aria-hidden="true">`, `<p aria-hidden="false">`, `<p id="5">`, `<p for="text">`, `<p http-equiv="text">`,
		`<p accept-charset="text">`, `<p stroke-width="5">`, `<p tabindex="0">`, `<p class="text">`, `<p width="NaN">`, `<p foo="obj&lt;">`,
		`<p data-x="a &amp; b &lt; &quot;c&quot; d&#x27;e">`, `data-n4999="4999"`,
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the page has no %s", want)
		}
	}
	for _, not := range []string{`onclick=`, `onClick=`, `onN4999`, `one=`, `bad name`, `9lives`, ` key=`, ` ref=`} {
		if strings.Contains(written, not) {
			t.Errorf("the page has %s", not)
		}
	}
	changed := withReactDOM(t, "function pushAttribute(target, name, value) {\n  switch (name) {", "function pushAttribute(target, name, value) /* changed */ {\n  switch (name) {")
	writeTree(t, changed, map[string]string{"attributes.tsx": attributeOrderPage})
	code, printed = buildPrinting(t, changed, "attributes.tsx")
	if attributeLookup.MatchString(code) {
		t.Error("the bundle looks up the names of attributes in a React DOM it does not know")
	}
	if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "does not keep the names of the attributes it has seen") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got := evalResult(t, code); got != written {
		t.Error("the page renders otherwise without the lookup")
	}
}

// nodesPage renders, twice, trees made of every kind of node that React's string renderer hands to
// retryNode: texts, numbers, arrays, sets, generators, fragments, context providers and consumers, a
// context as a child, classes, forwardRef and memo components, lazy components, promises as
// children and in use, Suspense boundaries with children that wait, and children that make
// React throw. A render that throws gives its message.
const nodesPage = "import './host'\n" +
	"import { renderToString, renderToStaticMarkup } from 'react-dom/server.edge'\n" +
	"import { Fragment, Suspense, StrictMode, Profiler, Component, createContext, forwardRef, lazy, memo, use, useId } from 'react'\n" +
	"const Ctx = createContext('default')\n" +
	"class Klass extends Component<{ n: number }> { render() { return <i>{this.props.n}{[1, 2]}</i> } }\n" +
	"const Fwd = forwardRef<HTMLElement, { label: string }>((p, ref) => <b ref={ref as never}>{p.label}</b>)\n" +
	"const Memo = memo(function M({ v }: { v: string }) { return <u>{v}</u> })\n" +
	"function* gen() { yield 'a'; yield <s key=\"s\">b</s>; yield 3 }\n" +
	"const Lazy = lazy(() => Promise.resolve({ default: () => <em>lazy</em> }))\n" +
	"const Never = lazy(() => new Promise<never>(() => {}))\n" +
	"const settled = Promise.resolve(<q>promise</q>)\n" +
	"const Uses = () => <p>{use(settled)}</p>\n" +
	"const Ids = () => <ul>{[0, 1, 2].map((i) => <li key={i} id={useId()}>{[i, i]}</li>)}</ul>\n" +
	"const Boom = () => { throw new Error('boom') }\n" +
	"const trees: [string, () => unknown][] = [\n" +
	"  ['text', () => 'plain & <text>'], ['empty', () => ''], ['number', () => 42], ['bigint', () => 10n], ['null', () => null],\n" +
	"  ['mixed', () => <p>{true}{false}{null}{undefined}{0}{'a'}{1n}{() => 1}</p>],\n" +
	"  ['array', () => [<a key=\"1\">1</a>, 'x', [<b key=\"2\">2</b>, [3]]]],\n" +
	"  ['set', () => new Set(['p', <i key=\"i\">q</i>])], ['generator', () => gen()],\n" +
	"  ['fragment', () => <><p>1</p><Fragment key=\"k\"><p>2</p></Fragment></>],\n" +
	"  ['context', () => <Ctx.Provider value=\"v\"><Ctx.Consumer>{(v: string) => <p>{v}</p>}</Ctx.Consumer></Ctx.Provider>],\n" +
	"  ['context as a child', () => <Ctx value=\"outer\"><p>{Ctx as never}<Ctx value=\"inner\">{Ctx as never}</Ctx></p></Ctx>],\n" +
	"  ['class', () => <Klass n={3} />], ['forwardRef', () => <Fwd label=\"l\" />], ['memo', () => <Memo v=\"m\" />],\n" +
	"  ['strict and profiler', () => <StrictMode><Profiler id=\"p\" onRender={() => {}}><p>s</p></Profiler></StrictMode>],\n" +
	"  ['lazy', () => <Suspense fallback=\"wait\"><Lazy /></Suspense>],\n" +
	"  ['lazy that waits', () => <p><Suspense fallback=\"wait\"><Never /></Suspense></p>],\n" +
	"  ['promise child', () => <p>{settled as never}</p>], ['use', () => <Suspense fallback=\"wait\"><Uses /></Suspense>],\n" +
	"  ['ids', () => <><Ids /><Ids /></>],\n" +
	"  ['throws', () => <p><Boom /></p>], ['object child', () => <p>{{ a: 1, b: 2 } as never}</p>], ['symbol child', () => <p>{Symbol('s') as never}</p>],\n" +
	"  ['throws in a boundary', () => <Suspense fallback=\"f\"><Boom /></Suspense>],\n" +
	"]\n" +
	"const out: string[] = []\n" +
	"for (let pass = 0; pass < 2; pass++) {\n" +
	"  await Promise.resolve()\n" +
	"  for (const [name, tree] of trees) {\n" +
	"    for (const render of [renderToString, renderToStaticMarkup]) {\n" +
	"      try {\n" +
	"        out.push(name + ': ' + render(tree() as never, { onError: () => {} } as never))\n" +
	"      } catch (e) {\n" +
	"        out.push(name + ' threw ' + (e as Error).message)\n" +
	"      }\n" +
	"    }\n" +
	"  }\n" +
	"}\n" +
	"globalThis.result = out.join('\\n')\n"

// retryNodeCalls counts, in a minified bundle, the tests of a task's replay that renderNodeDestructive
// makes before it hands a node to retryNode, in the function itself and in each place that writes it out.
func retryNodeCalls(code string) int { return strings.Count(code, "replay.slots==`number`") }

// React's string renderer hands every node to retryNode through renderNodeDestructive, a function
// that only forwards it. The tifa plugin writes its body where it is called with names and
// numbers as arguments, so a node costs one call less. Pages render the same as in a React DOM the
// plugin does not change.
func TestSSRHandsNodesOnWithoutAnExtraCall(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"nodes.tsx": nodesPage})
	code, printed := buildPrinting(t, root, "nodes.tsx")
	if strings.Contains(printed, "react-dom") {
		t.Errorf("the build prints:\n%s", printed)
	}
	written := evalResult(t, code)
	for _, want := range []string{
		"text: plain &amp; &lt;text&gt;", "number: 42", "array: <a>1</a>x<b>2</b>3", "set: p<i>q</i>", "generator: a<s>b</s>3",
		"context: <p>v</p>", "context as a child: <p>outer<!-- -->inner</p>", "class: <i>3<!-- -->1<!-- -->2</i>", "memo: <u>m</u>", "lazy: <em>lazy</em>",
		"promise child: <p><q>promise</q></p>", "use: <p><q>promise</q></p>", "throws threw boom", "object child threw Minified React error #31",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the page has no %s", want)
		}
	}
	changed := withReactDOM(t, "function renderNodeDestructive(request, task, node, childIndex) {", "function renderNodeDestructive(request, task, node, childIndex) /* changed */ {")
	writeTree(t, changed, map[string]string{"nodes.tsx": nodesPage})
	plain, printed := buildPrinting(t, changed, "nodes.tsx")
	if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "does not hand nodes to retryNode without calling it") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if retryNodeCalls(code) <= retryNodeCalls(plain) {
		t.Errorf("the bundle writes out %d tests of the replay, the bundle of a React DOM the plugin does not know %d", retryNodeCalls(code), retryNodeCalls(plain))
	}
	if got := evalResult(t, plain); got != written {
		t.Error("the page renders otherwise without the change")
	}
}
