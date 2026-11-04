package core

import (
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
	return c.bindForm(mediaType, v)
}

// BindForm is Bind for the input of a method handler that takes files: the body must be a form,
// multipart or URL-encoded, and JSON is answered with 415 like any other content type.
func BindForm(c *Ctx, v any) error {
	mediaType, _, _ := mime.ParseMediaType(c.r.Header.Get("Content-Type"))
	return c.bindForm(mediaType, v)
}

func (c *Ctx) bindForm(mediaType string, v any) error {
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
	return decodeForm(c.r.PostForm, files, v, "form field")
}

// BindQuery decodes the query string into the struct v, with the names and value types of a
// form body. Query parameters that name no field are ignored. A query string that does not parse,
// or a value that does not, comes back as Error with status 400.
func (c *Ctx) BindQuery(v any) error {
	// URL.Query drops the parameters it cannot parse, so a broken one would look absent.
	query, err := url.ParseQuery(c.r.URL.RawQuery)
	if err != nil {
		return publicError(http.StatusBadRequest, "invalid query string")
	}
	return decodeForm(query, nil, v, "query parameter")
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

// decodeForm fills the exported fields of a struct. A field is named by its form tag, then its
// json tag, then its Go name. files are the files of a multipart form, which fill the fields of
// type *multipart.FileHeader (the first file) and []*multipart.FileHeader. what names a value in
// errors, such as "form field".
func decodeForm(form url.Values, files map[string][]*multipart.FileHeader, v any, what string) error {
	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tifa: decoding a %s needs a pointer to a struct, got %T", what, v)
	}
	target = target.Elem()
	for i := range target.NumField() {
		field := target.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		name := formName(field)
		if name == "-" {
			continue
		}
		if filePointer(field.Type) || field.Type.Kind() == reflect.Slice && filePointer(field.Type.Elem()) {
			if err := setFiles(target.Field(i), files[name], form[name]); err != nil {
				return publicError(http.StatusBadRequest, fmt.Sprintf("invalid %s %q", what, name))
			}
			continue
		}
		values, ok := form[name]
		if !ok {
			continue
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
