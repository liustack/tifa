package core

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"strconv"
	"strings"
	"testing"
)

// referenceRounded is the rule applied to a full decode: the first integer token that does not
// read back as the same double, whatever its length.
func referenceRounded(data []byte) (token string, found bool) {
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return "", false
		}
		if tok.Kind() != '0' {
			continue
		}
		s := tok.String()
		if strings.ContainsAny(s, ".eE") {
			continue
		}
		f, _ := strconv.ParseFloat(s, 64)
		if strconv.FormatFloat(f, 'f', -1, 64) != s {
			return s, true
		}
	}
}

var roundedCases = []struct {
	json   string
	number string // "" when nothing is refused
	read   string
	path   string
}{
	{`{"id":9007199254740993}`, "9007199254740993", "9007199254740992", "id"},
	{`{"id":9007199254740991}`, "", "", ""},
	{`{"id":9007199254740992}`, "", "", ""},
	{`{"id":1763300000123}`, "", "", ""},
	{`{"id":1763300000123456789}`, "1763300000123456789", "1763300000123456800", "id"},
	{`18446744073709551615`, "18446744073709551615", "18446744073709552000", ""},
	{`-9007199254740993`, "-9007199254740993", "-9007199254740992", ""},
	{`[-0, 0, -9007199254740991]`, "", "", ""},
	{`{"rows":[{"id":1},{"id":2},{"a":[1,[9007199254740993]]}]}`, "9007199254740993", "9007199254740992", "rows[2].a[1][0]"},
	{`{"a b":{"x":9007199254740993}}`, "9007199254740993", "9007199254740992", `["a b"].x`},
	// Floats, even huge ones, are doubles already.
	{`{"f":100000000000000000}`, "", "", ""},
	{`{"f":1e+21,"g":1.5e300,"h":0.12345678901234567}`, "", "", ""},
	{`{"f":9007199254740993.0}`, "", "", ""},
	{`{"f":9007199254740993e0}`, "", "", ""},
	// Numbers inside strings are no numbers, also after an escaped quote.
	{`{"s":"9007199254740993","9007199254740993":"a"}`, "", "", ""},
	{`{"s":"a\"9007199254740993\"b","n":9007199254740993}`, "9007199254740993", "9007199254740992", "n"},
	{`{"s":"a\\","n":9007199254740993}`, "9007199254740993", "9007199254740992", "n"},
	{`{"s":"\\\"","n":1}`, "", "", ""},
	// At the very end, and a long integer that is not a double at all.
	{`[1,9007199254740993]`, "9007199254740993", "9007199254740992", "[1]"},
	{`1` + strings.Repeat("0", 400), "1" + strings.Repeat("0", 400), "+Inf", ""},
}

func TestCheckBrowserNumbers(t *testing.T) {
	for _, tc := range roundedCases {
		got := CheckBrowserNumbers([]byte(tc.json))
		if tc.number == "" {
			if got != nil {
				t.Errorf("%s: refused %s", tc.json, got.Number)
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: not refused, want %s", tc.json, tc.number)
			continue
		}
		if got.Number != tc.number || strings.TrimPrefix(got.Read, "+") != strings.TrimPrefix(tc.read, "+") && tc.read != "+Inf" {
			t.Errorf("%s: %s read as %s, want %s as %s", tc.json, got.Number, got.Read, tc.number, tc.read)
		}
		if p := PathString(got.Path); p != tc.path {
			t.Errorf("%s: path %q, want %q", tc.json, p, tc.path)
		}
	}
}

// What json v2 writes for floats and integers is what the rule is built on.
func TestJSONWriteFloatsPass(t *testing.T) {
	values := []float64{1e17, 1.2345678901234567e17, 1e21, 1e300, -1e17, math.MaxFloat64, 9007199254740993, 1 << 62, 0.1, 5e-324}
	for _, f := range values {
		data, err := MarshalJSON(f)
		if err != nil {
			t.Fatal(err)
		}
		if e := CheckBrowserNumbers(data); e != nil {
			t.Errorf("float64 %v written as %s was refused", f, data)
		}
	}
	data, _ := MarshalJSON(1e17)
	if string(data) != "100000000000000000" {
		t.Errorf("json v2 writes 1e17 as %s", data)
	}
	for _, v := range []any{int64(1<<53 + 1), uint64(math.MaxUint64), int64(math.MinInt64)} {
		data, _ := MarshalJSON(v)
		if CheckBrowserNumbers(data) == nil {
			t.Errorf("%T %s was not refused", v, data)
		}
	}
	for _, v := range []any{int64(1<<53 - 1), int64(-(1<<53 - 1)), int64(1763300000123)} {
		data, _ := MarshalJSON(v)
		if e := CheckBrowserNumbers(data); e != nil {
			t.Errorf("%T %s was refused", v, data)
		}
	}
}

func FuzzCheckBrowserNumbers(f *testing.F) {
	for _, tc := range roundedCases {
		f.Add(tc.json)
	}
	f.Add(`["\\",9007199254740993]`)
	f.Add(`{"a":"\"","b":[ 12345678901234567 , -12345678901234567 ]}`)
	f.Fuzz(func(t *testing.T, s string) {
		data := []byte(s)
		var v any
		if json.Unmarshal(data, &v) != nil || !jsontext.Value(data).IsValid() {
			t.Skip()
		}
		// tifa writes compact JSON, so compact the input.
		data = compacted(data)
		want, found := referenceRounded(data)
		got := CheckBrowserNumbers(data)
		if found != (got != nil) || got != nil && got.Number != want {
			t.Fatalf("%s: the scan found %v, a full decode found %q", data, got, want)
		}
	})
}

func compacted(data []byte) []byte {
	v := jsontext.Value(append([]byte(nil), data...))
	_ = v.Compact()
	return v
}
