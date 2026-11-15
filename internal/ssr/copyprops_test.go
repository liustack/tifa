package ssr

import (
	"testing"
	"time"

	"github.com/liustack/slowjs"
)

// Load gives every VM __tifa_copy_props, which the bundle's React asks for when it loads, and the
// function copies the keys of a config as React's loop does.
func TestLoadDefinesCopyProps(t *testing.T) {
	source := "globalThis.startRender = () => ''\n" +
		"const props = {}\n" +
		"const copied = typeof __tifa_copy_props === 'function'\n" +
		"  ? __tifa_copy_props({ a: 1, key: 'k', b: [2] }, props, Object.prototype.hasOwnProperty, false) : 'missing'\n" +
		"globalThis.copied = copied + ' ' + JSON.stringify(props)\n" +
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
	got, err := vm.Eval(`globalThis.copied`, slowjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got != `true {"a":1,"b":[2]}` {
		t.Errorf("copied = %v", got)
	}
}
