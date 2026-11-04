package tifatest

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
)

// File returns the *multipart.FileHeader of an uploaded file named name holding data, as a method
// handler gets it in a field of its input, for a test that calls the handler or Validate directly.
// Its Content-Type follows the extension of name, application/octet-stream when the extension says
// nothing. A file over 32 MiB is kept in a temporary file, as on the server, which is removed when
// the test ends.
func File(t testing.TB, name string, data []byte) *multipart.FileHeader {
	t.Helper()
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	contentType, _, _ = strings.Cut(contentType, ";")
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", multipartDisposition(name))
	h.Set("Content-Type", contentType)
	part, err := w.CreatePart(h)
	if err == nil {
		_, err = part.Write(data)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		t.Fatalf("tifatest: writing the file %s: %v", name, err)
	}
	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(32 << 20)
	if err != nil {
		t.Fatalf("tifatest: reading the file %s: %v", name, err)
	}
	t.Cleanup(func() {
		if err := form.RemoveAll(); err != nil {
			t.Errorf("tifatest: removing the temporary file of %s: %v", name, err)
		}
	})
	return form.File["file"][0]
}

// multipartDisposition is the Content-Disposition of a file part, quoted as multipart.Writer
// quotes it.
func multipartDisposition(name string) string {
	quote := strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace
	return `form-data; name="file"; filename="` + quote(name) + `"`
}
