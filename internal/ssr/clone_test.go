package ssr

import (
	"os"
	"testing"

	"github.com/liustack/quickjs"
)

func TestHostStructuredClone(t *testing.T) {
	p := hostPool(t)
	cases := []struct{ name, expr string }{
		{"resizable buffer", `(()=>{const b=new ArrayBuffer(8,{maxByteLength:32});new Uint8Array(b)[0]=7;const c=structuredClone(b);c.resize(16);return c.resizable&&c.maxByteLength===32&&c.byteLength===16&&new Uint8Array(c)[0]===7&&b.byteLength===8})()`},
		{"resizable views", `(()=>{const b=new ArrayBuffer(8,{maxByteLength:32});return [new Uint16Array(b,2),new Uint16Array(b,2,3),new DataView(b,2),new DataView(b,2,6)].every(x=>{try{structuredClone(x);return false}catch(e){return e.name==='DataCloneError'&&e.message.includes('resizable')&&b.byteLength===8}})})()`},
		{"transparent proxies", `(()=>{return [{x:{n:1}},[{n:1}]].every(target=>{const traps=new Set();const p=new Proxy(target,{ownKeys(t){traps.add('keys');return Reflect.ownKeys(t)},getOwnPropertyDescriptor(t,k){traps.add('descriptor');return Reflect.getOwnPropertyDescriptor(t,k)},get(t,k,r){traps.add('get');return Reflect.get(t,k,r)}});target.self=p;const c=structuredClone(p);return c!==p&&c.self===c&&Array.isArray(c)===Array.isArray(target)&&Object.getPrototypeOf(c)===(Array.isArray(target)?Array.prototype:Object.prototype)&&traps.size===3&&(Array.isArray(c)?c[0]!==target[0]&&c[0].n===1:c.x!==target.x&&c.x.n===1)})})()`},
		{"detached and out of bounds", `(()=>{const b=new ArrayBuffer(8);const a=new Uint8Array(b);b.transfer();const r=new ArrayBuffer(8,{maxByteLength:16});const oob=new Uint16Array(r,4,2);const d=new DataView(r,4,2);r.resize(2);return [b,a,oob,d,new SharedArrayBuffer(8)].every(x=>{try{structuredClone(x);return false}catch(e){return e.name==='DataCloneError'}})})()`},
		{"Intl objects", `[new Intl.NumberFormat(),new Intl.DateTimeFormat(),new Intl.PluralRules()].every(x=>{try{structuredClone(x);return false}catch(e){return e.name==='DataCloneError'}})`},
		{"error descriptors", `(()=>{let a=new Error;Object.defineProperty(a,'message',{get(){throw 1}});Object.defineProperty(a,'cause',{get(){throw 2}});a.name={toString(){throw 3}};return structuredClone(a).name==='Error'})()`},
		{"primitives", `[undefined,null,true,false,0,-0,NaN,Infinity,123n,'\ud800'].every(x=>Object.is(structuredClone(x),x))`},
		{"cycles", `(()=>{const a={};a.self=a;a.list=[a,a];const b=structuredClone(a);return b!==a&&b.self===b&&b.list[0]===b&&b.list[1]===b})()`},
		{"sparse array", `(()=>{let a=Array(4);a[2]=3;a.extra={v:1};let b=structuredClone(a);return b.length===4&&!(0 in b)&&b[2]===3&&b.extra!==a.extra&&b.extra.v===1})()`},
		{"properties", `(()=>{let n=0;const a=Object.create(null);Object.defineProperty(a,'x',{get(){n++;return {v:1}},enumerable:true});a[Symbol()]=()=>{};Object.defineProperty(a,'hidden',{value:()=>{}});Object.defineProperty(a,'__proto__',{value:{safe:true},enumerable:true});const b=structuredClone(a);return n===1&&b.x.v===1&&Object.getPrototypeOf(b)===Object.prototype&&Object.hasOwn(b,'__proto__')&&b.__proto__.safe&&Object.getOwnPropertyDescriptor(b,'x').writable})()`},
		{"class", `(()=>{class A{method(){}};let a=new A;a.x=1;return Object.getPrototypeOf(structuredClone(a))===Object.prototype})()`},
		{"proxy prototype", `(()=>{const a=Object.create(new Proxy({},{getPrototypeOf(){throw new Error('trap')}}));a.x=1;return structuredClone(a).x===1})()`},
		{"shadowed builtins", `(()=>{const d=new Date(123);d.getTime=()=>{throw 1};const r=/x/gi;Object.defineProperties(r,{source:{get(){throw 2}},flags:{get(){throw 3}}});const a=new Uint8Array([1,2]);Object.defineProperties(a,{constructor:{value:()=>{throw 4}},buffer:{get(){throw 5}},byteOffset:{value:99},length:{value:99}});const c=structuredClone({d,r,a});return +c.d===123&&c.r.source==='x'&&c.r.flags==='gi'&&c.a.length===2&&c.a[1]===2})()`},
		{"spoofed tags", `['Date','RegExp','Map','Set','Error','ArrayBuffer','DataView','Uint8Array','Number','String','Boolean','BigInt','Symbol','Promise'].every(tag=>{const a={x:1,[Symbol.toStringTag]:tag};const b=structuredClone(a);return Object.getPrototypeOf(b)===Object.prototype&&b.x===1})`},
		{"tag getter", `(()=>{const a={x:1};Object.defineProperty(a,Symbol.toStringTag,{get(){throw new Error('tag must not be read')}});return structuredClone(a).x===1})()`},
		{"real types with false tags", `(()=>{const date=new Date(123),map=new Map([[1,2]]),error=new TypeError('bad'),array=new Uint8Array([3]);for(const x of [date,map,error,array])Object.defineProperty(x,Symbol.toStringTag,{value:'Set'});const b=structuredClone({date,map,error,array});return +b.date===123&&b.map.get(1)===2&&b.error instanceof TypeError&&b.error.message==='bad'&&b.array instanceof Uint8Array&&b.array[0]===3})()`},
		{"builtin prototypes", `(()=>{const c=structuredClone({r:RegExp.prototype,e:Error.prototype});return Object.getPrototypeOf(c.r)===Object.prototype&&Object.keys(c.r).length===0&&Object.getPrototypeOf(c.e)===Object.prototype&&Object.keys(c.e).length===0})()`},
		{"brands without prototypes", `(()=>{const values=[new Date(123),/x/gi,new Map([[1,2]]),new Set([3]),new TypeError('bad'),new ArrayBuffer(8),new Uint8Array([4]),new DataView(new ArrayBuffer(4)),Object(5),Object('hi'),Object(true),Object(6n)];for(const value of values)Object.setPrototypeOf(value,null);const c=structuredClone(values);return +c[0]===123&&c[1].source==='x'&&c[2].get(1)===2&&c[3].has(3)&&Error.isError(c[4])&&c[4].message==='bad'&&c[5].byteLength===8&&c[6][0]===4&&c[7].byteLength===4&&c[8].valueOf()===5&&c[9].valueOf()==='hi'&&c[10].valueOf()===true&&c[11].valueOf()===6n})()`},
		{"proxy trap error", `(()=>{const error=new Error('trap');const p=new Proxy({x:1},{get(){throw error}});try{structuredClone(p)}catch(e){return e===error}})()`},
		{"shared buffer views", `[new Uint8Array(new SharedArrayBuffer(8)),new DataView(new SharedArrayBuffer(8))].every(x=>{try{structuredClone(x);return false}catch(e){return e.name==='DataCloneError'}})`},
		{"tag spoof", `(()=>{const a={x:1,[Symbol.toStringTag]:'Date'};return structuredClone(a).x===1})()`},
		{"date", `(()=>{let a=new Date(123);a.x=()=>{};let b=structuredClone(a);return b instanceof Date&&+b===123&& !('x' in b)&&Number.isNaN(+structuredClone(new Date(NaN)))})()`},
		{"regexp", `(()=>{let a=/x/gi;a.lastIndex=8;const b=structuredClone(a);return b instanceof RegExp&&b.source==='x'&&b.flags==='gi'&&b.lastIndex===0})()`},
		{"map and set", `(()=>{let a=new Map;let s=new Set;a.set(a,s);s.add(a);let b=structuredClone(a);return b instanceof Map&&b.get(b) instanceof Set&&b.get(b).has(b)})()`},
		{"map snapshot", `(()=>{const m=new Map;const key={get x(){m.set('late',1);return 1}};m.set(key,1);return structuredClone(m).size===1})()`},
		{"buffer views", `(()=>{let b=new ArrayBuffer(32);let a=new Uint16Array(b,4,4);a[0]=123;let d=new DataView(b,2,12);let c=structuredClone({b,a,d,again:a});c.a[1]=456;return c.b!==b&&c.a.buffer===c.b&&c.d.buffer===c.b&&c.a===c.again&&c.a.byteOffset===4&&c.a.length===4&&c.d.byteOffset===2&&c.d.byteLength===12&&c.a[0]===123&&a[1]===0})()`},
		{"typed arrays", `[Int8Array,Uint8Array,Uint8ClampedArray,Int16Array,Uint16Array,Int32Array,Uint32Array,BigInt64Array,BigUint64Array,Float16Array,Float32Array,Float64Array].every(C=>{const a=new C(2);const b=structuredClone(a);return b instanceof C&&b.length===2&&b.buffer!==a.buffer})`},
		{"boxed", `[Object(true),Object(123),Object(NaN),Object(-0),Object(42n),Object('hi')].every(a=>{a.extra=()=>{};const b=structuredClone(a);return a!==b&&Object.is(a.valueOf(),b.valueOf())&&!('extra' in b)})`},
		{"errors", `[Error,EvalError,RangeError,ReferenceError,SyntaxError,TypeError,URIError].every(C=>{let a=new C('bad');a.cause=a;a.extra=()=>{};let b=structuredClone(a);return b instanceof C&&b.message==='bad'&&b.cause===b&&b.stack===a.stack&&!('extra' in b)})`},
		{"error name", `(()=>{let a=new Error('bad');a.name='Custom';return structuredClone(a).name==='Error'})()`},
		{"aggregate", `(()=>{let a=new AggregateError([],'bad');a.errors=[a];let b=structuredClone(a);return b instanceof AggregateError&&b.errors[0]===b})()`},
		{"dom exception", `(()=>{let a=new DOMException('bad','DataCloneError');let b=structuredClone(a);return b instanceof DOMException&&b.message==='bad'&&b.name==='DataCloneError'&&b.code===25})()`},
		{"invalid", `[()=>{},Symbol(),Object(Symbol()),new WeakMap,new WeakSet,Promise.resolve(),new WeakRef({}),new FinalizationRegistry(()=>{}),new Proxy(new Date,{}),new URL('https://example.com'),new URLSearchParams('a=b'),new Headers,new Response,new AbortController, new AbortController().signal,(function*(){})()].every(x=>{try{structuredClone(x);return false}catch(e){return e instanceof DOMException&&e.name==='DataCloneError'&&e.code===25}})`},
		{"nested invalid", `[ {x:()=>{}},[Symbol()],new Map([[1,()=>{}]]),new Set([Symbol()]),new Error('bad',{cause:()=>{}})].every(x=>{try{structuredClone(x);return false}catch(e){return e.name==='DataCloneError'}})`},
		{"revoked proxy", `(()=>{const p=Proxy.revocable({},{});p.revoke();try{structuredClone(p.proxy)}catch(e){return e.name==='DataCloneError'}})()`},
		{"getter error", `(()=>{let err=new Error('read');try{structuredClone({get x(){throw err}})}catch(e){return e===err}})()`},
		{"transfer", `(()=>{let a=new ArrayBuffer(8);try{structuredClone(a,{transfer:[a]})}catch(e){return e.name==='DataCloneError'&&e.message.includes('transfer')&&a.byteLength===8}})()`},
		{"empty transfer", `structuredClone({x:1},{transfer:[]}).x===1`},
		{"required argument", `(()=>{try{structuredClone()}catch(e){return e instanceof TypeError}})()`},
	}
	for _, expression := range []string{"new Date()", "/x/", "new Map()", "new Set()", "new Error('bad')", "new ArrayBuffer(8)", "new DataView(new ArrayBuffer(8))", "new Uint8Array(8)", "Object(1)", "Object('x')", "Object(true)", "Object(1n)"} {
		cases = append(cases, struct{ name, expr string }{"builtin proxy " + expression, `(()=>{try{structuredClone(new Proxy(` + expression + `,{}));return false}catch(e){return e.name==='DataCloneError'}})()`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderExprs(t, p, attempt(tc.expr))[0]; got != "true" {
				t.Fatalf("%s = %s", tc.expr, got)
			}
		})
	}
}

// The clone implementation must work with the JS host alone, without a Go
// callback that inspects engine values or their private representation.
func TestHostStructuredCloneWithoutBridge(t *testing.T) {
	host, err := os.ReadFile("../../js/runtime/host.js")
	if err != nil {
		t.Fatal(err)
	}
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if _, err = vm.Eval(string(host), quickjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	result, err := vm.Eval(`(()=>{const a={n:1};a.self=a;const b=structuredClone(a);return b!==a&&b.self===b&&b.n===1})()`, quickjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if result != true {
		t.Fatalf("clone without a host bridge = %v", result)
	}
}

func TestHostStructuredCloneCapturedBuiltins(t *testing.T) {
	cases := []struct{ name, setup, check string }{
		{"date and boxes", `globalThis.probe={date:new Date(123),box:Object(7),read:Number.prototype.valueOf,apply:Reflect.apply};Date.prototype.getTime=()=>42;Number.prototype.valueOf=()=>42;globalThis.Date=function(){throw 'constructor'};`, `(()=>{const c=structuredClone({date:probe.date,box:probe.box});return +c.date===123&&probe.apply(probe.read,c.box,[])===7&&probe.date.getTime()===42})()`},
		{"array iterator", `globalThis.probe=new AggregateError([],'bad');probe.errors=[probe];Object.getPrototypeOf([][Symbol.iterator]()).next=()=>{throw 'iterator'};globalThis.Array=function(){throw 'constructor'};`, `(()=>{const c=structuredClone(probe);return c.message==='bad'&&c.errors[0]===c})()`},
		{"collections", `globalThis.probe={map:new Map([[1,{n:2}]]),set:new Set([3]),get:Map.prototype.get,has:Set.prototype.has,apply:Reflect.apply};Object.getPrototypeOf(probe.map.entries()).next=()=>({done:true});Object.getPrototypeOf(probe.set.values()).next=()=>({done:true});Map.prototype.forEach=()=>{};Set.prototype.forEach=()=>{};Map.prototype.set=()=>{};Set.prototype.add=()=>{};Map.prototype.has=()=>false;Map.prototype.get=()=>undefined;Set.prototype.has=()=>false;`, `(()=>{const c=structuredClone({map:probe.map,set:probe.set});return probe.apply(probe.get,c.map,[1]).n===2&&probe.apply(probe.has,c.set,[3])})()`},
		{"getters and buffers", `globalThis.probe={date:new Date(123),r:/x/gi,b:new Uint8Array([7,8]),d:new DataView(new ArrayBuffer(4)),box:Object(7),valueOf:Number.prototype.valueOf,global:Object.getOwnPropertyDescriptor(RegExp.prototype,'global').get,length:Object.getOwnPropertyDescriptor(DataView.prototype,'byteLength').get,apply:Reflect.apply};const ta=Object.getPrototypeOf(Uint8Array.prototype);Object.defineProperty(ta,'buffer',{get(){throw 'buffer'}});Object.defineProperty(ArrayBuffer.prototype,'resizable',{get(){throw 'resizable'}});Object.defineProperty(RegExp.prototype,'global',{get(){return false}});Object.defineProperty(DataView.prototype,'byteLength',{get(){throw 'length'}});Uint8Array.prototype.set=()=>{};Number.prototype.valueOf=()=>42;Object.getOwnPropertyDescriptor=()=>{throw 'descriptor'};Object.defineProperty=()=>{throw 'define'};Object.keys=()=>[];Object.hasOwn=()=>false;Object.getPrototypeOf=()=>null;Error.isError=()=>false;Function.prototype.call=function(){throw 'call'};Reflect.apply=()=>{throw 'apply'};`, `(()=>{const c=structuredClone({date:probe.date,r:probe.r,b:probe.b,d:probe.d,box:probe.box});return +c.date===123&&c.r.source==='x'&&c.r.flags.includes('i')&&probe.apply(probe.global,c.r,[])===true&&probe.apply(probe.length,c.d,[])===4&&c.b[0]===7&&c.b[1]===8&&probe.apply(probe.valueOf,c.box,[])===7})()`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkHostAfterMutation(t, tc.setup, tc.check) })
	}
}

func TestHostStructuredCloneIterators(t *testing.T) {
	p := hostPool(t)
	for _, expr := range []string{
		`[1,2].values()`, `new Map([[1,2]]).entries()`, `new Set([1]).values()`, `'ab'[Symbol.iterator]()`, `'ab'.matchAll(/./g)`,
		`Iterator.from({next(){throw new Error('must not advance')}})`,
		`Iterator.from([1]).map(x=>x)`, `Iterator.from([1]).filter(x=>true)`, `Iterator.from([1]).take(1)`,
		`Iterator.concat([1],[2])`, `Iterator.from([1]).drop(0)`, `Iterator.from([1]).flatMap(x=>[x])`,
	} {
		t.Run(expr, func(t *testing.T) {
			check := `(()=>{const it=` + expr + `;Object.defineProperty(it,'next',{value(){throw new Error('must not advance')}});return [it,{it}].every(value=>{try{structuredClone(value);return false}catch(e){return e.name==='DataCloneError'}})})()`
			if got := renderExprs(t, p, attempt(check))[0]; got != "true" {
				t.Fatalf("iterator clone: %s", got)
			}
		})
	}
}

// Some unsupported types can only be recognized by their prototype in JS.
// Document and preserve that limit rather than promising browser equivalence.
func TestHostStructuredClonePrototypeLimits(t *testing.T) {
	p := hostPool(t)
	for _, expr := range []string{`Promise.resolve(1)`, `new FinalizationRegistry(()=>{})`} {
		t.Run(expr, func(t *testing.T) {
			check := `(()=>{return [null,{}].every(proto=>{const value=` + expr + `;try{structuredClone(value);return false}catch(e){if(e.name!=='DataCloneError')return false}Object.setPrototypeOf(value,proto);value.marker=1;const copy=structuredClone(value);return copy!==value&&Object.getPrototypeOf(copy)===Object.prototype&&copy.marker===1&&Object.keys(copy).length===1})})()`
			if got := renderExprs(t, p, attempt(check))[0]; got != "true" {
				t.Fatalf("prototype recognition limit: %s", got)
			}
		})
	}
}
