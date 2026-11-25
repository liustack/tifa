package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fieldOf returns a field of the type the table names, to decode one value into.
func fieldOf(t *testing.T, kind string) reflect.Value {
	t.Helper()
	types := map[string]reflect.Type{
		"string": reflect.TypeFor[string](), "bool": reflect.TypeFor[bool](), "int": reflect.TypeFor[int](),
		"int8": reflect.TypeFor[int8](), "int16": reflect.TypeFor[int16](), "int32": reflect.TypeFor[int32](), "int64": reflect.TypeFor[int64](),
		"uint": reflect.TypeFor[uint](), "uint8": reflect.TypeFor[uint8](), "uint16": reflect.TypeFor[uint16](), "uint32": reflect.TypeFor[uint32](),
		"uint64": reflect.TypeFor[uint64](), "uintptr": reflect.TypeFor[uintptr](), "float": reflect.TypeFor[float64](),
	}
	typ, ok := types[kind]
	if !ok {
		t.Fatalf("the table has the unknown type %q", kind)
	}
	return reflect.New(typ).Elem()
}

// bitsOf is the column of the table that says what a number decoded to: the 64 bits of a number, 1 or
// 0 for a bool.
func bitsOf(f reflect.Value) uint64 {
	switch f.Kind() {
	case reflect.Bool:
		if f.Bool() {
			return 1
		}
		return 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return math.Float64bits(float64(f.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return math.Float64bits(float64(f.Uint()))
	case reflect.Float64:
		return math.Float64bits(f.Float())
	}
	return 0
}

// The same table is read by the decoder of the browser, so the two cannot disagree about a value.
func TestQueryValueTable(t *testing.T) {
	file, err := os.Open("testdata/query-values.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	n := 0
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
		n++
		f := fieldOf(t, cols[0])
		err := setQueryValue(f, input)
		switch {
		case cols[2] == "x" && err == nil:
			t.Errorf("%s %s: accepted, the table says it is not valid", cols[0], cols[1])
		case cols[2] == "ok" && err != nil:
			t.Errorf("%s %s: refused, the table says it is valid", cols[0], cols[1])
		case cols[2] == "ok" && cols[3] != "-":
			want, perr := strconv.ParseUint(cols[3], 16, 64)
			if perr != nil || bitsOf(f) != want {
				t.Errorf("%s %s: decoded to %x, the table says %s", cols[0], cols[1], bitsOf(f), cols[3])
			}
		}
	}
	if n < 150 {
		t.Errorf("read %d cases, the table has more", n)
	}
}

// A valid number is the only way to write it, so every number a page writes comes back as it was.
func TestQueryNumbersComeBackAsWritten(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		n := rng.Int64N(1<<53-1) - rng.Int64N(1<<53-1)
		s := strconv.FormatInt(n, 10)
		f := fieldOf(t, "int")
		if err := setQueryValue(f, s); err != nil || f.Int() != n {
			t.Fatalf("int %s: %v, %d", s, err, f.Int())
		}
	}
	for range 20000 {
		// a number with up to 15 significant digits, written the shortest way
		digits := 1 + rng.IntN(15)
		x := rng.Float64() * math.Pow(10, float64(rng.IntN(20)-6))
		x, _ = strconv.ParseFloat(strconv.FormatFloat(x, 'g', digits, 64), 64)
		if x < 1e-6 || x >= 1e21 {
			continue
		}
		s := strconv.FormatFloat(x, 'f', -1, 64)
		f := fieldOf(t, "float")
		if err := setQueryValue(f, s); err != nil {
			t.Fatalf("float %s: %v", s, err)
		}
		if back := strconv.FormatFloat(f.Float(), 'f', -1, 64); back != s {
			t.Fatalf("float %s came back as %s", s, back)
		}
	}
}

type queryInput struct {
	Status string   `json:"status"`
	Page   int      `json:"page"`
	Min    float64  `json:"min"`
	Stock  bool     `json:"stock"`
	Tags   []string `json:"tags"`
	IDs    []int    `json:"ids"`
	Skip   string   `json:"-"`
	Plain  string
}

func decodeRaw(raw string, v any) error {
	r := httptest.NewRequest("GET", "/list", nil)
	r.URL.RawQuery = raw
	return decodePageQuery(New(httptest.NewRecorder(), r, "/list", 1024), v)
}

func TestQueryDecoding(t *testing.T) {
	var in queryInput
	if err := decodeRaw("status=open&page=2&min=1.5&stock=true&tags=a&tags=&tags=b&ids=3&ids=-4&utm_source=x&Skip=y&Plain=p", &in); err != nil {
		t.Fatal(err)
	}
	want := queryInput{Status: "open", Page: 2, Min: 1.5, Stock: true, Tags: []string{"a", "", "b"}, IDs: []int{3, -4}, Plain: "p"}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("got %+v, want %+v", in, want)
	}

	// An empty value of a field that is not a string is a field that was not passed, and an empty
	// string is a value. An empty element of a list that is not of strings is left out.
	in = queryInput{}
	if err := decodeRaw("status=&page=&min=&stock=&tags=&ids=&ids=1&ids=", &in); err != nil {
		t.Fatal(err)
	}
	want = queryInput{Tags: []string{""}, IDs: []int{1}}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("empty values: got %+v, want %+v", in, want)
	}
}

func TestQueryDecodingRefusesWhatIsNotTheWrittenForm(t *testing.T) {
	for _, tc := range []struct{ raw, name string }{
		{"page=02", "page"}, {"page=%2B2", "page"}, {"page=-0", "page"}, {"page=2.0", "page"}, {"page=two", "page"},
		{"page=9007199254740992", "page"}, {"page=+2", "page"},
		{"min=1.0", "min"}, {"min=.5", "min"}, {"min=1e3", "min"}, {"min=0.1234567890123456", "min"},
		{"stock=TRUE", "stock"}, {"stock=1", "stock"}, {"stock=yes", "stock"},
		{"ids=1&ids=x", "ids"}, {"ids=01", "ids"},
		{"page=1&page=2", "page"}, {"page=&page=2", "page"}, {"status=a&status=b", "status"}, {"status=&status=", "status"},
		// A number too small to hold reads as 0, which is not what was written.
		{"min=0." + strings.Repeat("0", 400) + "1", "min"},
	} {
		var in queryInput
		var e Error
		err := decodeRaw(tc.raw, &in)
		if !errors.As(err, &e) || e.Status != 400 || e.Message != `invalid query parameter "`+tc.name+`"` {
			t.Errorf("?%s: got %v", tc.raw, err)
		}
	}
	// A query string that does not parse is 400 as a whole.
	var e Error
	if err := decodeRaw("page=1&%zz", &queryInput{}); !errors.As(err, &e) || e.Status != 400 || e.Message != "invalid query string" {
		t.Errorf("broken query string: got %v", err)
	}
	// The message does not carry a Go type.
	if err := decodeRaw("page=x", &queryInput{}); err != nil && strings.Contains(err.Error(), "int") {
		t.Errorf("the answer names a Go type: %v", err)
	}
}

// Text that is not UTF-8 is not refused: the page shows U+FFFD for the byte, as the browser reads
// the same address (the pages app checks the render in test/e2e).
func TestQueryStringFieldTakesBytesThatAreNotUTF8(t *testing.T) {
	var in queryInput
	if err := decodeRaw("status=%FF", &in); err != nil || in.Status != "\xff" {
		t.Fatalf("%v %q", err, in.Status)
	}
}

func TestQueryDecodingOfOtherWidths(t *testing.T) {
	var in struct {
		A int8   `json:"a"`
		B uint16 `json:"b"`
	}
	if err := decodeRaw("a=-128&b=65535", &in); err != nil || in.A != -128 || in.B != 65535 {
		t.Fatalf("%v %+v", err, in)
	}
	for _, raw := range []string{"a=128", "a=200", "b=65536", "b=-1"} {
		var e Error
		if err := decodeRaw(raw, &in); !errors.As(err, &e) || e.Status != 400 {
			t.Errorf("?%s: got %v", raw, err)
		}
	}
}

// A float32 cannot be a query field: the browser cannot write the shortest text of one, so it and Go
// could not read a value alike. The build refuses it, and the decoder does if one gets this far.
func TestQueryDecodingRefusesFloat32(t *testing.T) {
	for _, v := range []any{&struct{ C float32 }{}, &struct{ C []float32 }{}} {
		err := decodeRaw("", v)
		if err == nil || !strings.Contains(err.Error(), "float32") {
			t.Errorf("%T: got %v", v, err)
		}
	}
}

func TestSliceOfQueryValuesKeepsOrder(t *testing.T) {
	var in struct {
		N []int `json:"n"`
	}
	if err := decodeRaw(url.Values{"n": {"3", "1", "2"}}.Encode(), &in); err != nil || !slices.Equal(in.N, []int{3, 1, 2}) {
		t.Errorf("%v %v", err, in.N)
	}
}
