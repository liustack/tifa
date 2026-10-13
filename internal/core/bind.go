package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Bind decodes the request body into v by Content-Type: JSON, URL-encoded form or multipart form.
// Client mistakes come back as Error with status 400, 413 or 415.
func (c *Ctx) Bind(v any) error {
	mediaType, _, _ := mime.ParseMediaType(c.r.Header.Get("Content-Type"))
	switch mediaType {
	case "application/json":
		if err := json.NewDecoder(c.r.Body).Decode(v); err != nil {
			return bodyError(err, "invalid JSON body")
		}
		return nil
	case "application/x-www-form-urlencoded", "multipart/form-data":
		var err error
		if mediaType == "multipart/form-data" {
			err = c.r.ParseMultipartForm(32 << 20)
		} else {
			err = c.r.ParseForm()
		}
		if err != nil {
			return bodyError(err, "invalid form body")
		}
		return decodeForm(c.r.PostForm, v)
	default:
		return publicError(http.StatusUnsupportedMediaType, "")
	}
}

func bodyError(err error, message string) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return publicError(http.StatusRequestEntityTooLarge, "")
	}
	return publicError(http.StatusBadRequest, message)
}

// decodeForm fills the exported fields of a struct. A field is named by its form tag, then its json tag, then its Go name.
func decodeForm(form url.Values, v any) error {
	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tifa: Bind needs a pointer to a struct for form bodies, got %T", v)
	}
	target = target.Elem()
	for i := range target.NumField() {
		field := target.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		name := formName(field)
		values, ok := form[name]
		if name == "-" || !ok {
			continue
		}
		if err := setField(target.Field(i), values); err != nil {
			return publicError(http.StatusBadRequest, fmt.Sprintf("invalid form field %q", name))
		}
	}
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
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
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
