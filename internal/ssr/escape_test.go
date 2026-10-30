package ssr

import (
	"testing"
	"time"

	"github.com/liustack/slowjs"
)

// reactEscape is escapeTextForBrowser of React DOM 19 as its string renderer has it, without the
// tifa plugin's changes.
const reactEscape = `function reactEscape(text) {
  if ("boolean" === typeof text || "number" === typeof text || "bigint" === typeof text) return "" + text;
  text = "" + text;
  var match = /["'&<>]/.exec(text);
  if (match) {
    var html = "", index, lastIndex = 0;
    for (index = match.index; index < text.length; index++) {
      switch (text.charCodeAt(index)) {
        case 34: match = "&quot;"; break;
        case 38: match = "&amp;"; break;
        case 39: match = "&#x27;"; break;
        case 60: match = "&lt;"; break;
        case 62: match = "&gt;"; break;
        default: continue;
      }
      lastIndex !== index && (html += text.slice(lastIndex, index));
      lastIndex = index + 1;
      html += match;
    }
    text = lastIndex !== index ? html + text.slice(lastIndex, index) : html;
  }
  return text;
}`

// __tifa_escape is React DOM's escapeTextForBrowser, written in C in the engine: the VMs of the
// pool have it, and it gives the text React's function gives for strings, numbers and objects.
func TestEscapeIsReactsEscape(t *testing.T) {
	vm, err := slowjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vm.Close() })
	if got, err := vm.Eval(`typeof __tifa_escape`, slowjs.EvalGlobal); err != nil || got != "undefined" {
		t.Fatalf("a new VM has __tifa_escape: %v %v", got, err)
	}
	if err := registerEscape(vm); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Eval(reactEscape, slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	got, err := vm.Eval(`(() => {
  const inputs = ['', 'plain', '&', '<>', '"\'', 'a & b < c > d " e \' f', '订单 & 🙂', '\ud800 & lone', 'x'.repeat(5000) + '&',
    0, -0, 1.5, 10n, true, null, undefined, { toString: () => '<b>' }, ['a', '&']]
  for (let c = 0; c < 0x10000; c++) inputs.push('a' + String.fromCharCode(c) + 'b')
  const wrong = inputs.filter((x) => __tifa_escape(x) !== reactEscape(x))
  return inputs.length + ' ' + wrong.length
})()`, slowjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got != "65554 0" {
		t.Errorf("inputs and those escaped otherwise than React does: %s", got)
	}
}

// Load gives every VM __tifa_escape, which the bundle's React DOM asks for when it loads.
func TestLoadDefinesEscape(t *testing.T) {
	source := "globalThis.startRender = () => ''\n" +
		"globalThis.escaped = typeof __tifa_escape === 'function' ? __tifa_escape('<&>') : 'missing'\n" +
		"export {}\n"
	bytecode, err := Compile(source, 64<<20, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := Load(bytecode, 64<<20, 4096, 10*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	got, err := vm.Eval(`globalThis.escaped`, slowjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got != "&lt;&amp;&gt;" {
		t.Errorf("escaped = %v", got)
	}
}
