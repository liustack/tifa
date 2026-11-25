package core

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxExact is the largest whole number a browser holds exactly, 2^53-1. The query string of a page
// is read by Go and by the browser, and the two must agree on every value, so no integer is valid
// beyond it, whatever the width of the field.
const maxExact = 1<<53 - 1

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
// A value is valid only when it is the one way to write it: decoding it and writing the result
// gives the same text. A string is any text. A whole number is 0, or digits with no leading zero
// and an optional minus, and fits the width of its field and 2^53-1. A bool is true or false. A
// float has no exponent, no trailing zero after the point and at most 15 significant digits, and
// is 0 or between 1e-6 and 1e21 in size. A slice holds the values of the parameters of one name,
// in order.
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
	}
	for i := range target.NumField() {
		field := target.Type().Field(i)
		name := formName(field)
		if !field.IsExported() || name == "-" {
			continue
		}
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
// integer or float field, which formTypeProblem has checked.
func setQueryValue(f reflect.Value, s string) error {
	switch f.Kind() {
	case reflect.String:
		// Text that is not UTF-8 leaves the page as U+FFFD, so it would not come back as written.
		if !utf8.ValidString(s) {
			return errQueryValue
		}
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
		if !wholeNumber(s) {
			return errQueryValue
		}
		n, err := strconv.ParseInt(s, 10, f.Type().Bits())
		if err != nil || n > maxExact || n < -maxExact {
			return errQueryValue
		}
		f.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if !wholeNumber(s) || s[0] == '-' {
			return errQueryValue
		}
		n, err := strconv.ParseUint(s, 10, f.Type().Bits())
		if err != nil || n > maxExact {
			return errQueryValue
		}
		f.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if !plainFloat(s) {
			return errQueryValue
		}
		n, err := strconv.ParseFloat(s, 64)
		if a := math.Abs(n); err != nil || a != 0 && (a < 1e-6 || a >= 1e21) {
			return errQueryValue
		}
		// What the field holds must write back as s: a float32 keeps fewer digits than plainFloat
		// lets through, and digits too small for a float64 read as 0.
		if strconv.FormatFloat(n, 'f', -1, f.Type().Bits()) != s {
			return errQueryValue
		}
		f.SetFloat(n)
	default:
		return fmt.Errorf("unsupported query field type %s", f.Type())
	}
	return nil
}

// wholeNumber reports whether s is -?(0|[1-9][0-9]*) and not -0.
func wholeNumber(s string) bool {
	return unsignedWhole(strings.TrimPrefix(s, "-")) && s != "-0"
}

// unsignedWhole reports whether s is 0 or digits that do not start with a zero.
func unsignedWhole(s string) bool {
	return allDigits(s) && (len(s) == 1 || s[0] != '0')
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// plainFloat reports whether s is -?(0|[1-9][0-9]*)(\.[0-9]*[1-9])? and not -0, with at most 15
// significant digits.
func plainFloat(s string) bool {
	whole, fraction, hasPoint := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if !unsignedWhole(whole) || s == "-0" {
		return false
	}
	if hasPoint && (!allDigits(fraction) || fraction[len(fraction)-1] == '0') {
		return false
	}
	// The digits after the leading zeros of the number, the point not counted.
	digits := strings.TrimLeft(whole+fraction, "0")
	return len(digits) <= 15
}
