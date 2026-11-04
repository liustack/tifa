package core

import (
	"encoding"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// Bind decodes the request body into v by Content-Type: JSON, URL-encoded form or multipart form.
// A multipart form also fills the fields of type *multipart.FileHeader and []*multipart.FileHeader
// with its files. Client mistakes come back as Error with status 400, 408, 413 or 415.
func (c *Ctx) Bind(v any) error {
	mediaType, _, _ := mime.ParseMediaType(c.r.Header.Get("Content-Type"))
	if mediaType == "application/json" {
		// UnmarshalRead reads to the end of the body, so trailing data and an oversized body are not
		// accepted.
		if err := json.UnmarshalRead(c.r.Body, v); err != nil {
			return bodyError(err, "invalid JSON body")
		}
		return nil
	}
	return c.bindForm(mediaType, v, false)
}

// BindForm is Bind for the input of a method handler that takes files: the body must be a form,
// multipart or URL-encoded, and JSON is answered with 415 like any other content type. Every field
// of v but the files must be one a form fills, as for BindQuery.
func BindForm(c *Ctx, v any) error {
	mediaType, _, _ := mime.ParseMediaType(c.r.Header.Get("Content-Type"))
	return c.bindForm(mediaType, v, true)
}

func (c *Ctx) bindForm(mediaType string, v any, flat bool) error {
	var files map[string][]*multipart.FileHeader
	switch mediaType {
	case "multipart/form-data":
		if err := c.r.ParseMultipartForm(32 << 20); err != nil {
			return bodyError(err, "invalid form body")
		}
		// The multipart reader stops at the closing boundary. Read what follows through the body
		// limit too, so a body over the limit is refused before the handler, wherever its bytes are.
		if _, err := io.Copy(io.Discard, c.r.Body); err != nil {
			return bodyError(err, "invalid form body")
		}
		files = c.r.MultipartForm.File
	case "application/x-www-form-urlencoded":
		if err := c.r.ParseForm(); err != nil {
			return bodyError(err, "invalid form body")
		}
	default:
		return publicError(http.StatusUnsupportedMediaType, "")
	}
	return decodeForm(c.r.PostForm, files, v, "form field", flat)
}

// BindQuery decodes the query string into the struct v, with the names and value types of a
// form body. Query parameters that name no field are ignored. A query string that does not parse,
// or a value that does not, comes back as Error with status 400. Every exported field of v must be
// one a form fills (see formTypeProblem), or BindQuery returns an error whatever the query holds.
func (c *Ctx) BindQuery(v any) error {
	// URL.Query drops the parameters it cannot parse, so a broken one would look absent.
	query, err := url.ParseQuery(c.r.URL.RawQuery)
	if err != nil {
		return publicError(http.StatusBadRequest, "invalid query string")
	}
	return decodeForm(query, nil, v, "query parameter", true)
}

func bodyError(err error, message string) error {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return publicError(http.StatusRequestEntityTooLarge, "")
	case errors.Is(err, os.ErrDeadlineExceeded):
		// The read deadline passed before the body came: server.read_timeout, or the ReadTimeout of
		// the route, is too short for it.
		return publicError(http.StatusRequestTimeout, "")
	}
	return publicError(http.StatusBadRequest, message)
}

var fileHeaderType = reflect.TypeFor[multipart.FileHeader]()

// filePointer reports whether t is *multipart.FileHeader by its underlying type, the rule the
// analyzer knows a file by, so a named type such as type Photo *multipart.FileHeader is one too.
func filePointer(t reflect.Type) bool {
	return t.Kind() == reflect.Pointer && t.Elem() == fileHeaderType
}

// fileField reports whether a field of type t takes the files of a multipart form.
func fileField(t reflect.Type) bool {
	return filePointer(t) || t.Kind() == reflect.Slice && filePointer(t.Elem())
}

var (
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// formTypeProblem tells why the values of a query string or form do not fill a field of type t,
// or returns "". The values are decoded by kind, and text methods are never called: t is a bool,
// number or string kind, or a slice of one that is not a byte slice, and neither t nor its element
// has MarshalText or UnmarshalText, also on its pointer. A slice that reads itself as text would
// get a string such as "a,b" as one element, so t is checked before its element. queryType in
// internal/analyze has the same rule, and refuses such a field of a handler input at build time.
func formTypeProblem(t reflect.Type) string {
	if m := textMethods(t); m != "" {
		return "has " + m + ", which tifa does not call: it decodes each value by its kind"
	}
	if t.Kind() == reflect.Slice {
		if t.Elem() == reflect.TypeFor[byte]() {
			return "is a byte slice, which is base64 text in JSON and not a list of numbers"
		}
		if m := textMethods(t.Elem()); m != "" {
			return "has an element with " + m + ", which tifa does not call: it decodes each value by its kind"
		}
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return ""
	}
	return "is not a string, number or bool, or a slice of one"
}

// textMethods names the text methods of t or its pointer, as "MarshalText and UnmarshalText", or
// returns "".
func textMethods(t reflect.Type) string {
	var names []string
	for _, m := range []struct {
		name  string
		iface reflect.Type
	}{{"MarshalText", textMarshalerType}, {"UnmarshalText", textUnmarshalerType}} {
		if t.Implements(m.iface) || reflect.PointerTo(t).Implements(m.iface) {
			names = append(names, m.name)
		}
	}
	return strings.Join(names, " and ")
}

// decodeForm fills the exported fields of a struct. A field is named by its form tag, then its
// json tag, then its Go name. files are the files of a multipart form, which fill the fields of
// type *multipart.FileHeader (the first file) and []*multipart.FileHeader. what names a value in
// errors, such as "form field". When flat, v is an input that comes only as a form or query string,
// and every field must be one a form fills: another one is a mistake in the program, refused
// whatever the request holds. Otherwise v may be an input that also comes as JSON, and a value for
// a field a form does not fill is answered with 400.
func decodeForm(form url.Values, files map[string][]*multipart.FileHeader, v any, what string, flat bool) error {
	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tifa: decoding a %s needs a pointer to a struct, got %T", what, v)
	}
	target = target.Elem()
	// Check every field before filling any, so a wrong type fails whatever the request holds.
	for i := range target.NumField() {
		field := target.Type().Field(i)
		if !flat || !field.IsExported() || formName(field) == "-" || fileField(field.Type) {
			continue
		}
		if problem := formTypeProblem(field.Type); problem != "" {
			return fmt.Errorf("tifa: a query string or form cannot fill the field %s of %s: its type %s %s", field.Name, target.Type(), field.Type, problem)
		}
	}
	for i := range target.NumField() {
		field := target.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		name := formName(field)
		if name == "-" {
			continue
		}
		if fileField(field.Type) {
			if err := setFiles(target.Field(i), files[name], form[name]); err != nil {
				return publicError(http.StatusBadRequest, fmt.Sprintf("invalid %s %q", what, name))
			}
			continue
		}
		values, ok := form[name]
		if !ok {
			continue
		}
		if formTypeProblem(field.Type) != "" {
			return publicError(http.StatusBadRequest, fmt.Sprintf("invalid %s %q", what, name))
		}
		if err := setField(target.Field(i), values); err != nil {
			return publicError(http.StatusBadRequest, fmt.Sprintf("invalid %s %q", what, name))
		}
	}
	return nil
}

// setFiles fills a file field. A part without a file name is a text value to Go, and a browser
// sends a file input with no file chosen as an empty one, so an empty text value means no file.
// Any other text in place of a file is a mistake.
func setFiles(f reflect.Value, files []*multipart.FileHeader, texts []string) error {
	for _, s := range texts {
		if s != "" {
			return errors.New("text in place of a file")
		}
	}
	if len(files) == 0 {
		return nil
	}
	// Convert, since the field may have a named type of its own.
	if f.Kind() == reflect.Slice {
		slice := reflect.MakeSlice(f.Type(), len(files), len(files))
		for i, file := range files {
			slice.Index(i).Set(reflect.ValueOf(file).Convert(f.Type().Elem()))
		}
		f.Set(slice)
		return nil
	}
	f.Set(reflect.ValueOf(files[0]).Convert(f.Type()))
	return nil
}

func formName(field reflect.StructField) string {
	for _, tag := range []string{"form", "json"} {
		if name, _, _ := strings.Cut(field.Tag.Get(tag), ","); name != "" {
			return name
		}
	}
	return field.Name
}

func setField(f reflect.Value, values []string) error {
	if f.Kind() == reflect.Slice {
		slice := reflect.MakeSlice(f.Type(), len(values), len(values))
		for i, s := range values {
			if err := setScalar(slice.Index(i), s); err != nil {
				return err
			}
		}
		f.Set(slice)
		return nil
	}
	return setScalar(f, values[0])
}

func setScalar(f reflect.Value, s string) error {
	switch f.Kind() {
	case reflect.String:
		f.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(s, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetFloat(n)
	default:
		return fmt.Errorf("unsupported form field type %s", f.Type())
	}
	return nil
}
