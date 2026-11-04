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

func TestBindRefusesTextForAFile(t *testing.T) {
	body, contentType := formBody(t, [][2]string{{"title", "Sea"}, {"photo", "not a file"}}, nil)
	c, _ := newCtx("POST", body.String(), contentType, 1<<20)
	var in upload
	var e Error
	if err := c.Bind(&in); !errors.As(err, &e) || e.Status != 400 || e.Message != `invalid form field "photo"` {
		t.Fatalf("got %v", err)
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
