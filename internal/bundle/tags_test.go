package bundle

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/liustack/slowjs"
)

// tagsPage renders elements of many kinds with props of every kind the start tag of React DOM writes:
// class names, styles of numbers, units, custom properties and vendor names, booleans, URLs,
// data and aria names, event handlers, aliases, children that are texts, numbers and elements, tags
// of custom elements and of SVG, values to escape, and props the engine leaves to React (an
// object's toString, innerHTML, a javascript: URL, rare attributes, getters). It renders the page
// twice, so that the caches of names and tags are warm the second time, and a render that throws
// gives its message.
const tagsPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"const styles: any[] = [{ color: 'red', fontSize: 12, lineHeight: 1.5, opacity: 0, zIndex: 3, WebkitTransition: 'all', msTransform: 'none', '--x': ' 1 ' },\n" +
	"  { width: '', height: null, margin: 0, flex: 1, gridArea: 'a / b', fontFamily: '\"A\" & <B>' }, {}, { float: 'left', paddingTop: -1.25 }]\n" +
	"const props: any[] = [\n" +
	"  { className: 'a b', id: 'x', title: 'a & b < \"c\"', children: 'text & more' },\n" +
	"  { className: '', hidden: true, disabled: false, tabIndex: 0, role: 'button', 'aria-label': 'L', 'data-x': 7, children: 42 },\n" +
	"  { htmlFor: 'f', acceptCharset: 'utf-8', httpEquiv: 'x', onClick: () => 1, onclick: 'x', ref: null, suppressHydrationWarning: true },\n" +
	"  { href: '/a?b=1&c=2', src: 'https://x/y.png', action: '/post', formAction: '/act', autoFocus: true, multiple: '', inert: true },\n" +
	"  { href: 'javascript:alert(1)', children: 'js' },\n" +
	"  { foo: { toString: () => 'obj<' }, children: 'object' },\n" +
	"  { dangerouslySetInnerHTML: { __html: '<b>raw</b>' } },\n" +
	"  { download: true, rowSpan: 2, cols: 3, start: 1, capture: 'user' },\n" +
	"  { contentEditable: 'true', spellCheck: false, draggable: true, translate: 'no', value: 'v', defaultValue: 'd' },\n" +
	"  { 'bad name': 'x', '9lives': 'y', 'x:y': 'z', 'data-Big': 'B', 'aria-hidden': true, xmlLang: 'en', xlinkTitle: 't' },\n" +
	"  { children: ['a', 1, null, false, 'b'] },\n" +
	"  { children: '' },\n" +
	"  { width: 10, height: 1e21, x: -0, y: NaN, viewBox: '0 0 1 1', strokeWidth: 2, fillOpacity: 0.5 },\n" +
	"  { title: '\\ud800 lone 订单 🙂', lang: 'zh', dir: 'rtl' },\n" +
	"  Object.defineProperty({ id: 'getter' }, 'title', { get: () => 'got', enumerable: true }),\n" +
	"  ...styles.map((style) => ({ style, children: 'styled' })),\n" +
	"]\n" +
	"const tags = ['div', 'span', 'p', 'a', 'button', 'section', 'li', 'td', 'label', 'svg', 'path', 'my-element', 'h1']\n" +
	"function Page() {\n" +
	"  return <main>{tags.map((Tag: any, i) => <ul key={i}>{props.map((p, j) => <Tag key={j} {...p} />)}</ul>)}</main>\n" +
	"}\n" +
	"const render = () => { try { return renderToString(<Page />) } catch (e) { return 'threw ' + (e as Error).message } }\n" +
	"globalThis.result = render() + '\\n' + render()\n"

// engineTags makes a VM give a bundle the start and end tags of the engine as the pool of
// internal/ssr does, under the names __tifa_start_tag and __tifa_end_tag, and count the calls of
// the start tag in globalThis.startCalls and those it leaves to React in globalThis.declined.
func engineTags(t *testing.T) func(*slowjs.VM) {
	return func(vm *slowjs.VM) {
		t.Helper()
		if err := vm.RegisterStartTag("__engine_start_tag"); err != nil {
			t.Fatal(err)
		}
		if err := vm.RegisterEndTag("__tifa_end_tag"); err != nil {
			t.Fatal(err)
		}
		if _, err := vm.Eval("globalThis.startCalls = 0, globalThis.declined = 0\n"+
			"globalThis.__tifa_start_tag = (target, props, type, context, has, decline) => {\n"+
			"  startCalls++\n"+
			"  const r = __engine_start_tag(target, props, type, context, has, decline)\n"+
			"  if (r === decline) declined++\n"+
			"  return r\n"+
			"}", slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
	}
}

// The tifa plugin makes React's string renderer ask the engine for the start tag of each element
// that pushStartGenericElement writes and for its end tag, when the VM has the functions. Pages
// render the same HTML with them, without them and in a React DOM the plugin cannot change.
func TestSSRAsksTheEngineToWriteTags(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"tags.tsx": tagsPage})
	code, printed := buildPrinting(t, root, "tags.tsx")
	if strings.Contains(printed, "start and end tags") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if !strings.Contains(code, "__tifa_start_tag") || !strings.Contains(code, "__tifa_end_tag") {
		t.Fatal("the bundle does not ask for the tags of the engine")
	}
	unknown := withReactDOM(t, "function sanitizeURL(url) {", "function sanitizeURL(url) { void 0;")
	writeTree(t, unknown, map[string]string{"tags.tsx": tagsPage})
	unchanged, _ := buildPrinting(t, unknown, "tags.tsx")
	want := evalResult(t, unchanged)
	if strings.Contains(want, "threw") || !strings.Contains(want, `<div class="a b" id="x" title="a &amp; b &lt; &quot;c&quot;">text &amp; more</div>`) {
		t.Fatalf("the page:\n%.2000s", want)
	}
	if got := evalResult(t, code); got != want {
		t.Error("the page renders otherwise without the tags of the engine")
	}
	if got := evalResultWith(t, code, engineTags(t)); got != want {
		t.Errorf("the page renders otherwise with the tags of the engine:\n%s", firstDifference(got, want))
	}
	counts := evalResultWith(t, code+"\nglobalThis.result = startCalls + ' ' + declined", engineTags(t))
	var calls, declined int
	if _, err := fmt.Sscan(counts, &calls, &declined); err != nil || calls < 300 || declined == 0 || declined*3 > calls {
		t.Errorf("start tags asked of the engine and left to React: %s", counts)
	}
}

// firstDifference shows where two texts first differ.
func firstDifference(got, want string) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	from := max(0, i-200)
	return fmt.Sprintf("at %d\ngot:  %.400s\nwant: %.400s", i, got[from:], want[from:])
}

// A React DOM in which a part the tags of the engine stand for differs from React DOM 19.3, or the
// calls of them are written otherwise, keeps React's functions for every tag, and the build warns.
func TestSSRDoesNotAskTheEngineForTagsOfAnotherReactDOM(t *testing.T) {
	for name, change := range map[string][2]string{
		"pushStyleAttribute":      {"function pushStyleAttribute(target, style) {", "function pushStyleAttribute(target, style) { void 0;"},
		"aliases":                 {"  aliases = new Map([", "  aliases = new Map([\n    [\"tifaName\", \"tifa-name\"],"},
		"endChunkForTag":          {"function endChunkForTag(tag) {", "function endChunkForTag(tag) { void 0;"},
		"renderElement":           {"function renderElement(request, task, keyPath, type, props, ref) {", "function renderElement(request, task, keyPath, type, props, ref) { void 0;"},
		"the call of the end tag": {"        target.push(endChunkForTag(type));\n", "        target.push(endChunkForTag( type ));\n"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := withReactDOM(t, change[0], change[1])
			writeTree(t, changed, map[string]string{"tags.tsx": tagsPage})
			code, printed := buildPrinting(t, changed, "tags.tsx")
			if strings.Contains(code, "__tifa_start_tag") || strings.Contains(code, "__tifa_end_tag") {
				t.Error("the bundle asks the engine for tags")
			}
			if !strings.Contains(printed, installedReact(t, "react-dom")+": ") || !strings.Contains(printed, "the tifa plugin does not ask the engine to write the start and end tags") {
				t.Errorf("the build prints:\n%s", printed)
			}
			if !strings.HasPrefix(name, "the call") && !strings.Contains(printed, ": "+name) && !strings.Contains(printed, ": var "+name) {
				t.Errorf("the warning does not name %s:\n%s", name, printed)
			}
		})
	}
}

// With TIFA_EMIT_CHECK=1 the plugin builds the test hook: React writes every tag, the engine writes
// each one too, and each comparison goes to __tifa_emit_check, without which the bundle does not
// load. The page is the one React writes, and no tag differs.
func TestSSRChecksTheTagsOfTheEngine(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"tags.tsx": tagsPage})
	plain, _ := buildPrinting(t, root, "tags.tsx")
	t.Setenv("TIFA_EMIT_CHECK", "1")
	code, printed := buildPrinting(t, root, "tags.tsx")
	if strings.Contains(printed, "start and end tags") || !strings.Contains(code, "__tifa_emit_check") {
		t.Fatalf("the build of the test hook prints:\n%s", printed)
	}
	vm, err := slowjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if err := evalModule(vm, code); err == nil || !strings.Contains(err.Error(), "__tifa_emit_check") {
		t.Errorf("the bundle of the test hook loads without __tifa_emit_check: %v", err)
	}
	want := evalResult(t, plain)
	check := func(vm *slowjs.VM) {
		engineTags(t)(vm)
		if _, err := vm.Eval("globalThis.verdicts = { same: 0, declined: 0, differ: 0 }, globalThis.differences = []\n"+
			"globalThis.__tifa_emit_check = (verdict, tag, props, c, js) => {\n"+
			"  verdicts[verdict]++\n"+
			"  if (verdict === 'differ') differences.push(tag + ' ' + props + '\\n  C:     ' + c + '\\n  React: ' + js)\n"+
			"}", slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
	}
	if got := evalResultWith(t, code, check); got != want {
		t.Errorf("the page of the test hook differs:\n%s", firstDifference(got, want))
	}
	got := evalResultWith(t, code+"\nglobalThis.result = JSON.stringify(verdicts) + '\\n' + differences.join('\\n')", check)
	var same, declined, differ int
	if _, err := fmt.Sscanf(got, `{"same":%d,"declined":%d,"differ":%d}`, &same, &declined, &differ); err != nil || same < 300 || declined == 0 || differ != 0 {
		t.Errorf("verdicts: %s", got)
	}
}

// anchorsPage renders a and button, which React DOM writes with a case of its own in
// pushStartInstance, with the props those cases handle: href that is "", null and a javascript:
// URL, the form props of a button (name, formAction as text and as a function, formEncType,
// formMethod, formTarget), disabled, children of every kind, dangerouslySetInnerHTML and both of them
// together, which React throws on. Each element has a data-case that names it. Each is rendered
// alone, so that a throw is the case's own, and twice, so that the caches are warm.
const anchorsPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"const action = () => 1\n" +
	"const cases: Record<string, [string, any]> = {\n" +
	"  'a plain': ['a', { className: 'link', href: '/x?a=1&b=2', children: 'text & more' }],\n" +
	"  'a empty href': ['a', { href: '', children: 'empty' }],\n" +
	"  'a null href': ['a', { href: null, id: 'n', children: 'null' }],\n" +
	"  'a no href': ['a', { id: 'n', children: 'none' }],\n" +
	"  'a javascript': ['a', { href: 'javascript:alert(1)', children: 'js' }],\n" +
	"  'a function href': ['a', { href: action, children: 'f' }],\n" +
	"  'a flags': ['a', { href: '/y', hidden: true, rel: 'noopener', target: '_blank', 'aria-current': 'page', tabIndex: 0, onClick: action, children: 7 }],\n" +
	"  'a style': ['a', { href: '/z', style: { color: 'red', fontSize: 12 }, children: ['a', 1, null, <b key='b'>bold</b>] }],\n" +
	"  'a html': ['a', { href: '/h', dangerouslySetInnerHTML: { __html: '<i>raw</i>' } }],\n" +
	"  'a both': ['a', { href: '/h', children: 'x', dangerouslySetInnerHTML: { __html: '<i>raw</i>' } }],\n" +
	"  'a download': ['a', { href: '/d', download: true, children: 'd' }],\n" +
	"  'a empty text': ['a', { href: '/e', children: '' }],\n" +
	"  'button plain': ['button', { className: 'btn', type: 'submit', disabled: true, children: 'Save & close' }],\n" +
	"  'button name': ['button', { name: 'n', value: 'v', children: 'name' }],\n" +
	"  'button formAction': ['button', { formAction: '/act', children: 'action' }],\n" +
	"  'button formAction function': ['button', { formAction: action, name: 'q', children: 'function' }],\n" +
	"  'button form props': ['button', { formEncType: 'multipart/form-data', formMethod: 'post', formTarget: '_blank', children: 'form' }],\n" +
	"  'button formMethod': ['button', { formMethod: 'get', children: 'method' }],\n" +
	"  'button null form props': ['button', { name: null, formAction: undefined, formEncType: null, formMethod: null, formTarget: null, type: 'button', children: 'nulls' }],\n" +
	"  'button flags': ['button', { disabled: false, autoFocus: true, onClick: action, 'aria-pressed': true, 'data-x': 1, style: { margin: 0 }, children: 3 }],\n" +
	"  'button children': ['button', { type: 'button', children: [<span key='s'>icon</span>, ' label', 2] }],\n" +
	"  'button html': ['button', { dangerouslySetInnerHTML: { __html: '<b>x</b>' } }],\n" +
	"  'button both': ['button', { children: 'x', dangerouslySetInnerHTML: { __html: '<b>x</b>' } }],\n" +
	"  'button empty': ['button', {}],\n" +
	"}\n" +
	"const render = (Tag: any, p: any) => { try { return renderToString(<Tag data-case={p.case} {...p.props} />) } catch (e) { return 'threw ' + (e as Error).message } }\n" +
	"const once = () => Object.entries(cases).map(([name, [Tag, props]]) => name + ': ' + render(Tag, { case: name, props })).join('\\n')\n" +
	"globalThis.result = once() + '\\n' + once()\n"

// anchorTags is engineTags that also records in globalThis.asked, for each element with a data-case,
// "engine" when the engine wrote its start tag and "react" when it left it to React DOM. An element
// the engine was not asked about has no entry.
func anchorTags(t *testing.T) func(*slowjs.VM) {
	return func(vm *slowjs.VM) {
		t.Helper()
		engineTags(t)(vm)
		if _, err := vm.Eval("globalThis.asked = {}\n"+
			"const asking = globalThis.__tifa_start_tag\n"+
			"globalThis.__tifa_start_tag = (target, props, type, context, has, decline) => {\n"+
			"  const r = asking(target, props, type, context, has, decline)\n"+
			"  asked[props['data-case']] = r === decline ? 'react' : 'engine'\n"+
			"  return r\n"+
			"}", slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
	}
}

// React DOM writes a and button with a case of its own. The plugin makes them take the engine's start
// tag when that case writes what pushStartGenericElement does: an a whose href is not "", and a
// button with none of name, formAction, formEncType, formMethod and formTarget. The others stay in
// React's case, which the engine is not asked about. The HTML is the same as in React DOM unchanged.
func TestSSRAsksTheEngineForAnchorsAndButtons(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"anchors.tsx": anchorsPage})
	code, printed := buildPrinting(t, root, "anchors.tsx")
	if strings.Contains(printed, "start and end tags") {
		t.Errorf("the build prints:\n%s", printed)
	}
	unknown := withReactDOM(t, "function sanitizeURL(url) {", "function sanitizeURL(url) { void 0;")
	writeTree(t, unknown, map[string]string{"anchors.tsx": anchorsPage})
	unchanged, _ := buildPrinting(t, unknown, "anchors.tsx")
	want := evalResult(t, unchanged)
	for _, text := range []string{
		`a plain: <a data-case="a plain" class="link" href="/x?a=1&amp;b=2">text &amp; more</a>`,
		`a empty href: <a data-case="a empty href" href="">empty</a>`,
		"a both: threw",
		`button name: <button data-case="button name" value="v" name="n">name</button>`,
		`button formAction function: <button data-case="button formAction function" formAction="javascript:throw new Error(&#x27;React form unexpectedly submitted.&#x27;)">function</button>`,
		`button empty: <button data-case="button empty"></button>`,
	} {
		if !strings.Contains(want, text) {
			t.Fatalf("React DOM unchanged does not write %q:\n%.3000s", text, want)
		}
	}
	if got := evalResult(t, code); got != want {
		t.Errorf("the page renders otherwise without the tags of the engine:\n%s", firstDifference(got, want))
	}
	if got := evalResultWith(t, code, anchorTags(t)); got != want {
		t.Errorf("the page renders otherwise with the tags of the engine:\n%s", firstDifference(got, want))
	}
	asked := evalResultWith(t, code+"\nglobalThis.result = JSON.stringify(asked)", anchorTags(t))
	var verdicts map[string]string
	if err := json.Unmarshal([]byte(asked), &verdicts); err != nil {
		t.Fatalf("%v: %s", err, asked)
	}
	for name, verdict := range map[string]string{
		"a plain": "engine", "a null href": "engine", "a no href": "engine", "a flags": "engine", "a style": "engine",
		"a empty text": "engine", "a download": "react", "a function href": "engine", "a empty href": "",
		"a javascript": "react", "a html": "react", "a both": "react",
		"button plain": "engine", "button null form props": "engine", "button flags": "engine", "button children": "engine",
		"button empty": "engine", "button html": "react", "button both": "react",
		"button name": "", "button formAction": "", "button formAction function": "", "button form props": "", "button formMethod": "",
	} {
		if verdicts[name] != verdict {
			t.Errorf("%s: the engine wrote it or not: %q, want %q", name, verdicts[name], verdict)
		}
	}
}

// With TIFA_EMIT_CHECK=1 the engine's start tag of each a and button the plugin asks it about is
// compared with what React DOM's own case writes, and none differs.
func TestSSRChecksTheTagsOfAnchorsAndButtons(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"anchors.tsx": anchorsPage})
	plain, _ := buildPrinting(t, root, "anchors.tsx")
	t.Setenv("TIFA_EMIT_CHECK", "1")
	code, _ := buildPrinting(t, root, "anchors.tsx")
	check := func(vm *slowjs.VM) {
		engineTags(t)(vm)
		if _, err := vm.Eval("globalThis.tagsSame = {}, globalThis.differences = []\n"+
			"globalThis.__tifa_emit_check = (verdict, tag, props, c, js) => {\n"+
			"  if (verdict === 'same') tagsSame[tag] = (tagsSame[tag] || 0) + 1\n"+
			"  if (verdict === 'differ') differences.push(tag + ' ' + props + '\\n  C:     ' + c + '\\n  React: ' + js)\n"+
			"}", slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := evalResultWith(t, code, check), evalResult(t, plain); got != want {
		t.Errorf("the page of the test hook differs:\n%s", firstDifference(got, want))
	}
	got := evalResultWith(t, code+"\nglobalThis.result = JSON.stringify(tagsSame) + '\\n' + differences.join('\\n')", check)
	line, differences, _ := strings.Cut(got, "\n")
	var same map[string]int
	if err := json.Unmarshal([]byte(line), &same); err != nil || same["a"] < 10 || same["button"] < 8 || differences != "" {
		t.Errorf("tags the engine wrote as React does: %s", got)
	}
}
