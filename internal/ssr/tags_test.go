package ssr

import (
	"testing"
	"time"

	"github.com/liustack/slowjs"
)

// Load gives every VM __tifa_start_tag and __tifa_end_tag, which the bundle's React DOM asks for
// when it loads: the start tag of an element with its text in one string, and the end tag.
func TestLoadDefinesStartAndEndTags(t *testing.T) {
	source := "globalThis.startRender = () => ''\n" +
		"const target = [], decline = {}\n" +
		"const tags = typeof __tifa_start_tag === 'function' && typeof __tifa_end_tag === 'function'\n" +
		"globalThis.tags = !tags ? 'missing' : [\n" +
		"  __tifa_start_tag(target, { className: 'a', children: 'x<' }, 'div', { viewTransition: null }, Object.prototype.hasOwnProperty, decline),\n" +
		"  __tifa_start_tag(target, { dangerouslySetInnerHTML: { __html: '' } }, 'div', { viewTransition: null }, Object.prototype.hasOwnProperty, decline) === decline,\n" +
		"  target, __tifa_end_tag('div')]\n" +
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
	got, err := vm.Eval(`JSON.stringify(globalThis.tags)`, slowjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got != `[null,true,["<div class=\"a\">x&lt;"],"</div>"]` {
		t.Errorf("tags = %v", got)
	}
}
