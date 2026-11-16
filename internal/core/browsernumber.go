package core

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"
)

// A browser, and the JSON.parse of the server render, read every JSON number as a double. Go
// writes every digit of an integer, so 9007199254740993 arrives as 9007199254740992 without any
// error. Everything tifa writes for a page to read goes through CheckBrowserNumbers, which refuses
// such a number: a wrong id on a page is worse than an error.
//
// The rule: an integer token (no fraction, no exponent) is refused when it does not survive being
// read as a float64 and written back without an exponent. Floats are never refused: json v2 writes
// a float64 in the shortest form that reads back to the same double.

// RoundedNumberError reports a number a browser would read as another number.
type RoundedNumberError struct {
	What   string // what was being sent, such as "the Load of /orders", set by the caller
	Path   []PathStep
	Number string // as written
	Read   string // as a browser reads it
}

// PathStep is one step from the root of a JSON value to a number: an object name or an array index.
type PathStep struct {
	Key   string
	Index int
	Array bool
}

// String writes the path the way code reads it: rows[3].id, ["a b"].
func PathString(path []PathStep) string {
	var b strings.Builder
	for _, s := range path {
		switch {
		case s.Array:
			fmt.Fprintf(&b, "[%d]", s.Index)
		case isIdentifier(s.Key):
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.Key)
		default:
			b.WriteString("[" + strconv.Quote(s.Key) + "]")
		}
	}
	return b.String()
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func (e *RoundedNumberError) Error() string {
	where := "at the top level"
	if len(e.Path) > 0 {
		where = "at " + PathString(e.Path)
	}
	return fmt.Sprintf("tifa: %s writes the number %s %s, and a browser reads it as %s. "+
		"Tag the field `json:\",string\"` to send it as a string (TypeScript string), or keep the value within ±9007199254740991 (for a time, send milliseconds, not nanoseconds)",
		e.What, e.Number, where, e.Read)
}

// MarshalForBrowser is MarshalJSON for a value a page reads. what names it in the error.
func MarshalForBrowser(v any, what string) ([]byte, error) {
	data, err := MarshalJSON(v)
	if err != nil {
		return nil, err
	}
	if e := CheckBrowserNumbers(data); e != nil {
		e.What = what
		return nil, e
	}
	return data, nil
}

// CheckBrowserNumbers returns nil when a double reads every number of the JSON text data as the
// same number. The first pass is one scan of the bytes. The path of the number is found in a
// second pass, only when the first found one. data must be valid JSON, as tifa writes it.
func CheckBrowserNumbers(data []byte) *RoundedNumberError {
	start, end, read := firstRounded(data)
	if start < 0 {
		return nil
	}
	return &RoundedNumberError{Path: pathTo(data, end), Number: string(data[start:end]), Read: read}
}

// firstRounded returns the bounds of the first integer token that a double does not read back as
// the same number, and what it reads as. start is -1 when there is none.
func firstRounded(b []byte) (start, end int, read string) {
	i := 0
	for i < len(b) {
		c := b[i]
		switch {
		case c == '"':
			i = skipString(b, i+1)
		case c == '-' || c >= '0' && c <= '9':
			j := i + 1
			for j < len(b) && numberByte[b[j]] {
				j++
			}
			// 15 digits always survive. Only a token made of digits only is an integer.
			if j-i >= 16 {
				if r, ok := roundedInteger(b[i:j]); ok {
					return i, j, r
				}
			}
			i = j
		default:
			i++
		}
	}
	return -1, 0, ""
}

// numberByte marks the bytes a number token is made of.
var numberByte = func() (t [256]bool) {
	for _, c := range "0123456789.eE+-" {
		t[c] = true
	}
	return
}()

// skipString returns the index after the string whose first byte after the opening quote is at i.
func skipString(b []byte, i int) int {
	for i < len(b) {
		k := bytes.IndexByte(b[i:], '"')
		if k < 0 {
			return len(b)
		}
		i += k
		n := 0
		for p := i - 1; p >= 0 && b[p] == '\\'; p-- {
			n++
		}
		i++
		if n%2 == 0 {
			return i
		}
	}
	return i
}

// roundedInteger reports whether tok is an integer a double reads as another number.
func roundedInteger(tok []byte) (read string, rounded bool) {
	digits := tok
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) < 16 {
		return "", false
	}
	for _, d := range digits {
		if d < '0' || d > '9' {
			return "", false // a fraction or an exponent: a float
		}
	}
	f, _ := strconv.ParseFloat(string(tok), 64)
	var buf [32]byte
	back := strconv.AppendFloat(buf[:0], f, 'f', -1, 64)
	if bytes.Equal(back, tok) {
		return "", false
	}
	return string(back), true
}

// pathTo finds the path of the value that ends at offset end of data.
func pathTo(data []byte, end int) []PathStep {
	type frame struct {
		array     bool
		n         int // index of the current element
		key       string
		expectKey bool
	}
	var stack []frame
	var path []PathStep
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	step := func() {
		path = path[:0]
		for _, f := range stack {
			if f.array {
				path = append(path, PathStep{Index: f.n, Array: true})
			} else {
				path = append(path, PathStep{Key: f.key})
			}
		}
	}
	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil
		}
		kind := tok.Kind()
		if kind == '}' || kind == ']' {
			stack = stack[:len(stack)-1]
			if n := len(stack); n > 0 && !stack[n-1].array {
				stack[n-1].expectKey = true
			}
			continue
		}
		if n := len(stack); n > 0 {
			top := &stack[n-1]
			switch {
			case top.array:
				top.n++
			case top.expectKey:
				top.key, top.expectKey = tok.String(), false
				continue
			}
		}
		switch kind {
		case '{':
			stack = append(stack, frame{expectKey: true})
		case '[':
			stack = append(stack, frame{array: true, n: -1})
		default:
			if int(dec.InputOffset()) == end {
				step()
				return append([]PathStep(nil), path...)
			}
			if n := len(stack); n > 0 && !stack[n-1].array {
				stack[n-1].expectKey = true
			}
		}
	}
}
