package ssr

import (
	"context"
	"os"
	"testing"
)

func TestHostBase64(t *testing.T) {
	p := hostPool(t)
	cases := []struct{ name, expr, want string }{
		{"bytes", `btoa(String.fromCharCode(...Array.from({length:256}, (_,i)=>i))) === new Uint8Array(Array.from({length:256}, (_,i)=>i)).toBase64()`, "true"},
		{"round trip", `atob(btoa('\x00\x80\xff')).split('').map(c=>c.charCodeAt(0)).join()`, "0,128,255"},
		{"whitespace", `atob(' \tY\nQ\f=\r= ')`, "a"},
		{"no padding", `atob('YQ') + atob('YWI')`, "aab"},
		{"discarded bits", `atob('YR==') + atob('YWJ=')`, "aab"},
		{"empty", `atob('') + btoa('')`, ""},
		{"conversion", `btoa(null) + ',' + btoa(undefined)`, "bnVsbA==,dW5kZWZpbmVk"},
		{"invalid inputs", `['A','YQ=','Y===','YQ===','Y=Q=','YQ==x','-_==','YQ\v==','YQ\u00a0==','你好'].every(s=>{try{atob(s);return false}catch(e){return e instanceof DOMException && e.name==='InvalidCharacterError' && e.code===5}})`, "true"},
		{"non bytes", `['\u0100','🙂','\ud800'].every(s=>{try{btoa(s);return false}catch(e){return e instanceof DOMException && e.name==='InvalidCharacterError'}})`, "true"},
		{"required arguments", `[atob,btoa].every(f=>{try{f();return false}catch(e){return e instanceof TypeError}})`, "true"},
		{"symbols", `[atob,btoa].every(f=>{try{f(Symbol());return false}catch(e){return e instanceof TypeError}})`, "true"},
		{"conversion error", `(()=>{let error=new Error('sentinel');try{atob({toString(){throw error}})}catch(e){return e===error}})()`, "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderExprs(t, p, attempt(tc.expr))[0]; got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestHostDOMException(t *testing.T) {
	p := hostPool(t)
	for _, expr := range []string{
		`new DOMException() instanceof Error`,
		`new DOMException('', 'IndexSize').code === 0 && DOMException.URL_MISMATCH_ERR === 21`,
		`new DOMException().name === 'Error' && new DOMException().message === '' && new DOMException().code === 0`,
		`new DOMException('bad','DataCloneError').toString() === 'DataCloneError: bad'`,
		`new DOMException('bad','DataCloneError').code === 25 && DOMException.DATA_CLONE_ERR === 25`,
		`new DOMException('bad','QuotaExceededError').code === 22 && DOMException.prototype.QUOTA_EXCEEDED_ERR === 22`,
		`Object.prototype.toString.call(new DOMException()) === '[object DOMException]'`,
		`(()=>{try{DOMException()}catch(e){return e instanceof TypeError}})()`,
		`(()=>{try{new DOMException(Symbol())}catch(e){return e instanceof TypeError}})()`,
	} {
		if got := renderExprs(t, p, attempt(expr))[0]; got != "true" {
			t.Errorf("%s = %s", expr, got)
		}
	}
}

// Both successful renders use the same single-VM pool. Nothing restores the
// application's mutations between requests.
func checkHostAfterMutation(t *testing.T, setup, check string) {
	t.Helper()
	host, err := os.ReadFile("../../js/runtime/host.js")
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPool(t, string(host)+`
let renders=0;globalThis.startRender=()=>{
 if (++renders===1) { `+setup+`;globalThis.renderResult='<p>mutated</p>' }
 else { globalThis.renderResult=(`+check+`)?'<p>ok</p>':'<p>failed</p>' }
 }`, testConfig(1))
	for _, want := range []string{"<p>mutated</p>", "<p>ok</p>"} {
		got, err := p.Render(context.Background(), Input{BuildID: "b1", URL: "/"})
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("render = %q, want %q", got, want)
		}
	}
}

func TestHostBase64CapturedBuiltins(t *testing.T) {
	checkHostAfterMutation(t, `
 globalThis.U8=Uint8Array;
 Uint8Array.fromBase64=()=>new U8(1);
 Uint8Array.prototype.toBase64=()=> 'wrong';
 String.fromCharCode=()=> 'wrong';
 String.prototype.charCodeAt=()=>0;
 String.prototype.replace=()=> 'wrong';
 RegExp.prototype.exec=()=>null;
 globalThis.Uint8Array=function(){throw 'constructor'};
 Function.prototype.call=function(){throw 'call'};
 Reflect.apply=function(){throw 'apply'};
 `, `atob(' YQ== ')==='a' && btoa('a')==='YQ==' && U8.fromBase64('anything')[0]===0`)
}
