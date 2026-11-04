package server

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

type photoInput struct {
	Title string                `json:"title"`
	Photo *multipart.FileHeader `json:"photo"`
}

func (in photoInput) Validate() error {
	if in.Photo == nil {
		return tifa.Fields{"photo": "choose a picture"}
	}
	return nil
}

type photoResult struct {
	Title string `json:"title"`
	Name  string `json:"name"`
	Bytes string `json:"bytes"`
}

func photoHandler(c *tifa.Ctx, in photoInput) (photoResult, error) {
	f, err := in.Photo.Open()
	if err != nil {
		return photoResult{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return photoResult{Title: in.Title, Name: in.Photo.Filename, Bytes: string(b)}, err
}

// multipartBody writes a form with a title and, when name is not empty, a file.
func multipartBody(t *testing.T, title, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if err := w.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		part, err := w.CreateFormFile("photo", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func sendBody(s http.Handler, method, target, contentType string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// The input of a handler that takes files comes as a form: its files fill the file fields, and
// Validate and its 422 work as for any input. JSON is refused.
func TestFileInput(t *testing.T) {
	s := newAppServer(t, App{Routes: []Route{{ID: "app/photos/route", Path: "/photos", POST: InputOutput(photoHandler, BindForm)}}})
	body, contentType := multipartBody(t, "Sea", "sea.png", []byte("png bytes"))
	w := sendBody(s, "POST", "/photos", contentType, body)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"title":"Sea","name":"sea.png","bytes":"png bytes"}` {
		t.Errorf("upload: %d %s", w.Code, w.Body.String())
	}
	body, contentType = multipartBody(t, "Sea", "", nil)
	if w := sendBody(s, "POST", "/photos", contentType, body); w.Code != 422 || !strings.Contains(w.Body.String(), `"photo":"choose a picture"`) {
		t.Errorf("no file: %d %s", w.Code, w.Body.String())
	}
	if w := sendBody(s, "POST", "/photos", "application/json", strings.NewReader(`{"title":"Sea"}`)); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("JSON: %d %s", w.Code, w.Body.String())
	}
}

// BodyLimit of a route replaces server.body_limit for the requests its method handlers answer.
// OnRequest still sees the request before anything reads the body, so it can change it again.
func TestRouteBodyLimit(t *testing.T) {
	read := func(c *tifa.Ctx) error {
		var in struct {
			Title string `json:"title"`
		}
		if err := c.Bind(&in); err != nil {
			return err
		}
		return c.NoContent(204)
	}
	app := App{
		Hooks: Hooks{OnRequest: func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			if c.Header("X-Small") != "" {
				if err := c.SetBodyLimit(1024); err != nil {
					return err
				}
			}
			return next(c)
		}},
		Routes: []Route{
			{ID: "app/big/route", Path: "/big", POST: read, BodyLimit: 2 << 20},
			{ID: "app/small/route", Path: "/small", POST: read},
		},
	}
	s := newAppServer(t, app)
	form := func(n int) io.Reader { return strings.NewReader("title=" + strings.Repeat("x", n)) }
	const urlencoded = "application/x-www-form-urlencoded"
	if w := sendBody(s, "POST", "/big", urlencoded, form(3<<19)); w.Code != 204 {
		t.Errorf("1.5 MiB within the limit of the route: %d %s", w.Code, w.Body.String())
	}
	if w := sendBody(s, "POST", "/big", urlencoded, form(3<<20)); w.Code != 413 {
		t.Errorf("3 MiB over the limit of the route: %d", w.Code)
	}
	if w := sendBody(s, "POST", "/small", urlencoded, form(3<<19)); w.Code != 413 {
		t.Errorf("1.5 MiB on a route with server.body_limit: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/big", form(4096))
	r.Header.Set("Content-Type", urlencoded)
	r.Header.Set("X-Small", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Errorf("OnRequest lowered the limit of the route: %d", w.Code)
	}
}

func TestRegisterRefusesNegativeLimits(t *testing.T) {
	for _, r := range []Route{
		{ID: "app/a/route", Path: "/a", POST: func(*tifa.Ctx) error { return nil }, BodyLimit: -1},
		{ID: "app/a/route", Path: "/a", POST: func(*tifa.Ctx) error { return nil }, ReadTimeout: -time.Second},
	} {
		msg := func() (msg string) {
			defer func() { msg, _ = recover().(string) }()
			newAppServer(t, App{Routes: []Route{r}})
			return ""
		}()
		if !strings.Contains(msg, "app/a/route") {
			t.Errorf("%+v: panic %q", r, msg)
		}
	}
}

// trickleBody sends its parts one at a time with a pause before each, like an upload on a slow line.
type trickleBody struct {
	parts [][]byte
	pause time.Duration
}

func (b *trickleBody) Read(p []byte) (int, error) {
	if len(b.parts) == 0 {
		return 0, io.EOF
	}
	time.Sleep(b.pause)
	n := copy(p, b.parts[0])
	b.parts[0] = b.parts[0][n:]
	if len(b.parts[0]) == 0 {
		b.parts = b.parts[1:]
	}
	return n, nil
}

// ReadTimeout of a route gives the body of its requests that long to arrive, in place of
// server.read_timeout, and the answer server.write_timeout after that.
func TestRouteReadTimeout(t *testing.T) {
	cfg := config.Default()
	cfg.Server.ReadTimeout = 300 * time.Millisecond
	cfg.Server.WriteTimeout = 300 * time.Millisecond
	app := App{Routes: []Route{
		{ID: "app/slow/route", Path: "/slow", POST: InputOutput(photoHandler, BindForm), ReadTimeout: 5 * time.Second},
		{ID: "app/quick/route", Path: "/quick", POST: InputOutput(photoHandler, BindForm)},
	}}
	s := newServer(cfg, app.Hooks)
	t.Cleanup(s.close)
	s.register(app.Layouts, app.Routes)
	srv := httptest.NewUnstartedServer(s)
	srv.Config.ReadTimeout = cfg.Server.ReadTimeout
	srv.Config.WriteTimeout = cfg.Server.WriteTimeout
	srv.Start()
	t.Cleanup(srv.Close)

	post := func(path string) (int, string, error) {
		body, contentType := multipartBody(t, "Sea", "sea.png", []byte(strings.Repeat("png bytes ", 100)))
		data := body.Bytes()
		var parts [][]byte
		for len(data) > 0 {
			n := min(len(data), len(data)/4+1)
			parts = append(parts, data[:n])
			data = data[n:]
		}
		req, err := http.NewRequest("POST", srv.URL+path, &trickleBody{parts: parts, pause: 200 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Origin", srv.URL)
		res, err := srv.Client().Do(req)
		if err != nil {
			return 0, "", err
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		return res.StatusCode, string(b), err
	}
	if status, body, err := post("/slow"); err != nil || status != 200 || !strings.Contains(body, `"title":"Sea"`) {
		t.Errorf("slow upload to a route with ReadTimeout: %d %s %v", status, body, err)
	}
	if status, _, err := post("/quick"); err == nil && status == 200 {
		t.Errorf("slow upload to a route with server.read_timeout went through")
	}
}

func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A multipart body over the memory of the form goes to temporary files, which are gone once the
// request is over, whatever the handler did.
func TestUploadTemporaryFilesAreRemoved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	big := bytes.Repeat([]byte("x"), 33<<20) // over the 32 MiB a form keeps in memory
	var during []string
	handler := func(c *tifa.Ctx, in photoInput) error {
		during = tempFiles(t, dir)
		switch in.Title {
		case "fail":
			return errors.New("storage down")
		case "panic":
			panic("storage down")
		}
		return nil
	}
	app := App{
		Hooks: Hooks{
			OnRequest: func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
				if c.Header("X-Copy") != "" {
					return next(c.WithContext(c.Context()))
				}
				return next(c)
			},
			OnError: func(c *tifa.Ctx, err error) tifa.Error { return tifa.Error{Status: 500, Message: "failed"} },
		},
		Routes: []Route{{ID: "app/photos/route", Path: "/photos", POST: Input(handler, BindForm), BodyLimit: 64 << 20}},
	}
	s := newAppServer(t, app)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		title, copy string
		status      int
	}{{"ok", "", 204}, {"fail", "", 500}, {"panic", "", 500}, {"ok", "1", 204}} {
		during = nil
		body, contentType := multipartBody(t, tc.title, "big.bin", big)
		req, err := http.NewRequest("POST", srv.URL+"/photos", body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Origin", srv.URL)
		if tc.copy != "" {
			req.Header.Set("X-Copy", tc.copy)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("%s copy=%q: status %d, want %d", tc.title, tc.copy, res.StatusCode, tc.status)
		}
		if len(during) == 0 {
			t.Errorf("%s copy=%q: no temporary file while the handler ran", tc.title, tc.copy)
		}
		// net/http removes the files of the request it made after the handler returned, which may
		// be just after the answer arrived.
		left := tempFiles(t, dir)
		for deadline := time.Now().Add(2 * time.Second); len(left) > 0 && time.Now().Before(deadline); left = tempFiles(t, dir) {
			time.Sleep(10 * time.Millisecond)
		}
		if len(left) > 0 {
			t.Errorf("%s copy=%q: left %v", tc.title, tc.copy, left)
		}
	}
}
