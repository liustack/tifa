package core

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

type upload struct {
	Title  string                  `json:"title"`
	Photo  *multipart.FileHeader   `json:"photo"`
	Photos []*multipart.FileHeader `json:"photos"`
	Cover  *multipart.FileHeader   `json:"cover"`
}

// formBody writes a multipart form: the values as text parts, then the files as file parts
// (name, file name, content). A file name of "" writes the empty part a browser sends for a file
// input with no file chosen.
func formBody(t *testing.T, values [][2]string, files [][3]string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, v := range values {
		if err := w.WriteField(v[0], v[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+f[0]+`"; filename="`+f[1]+`"`)
		h.Set("Content-Type", "application/octet-stream")
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, f[2]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func content(t *testing.T, f *multipart.FileHeader) string {
	t.Helper()
	r, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBindFillsTheFilesOfAMultipartForm(t *testing.T) {
	for _, bind := range []func(*Ctx, any) error{(*Ctx).Bind, BindForm} {
		body, contentType := formBody(t, [][2]string{{"title", "Sea"}},
			[][3]string{{"photo", "sea.png", "png bytes"}, {"photos", "a.jpg", "a"}, {"photos", "b.jpg", "bb"}, {"cover", "", ""}})
		c, _ := newCtx("POST", body.String(), contentType, 1<<20)
		var in upload
		if err := bind(c, &in); err != nil {
			t.Fatal(err)
		}
		if in.Title != "Sea" || in.Photo == nil || in.Photo.Filename != "sea.png" || content(t, in.Photo) != "png bytes" {
			t.Fatalf("photo: %+v", in)
		}
		if len(in.Photos) != 2 || content(t, in.Photos[0]) != "a" || content(t, in.Photos[1]) != "bb" {
			t.Fatalf("photos: %+v", in.Photos)
		}
		// The browser sends a file input with no file as an empty part without a file name.
		if in.Cover != nil {
			t.Fatalf("cover of an empty part = %+v, want nil", in.Cover)
		}
	}
}

type (
	namedFiles []*multipart.FileHeader
	namedPhoto *multipart.FileHeader
)

// A named type whose underlying type is a file, or a slice of files, is filled as the analyzer
// takes it: as files.
func TestBindFillsNamedFileTypes(t *testing.T) {
	body, contentType := formBody(t, nil, [][3]string{{"files", "a.jpg", "a"}, {"files", "b.jpg", "bb"}, {"cover", "c.png", "c"}, {"extras", "d.png", "d"}})
	c, _ := newCtx("POST", body.String(), contentType, 1<<20)
	var in struct {
		Files  namedFiles   `json:"files"`
		Cover  namedPhoto   `json:"cover"`
		Extras []namedPhoto `json:"extras"`
	}
	if err := BindForm(c, &in); err != nil {
		t.Fatal(err)
	}
	if len(in.Files) != 2 || content(t, in.Files[1]) != "bb" || in.Cover == nil || content(t, in.Cover) != "c" || len(in.Extras) != 1 || content(t, in.Extras[0]) != "d" {
		t.Fatalf("named files: %+v", in)
	}
}

// The multipart reader stops at the closing boundary, so data sent after it counts toward the
// body limit too: a body over the limit is 413 before the handler, wherever the bytes are.
func TestBindCountsDataAfterTheClosingBoundary(t *testing.T) {
	for _, bind := range []func(*Ctx, any) error{(*Ctx).Bind, BindForm} {
		body, contentType := formBody(t, [][2]string{{"title", "Sea"}}, [][3]string{{"photo", "sea.png", "png bytes"}})
		form := body.Len()
		body.WriteString(strings.Repeat("x", 64<<10))
		c, _ := newCtx("POST", body.String(), contentType, int64(form+1024))
		var in upload
		var e Error
		if err := bind(c, &in); !errors.As(err, &e) || e.Status != http.StatusRequestEntityTooLarge {
			t.Fatalf("data after the closing boundary over the limit: %v", err)
		}
		// Within the limit, what follows the form is left out and the form is read.
		body, contentType = formBody(t, [][2]string{{"title", "Sea"}}, nil)
		body.WriteString("trailing")
		c, _ = newCtx("POST", body.String(), contentType, 1<<20)
		in = upload{}
		if err := bind(c, &in); err != nil || in.Title != "Sea" {
			t.Fatalf("data after the closing boundary within the limit: %v %+v", err, in)
		}
	}
}

func TestBindRefusesTextForAFile(t *testing.T) {
	body, contentType := formBody(t, [][2]string{{"title", "Sea"}, {"photo", "not a file"}}, nil)
	c, _ := newCtx("POST", body.String(), contentType, 1<<20)
	var in upload
	var e Error
	if err := c.Bind(&in); !errors.As(err, &e) || e.Status != 400 || e.Message != `invalid form field "photo"` {
		t.Fatalf("got %v", err)
	}
}

// textTags writes itself as one string, so its TypeScript type is a string.
type textTags []string

func (t textTags) MarshalText() ([]byte, error)  { return []byte(strings.Join(t, ",")), nil }
func (t *textTags) UnmarshalText(b []byte) error { *t = strings.Split(string(b), ","); return nil }

// textIDs is one string too, for a slice of numbers.
type textIDs []int

func (ids textIDs) MarshalText() ([]byte, error)  { return nil, nil }
func (ids *textIDs) UnmarshalText(b []byte) error { return nil }

// textLevel reads itself from text on its pointer.
type textLevel int

func (l *textLevel) UnmarshalText(b []byte) error { return nil }

// A query string or a form is decoded by kind, and text methods are never called: the analyzer
// refuses an input field whose type, or its element, has one. The decoder refuses the same fields,
// as a mistake in the program and not in the request, whether or not the value came: a slice
// that reads itself as text would otherwise get "a,b" as one element.
func TestFormDecodingRefusesTextMethods(t *testing.T) {
	type tags struct {
		Tags textTags `json:"tags"`
	}
	type ids struct {
		IDs textIDs `json:"ids"`
	}
	type levels struct {
		Levels []textLevel `json:"levels"`
	}
	type level struct {
		Level textLevel `json:"level"`
	}
	type raw struct {
		Raw []byte `json:"raw"`
	}
	type nested struct {
		Inner struct{ A string } `json:"inner"`
	}
	for _, in := range []any{&tags{}, &ids{}, &levels{}, &level{}, &raw{}, &nested{}} {
		for _, query := range []string{"tags=a,b&ids=1,2&levels=1&level=1&raw=1&inner=x", ""} {
			c, _ := newCtx("GET", "", "", 1<<20)
			c.r.URL.RawQuery = query
			var e Error
			err := c.BindQuery(in)
			if err == nil || errors.As(err, &e) || !strings.Contains(err.Error(), "query string") {
				t.Errorf("BindQuery %T from %q: got %v, %+v", in, query, err, in)
			}
		}
		body, contentType := formBody(t, [][2]string{{"tags", "a,b"}, {"ids", "1,2"}}, nil)
		c, _ := newCtx("POST", body.String(), contentType, 1<<20)
		var e Error
		if err := BindForm(c, in); err == nil || errors.As(err, &e) {
			t.Errorf("BindForm %T: got %v, %+v", in, err, in)
		}
	}
}

// Bind takes a form for an input that also comes as JSON, so its other fields may be anything: a
// form fills the fields it can and answers 400 for a value of any other one, a field with text
// methods included, which would otherwise get "a,b" as one element.
func TestBindFormLeavesFieldsAFormDoesNotFill(t *testing.T) {
	type input struct {
		Title string   `json:"title"`
		Tags  textTags `json:"tags"`
		Inner struct {
			A string
		} `json:"inner"`
	}
	c, _ := newCtx("POST", "title=Sea", "application/x-www-form-urlencoded", 1<<20)
	var in input
	if err := c.Bind(&in); err != nil || in.Title != "Sea" {
		t.Fatalf("got %v, %+v", err, in)
	}
	for _, body := range []string{"title=Sea&tags=a,b", "title=Sea&inner=x"} {
		c, _ := newCtx("POST", body, "application/x-www-form-urlencoded", 1<<20)
		var in input
		var e Error
		if err := c.Bind(&in); !errors.As(err, &e) || e.Status != 400 {
			t.Errorf("%s: got %v, %+v", body, err, in)
		}
	}
}

// BindForm decodes the input of a handler that takes files: a form, and never JSON. A form without
// a file leaves the file fields empty.
func TestBindFormTakesFormsOnly(t *testing.T) {
	c, _ := newCtx("POST", `{"title":"Sea"}`, "application/json", 1<<20)
	var in upload
	var e Error
	if err := BindForm(c, &in); !errors.As(err, &e) || e.Status != http.StatusUnsupportedMediaType {
		t.Fatalf("JSON body: %v", err)
	}
	c, _ = newCtx("POST", "title=Sea", "application/x-www-form-urlencoded", 1<<20)
	in = upload{}
	if err := BindForm(c, &in); err != nil || in.Title != "Sea" || in.Photo != nil || in.Photos != nil {
		t.Fatalf("URL-encoded form: %v %+v", err, in)
	}
}

// deadlineBody fails the way a connection does once its read deadline has passed.
type deadlineBody struct{ r io.Reader }

func (b deadlineBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, &os.PathError{Op: "read", Path: "conn", Err: os.ErrDeadlineExceeded}
	}
	return n, err
}

func (deadlineBody) Close() error { return nil }

// A body that stops coming because the read deadline passed is answered with 408, not 400, so the
// sender can tell a slow upload from a broken one.
func TestBindReportsAReadDeadlineAs408(t *testing.T) {
	// The body stops in the middle of the file, where a slow upload stops.
	body, contentType := formBody(t, [][2]string{{"title", "Sea"}}, [][3]string{{"photo", "sea.png", strings.Repeat("png bytes ", 1000)}})
	half := body.Bytes()[:body.Len()/2]
	for _, tc := range []struct{ body, contentType string }{
		{string(half), contentType},
		{`{"title":"Se`, "application/json"},
		{"title=Se", "application/x-www-form-urlencoded"},
	} {
		r := httptest.NewRequest("POST", "/photos", nil)
		r.Header.Set("Content-Type", tc.contentType)
		r.Body = deadlineBody{strings.NewReader(tc.body)}
		c := New(httptest.NewRecorder(), r, "/photos", 1<<20)
		var in upload
		var e Error
		if err := c.Bind(&in); !errors.As(err, &e) || e.Status != http.StatusRequestTimeout {
			t.Errorf("%s: got %v", tc.contentType, err)
		}
	}
}
