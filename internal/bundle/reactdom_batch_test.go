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
