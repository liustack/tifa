package core

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// maxExact is the largest whole number a browser holds exactly, 2^53-1. The query string of a page
// is read by Go and by the browser, and the two must agree on every value, so no integer is valid
// beyond it, whatever the width of the field.
const maxExact = 1<<53 - 1

// maxNumberText is the longest text of a number, in bytes (see readNumber).
const maxNumberText = 64

// decodePageQuery decodes the query string of the request into the struct v, the second parameter
// of Load and Layout. A query string that does not parse, or a value that is not valid, comes back as
// Error with status 400 (see decodeQuery for the rules). Every exported field of v must be one the
// query string fills (see formTypeProblem), or it returns an error whatever the query holds. The
// input of a GET or DELETE handler is read by (*Ctx).BindQuery, which is looser: a page has one
// address and a render memo key, and the browser must read the same query the same way.
func decodePageQuery(c *Ctx, v any) error {
	// URL.Query drops the parameters it cannot parse, so a broken one would look absent.
	query, err := url.ParseQuery(c.r.URL.RawQuery)
	if err != nil {
		return publicError(http.StatusBadRequest, "invalid query string")
	}
	return decodeQuery(query, v)
}

// decodeQuery fills the exported fields of the struct v from the parameters of a query string. A
// field is named by its json tag, then its Go name. A parameter that names no field is left out.
//
// A string is any text. A bool is true or false. A whole number is digits with an optional sign
// and an optional point with zeros after it (+007, 1.0, -0), fits the width of its field and 2^53-1.
// A float64 is digits with an optional sign, an optional fraction and an optional exponent (010.50,
// .5, 5., 1e3), and is 0 or between 1e-6 and 1e21 in size. A number written another way than the
// shortest one is the same number, and the page is rendered for the shortest text (see
// CanonicalNumber). Anything else is not valid, such as 10.50 for a whole number, a space, 0x10 or
// NaN, or a text of more than 64 bytes. A slice holds the values of the parameters of one name, in order.
//
// An empty value of a field that is not a string, or of a list of numbers or bools, is a
// parameter that was not passed (the browser sends it for a form field left empty). A field that
// is not a slice and whose name comes twice is an error, since only one of the values could
// be written back.
func decodeQuery(query url.Values, v any) error {
	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tifa: decoding a query string needs a pointer to a struct, got %T", v)
	}
	target = target.Elem()
	// Check every field before filling any, so a wrong type fails whatever the request holds.
	for i := range target.NumField() {
		field := target.Type().Field(i)
		if !field.IsExported() || formName(field) == "-" {
			continue
		}
		if problem := formTypeProblem(field.Type); problem != "" {
			return fmt.Errorf("tifa: a query string or form cannot fill the field %s of %s: its type %s %s", field.Name, target.Type(), field.Type, problem)
		}
		if t := field.Type; t.Kind() == reflect.Float32 || t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Float32 {
			return fmt.Errorf("tifa: a query string cannot fill the field %s of %s: its type %s is a float32, which a browser cannot write the way Go does, so declare it float64", field.Name, target.Type(), t)
		}
	}
	// The first value that is not valid in the order of the names is the one the answer names.
	var names []string
	index := map[string]int{}
	for i := range target.NumField() {
		if field := target.Type().Field(i); field.IsExported() && formName(field) != "-" {
			names = append(names, formName(field))
			index[formName(field)] = i
		}
	}
	slices.Sort(names)
	for _, name := range names {
		i := index[name]
		values, ok := query[name]
		if !ok {
			continue
		}
		if err := setQueryField(target.Field(i), values); err != nil {
			return publicError(http.StatusBadRequest, fmt.Sprintf("invalid query parameter %q", name))
		}
	}
	return nil
}

var errQueryValue = errors.New("not a valid query value")

func setQueryField(f reflect.Value, values []string) error {
	if f.Kind() == reflect.Slice {
		elem := f.Type().Elem()
		slice := reflect.MakeSlice(f.Type(), 0, len(values))
		for _, s := range values {
			if s == "" && elem.Kind() != reflect.String {
				continue
			}
			e := reflect.New(elem).Elem()
			if err := setQueryValue(e, s); err != nil {
				return err
			}
			slice = reflect.Append(slice, e)
		}
		if slice.Len() > 0 {
			f.Set(slice)
		}
		return nil
	}
	if len(values) > 1 {
		return errQueryValue
	}
	if values[0] == "" && f.Kind() != reflect.String {
		return nil
	}
	return setQueryValue(f, values[0])
}

// setQueryValue sets f from one value, which must be valid for its kind. f is a string, bool,
// integer or float64 field, which formTypeProblem and decodeQuery have checked.
func setQueryValue(f reflect.Value, s string) error {
	switch f.Kind() {
	case reflect.String:
		// Text that is not UTF-8 is taken as it is: the page gets U+FFFD for each such byte, which is
		// what the router reads from the same address in the browser.
		f.SetString(s)
	case reflect.Bool:
		switch s {
		case "true":
			f.SetBool(true)
		case "false":
			f.SetBool(false)
		default:
			return errQueryValue
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		x, ok := readNumber(s, true)
		if !ok || f.OverflowInt(int64(x)) {
			return errQueryValue
		}
		f.SetInt(int64(x))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		x, ok := readNumber(s, true)
		if !ok || x < 0 || f.OverflowUint(uint64(x)) {
			return errQueryValue
		}
		f.SetUint(uint64(x))
	case reflect.Float64:
		x, ok := readNumber(s, false)
		if !ok {
			return errQueryValue
		}
		f.SetFloat(x)
	default:
		return fmt.Errorf("unsupported query field type %s", f.Type())
	}
	return nil
}

var (
	wholeText = regexp.MustCompile(`^[+-]?[0-9]+(\.0*)?$`)
	floatText = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)
)

// readNumber reads the number s writes, as a browser reads it: whole is true for a whole number, which
// has no fraction but zeros and no exponent, and false for a float64. The number is 0 or between 1e-6
// and 1e21 in size, a whole number is at most 2^53-1 in size, and -0 is 0. ok is false for a text that
// is no such number.
func readNumber(s string, whole bool) (n float64, ok bool) {
	// A very long decimal is rounded differently by Go and by a browser, so no text is longer than
	// maxNumberText, which no number a person types or a form sends reaches.
	if len(s) > maxNumberText || whole && !wholeText.MatchString(s) || !whole && !floatText.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if a := math.Abs(n); err != nil || a != 0 && (a < 1e-6 || a >= 1e21) || whole && a > maxExact {
		return 0, false
	}
	// A number too small to hold reads as 0, which is not what the text says.
	if mantissa, _, _ := strings.Cut(strings.ToLower(s), "e"); n == 0 && strings.ContainsAny(mantissa, "123456789") {
		return 0, false
	}
	if n == 0 {
		return 0, true
	}
	return n, true
}

// CanonicalNumber is the shortest text of the number s writes (see readNumber), the one the page is
// rendered for, "" when s is no such number. The browser writes the same text: String(n).
func CanonicalNumber(s string, whole bool) string {
	n, ok := readNumber(s, whole)
	if !ok {
		return ""
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
