package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

const chrome = "gzip, deflate, br, zstd" // what Chrome sends

// countCompressions counts the pages compressPage compresses until the test ends.
func countCompressions(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	onCompressPage = func() { n.Add(1) }
	t.Cleanup(func() { onCompressPage = nil })
	return &n
}

// keptPages returns the pages the cache keeps, most recently used first.
func keptPages(s *server) []cachedPage {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	var out []cachedPage
	for e := s.cache.order.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value.(*cached).page)
	}
	return out
}

func lengthOf(w *httptest.ResponseRecorder) string { return strconv.Itoa(w.Body.Len()) }

// The cache keeps one form of a page. With server.compress on, a page of at least gzhttp's minimum
// size is kept as gzip, made once by the miss whatever the request takes, and the HTML is gone. The
// request that rendered the page gets what it asked for, and so does each hit.
func TestACachedPageIsKeptAsGzipAlone(t *testing.T) {
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	for _, tc := range []struct {
		name, accept, encoding string
	}{
		{"identity", "", ""},
		{"gzip", "gzip", "gzip"},
		{"chrome", chrome, "gzip"},
		{"zstd", "zstd", "zstd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := countCompressions(t)
			s := newBigServer(t, config.Default(), frontApp())
			miss := get(s, "/stats", "Accept-Encoding", tc.accept)
			if miss.Header().Get("X-Tifa-Render") != "vm" || miss.Header().Get("Content-Encoding") != tc.encoding || decoded(t, miss) != html {
				t.Fatalf("miss: %v", miss.Header())
			}
			kept := keptPages(s)
			if len(kept) != 1 || kept[0].gzip == nil || kept[0].html != "" || kept[0].n != len(html) || n.Load() != 1 {
				t.Fatalf("kept %d pages %+v, %d compressions", len(kept), kept[0], n.Load())
			}
			if cap(kept[0].gzip) != len(kept[0].gzip) || s.cache.used != int64(len(kept[0].gzip)+len(kept[0].policy)) {
				t.Errorf("charged %d for %d bytes of gzip (cap %d) and %d of policy", s.cache.used, len(kept[0].gzip), cap(kept[0].gzip), len(kept[0].policy))
			}
		})
	}
}

// A gzip hit sends the stored bytes as they are, with their length.
func TestAGzipHitSendsTheStoredBytes(t *testing.T) {
	n := countCompressions(t)
	s := newBigServer(t, config.Default(), frontApp())
	get(s, "/stats", "Accept-Encoding", "gzip")
	stored := keptPages(s)[0].gzip
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	for _, accept := range []string{"gzip", chrome} {
		hit := get(s, "/stats", "Accept-Encoding", accept)
		if hit.Header().Get("X-Tifa-Render") != "cache" || hit.Header().Get("Content-Encoding") != "gzip" || !bytes.Equal(hit.Body.Bytes(), stored) ||
			hit.Header().Get("Content-Length") != strconv.Itoa(len(stored)) || !strings.HasPrefix(hit.Header().Get("ETag"), `W/"`) || !varies(hit, "Accept-Encoding") {
			t.Errorf("gzip hit with %q: %v", accept, hit.Header())
		}
		if decoded(t, hit) != html {
			t.Errorf("gzip hit with %q: the page reads differently", accept)
		}
	}
	if n.Load() != 1 {
		t.Errorf("%d compressions", n.Load())
	}
}

// A client that takes no compression gets the exact HTML with its Content-Length from a gzip entry,
// and the entry stays as it was. HEAD and No-Gzip-Compression are such clients. HEAD says what GET
// says and sends no body.
func TestAnIdentityHitUnzipsTheEntry(t *testing.T) {
	n := countCompressions(t)
	s := newBigServer(t, config.Default(), frontApp())
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	get(s, "/stats", "Accept-Encoding", "gzip")
	before := keptPages(s)[0]
	for _, accept := range []string{"", "br", "gzip;q=0", "identity"} {
		w := get(s, "/stats", "Accept-Encoding", accept)
		if w.Header().Get("X-Tifa-Render") != "cache" || w.Header().Get("Content-Encoding") != "" || w.Body.String() != html ||
			w.Header().Get("Content-Length") != strconv.Itoa(len(html)) || !varies(w, "Accept-Encoding") || !strings.HasPrefix(w.Header().Get("ETag"), `W/"`) {
			t.Errorf("identity hit with %q: %v", accept, w.Header())
		}
	}
	get1 := get(s, "/stats")
	head := httptest.NewRequest("HEAD", "/stats", nil)
	head.Header.Set("Accept-Encoding", "gzip")
	hw := httptest.NewRecorder()
	s.ServeHTTP(hw, head)
	for _, name := range []string{"Content-Length", "Content-Encoding", "Content-Type", "ETag", "Vary"} {
		if hw.Header().Get(name) != get1.Header().Get(name) {
			t.Errorf("HEAD %s %q, GET %q", name, hw.Header().Get(name), get1.Header().Get(name))
		}
	}
	if hw.Body.Len() != 0 {
		t.Errorf("HEAD sent %d bytes", hw.Body.Len())
	}
	after := keptPages(s)[0]
	if !bytes.Equal(after.gzip, before.gzip) || &after.gzip[0] != &before.gzip[0] || after.html != "" || n.Load() != 1 {
		t.Errorf("the entry changed, %d compressions", n.Load())
	}
}

// A client that ranks zstd above gzip, or takes no gzip, gets zstd, made from the gzip entry as it
// is sent: no Content-Length, nothing stored.
func TestAZstdHitIsMadeFromTheGzipEntry(t *testing.T) {
	n := countCompressions(t)
	s := newBigServer(t, config.Default(), frontApp())
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	get(s, "/stats", "Accept-Encoding", "gzip")
	before := keptPages(s)[0]
	for _, accept := range []string{"zstd", "zstd;q=1, gzip;q=0.5", "gzip;q=0, zstd"} {
		w := get(s, "/stats", "Accept-Encoding", accept)
		if w.Header().Get("X-Tifa-Render") != "cache" || w.Header().Get("Content-Encoding") != "zstd" || decoded(t, w) != html ||
			w.Header().Get("Content-Length") != "" || !varies(w, "Accept-Encoding") || !strings.HasPrefix(w.Header().Get("ETag"), `W/"`) {
			t.Errorf("zstd hit with %q: %v", accept, w.Header())
		}
	}
	if after := keptPages(s)[0]; &after.gzip[0] != &before.gzip[0] || after.html != "" || n.Load() != 1 || s.cache.used != int64(len(before.gzip)+len(before.policy)) {
		t.Errorf("the entry changed, %d compressions", n.Load())
	}
}

// If-None-Match is answered without unzipping, with the headers of a 200.
func TestA304NeedsNoUnzipping(t *testing.T) {
	s := newBigServer(t, config.Default(), frontApp())
	etag := get(s, "/stats", "Accept-Encoding", "gzip").Header().Get("ETag")
	page := keptPages(s)[0]
	page.gzip[0] ^= 0xff // a hit that unzipped it would fail
	defer func() { page.gzip[0] ^= 0xff }()
	for _, accept := range []string{"", "gzip", "zstd"} {
		w := get(s, "/stats", "Accept-Encoding", accept, "If-None-Match", etag)
		if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != etag || !varies(w, "Accept-Encoding") {
			t.Errorf("304 with %q: %d %v", accept, w.Code, w.Header())
		}
	}
}

// A stored page that does not unzip is our own bytes gone bad. The answer is cut off, not hidden.
func TestABrokenEntryAbortsTheConnection(t *testing.T) {
	s := newBigServer(t, config.Default(), frontApp())
	get(s, "/stats", "Accept-Encoding", "gzip")
	page := keptPages(s)[0]
	for i := len(page.gzip) / 2; i < len(page.gzip)/2+16; i++ {
		page.gzip[i] ^= 0xff
	}
	for _, accept := range []string{"", "zstd"} {
		func() {
			defer func() {
				if p := recover(); p != http.ErrAbortHandler {
					t.Errorf("with %q: recovered %v, want http.ErrAbortHandler", accept, p)
				}
			}()
			get(s, "/stats", "Accept-Encoding", accept)
		}()
	}
}

// Requests that share one render each get the form they take, and the page is compressed once.
// The entry the render published is the one gzip body.
func TestSharedRenderCompressesOnce(t *testing.T) {
	n := countCompressions(t)
	s, g := newHeldServer(t, config.Default(), frontApp())
	accepts := []string{"", "gzip", "br", chrome, "zstd", "gzip;q=0"}
	wants := []string{"", "gzip", "", "gzip", "zstd", ""}
	out := burst(t, s, g, 30, "/stats", func(i int) []string { return []string{"Accept-Encoding", accepts[i%len(accepts)]} })
	page, _ := bigPage(ssr.Input{URL: "/stats"})
	for i, w := range out {
		enc := wants[i%len(wants)]
		if w.Header().Get("Content-Encoding") != enc || decoded(t, w) != page {
			t.Errorf("request %d with %q: %v", i, accepts[i%len(accepts)], w.Header())
		}
		if enc == "" && w.Header().Get("Content-Length") != strconv.Itoa(len(page)) {
			t.Errorf("request %d with %q: Content-Length %q", i, accepts[i%len(accepts)], w.Header().Get("Content-Length"))
		}
	}
	if kept := keptPages(s); g.renders() != 1 || n.Load() != 1 || len(kept) != 1 || kept[0].gzip == nil || kept[0].html != "" {
		t.Errorf("%d renders, %d compressions", g.renders(), n.Load())
	}
}

// With server.compress off, or for a page under gzhttp's minimum size, the cache keeps the HTML
// and nothing is compressed for it.
func TestAPageThatIsNotCompressedIsKeptAsHTML(t *testing.T) {
	n := countCompressions(t)
	off := config.Default()
	off.Server.Compress = false
	s := newBigServer(t, off, frontApp())
	html := get(s, "/stats", "Accept-Encoding", "gzip").Body.String()
	if w := get(s, "/stats", "Accept-Encoding", "gzip"); w.Body.String() != html || w.Header().Get("Content-Encoding") != "" || w.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("compress off: %v", w.Header())
	}
	if kept := keptPages(s); len(kept) != 1 || kept[0].gzip != nil || kept[0].html != html || s.cache.used != int64(len(html)+len(kept[0].policy)) {
		t.Errorf("compress off kept %+v", kept[0])
	}
	small, _ := newFrontServer(t, frontApp())
	get(small, "/stats", "Accept-Encoding", "gzip")
	if kept := keptPages(small); len(kept) != 1 || kept[0].gzip != nil || kept[0].html == "" {
		t.Errorf("a small page kept %+v", kept[0])
	}
	if n.Load() != 0 {
		t.Errorf("%d compressions", n.Load())
	}
}

// The cache admits a page for what it keeps: a page whose HTML is larger than the cache but whose
// gzip form fits is kept, one whose form does not fit is not, and cache.size 0 compresses nothing.
func TestAdmissionCountsTheKeptForm(t *testing.T) {
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	gz := compressPage(html)
	for _, tc := range []struct {
		size int64
		kept bool
	}{
		{int64(len(gz)) + 600, true}, // the policy of the page is in it too
		{int64(len(html)) - 1, true},
		{int64(len(gz)) - 1, false},
		{0, false},
	} {
		n := countCompressions(t)
		cfg := config.Default()
		cfg.Cache.Size = tc.size
		s := newBigServer(t, cfg, frontApp())
		get(s, "/stats", "Accept-Encoding", "gzip")
		w := get(s, "/stats", "Accept-Encoding", "gzip")
		if (w.Header().Get("X-Tifa-Render") == "cache") != tc.kept || len(keptPages(s)) != map[bool]int{true: 1}[tc.kept] || decoded(t, w) != html {
			t.Errorf("cache.size %d: %v, %d pages", tc.size, w.Header(), len(keptPages(s)))
		}
		if s.cache.used > tc.size {
			t.Errorf("cache.size %d: %d charged", tc.size, s.cache.used)
		}
		if tc.size == 0 && n.Load() != 0 {
			t.Errorf("cache.size 0: %d compressions", n.Load())
		}
	}
}

// A page with Cache goes out in each encoding with the same tag as the page the render made, from a
// hit of the render cache as from the miss.
func TestPageWithCacheKeepsGzipAndTheSameTags(t *testing.T) {
	routes := func(app App) App {
		app.Routes = append(app.Routes,
			Route{ID: "app/cached", Path: "/cached", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"}},
		)
		return app
	}
	off := config.Default()
	off.Server.Compress = false
	on, plain := newBigServer(t, config.Default(), routes(frontApp())), newBigServer(t, off, routes(frontApp()))
	want := get(plain, "/cached")
	html := want.Body.String()
	for _, accept := range []string{"gzip", "", "zstd", "gzip"} {
		w := get(on, "/cached", "Accept-Encoding", accept)
		if w.Code != 200 || decoded(t, w) != html || w.Header().Get("ETag") != "W/"+want.Header().Get("ETag") {
			t.Errorf("with %q: %d %v, ETag want W/%s", accept, w.Code, w.Header(), want.Header().Get("ETag"))
		}
	}
	if hit := get(on, "/cached"); hit.Header().Get("X-Tifa-Render") != "cache" || hit.Header().Get("Content-Length") != strconv.Itoa(len(html)) {
		t.Errorf("hit: %v", hit.Header())
	}
}

// What the stored gzip reads is the HTML the render made.
func TestStoredGzipIsTheRender(t *testing.T) {
	s := newBigServer(t, config.Default(), frontApp())
	get(s, "/stats")
	r, err := gzip.NewReader(bytes.NewReader(keptPages(s)[0].gzip))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	out.ReadFrom(r)
	if want, _ := bigPage(ssr.Input{URL: "/stats"}); out.String() != want {
		t.Error("the stored form is not the page")
	}
}

// The zstd form of a hit is made as it is sent, so a Content-Length set before it, by the app or
// a middleware, is dropped: it would give the length of something else.
func TestAZstdHitDropsAContentLength(t *testing.T) {
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	page := cachedPage{gzip: compressPage(html), n: len(html)}
	r := httptest.NewRequest("GET", "/stats", nil)
	w := httptest.NewRecorder()
	c := core.New(w, r, "/stats", config.Default().Server.BodyLimit)
	w.Header().Set("Content-Length", "12")
	if err := writeCached(c, 200, page, "zstd"); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length %q on a zstd hit", got)
	}
}
