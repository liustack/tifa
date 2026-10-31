package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// clsxPage calls clsx with every kind of argument, and records what each call gives, what it reads
// of the arguments and how many times the clsx it had before the plugin was called with apply, which
// the plugin's function does for a call it does not answer itself. The arguments are written as
// functions, so that every case builds its own objects.
const clsxPage = `import './host'
import clsx from 'clsx'

let applies = 0
const apply = Function.prototype.apply
Function.prototype.apply = function (this: Function, ...args: unknown[]) {
  applies++
  return Reflect.apply(apply, this, args)
}

class Named {
  toString() {
    return 'toString'
  }
}
const reads: string[] = []
const spy = () =>
  new Proxy(
    { shown: 1 },
    {
      get(target, key, receiver) {
        reads.push(String(key))
        return Reflect.get(target, key, receiver)
      },
      ownKeys(target) {
        reads.push('ownKeys')
        return Reflect.ownKeys(target)
      },
    },
  )

const cases: [string, () => unknown[]][] = [
  ['none', () => []],
  ['one', () => ['a']],
  ['two', () => ['a', 'b']],
  ['three', () => ['a', 'b', 'c']],
  ['four', () => ['a', 'b', 'c', 'd']],
  ['five', () => ['a', 'b', 'c', 'd', 'e']],
  ['six', () => ['a', 'b', 'c', 'd', 'e', 'f']],
  ['six, objects in the fifth and sixth places', () => ['a', 'b', 'c', 'd', { e: 1 }, ['f', { g: 1 }]]],
  ['seven', () => ['a', 'b', 'c', 'd', 'e', 'f', 'g']],
  ['seven, the last undefined', () => ['a', 'b', 'c', 'd', 'e', 'f', undefined]],
  ['eight, an object in the seventh place', () => ['a', undefined, undefined, undefined, undefined, undefined, { z: 1 }, 'h']],
  ['five, the last undefined', () => ['a', 'b', 'c', 'd', undefined]],
  ['six, the last two undefined', () => ['a', undefined, undefined, undefined, undefined, undefined]],
  ['cva: a base, two arrays and two classes', () => ['base', ['v1', undefined, 'v2'], [null, 'c1'], undefined, 'extra']],
  ['a string after four falsy values', () => [undefined, null, false, '', 'late']],
  ['empty strings', () => ['', '']],
  ['empty, then a string', () => ['', 'a']],
  ['a string, then empty', () => ['a', '']],
  ['falsy values between strings', () => ['a', undefined, 'b', null, 'c', false]],
  ['falsy values between strings, four', () => ['a', undefined, 'b', null]],
  ['falsy values between strings, three', () => [undefined, 'a', null, 'b']],
  ['undefined', () => [undefined]],
  ['null', () => [null]],
  ['false', () => [false]],
  ['zero', () => [0]],
  ['minus zero', () => [-0]],
  ['NaN', () => [NaN, 'a']],
  ['zero and a string', () => [0, 'a']],
  ['a string and zero', () => ['a', 0]],
  ['a number', () => [1]],
  ['a string and a number', () => ['a', 2]],
  ['a number and a string', () => [2.5, 'a']],
  ['a number in the middle', () => ['a', 7, 'b']],
  ['a negative number', () => [-1]],
  ['a big number', () => [1e21, 'a']],
  ['true', () => [true]],
  ['true and a string', () => [true, 'a']],
  ['a string and true', () => ['a', true]],
  ['strings that look false', () => ['0', 'false', 'null', 'undefined']],
  ['spaces in strings', () => [' a ', 'b  c', ' ']],
  ['a space only', () => [' ']],
  ['a newline', () => ['a\nb', 'c']],
  ['a long string', () => ['x'.repeat(5000), 'y']],
  ['non-BMP and Chinese text', () => ['订单 🙂', '\u{1F600}x', ' ']],
  ['an object', () => [{ a: 1, b: 0, c: true, d: '', e: 'yes' }]],
  ['an empty object', () => [{}]],
  ['a string and an object', () => ['a', { b: 1 }]],
  ['an object and a string', () => [{ b: 1 }, 'a']],
  ['an object in the middle', () => ['a', { b: 1 }, 'c']],
  ['an object in the fourth place', () => ['a', 'b', 'c', { d: 1 }]],
  ['an array', () => [['x', 'y']]],
  ['an empty array', () => [[]]],
  ['nested arrays and objects', () => ['a', ['x', ['y', { z: 1, w: 0 }], null], 'b']],
  ['an array with falsy values', () => [[0, '', null, undefined, false, 'k']]],
  ['a function', () => [() => 'f', 'a']],
  ['a string and a function', () => ['a', function () {}]],
  ['a symbol', () => [Symbol('s'), 'a']],
  ['a bigint', () => [10n, 'a']],
  ['a Date', () => ['a', new Date(0)]],
  ['a String object', () => [new String('boxed')]],
  ['a class instance with toString', () => [new Named(), 'a']],
  ['an object with a getter', () => [{ get a() { return 'got' } }]],
  ['an object that records what is read', () => ['a', spy()]],
  ['an object after a string, four arguments', () => ['a', 'b', 'c', spy()]],
  ['an object in the sixth place', () => ['a', 'b', 'c', 'd', 'e', spy()]],
  ['an object in the seventh place', () => ['a', 'b', 'c', 'd', 'e', 'f', spy()]],
  ['an object with a null prototype', () => [Object.assign(Object.create(null), { n: 1 })]],
  ['an arguments object', () => (function (..._: unknown[]) { return [arguments, 'a'] })('p', 'q')],
]

const out: any[] = []
for (const [name, args] of cases) {
  reads.length = 0
  applies = 0
  let result: string
  try {
    result = JSON.stringify(clsx(...(args() as [])))
  } catch (e) {
    result = 'throws ' + (e as Error).message
  }
  out.push({ name, result, applies, reads: reads.slice() })
}
globalThis.result = JSON.stringify({ out })
`

type clsxRecord struct {
	Name    string
	Result  string
	Applies int
	Reads   []string
}

type clsxResult struct {
	Out []clsxRecord
}

func clsxRun(t *testing.T, root string) (clsxResult, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"clsx.tsx": clsxPage})
	code, printed := buildPrinting(t, root, "clsx.tsx")
	var r clsxResult
	if err := json.Unmarshal([]byte(evalResult(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Out) < 68 {
		t.Fatalf("%d calls", len(r.Out))
	}
	return r, printed
}

// withClsx returns a copy of testdata/ssr whose clsx has the file dist/file changed by change.
func withClsx(t *testing.T, file string, change func(string) string) string {
	t.Helper()
	return withChangedPackage(t, "clsx", filepath.Join("dist", file), change)
}

// clsx joins class names, and runs for the className of nearly every component of MUI. It reads
// its arguments with the arguments object and calls a function for each one, a string among them.
// The tifa plugin does the same for a call of up to six arguments without the loop and without the
// call for a string, and hands a call of more arguments to the clsx it had. Every call gives the
// same string as the clsx the plugin does not know and reads what it reads, and goes to the old
// function exactly when it has more than six arguments. A clsx file of another form is left as it
// is, with a warning.
func TestSSRJoinsClassNamesOfUpToSixArgumentsWithoutALoop(t *testing.T) {
	changed, printed := clsxRun(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "clsx") {
		t.Errorf("the build prints:\n%s", printed)
	}
	plain, printed := clsxRun(t, withClsx(t, "clsx.mjs", func(source string) string { return source + "\n/* changed */" }))
	if strings.Count(printed, "clsx 2.1.1") != 1 || !strings.Contains(printed, "has a form the tifa plugin does not know") {
		t.Errorf("an unknown clsx.mjs: the build prints:\n%s", printed)
	}
	// The calls that go to the old function: the ones with more than six arguments.
	old := map[string]bool{
		"seven": true, "seven, the last undefined": true, "eight, an object in the seventh place": true,
		"an object in the seventh place": true,
	}
	if len(plain.Out) != len(changed.Out) {
		t.Fatalf("%d calls, want %d", len(changed.Out), len(plain.Out))
	}
	for i, p := range plain.Out {
		c := changed.Out[i]
		if c.Name != p.Name {
			t.Fatalf("call %d is %s, want %s", i, c.Name, p.Name)
		}
		if c.Result != p.Result {
			t.Errorf("%s: clsx gives %s, want %s", c.Name, c.Result, p.Result)
		}
		if strings.Join(c.Reads, ",") != strings.Join(p.Reads, ",") {
			t.Errorf("%s: clsx reads %v, want %v", c.Name, c.Reads, p.Reads)
		}
		if p.Applies != 0 {
			t.Errorf("%s: the clsx the plugin does not know is called with apply %d times", p.Name, p.Applies)
		}
		want := 0
		if old[c.Name] {
			want = 1
		}
		if c.Applies != want {
			t.Errorf("%s: the call goes to the old function %d times, want %d", c.Name, c.Applies, want)
		}
	}
}
