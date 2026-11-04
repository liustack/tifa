package photos

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"

	"uploads"
)

func TestValidate(t *testing.T) {
	in := UploadInput{Title: "Sea", File: tifatest.File(t, "sea.png", []byte("\x89PNG\r\n\x1a\n"))}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	in.File = nil
	var fields tifa.Fields
	if err := in.Validate(); !errors.As(err, &fields) || fields["file"] == "" {
		t.Fatalf("no file: %v", err)
	}
}

func TestPOSTRefusesText(t *testing.T) {
	uploads.UploadDir = t.TempDir()
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("POST", "/photos", nil))
	_, err := POST(c, UploadInput{Title: "Text", File: tifatest.File(t, "a.png", []byte("just text"))})
	var fields tifa.Fields
	if !errors.As(err, &fields) || fields["file"] != "Only JPEG, PNG, WebP and GIF pictures." {
		t.Fatalf("text named a.png: %v", err)
	}
}
