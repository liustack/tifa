package ssr

import "testing"

func TestHostCrypto(t *testing.T) {
	p := hostPool(t)
	cases := []struct{ name, expr string }{
		{"detached view", `(()=>{let b=new ArrayBuffer(4);let a=new Uint8Array(b);b.transfer();return crypto.getRandomValues(a)===a})()`},
		{"integer views", `[Int8Array,Uint8Array,Uint8ClampedArray,Int16Array,Uint16Array,Int32Array,Uint32Array,BigInt64Array,BigUint64Array].every(C=>{let a=new C(16);return crypto.getRandomValues(a)===a && new Uint8Array(a.buffer).some(x=>x!==0)})`},
		{"offset", `(()=>{let a=new Uint8Array(40);a.fill(42);let b=new Uint32Array(a.buffer,8,4);crypto.getRandomValues(b);return a.slice(0,8).every(x=>x===42)&&a.slice(24).every(x=>x===42)&&a.slice(8,24).some(x=>x!==42)})()`},
		{"quota", `(()=>{try{crypto.getRandomValues(new Uint32Array(16385))}catch(e){return e instanceof DOMException && e.name==='QuotaExceededError' && e.code===22}})()`},
		{"limit and empty", `crypto.getRandomValues(new Uint8Array(65536)).length===65536 && crypto.getRandomValues(new Uint8Array(0)).length===0`},
		{"float and data view", `[new Float32Array(1),new Float64Array(1),new DataView(new ArrayBuffer(4))].every(a=>{try{crypto.getRandomValues(a)}catch(e){return e instanceof DOMException && e.name==='TypeMismatchError' && e.code===17}})`},
		{"not views", `[null,undefined,{},[],new ArrayBuffer(4),new Proxy(new Uint8Array(4),{})].every(a=>{try{crypto.getRandomValues(a)}catch(e){return e instanceof TypeError}})`},
		{"uuid", `(()=>{const ids=Array.from({length:64},()=>crypto.randomUUID());return ids.every(id=>/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(id))&&new Set(ids).size===64})()`},
		{"no subtle", `!('subtle' in crypto)`},
		{"fresh bytes", `crypto.getRandomValues(new Uint8Array(32)).toHex()!==crypto.getRandomValues(new Uint8Array(32)).toHex()`},
		{"receiver", `(()=>{try{crypto.getRandomValues.call({},new Uint8Array(1))}catch(e){return e instanceof TypeError}})()`},
		{"shadowed properties", `(()=>{let a=new Uint8Array(16);Object.defineProperties(a,{byteLength:{value:0},buffer:{value:null},byteOffset:{value:300},[Symbol.toStringTag]:{value:'Float32Array'}});return crypto.getRandomValues(a)===a && a.some(x=>x!==0)})()`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderExprs(t, p, attempt(tc.expr))[0]; got != "true" {
				t.Fatalf("%s = %s", tc.expr, got)
			}
		})
	}
}

func TestHostCryptoCapturedBuiltins(t *testing.T) {
	checkHostAfterMutation(t, `
 globalThis.randomProbe={a:new Uint8Array(32),b:new Uint8Array(32),U8:Uint8Array};
 Uint8Array.fromBase64=()=>new randomProbe.U8(16);
 Uint8Array.prototype.toHex=()=> '00000000000000000000000000000000';
 Uint8Array.prototype.set=()=>{};
 ArrayBuffer.isView=()=>false;
 String.prototype.slice=()=> 'wrong';
 RegExp.prototype.test=()=>false;
 globalThis.Uint8Array=function(){throw 'constructor'};
 Function.prototype.call=function(){throw 'call'};
 Reflect.apply=function(){throw 'apply'};
 `, `(()=>{const a=randomProbe.a,b=randomProbe.b;const same=crypto.getRandomValues(a)===a&&crypto.getRandomValues(b)===b;let differs=false;for(let i=0;i<a.length;i++)if(a[i]!==b[i])differs=true;const x=crypto.randomUUID(),y=crypto.randomUUID();return same&&differs&&x!==y&&x.length===36&&x[14]==='4'&&'89ab'.includes(x[19])&&ArrayBuffer.isView(a)===false})()`)
}

func TestHostCryptoBufferRestrictions(t *testing.T) {
	p := hostPool(t)
	for _, buffer := range []string{`new SharedArrayBuffer(70000)`, `new ArrayBuffer(70000,{maxByteLength:80000})`} {
		for _, view := range []string{`new Uint8Array(b)`, `new Uint8Array(b,0,2)`, `new Uint8Array(b,0,0)`, `new Float32Array(b,0,1)`, `new DataView(b)`, `new DataView(b,0,0)`} {
			t.Run(buffer+view, func(t *testing.T) {
				check := `(()=>{const b=` + buffer + `;const a=` + view + `;try{crypto.getRandomValues(a);return false}catch(e){return e instanceof TypeError}})()`
				if got := renderExprs(t, p, attempt(check))[0]; got != "true" {
					t.Fatalf("buffer conversion: %s", got)
				}
			})
		}
	}
}
