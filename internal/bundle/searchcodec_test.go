package bundle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/core"
)

// queryCase is a line of internal/core/testdata/query-values.txt, the cases that the Go decoder, the
// codec of the browser in Node and the codec in beastjs must answer alike.
type queryCase struct {
	Kind  string `json:"kind"`
	Input string `json:"input"`
	Want  string `json:"want"`
	// Canon is the shortest text of a number, "-" for a string, a bool and a value that is not valid.
	Canon string `json:"canon"`
}

func readQueryCases(t *testing.T) []queryCase {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "core", "testdata", "query-values.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var cases []queryCase
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 {
			t.Fatalf("a line of the table needs 5 columns: %q", line)
		}
		var input string
		if err := json.Unmarshal([]byte(cols[1]), &input); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		kind := cols[0]
		if kind == "float" {
			kind = "float64"
		}
		want, canon := "x", "-"
		if cols[2] == "ok" {
			want = cols[3]
			if kind != "string" && kind != "bool" {
				if err := json.Unmarshal([]byte(cols[4]), &canon); err != nil {
					t.Fatalf("%q: %v", line, err)
				}
			}
		}
		cases = append(cases, queryCase{kind, input, want, canon})
	}
	cases = append(cases, randomQueryCases()...)
	return append(cases, spelledQueryCases()...)
}

// randomQueryCases are numbers written the one valid way, which every codec must read as the number
// Go wrote: whole numbers up to 2^53-1 and floats of up to 15 significant digits.
func randomQueryCases() []queryCase {
	rng := rand.New(rand.NewPCG(1, 2))
	var cases []queryCase
	for range 20000 {
		n := rng.Int64N(1<<53-1) - rng.Int64N(1<<53-1)
		text := strconv.FormatInt(n, 10)
		cases = append(cases, queryCase{"int", text, fmt.Sprintf("%016x", math.Float64bits(float64(n))), text})
	}
	for len(cases) < 40000 {
		digits := 1 + rng.IntN(15)
		x := rng.Float64() * math.Pow(10, float64(rng.IntN(20)-6))
		x, _ = strconv.ParseFloat(strconv.FormatFloat(x, 'g', digits, 64), 64)
		if x < 1e-6 || x >= 1e21 {
			continue
		}
		text := strconv.FormatFloat(x, 'f', -1, 64)
		cases = append(cases, queryCase{"float64", text, fmt.Sprintf("%016x", math.Float64bits(x)), text})
	}
	return cases
}

// spelledQueryCases are numbers written in the many ways a person or a number input writes them
// (leading zeros, a sign, a trailing point or zeros, an exponent, up to 30 digits), and texts that are
// not numbers. Go says what each reads as, and every codec must say the same: that is the number, and
// the shortest text of it. It is what makes the table above enough: the table holds the rules, this
// holds the volume.
func spelledQueryCases() []queryCase {
	rng := rand.New(rand.NewPCG(7, 8))
	digits := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte('0' + rng.IntN(10))
		}
		return string(b)
	}
	var cases []queryCase
	for range 60000 {
		text := digits(rng.IntN(8))
		if rng.IntN(3) == 0 {
			text = "0" + text
		}
		if text == "" {
			text = "0"
		}
		if rng.IntN(2) == 0 {
			text += "." + digits(rng.IntN(20))
		}
		if rng.IntN(5) == 0 {
			text = digits(rng.IntN(30)+1) + "." + digits(rng.IntN(6)+1)
		}
		if rng.IntN(6) == 0 {
			text += []string{"e", "E", "e+", "e-"}[rng.IntN(4)] + digits(1+rng.IntN(3))
		}
		if rng.IntN(5) == 0 {
			text = "-" + text
		} else if rng.IntN(8) == 0 {
			text = "+" + text
		}
		for _, whole := range []bool{false, true} {
			kind := "float64"
			if whole {
				kind = "int"
			}
			want, canon := "x", "-"
			if c := core.CanonicalNumber(text, whole); c != "" {
				n, _ := strconv.ParseFloat(c, 64)
				want, canon = fmt.Sprintf("%016x", math.Float64bits(n)), c
			}
			cases = append(cases, queryCase{kind, text, want, canon})
		}
	}
	return cases
}

// searchScenarios are what the codec does with a whole search, in the form the script writes it.
// "in" is what parseSearch or a Link gives (JSON), "want" what validateSearch returns, or the error.
// An undefined value is written as null, since JSON has none.
var searchScenarios = []struct{ spec, in, want string }{
	{`{"page":"int","q":"string"}`, `{"page":"2","q":"a"}`, `{"page":2,"q":"a"}`},
	// A number written another way is the same number, and a string is not changed.
	{`{"page":"int","q":"string"}`, `{"q":"007","page":"007"}`, `{"q":"007","page":7}`},
	{`{"page":"int","min":"float64"}`, `{"page":"+2.0","min":".5"}`, `{"page":2,"min":0.5}`},
	{`{"page":"int"}`, `{"page":"2.5"}`, `error invalid query parameter "page"`},
	{`{"page":"int","q":"string"}`, `{"q":"007"}`, `{"q":"007"}`},
	// An empty value of a number or bool is a value that was not passed, and a key the router would
	// keep is returned as undefined.
	{`{"page":"int","on":"bool","q":"string"}`, `{"page":"","on":"","q":""}`, `{"page":null,"on":null,"q":""}`},
	// A value that is already typed, as the route above hands it down or a Link gives it.
	{`{"page":"int","on":"bool","min":"float64"}`, `{"page":3,"on":true,"min":1.5}`, `{"page":3,"on":true,"min":1.5}`},
	{`{"page":"int"}`, `{"page":1.5}`, `error invalid query parameter "page"`},
	{`{"page":"int8"}`, `{"page":200}`, `error invalid query parameter "page"`},
	{`{"page":"int8"}`, `{"page":"200"}`, `error invalid query parameter "page"`},
	{`{"page":"uint8"}`, `{"page":"200"}`, `{"page":200}`},
	{`{"min":"float64"}`, `{"min":1e21}`, `error invalid query parameter "min"`},
	{`{"on":"bool"}`, `{"on":1}`, `error invalid query parameter "on"`},
	{`{"q":"string"}`, `{"q":5}`, `error invalid query parameter "q"`},
	// A name that is not a list cannot come twice.
	{`{"page":"int"}`, `{"page":["1","2"]}`, `error invalid query parameter "page"`},
	{`{"q":"string"}`, `{"q":["a","b"]}`, `error invalid query parameter "q"`},
	{`{"ids":"int[]","tags":"string[]"}`, `{"ids":["1","","3"],"tags":["a","","b"]}`, `{"ids":[1,3],"tags":["a","","b"]}`},
	{`{"ids":"int[]","tags":"string[]"}`, `{"ids":"7","tags":"x"}`, `{"ids":[7],"tags":["x"]}`},
	{`{"ids":"int[]","tags":"string[]"}`, `{"ids":["",""],"tags":[""]}`, `{"ids":null,"tags":[""]}`},
	{`{"ids":"int[]"}`, `{"ids":["1","01"]}`, `{"ids":[1,1]}`},
	{`{"ids":"int[]"}`, `{"ids":["1","x"]}`, `error invalid query parameter "ids"`},
	{`{"ids":"int[]"}`, `{"ids":[1,2]}`, `{"ids":[1,2]}`},
	// Parameters the spec does not name stay as they are, in the order of the address.
	{`{"page":"int"}`, `{"utm":"x","page":"2","a":["1","2"]}`, `{"utm":"x","page":2,"a":["1","2"]}`},
	// A parameter may be called __proto__, which is an ordinary name.
	{`{"__proto__":"int"}`, `{"__proto__":"2"}`, `{"__proto__":2}`},
	{`{"__proto__":"int"}`, `{"__proto__":"x"}`, `error invalid query parameter "__proto__"`},
	// With two that are not valid the error names the first by name.
	{`{"zeta":"int","alpha":"int"}`, `{"zeta":"x","alpha":"y"}`, `error invalid query parameter "alpha"`},
}

// The script gives each case to the codec and writes what came out as one JSON text in
// globalThis.result. The same bundle runs in beastjs, which renders on the server, and in Node.
func searchCodecScript(cases []queryCase) string {
	table, _ := json.Marshal(cases)
	scenarios := make([][]string, len(searchScenarios))
	for i, s := range searchScenarios {
		scenarios[i] = []string{s.spec, s.in}
	}
	scenarioJSON, _ := json.Marshal(scenarios)
	_, file, _, _ := runtime.Caller(0)
	codec := filepath.ToSlash(filepath.Join(filepath.Dir(file), "..", "..", "js", "runtime", "searchcodec.ts"))
	return fmt.Sprintf(`import { canonicalNumber, decodeValue, searchCodec } from %q
const bits = (n) => {
  const view = new DataView(new ArrayBuffer(8))
  view.setFloat64(0, n)
  return view.getUint32(0).toString(16).padStart(8, '0') + view.getUint32(4).toString(16).padStart(8, '0')
}
const table = %s.map(({ kind, input }) => {
  const value = decodeValue(kind, input)
  if (value === undefined) return 'x -'
  if (typeof value === 'string') return '- -'
  if (typeof value === 'boolean') return (value ? '1' : '0') + ' -'
  return bits(value) + ' ' + canonicalNumber(input, kind !== 'float64')
})
const scenarios = %s.map(([spec, input]) => {
  try {
    const out = searchCodec(Object.entries(JSON.parse(spec)))(Object.assign(Object.create(null), JSON.parse(input)))
    return JSON.stringify(out, (key, value) => (value === undefined ? null : value))
  } catch (error) {
    return 'error ' + error.message + ' ' + error.status
  }
})
globalThis.result = JSON.stringify({ table, scenarios })
`, codec, table, scenarioJSON)
}

func TestSearchCodecAnswersLikeGo(t *testing.T) {
	cases := readQueryCases(t)
	if len(cases) < 150 {
		t.Fatalf("read %d cases, the table has more", len(cases))
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{"codec.ts": searchCodecScript(cases)})
	code, _ := buildScript(t, root, "codec.ts")

	check := func(t *testing.T, result string) {
		var got struct {
			Table     []string `json:"table"`
			Scenarios []string `json:"scenarios"`
		}
		if err := json.Unmarshal([]byte(result), &got); err != nil {
			t.Fatalf("%v: %s", err, result)
		}
		for i, c := range cases {
			if want := c.Want + " " + c.Canon; got.Table[i] != want {
				t.Errorf("%s %q: the codec says %s, the table says %s", c.Kind, c.Input, got.Table[i], want)
			}
		}
		for i, s := range searchScenarios {
			want := s.want
			if strings.HasPrefix(want, "error ") {
				want += " 400"
			}
			if got.Scenarios[i] != want {
				t.Errorf("spec %s on %s: got %s, want %s", s.spec, s.in, got.Scenarios[i], want)
			}
		}
	}
	t.Run("beastjs", func(t *testing.T) { check(t, evalResult(t, code)) })
	t.Run("node", func(t *testing.T) {
		file := filepath.Join(root, "codec.mjs")
		if err := os.WriteFile(file, []byte(code+"\nconsole.log(globalThis.result)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("node", file).Output()
		if err != nil {
			t.Fatal(err)
		}
		check(t, strings.TrimSpace(string(out)))
	})
}
