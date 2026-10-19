package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/klauspost/compress/gzhttp"
	"github.com/klauspost/compress/zstd"
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// bigPage is the HTML of the fake render in these tests, large enough to be compressed.
func bigPage(in ssr.Input) (string, error) {
	return "<html>" + in.URL + strings.Repeat("<p>tifa renders this page</p>", 200) + "</html>", nil
}

// newBigServer is newFrontServer with pages of bigPage, built with cfg.
func newBigServer(t *testing.T, cfg config.Config, app App) *Server {
	t.Helper()
	s := New(cfg, app.Hooks)
	s.register(app.Layouts, app.Routes)
	if err := s.setFront(dist, (&renders{result: bigPage}).render); err != nil {
		t.Fatal(err)
	}
	return s
}

// decoded returns the body of w as the client reads it.
func decoded(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	switch w.Header().Get("Content-Encoding") {
	case "":
		return w.Body.String()
	case "gzip":
		r, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	case "zstd":
		r, err := zstd.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatalf("unknown Content-Encoding %q", w.Header().Get("Content-Encoding"))
	return ""
}

func varies(w *httptest.ResponseRecorder, name string) bool {
	for _, v := range w.Header().Values("Vary") {
		for _, field := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(field), name) {
				return true
			}
		}
	}
	return false
}

// A page goes out in the encoding the client prefers, zstd when it takes zstd and gzip alike.
// One page goes out in several encodings, so its ETag is weak in each of them, and the 304 of
// If-None-Match carries the same ETag as the 200 (RFC 9110, 15.4.5).
func TestPagesAreCompressed(t *testing.T) {
	s := newBigServer(t, config.Default(), frontApp())
	plain := get(s, "/projects/p1")
	if plain.Header().Get("Content-Encoding") != "" || !varies(plain, "Accept-Encoding") || !strings.HasPrefix(plain.Header().Get("ETag"), `W/"`) {
		t.Fatalf("without Accept-Encoding: %v", plain.Header())
	}
	for _, tc := range []struct{ accept, want string }{
		{"gzip", "gzip"},
		{"gzip, deflate, br, zstd", "zstd"},
		{"zstd;q=0.5, gzip", "gzip"},
		{"br", ""},
		{"gzip;q=0", ""},
		{"identity", ""},
	} {
		w := get(s, "/projects/p1", "Accept-Encoding", tc.accept)
		if got := w.Header().Get("Content-Encoding"); w.Code != 200 || got != tc.want {
			t.Errorf("%s: %d, Content-Encoding %q, want %q", tc.accept, w.Code, got, tc.want)
			continue
		}
		if body := decoded(t, w); body != plain.Body.String() {
			t.Errorf("%s: the page reads %q", tc.accept, body)
		}
		if !varies(w, "Accept-Encoding") || !varies(w, "X-Tifa-Route") {
			t.Errorf("%s: Vary %v", tc.accept, w.Header().Values("Vary"))
		}
		etag := w.Header().Get("ETag")
		if etag != plain.Header().Get("ETag") || tc.want != "" && w.Header().Get("Content-Length") != "" {
			t.Errorf("%s: ETag %q, Content-Length %q", tc.accept, etag, w.Header().Get("Content-Length"))
		}
		for _, sent := range []string{etag, strings.TrimPrefix(etag, "W/")} {
			again := get(s, "/projects/p1", "Accept-Encoding", tc.accept, "If-None-Match", sent)
			if again.Code != 304 || again.Body.Len() != 0 || again.Header().Get("ETag") != etag {
				t.Errorf("%s: If-None-Match %s: %d %v %q", tc.accept, sent, again.Code, again.Header(), again.Body.String())
			}
		}
	}
}

// Navigation data and JSON answers are compressed too, and answers under the threshold are not.
func TestJSONIsCompressedFromTheThreshold(t *testing.T) {
	app := frontApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/big", Path: "/big", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return strings.Repeat("order ", 400), nil },
			POST: func(c *tifa.Ctx) error { return c.JSON(200, strings.Repeat("order ", 400)) }},
	)
	s := newBigServer(t, config.Default(), app)
	data := get(s, "/big", "Accept-Encoding", "gzip", "X-Tifa-Route", "app/big", "X-Tifa-Build", "b1")
	if data.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(decoded(t, data), `"app/big":{"status":200,"data":"order order`) {
		t.Errorf("navigation data: %v", data.Header())
	}
	r := httptest.NewRequest("POST", "/big", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	api := httptest.NewRecorder()
	s.ServeHTTP(api, r)
	if api.Code != 200 || api.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("API answer: %d %v", api.Code, api.Header())
	}
	small := get(s, "/projects/p1", "Accept-Encoding", "gzip", "X-Tifa-Route", "app/layout", "X-Tifa-Build", "b1")
	if small.Header().Get("Content-Encoding") != "" || small.Header().Get("Content-Length") != strconv.Itoa(small.Body.Len()) || !varies(small, "Accept-Encoding") {
		t.Errorf("small answer: %v", small.Header())
	}
}

// Only text formats are compressed: a PNG is compressed already.
func TestOnlyTextIsCompressed(t *testing.T) {
	cfg := config.Default()
	s := New(cfg, Hooks{})
	s.Handle("GET", "/image", func(c *tifa.Ctx) error {
		c.SetHeader("Content-Type", "image/png")
		_, err := c.Writer().Write(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1000))
		return err
	})
	s.Handle("GET", "/feed", func(c *tifa.Ctx) error {
		c.SetHeader("Content-Type", "application/rss+xml")
		_, err := io.WriteString(c.Writer(), strings.Repeat("<item/>", 500))
		return err
	})
	for target, want := range map[string]string{"/image": "", "/feed": "gzip"} {
		w := get(s, target, "Accept-Encoding", "gzip")
		if got := w.Header().Get("Content-Encoding"); got != want {
			t.Errorf("%s: Content-Encoding %q, want %q", target, got, want)
		}
	}
}

// An ETag the application sets turns weak when the answer is compressed, and a weak one stays.
func TestApplicationETagsTurnWeakWhenCompressed(t *testing.T) {
	s := New(config.Default(), Hooks{})
	for path, etag := range map[string]string{"/strong": `"v1"`, "/weak": `W/"v1"`} {
		s.Handle("GET", path, func(c *tifa.Ctx) error {
			c.SetHeader("ETag", etag)
			return c.Text(200, strings.Repeat("tifa ", 500))
		})
	}
	for path, want := range map[string]string{"/strong": `W/"v1"`, "/weak": `W/"v1"`} {
		if w := get(s, path, "Accept-Encoding", "gzip"); w.Header().Get("ETag") != want || w.Header().Get("Content-Encoding") != "gzip" {
			t.Errorf("%s: %v", path, w.Header())
		}
	}
	if w := get(s, "/strong"); w.Header().Get("ETag") != `"v1"` {
		t.Errorf("uncompressed: ETag %q", w.Header().Get("ETag"))
	}
}

// An application that sets Vary itself, as Cookie in OnRequest, keeps Accept-Encoding in it:
// a page from the VM, a cached page, a 304 and a text answer. A Vary that names it already, or
// is *, stays as it is.
func TestVaryKeepsAcceptEncoding(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		c.SetHeader("Vary", c.Request().URL.Query().Get("vary"))
		return c.Next()
	}
	s := newBigServer(t, config.Default(), app)
	s.Handle("GET", "/api", func(c *tifa.Ctx) error {
		c.SetHeader("Vary", "Origin")
		return c.Text(200, strings.Repeat("tifa ", 1000))
	})
	count := func(w *httptest.ResponseRecorder) (n int) {
		for _, v := range w.Header().Values("Vary") {
			for _, field := range strings.Split(v, ",") {
				if strings.EqualFold(strings.TrimSpace(field), "Accept-Encoding") {
					n++
				}
			}
		}
		return n
	}
	page := get(s, "/projects/p1?vary=Cookie", "Accept-Encoding", "gzip")
	for name, w := range map[string]*httptest.ResponseRecorder{
		"page":              page,
		"uncompressed page": get(s, "/projects/p1?vary=Cookie"),
		"304":               get(s, "/projects/p1?vary=Cookie", "Accept-Encoding", "gzip", "If-None-Match", page.Header().Get("ETag")),
		"cache miss":        get(s, "/stats?vary=Cookie", "Accept-Encoding", "gzip"),
		"cache hit":         get(s, "/stats?vary=Cookie", "Accept-Encoding", "gzip"),
		"text":              get(s, "/api", "Accept-Encoding", "gzip"),
		"named":             get(s, "/projects/p1?vary=Cookie,+accept-encoding", "Accept-Encoding", "gzip"),
	} {
		if count(w) != 1 || !varies(w, "Cookie") && !varies(w, "Origin") {
			t.Errorf("%s: %d, Vary %q", name, w.Code, w.Header().Values("Vary"))
		}
	}
	if w := get(s, "/projects/p1?vary=*", "Accept-Encoding", "gzip"); count(w) != 0 || !varies(w, "*") {
		t.Errorf("Vary %q, want * without Accept-Encoding", w.Header().Values("Vary"))
	}
}

// With server.compress off nothing is compressed and nothing varies by Accept-Encoding.
func TestCompressOff(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Compress = false
	s := newBigServer(t, cfg, frontApp())
	w := get(s, "/projects/p1", "Accept-Encoding", "gzip, zstd")
	if w.Header().Get("Content-Encoding") != "" || varies(w, "Accept-Encoding") || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) ||
		!strings.HasPrefix(w.Header().Get("ETag"), `"`) {
		t.Errorf("%v", w.Header())
	}
	if again := get(s, "/projects/p1", "If-None-Match", w.Header().Get("ETag")); again.Code != 304 || again.Header().Get("ETag") != w.Header().Get("ETag") {
		t.Errorf("304: %d %v", again.Code, again.Header())
	}
}

// With compression off, No-Gzip-Compression is removed before the answer goes out too, as
// gzhttp removes it when compression is on.
func TestCompressOffDropsNoGzipCompression(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		c.SetHeader(gzhttp.HeaderNoCompression, "1")
		return c.Next()
	}
	cfg := config.Default()
	cfg.Server.Compress = false
	s := newBigServer(t, cfg, app)
	s.Handle("GET", "/api", func(c *tifa.Ctx) error { return c.Text(200, "ok") })
	s.Handle("GET", "/stream", func(c *tifa.Ctx) error { return http.NewResponseController(c.Writer()).Flush() })
	for _, path := range []string{"/projects/p1", "/stats", "/stats", "/api", "/stream", "/nowhere"} {
		if w := get(s, path, "Accept-Encoding", "gzip"); w.Header().Get(gzhttp.HeaderNoCompression) != "" {
			t.Errorf("%s: %d %v", path, w.Code, w.Header())
		}
	}
}

// Over a real connection: HEAD is not compressed, a flushed stream
// reaches the client before the handler returns, and a handler can still take the connection.
func TestCompressionOverTheWire(t *testing.T) {
	s := newBigServer(t, config.Default(), frontApp())
	flushed := make(chan struct{})
	var release sync.Once
	s.Handle("GET", "/events", func(c *tifa.Ctx) error {
		c.SetHeader("Content-Type", "text/event-stream")
		io.WriteString(c.Writer(), "data: first\n\n")
		if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
			return err
		}
		<-flushed
		return nil
	})
	s.Handle("GET", "/hijack", func(c *tifa.Ctx) error {
		conn, rw, err := http.NewResponseController(c.Writer()).Hijack()
		if err != nil {
			return err
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nhi")
		return rw.Flush()
	})
	srv := httptest.NewServer(s)
	defer srv.Close()
	defer release.Do(func() { close(flushed) }) // before Close, which waits for the handler
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	do := func(method, path string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	get, head := do("GET", "/projects/p1"), do("HEAD", "/projects/p1")
	getBody, _ := io.ReadAll(get.Body)
	headBody, _ := io.ReadAll(head.Body)
	get.Body.Close()
	head.Body.Close()
	if get.Header.Get("Content-Encoding") != "gzip" || len(getBody) == 0 {
		t.Errorf("GET: %v, %d bytes", get.Header, len(getBody))
	}
	// gzhttp, nginx and Traefik leave HEAD uncompressed, so HEAD answers as it did before.
	if head.StatusCode != 200 || len(headBody) != 0 || head.Header.Get("Content-Encoding") != "" ||
		head.Header.Get("ETag") != get.Header.Get("ETag") || head.Header.Get("Content-Length") == "" {
		t.Errorf("HEAD: %d %v, %d bytes", head.StatusCode, head.Header, len(headBody))
	}

	events := do("GET", "/events")
	defer events.Body.Close()
	zr, err := gzip.NewReader(events.Body)
	if err != nil {
		t.Fatalf("event stream: %v %v", events.Header, err)
	}
	line, err := bufio.NewReader(zr).ReadString('\n')
	release.Do(func() { close(flushed) })
	if err != nil || line != "data: first\n" {
		t.Errorf("first event: %q, %v", line, err)
	}

	hijacked := do("GET", "/hijack")
	body, _ := io.ReadAll(hijacked.Body)
	hijacked.Body.Close()
	if string(body) != "hi" {
		t.Errorf("hijacked connection: %d %q", hijacked.StatusCode, body)
	}
}

// A file of the browser bundle that tifa build compressed goes out as its .gz file, with the
// type and cache of the file itself, to a GET that accepts gzip. Anything else gets the file.
func TestBundleFilesGoOutPrecompressed(t *testing.T) {
	script := strings.Repeat("console.log('tifa');\n", 200)
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	zw.Write([]byte(script))
	zw.Close()
	files := fstest.MapFS{
		"assets/app-2.js":     {Data: []byte(script)},
		"assets/app-2.js.gz":  {Data: gz.Bytes()},
		"public/notes.txt":    {Data: []byte(script)},
		"public/notes.txt.gz": {Data: []byte("not the notes")},
		"public/download.gz":  {Data: gz.Bytes()},
	}
	for name, file := range dist {
		files[name] = file
	}
	for _, compress := range []bool{true, false} {
		cfg := config.Default()
		cfg.Server.Compress = compress
		s := New(cfg, Hooks{})
		if err := s.setFront(files, nil); err != nil {
			t.Fatal(err)
		}
		w := get(s, "/assets/app-2.js", "Accept-Encoding", "gzip")
		h := w.Header()
		if !compress {
			if h.Get("Content-Encoding") != "" || w.Body.String() != script {
				t.Errorf("compress off: %v", h)
			}
			continue
		}
		if w.Code != 200 || h.Get("Content-Encoding") != "gzip" || !bytes.Equal(w.Body.Bytes(), gz.Bytes()) ||
			h.Get("Content-Length") != strconv.Itoa(gz.Len()) || !strings.HasPrefix(h.Get("Content-Type"), "text/javascript") ||
			h.Get("Cache-Control") != "public, max-age=31536000, immutable" || !varies(w, "Accept-Encoding") {
			t.Errorf("gzip: %d %v, %d bytes", w.Code, h, w.Body.Len())
		}
		// A range of the compressed file, as nginx and Caddy answer.
		if w := get(s, "/assets/app-2.js", "Accept-Encoding", "gzip", "Range", "bytes=0-9"); w.Code != 206 ||
			w.Header().Get("Content-Encoding") != "gzip" || !bytes.Equal(w.Body.Bytes(), gz.Bytes()[:10]) {
			t.Errorf("range: %d %v", w.Code, w.Header())
		}
		// Without gzip the file goes out as it is, or compressed while it is sent.
		for accept, want := range map[string]string{"": "", "gzip;q=0": "", "zstd": "zstd"} {
			w := get(s, "/assets/app-2.js", "Accept-Encoding", accept)
			if w.Header().Get("Content-Encoding") != want || decoded(t, w) != script {
				t.Errorf("%q: %v", accept, w.Header())
			}
		}
		head := httptest.NewRequest("HEAD", "/assets/app-2.js", nil)
		head.Header.Set("Accept-Encoding", "gzip")
		hw := httptest.NewRecorder()
		s.ServeHTTP(hw, head)
		if hw.Header().Get("Content-Encoding") != "" || hw.Header().Get("Content-Length") != strconv.Itoa(len(script)) {
			t.Errorf("HEAD: %v", hw.Header())
		}
		// Files of the public folder are the app's own: a .gz next to one is a file of its own.
		if w := get(s, "/notes.txt", "Accept-Encoding", "gzip"); decoded(t, w) != script {
			t.Errorf("public file: %v", w.Header())
		}
		if w := get(s, "/download.gz", "Accept-Encoding", "gzip"); w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), gz.Bytes()) {
			t.Errorf("public .gz file: %v", w.Header())
		}
	}
}

// zstd compresses at its default level, as Caddy and Traefik do, not at gzhttp's fastest,
// whose pages come out larger than gzip's.
func TestZstdUsesItsDefaultLevel(t *testing.T) {
	app := frontApp()
	s := New(config.Default(), app.Hooks)
	s.register(app.Layouts, app.Routes)
	// A table of orders, varied the way real pages are, where the levels differ.
	r := rand.New(rand.NewPCG(1, 2))
	var page strings.Builder
	for i := range 600 {
		fmt.Fprintf(&page, `<tr><td>SH-%d</td><td>%s</td><td>¥%d.%02d</td></tr>`, 1000+i, []string{"林夏", "王小明", "陈一", "周末"}[r.IntN(4)], r.IntN(900), r.IntN(100))
	}
	if err := s.setFront(dist, (&renders{result: func(ssr.Input) (string, error) { return page.String(), nil }}).render); err != nil {
		t.Fatal(err)
	}
	plain := []byte(page.String())
	w := get(s, "/projects/p1", "Accept-Encoding", "zstd")
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(1),
		zstd.WithLowerEncoderMem(true), zstd.WithWindowSize(128<<10))
	if err != nil {
		t.Fatal(err)
	}
	if want := enc.EncodeAll(plain, nil); w.Header().Get("Content-Encoding") != "zstd" || !bytes.Equal(w.Body.Bytes(), want) {
		t.Errorf("zstd answer of %d bytes, want the %d of the default level", w.Body.Len(), len(want))
	}
}

// A stream that sends its header and then waits, as server-sent events do, gets the header at
// once: a Flush before any byte of the body sends the answer uncompressed. gzhttp alone would
// hold the header until the first bytes, since it picks the encoding from them.
func TestFlushBeforeTheBodySendsTheHeader(t *testing.T) {
	s := New(config.Default(), Hooks{})
	release := make(chan struct{})
	var once sync.Once
	stream := func(writeHeader bool) func(*tifa.Ctx) error {
		return func(c *tifa.Ctx) error {
			c.SetHeader("Content-Type", "text/event-stream")
			if writeHeader {
				c.Writer().WriteHeader(200)
			}
			if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
				return err
			}
			<-release
			_, err := io.WriteString(c.Writer(), "data: "+strings.Repeat("tifa ", 400)+"\n\n")
			return err
		}
	}
	s.Handle("GET", "/events", stream(true))
	s.Handle("GET", "/events-no-status", stream(false))
	srv := httptest.NewServer(s)
	defer srv.Close()
	defer once.Do(func() { close(release) }) // before Close, which waits for the handlers
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableCompression: true}}
	for _, path := range []string{"/events", "/events-no-status"} {
		for _, accept := range []string{"gzip", "zstd", "zstd, gzip"} {
			req, _ := http.NewRequest("GET", srv.URL+path, nil)
			req.Header.Set("Accept-Encoding", accept)
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s with %s: no header: %v", path, accept, err)
			}
			if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" ||
				res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get(gzhttp.HeaderNoCompression) != "" {
				t.Errorf("%s with %s: %d %v", path, accept, res.StatusCode, res.Header)
			}
			res.Body.Close()
		}
	}
	once.Do(func() { close(release) })
}

// A cached page keeps its gzip and zstd forms, so a hit is sent as stored, with its length,
// instead of being compressed again. Clients that take neither, HEAD and No-Gzip-Compression
// get the HTML.
func TestCacheHitsSendTheirStoredCompression(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		if c.Request().URL.Query().Has("plain") {
			c.SetHeader(gzhttp.HeaderNoCompression, "1")
		}
		return c.Next()
	}
	s := New(config.Default(), app.Hooks)
	s.register(app.Layouts, app.Routes)
	r := &renders{result: bigPage}
	if err := s.setFront(dist, r.render); err != nil {
		t.Fatal(err)
	}
	first := get(s, "/stats", "Accept-Encoding", "gzip")
	html := decoded(t, first)
	for _, tc := range []struct{ path, accept, want string }{
		{"/stats", "gzip", "gzip"},
		{"/stats", "gzip, zstd", "zstd"},
		{"/stats", "zstd;q=0.5, gzip", "gzip"},
		{"/stats", "br", ""},
		{"/stats", "", ""},
		{"/stats?plain", "gzip", ""},
		{"/stats?plain", "gzip", ""},
	} {
		w := get(s, tc.path, "Accept-Encoding", tc.accept)
		h := w.Header()
		if h.Get("Content-Encoding") != tc.want || h.Get("Content-Length") != strconv.Itoa(w.Body.Len()) ||
			h.Get("Content-Type") != "text/html; charset=utf-8" || !strings.HasPrefix(h.Get("ETag"), `W/"`) ||
			!varies(w, "Accept-Encoding") || h.Get(gzhttp.HeaderNoCompression) != "" {
			t.Errorf("%s with %q: %v", tc.path, tc.accept, h)
		}
		if body := decoded(t, w); !strings.HasPrefix(body, "<html>"+tc.path) || len(body) != len(html)+len(tc.path)-len("/stats") {
			t.Errorf("%s with %q: the page reads %q", tc.path, tc.accept, body)
		}
	}
	if w := get(s, "/stats", "Accept-Encoding", "zstd"); w.Header().Get("X-Tifa-Render") != "cache" || r.count() != 2 {
		t.Errorf("%d renders, render %q", r.count(), w.Header().Get("X-Tifa-Render"))
	}
	head := httptest.NewRequest("HEAD", "/stats", nil)
	head.Header.Set("Accept-Encoding", "gzip")
	hw := httptest.NewRecorder()
	s.ServeHTTP(hw, head)
	if hw.Header().Get("Content-Encoding") != "" || hw.Header().Get("Content-Length") != strconv.Itoa(len(html)) {
		t.Errorf("HEAD: %v", hw.Header())
	}
}

// A cache hit picks its stored form the way gzhttp picks the encoding of any other answer, for
// every Accept-Encoding, odd q values among them.
func TestCacheHitsPickTheEncodingOfGzhttp(t *testing.T) {
	s := newBigServer(t, config.Default(), frontApp())
	for i, accept := range []string{
		"gzip", "zstd", "gzip, zstd", "zstd;q=0.5, gzip", "gzip;q=0.5, zstd", "gzip;q=2, zstd",
		"zstd;q=2, gzip;q=1", "gzip;q=1.5, zstd;q=1.2", "zstd;q=inf, gzip", "gzip;q=-1, zstd;q=0.1",
		"gzip;q=NaN", "gzip;q=NaN, zstd", "gzip;q=abc, zstd;q=0.1", "gzip;q=0.001", "gzip;q=0, zstd;q=0",
		"gzip, gzip;q=0", "GZIP", "x-gzip", "*", "*, gzip;q=0", "identity;q=0", "br", "",
	} {
		live := get(s, "/projects/p1", "Accept-Encoding", accept)
		path := fmt.Sprintf("/stats?i=%d", i)
		get(s, path, "Accept-Encoding", accept)
		hit := get(s, path, "Accept-Encoding", accept)
		if hit.Header().Get("X-Tifa-Render") != "cache" || hit.Header().Get("Content-Encoding") != live.Header().Get("Content-Encoding") {
			t.Errorf("%q: gzhttp gives %q, the cache hit %q", accept, live.Header().Get("Content-Encoding"), hit.Header().Get("Content-Encoding"))
		}
	}
}

// A miss of the cache sends the form it compressed for the cache, with its length, instead of
// compressing the page once more. A page the cache cannot hold, with cache.size 0 or larger than
// the cache, is compressed as it is sent, like a page without Cache. A page whose HTML fits but
// not its forms is kept as HTML.
func TestCacheMissesSendTheFormTheyStore(t *testing.T) {
	html := decoded(t, get(newBigServer(t, config.Default(), frontApp()), "/stats"))
	for _, tc := range []struct {
		size       int64
		missStored bool // the miss sends a stored form
		hit        bool
	}{
		{64 << 20, true, true},
		{0, false, false},
		{int64(len(html)) - 1, false, false},
		{int64(len(html)) + 10, true, true}, // the forms are larger than 10 bytes
	} {
		cfg := config.Default()
		cfg.Cache.Size = tc.size
		s := newBigServer(t, cfg, frontApp())
		for i, want := range []bool{tc.missStored, tc.size == 64<<20} {
			w := get(s, "/stats", "Accept-Encoding", "gzip")
			if decoded(t, w) != html || w.Header().Get("Content-Encoding") != "gzip" ||
				(w.Header().Get("Content-Length") != "") != want || (w.Header().Get("X-Tifa-Render") == "cache") != (i == 1 && tc.hit) {
				t.Errorf("cache.size %d, request %d: %v", tc.size, i, w.Header())
			}
		}
	}
}

// A miss for a client that takes no compression, HEAD or No-Gzip-Compression stores the HTML
// alone, since it sends the HTML anyway. The first client that takes compression compresses the
// page into its forms, and the hits after it send them. A page whose forms do not fit is not
// compressed for the cache again.
func TestCacheCompressesForTheFirstClientThatTakesIt(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		if c.Header("X-Plain") != "" {
			c.SetHeader(gzhttp.HeaderNoCompression, "1")
		}
		return c.Next()
	}
	stored := func(s *Server) cachedPage {
		t.Helper()
		s.cache.mu.Lock()
		defer s.cache.mu.Unlock()
		if len(s.cache.pages) != 1 {
			t.Fatalf("%d pages cached", len(s.cache.pages))
		}
		return s.cache.order.Front().Value.(*cached).page
	}
	html := decoded(t, get(newBigServer(t, config.Default(), app), "/stats"))
	for _, tc := range []struct {
		name  string
		size  int64
		first func(*Server)
	}{
		{"no Accept-Encoding", 64 << 20, func(s *Server) { get(s, "/stats") }},
		{"No-Gzip-Compression", 64 << 20, func(s *Server) { get(s, "/stats", "Accept-Encoding", "gzip", "X-Plain", "1") }},
		{"HEAD", 64 << 20, func(s *Server) {
			r := httptest.NewRequest("HEAD", "/stats", nil)
			r.Header.Set("Accept-Encoding", "gzip")
			s.ServeHTTP(httptest.NewRecorder(), r)
		}},
		{"forms that do not fit", int64(len(html)) + 10, func(s *Server) { get(s, "/stats") }},
	} {
		cfg := config.Default()
		cfg.Cache.Size = tc.size
		s := newBigServer(t, cfg, app)
		tc.first(s)
		if p := stored(s); p.gzip != nil || p.zstd != nil || p.compressed {
			t.Errorf("%s: the miss compressed the page for the cache", tc.name)
		}
		fits := tc.size == 64<<20
		// The first hit sends the form it compressed, kept or not.
		for i := range 2 {
			w := get(s, "/stats", "Accept-Encoding", "gzip")
			if w.Header().Get("X-Tifa-Render") != "cache" || w.Header().Get("Content-Encoding") != "gzip" ||
				(w.Header().Get("Content-Length") != "") != (fits || i == 0) || decoded(t, w) != html {
				t.Errorf("%s, hit %d with gzip: %v", tc.name, i, w.Header())
			}
		}
		if p := stored(s); (p.gzip != nil) != fits || (p.zstd != nil) != fits || !p.compressed {
			t.Errorf("%s: forms %t %t, compressed %t after clients that take gzip", tc.name, p.gzip != nil, p.zstd != nil, p.compressed)
		}
	}
}
