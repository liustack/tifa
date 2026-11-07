package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pngHead is the signature of a PNG file, which is what http.DetectContentType looks for.
var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// uploadForm writes a multipart form of title and, when name is not empty, one file named name
// in the part "file".
func uploadForm(t *testing.T, title, name string, data []byte) (body []byte, contentType string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		part, err := w.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(data)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// post sends body to url as contentType, as the generated function does, and returns the status
// and the body of the answer.
func post(t *testing.T, url, contentType string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Tifa-Call", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(data)
}

// TestUploadApp builds test/apps/upload, a page that uploads pictures through the generated
// function of a handler whose input holds a *multipart.FileHeader, and whose route.go raises the
// body limit with BodyLimit = an option of the app. The server keeps a body limit of 8 KiB and
// the upload route gets 64 KiB.
func TestUploadApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "upload")
	typecheck(t, dir)
	files := t.TempDir()
	base, _, _ := start(t, exe, "--server-body-limit", "8KiB", "--upload-limit", "64KiB", "--upload-dir", files)

	// A picture of 32 KiB, over the limit of the server and under the one of the route.
	picture := append(append([]byte{}, pngHead...), bytes.Repeat([]byte{7}, 32<<10)...)
	body, contentType := uploadForm(t, " Sea ", "sea.png", picture)
	status, answer := post(t, base+"/photos", contentType, body)
	var saved struct {
		Result struct{ ID, Title string }
	}
	if err := json.Unmarshal([]byte(answer), &saved); status != 200 || err != nil || saved.Result.Title != "Sea" || !strings.HasSuffix(saved.Result.ID, ".png") {
		t.Fatalf("upload: %d %s (%v)", status, answer, err)
	}
	status, header, served := get(t, base+"/photos/"+saved.Result.ID)
	if status != 200 || header.Get("Content-Type") != "image/png" || served != string(picture) {
		t.Errorf("served picture: %d %v, %d bytes", status, header, len(served))
	}
	if status, _, page := get(t, base+"/photos"); status != 200 || !strings.Contains(page, `<img src="/photos/`+saved.Result.ID+`" alt="Sea"`) {
		t.Errorf("page: %d %.600s", status, page)
	}

	// A body over the limit of the route is refused before the handler runs.
	body, contentType = uploadForm(t, "Big", "big.png", append(append([]byte{}, pngHead...), make([]byte, 65<<10)...))
	if status, answer := post(t, base+"/photos", contentType, body); status != 413 {
		t.Errorf("over the limit: %d %s", status, answer)
	}
	// An input that holds a file comes as a form, never as JSON.
	if status, answer := post(t, base+"/photos", "application/json", []byte(`{"title":"Sea"}`)); status != 415 {
		t.Errorf("JSON: %d %s", status, answer)
	}
	// Validate sees the missing file and title, and the handler the kind of the file.
	body, contentType = uploadForm(t, "", "", nil)
	if status, answer := post(t, base+"/photos", contentType, body); status != 422 || !strings.Contains(answer, `"file":"Choose a picture."`) || !strings.Contains(answer, `"title":"Give the picture a title."`) {
		t.Errorf("no file: %d %s", status, answer)
	}
	body, contentType = uploadForm(t, "Text", "a.png", []byte("just text"))
	if status, answer := post(t, base+"/photos", contentType, body); status != 422 || !strings.Contains(answer, `"file":"Only JPEG, PNG, WebP and GIF pictures."`) {
		t.Errorf("text named a.png: %d %s", status, answer)
	}
	// A text value in the place of the file is no file.
	body, contentType = uploadFormText(t, "Sea", "file", "sea.png")
	if status, answer := post(t, base+"/photos", contentType, body); status != 400 {
		t.Errorf("file name as text: %d %s", status, answer)
	}

	t.Run("browser", func(t *testing.T) { uploadInBrowser(t, base, picture) })
}

// uploadFormText writes a multipart form of title and a text field name holding value.
func uploadFormText(t *testing.T, title, name, value string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("title", title)
	w.WriteField(name, value)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// uploadInBrowser picks a picture in the form of the page in a real browser, sends it with the
// generated function, and checks that the list shows it without the page loading again.
func uploadInBrowser(t *testing.T, base string, picture []byte) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	pw, dir := openBrowser(t, "tifa-e2e-upload")
	// playwright-cli reads files only in the folder it runs in, by the path without symbolic links.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "harbour.png")
	if err := os.WriteFile(path, picture, 0o644); err != nil {
		t.Fatal(err)
	}
	pw("open", base+"/photos")
	text := func(script string) string {
		t.Helper()
		out := pw("eval", script)
		_, result, ok := strings.Cut(out, "### Result\n")
		if !ok {
			t.Fatalf("no result in %s", out)
		}
		var v string
		if err := json.Unmarshal([]byte(strings.SplitN(result, "\n", 2)[0]), &v); err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		return v
	}
	ref := func(line string) string {
		t.Helper()
		out := pw("snapshot")
		for _, l := range strings.Split(out, "\n") {
			if i := strings.Index(l, "[ref="); strings.Contains(l, line) && i >= 0 {
				return l[i+5 : i+5+strings.Index(l[i+5:], "]")]
			}
		}
		t.Fatalf("no %q in the snapshot:\n%s", line, out)
		return ""
	}
	waitFor(t, "the page to hydrate", func() bool {
		return text(`() => String(!document.querySelector('#upload button').disabled)`) == "true"
	})
	pw("eval", `() => { window.__stays = "same document"; return 1 }`)

	// Sent empty: Validate answers with a message under each input.
	pw("click", ref(`button "Upload"`))
	waitFor(t, "the messages", func() bool {
		return text(`() => [...document.querySelectorAll('.error')].map((e) => e.textContent).join('|')`) == "Give the picture a title.|Choose a picture."
	})

	pw("fill", ref(`textbox "Title"`), "Harbour")
	pw("click", ref(`button "Picture"`))
	if out := pw("upload", path); strings.Contains(out, "### Error") {
		t.Fatalf("upload: %s", out)
	}
	pw("click", ref(`button "Upload"`))
	waitFor(t, "the list to show the picture", func() bool {
		return strings.Contains(text(`() => document.getElementById('photos').innerHTML`), `alt="Harbour"`)
	})
	if got := text(`() => [String(window.__stays), String(document.querySelectorAll('.error').length), String(performance.getEntriesByType('navigation').length)].join(' ')`); got != "same document 0 1" {
		t.Errorf("after the upload: %s", got)
	}
}
