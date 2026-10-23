package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/tifa/internal/ssr"
)

// Build the actual packages with tifa's Vite SSR plugin and checked-in host, then
// use the production Compile/Load path. In particular, serialize-javascript calls
// crypto while its module loads, before a component can install a fallback.
func TestSSRWebAPILibraries(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	host, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"host.js": string(host), "instrument.js": `
import './host.js'
globalThis.calls = {random:0, uuid:0, clone:0}
for (const [object,key,counter] of [[globalThis.crypto,'getRandomValues','random'],[globalThis.crypto,'randomUUID','uuid'],[globalThis,'structuredClone','clone']]) {
  if (!object || typeof object[key] !== 'function') continue
  const original = object[key]
  object[key] = function(...args) { calls[counter]++; return original.apply(this,args) }
}
Math.random = () => { throw new Error('Unexpected insecure random fallback') }
`})
	cases := []struct{ name, source, want string }{
		{"zod base64 validation", `import {z} from 'zod'; globalThis.result=String(z.base64().safeParse('YQ==').success);`, "true"},
		{"zod base64 encoding", `import {util} from 'zod/v4/core'; globalThis.result=util.uint8ArrayToBase64(new Uint8Array([97]));`, "YQ=="},
		{"uuid", `import {v4} from 'uuid';const id=v4();const fallback=v4({});globalThis.result=String(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(id)&&id!==fallback&&calls.uuid===1&&calls.random===1);`, "true"},
		{"serialize load", `import serialize from 'serialize-javascript';globalThis.result=String(calls.random>0&&serialize({x:/x/}).includes('RegExp'));`, "true"},
		{"field array", `import {useForm,useFieldArray} from 'react-hook-form';function Page(){const {control}=useForm({defaultValues:{rows:[{name:'A'},{name:'B'}]}});const {fields}=useFieldArray({control,name:'rows'});return <p>{fields.map(f=>f.name).join(',')}</p>}globalThis.result=renderToString(<Page/>)+','+calls.uuid;`, "<p>A,B</p>,2"},
		{"Ajv resolver", `import {ajvResolver} from '@hookform/resolvers/ajv';const input={count:'2'};const resolver=ajvResolver({type:'object',properties:{count:{type:'integer'},label:{type:'string',default:'added'}},required:['count']},{coerceTypes:true,useDefaults:true});resolver(input,{}, {fields:{},shouldUseNativeValidation:false}).then(r=>{globalThis.result=String(r.values.count===2&&r.values.label==='added'&&input.count==='2'&&!('label' in input)&&calls.clone===1)},e=>{globalThis.result=e.name+': '+e.message});`, "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeTree(t, root, map[string]string{"webapi.tsx": "import './instrument.js'\nimport {renderToString} from 'react-dom/server.edge'\n" + tc.source})
			code, _ := buildScript(t, root, "webapi.tsx")
			bytecode, err := ssr.Compile(code, 128<<20, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			vm, err := ssr.Load(bytecode, 128<<20, 0, 5*time.Second, false)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			if _, err = vm.ExecutePendingJobs(); err != nil {
				t.Fatal(err)
			}
			got, err := vm.Eval("globalThis.result", slowjs.EvalGlobal)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
			if strings.Contains(code, "Unexpected insecure random fallback") == false {
				t.Fatal("instrumentation missing from bundle")
			}
		})
	}
}
