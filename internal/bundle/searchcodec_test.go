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
)

// queryCase is a line of internal/core/testdata/query-values.txt, the cases that the Go decoder, the
// codec of the browser in Node and the codec in beastjs must answer alike.
type queryCase struct {
	Kind  string `json:"kind"`
	Input string `json:"input"`
	Want  string `json:"want"`
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
		if len(cols) != 4 {
			t.Fatalf("a line of the table needs 4 columns: %q", line)
		}
		var input string
		if err := json.Unmarshal([]byte(cols[1]), &input); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		kind := cols[0]
		if kind == "float" {
			kind = "float64"
		}
		want := "x"
		if cols[2] == "ok" {
			want = cols[3]
		}
		cases = append(cases, queryCase{kind, input, want})
	}
	return append(cases, randomQueryCases()...)
}

// randomQueryCases are numbers written the one valid way, which every codec must read as the number
// Go wrote: whole numbers up to 2^53-1 and floats of up to 15 significant digits.
func randomQueryCases() []queryCase {
	rng := rand.New(rand.NewPCG(1, 2))
	var cases []queryCase
	for range 20000 {
		n := rng.Int64N(1<<53-1) - rng.Int64N(1<<53-1)
		cases = append(cases, queryCase{"int", strconv.FormatInt(n, 10), fmt.Sprintf("%016x", math.Float64bits(float64(n)))})
	}
	for len(cases) < 40000 {
		digits := 1 + rng.IntN(15)
		x := rng.Float64() * math.Pow(10, float64(rng.IntN(20)-6))
		x, _ = strconv.ParseFloat(strconv.FormatFloat(x, 'g', digits, 64), 64)
		if x < 1e-6 || x >= 1e21 {
			continue
		}
		cases = append(cases, queryCase{"float64", strconv.FormatFloat(x, 'f', -1, 64), fmt.Sprintf("%016x", math.Float64bits(x))})
	}
	return cases
}

// searchScenarios are what the codec does with a whole search, in the form the script writes it.
// "in" is what parseSearch or a Link gives (JSON), "want" what validateSearch returns, or the error.
// An undefined value is written as null, since JSON has none.
var searchScenarios = []struct{ spec, in, want string }{
	{`{"page":"int","q":"string"}`, `{"page":"2","q":"a"}`, `{"page":2,"q":"a"}`},
	{`{"page":"int","q":"string"}`, `{"q":"007","page":"007"}`, `error invalid query parameter "page"`},
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
	{`{"ids":"int[]"}`, `{"ids":["1","01"]}`, `error invalid query parameter "ids"`},
	{`{"ids":"int[]"}`, `{"ids":[1,2]}`, `{"ids":[1,2]}`},
	// Parameters the spec does not name stay as they are, in the order of the address.
	{`{"page":"int"}`, `{"utm":"x","page":"2","a":["1","2"]}`, `{"utm":"x","page":2,"a":["1","2"]}`},
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
	return fmt.Sprintf(`import { decodeValue, searchCodec } from %q
const bits = (n) => {
  const view = new DataView(new ArrayBuffer(8))
  view.setFloat64(0, n)
  return view.getUint32(0).toString(16).padStart(8, '0') + view.getUint32(4).toString(16).padStart(8, '0')
}
const table = %s.map(({ kind, input }) => {
  const value = decodeValue(kind, input)
  if (value === undefined) return 'x'
  if (typeof value === 'string') return '-'
  return typeof value === 'boolean' ? (value ? '1' : '0') : bits(value)
})
const scenarios = %s.map(([spec, input]) => {
  try {
    const out = searchCodec(JSON.parse(spec))(Object.assign(Object.create(null), JSON.parse(input)))
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
			if got.Table[i] != c.Want {
				t.Errorf("%s %q: the codec says %s, the table says %s", c.Kind, c.Input, got.Table[i], c.Want)
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
