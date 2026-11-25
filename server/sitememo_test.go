package server

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	internalcontent "github.com/liustack/tifa/internal/content"
)

// siteEntry is an entry of a collection as SitemapPage.Content takes it.
type siteEntry struct{ internalcontent.Handle }

// readCounter is the dist of the entries of a test: it counts the files the Markdown is read from,
// and holds each read until gate is closed, when there is one.
type readCounter struct {
	files fstest.MapFS
	reads atomic.Int32
	gate  chan struct{}
}

func (r *readCounter) Open(name string) (fs.File, error) {
	r.reads.Add(1)
	if r.gate != nil {
		<-r.gate
	}
	return r.files.Open(name)
}

// entry makes the entry id of the collection notes in language, with the Markdown text.
func (r *readCounter) entry(language, id, text string) siteEntry {
	file := "content/notes/" + language + "/" + id + ".html"
	if r.files == nil {
		r.files = fstest.MapFS{}
	}
	r.files[internalcontent.MarkdownFile(file)] = &fstest.MapFile{Data: []byte(text)}
	return siteEntry{internalcontent.NewHandle(&internalcontent.Body{Collection: "notes", Language: language, ID: id, Dist: r, File: file})}
}

// theSite is the sitemap function of a test: it counts its calls and returns what sm holds now.
type theSite struct {
	sm    tifa.Sitemap
	err   error
	calls atomic.Int32
}

func (t *theSite) fn(*tifa.Ctx) (tifa.Sitemap, error) {
	t.calls.Add(1)
	return t.sm, t.err
}

// staleLog collects what the server logs and counts the lines of one mistake that every list of
// the tests makes: an Exclude pattern of no page.
type staleLog struct{ buf *bytes.Buffer }

func captureLog(t *testing.T) staleLog {
	t.Helper()
	l := staleLog{&bytes.Buffer{}}
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	slog.SetDefault(slog.New(slog.NewTextHandler(l.buf, nil)))
	return l
}

func (l staleLog) count() int {
	return strings.Count(l.buf.String(), "Exclude patterns that match no page")
}

func noteApp() App {
	app := siteApp()
	return app
}

// noteSite is a server for a site with the notes a (zh-CN) and hi (en-US), the sitemap function
// listing them, and the log of its mistakes.
func noteSite(t *testing.T, cfg *config.Config) (*server, *theSite, *readCounter, staleLog) {
	t.Helper()
	log := captureLog(t)
	app := noteApp()
	var s *server
	if cfg == nil {
		s = newSiteServer(t, app, siteDist())
	} else {
		s = newServer(*cfg, app.Hooks)
		t.Cleanup(s.close)
		s.setI18n(app.I18n)
		s.setSite(app.SiteURL)
		s.register(app.Layouts, app.Routes)
		if err := s.setFront(siteDist(), (&renders{}).render); err != nil {
			t.Fatal(err)
		}
	}
	files := &readCounter{}
	site := &theSite{sm: tifa.Sitemap{
		Name: "Shop", Exclude: []string{"/projects", "/nope"},
		Pages: []tifa.SitemapPage{
			{Language: "zh-CN", Path: "/store/journal/a", Title: "A", Content: files.entry("zh-CN", "a", "text of a\n")},
			{Language: "en-US", Path: "/store/journal/hi", Title: "Hi", Content: files.entry("en-US", "hi", "text of hi\n")},
		},
	}}
	s.sitemap = site.fn
	return s, site, files, log
}

func sameHeaders(t *testing.T, what string, a, b http.Header, names ...string) {
	t.Helper()
	for _, n := range names {
		if a.Get(n) != b.Get(n) {
			t.Errorf("%s: %s is %q then %q", what, n, a.Get(n), b.Get(n))
		}
	}
}

// The function and OnRequest run on every request, and the answer is made again only when what
// the function returns changes.
func TestSiteFilesAreNotMadeAgainForTheSameList(t *testing.T) {
	s, site, files, log := noteSite(t, nil)
	var onRequest atomic.Int32
	s.hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		onRequest.Add(1)
		return next(c)
	}
	for _, p := range []string{"/sitemap.xml", "/llms.txt", "/llms-full.txt", "/store/journal/a.md", "/en-US/store/journal/hi.md"} {
		callsBefore, readsBefore, hooksBefore := site.calls.Load(), files.reads.Load(), onRequest.Load()
		first := get(s, p)
		if first.Code != 200 {
			t.Fatalf("GET %s: %d %s", p, first.Code, first.Body.String())
		}
		for range 2 {
			again := get(s, p)
			if again.Code != 200 || again.Body.String() != first.Body.String() {
				t.Errorf("GET %s again: %d, the body differs", p, again.Code)
			}
			sameHeaders(t, p, first.Header(), again.Header(), "Content-Type", "Content-Length", "Link", "ETag", "Cache-Control", "Vary", "Content-Encoding")
		}
		if got := site.calls.Load() - callsBefore; got != 3 {
			t.Errorf("GET %s: the function ran %d times for 3 requests", p, got)
		}
		if got := onRequest.Load() - hooksBefore; got != 3 {
			t.Errorf("GET %s: OnRequest ran %d times for 3 requests", p, got)
		}
		if got := files.reads.Load() - readsBefore; strings.HasSuffix(p, ".md") && got != 1 || p == "/llms-full.txt" && got != 2 || strings.HasSuffix(p, ".xml") && got != 0 {
			t.Errorf("GET %s: %d Markdown reads", p, got)
		}
	}
	if w := get(s, "/store/journal/a.md"); w.Header().Get("Link") != `<https://shop.example.com/store/journal/a>; rel="canonical"` || w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" || w.Body.String() != "# A\n\ntext of a\n" {
		t.Errorf("a kept .md: %v %q", w.Header(), w.Body.String())
	}
	// The pages of a list are checked once: one list for zh-CN, the language of the site files and of
	// the first .md, one for en-US.
	if log.count() != 2 {
		t.Errorf("the mistake of the lists was logged %d times", log.count())
	}
	if st, _ := s.memo.stats(); st.Entries != 5 || st.Misses != 5 || st.Hits != 11 {
		t.Errorf("memo %+v", st)
	}
}

// A change of the list shows at once, whatever it is, with nothing kept for a time.
func TestChangedListGivesTheNewAnswerAtOnce(t *testing.T) {
	s, site, files, _ := noteSite(t, nil)
	files.entry("zh-CN", "b", "text of b\n")
	step := func(change func(), path, want string) {
		t.Helper()
		change()
		if w := get(s, path); w.Code != 200 || !strings.Contains(decoded(t, w), want) {
			t.Errorf("GET %s after the change, want %q: %d %s", path, want, w.Code, w.Body.String())
		}
	}
	get(s, "/llms-full.txt")
	get(s, "/llms.txt")
	get(s, "/sitemap.xml")
	get(s, "/store/journal/a.md")
	step(func() {
		site.sm.Pages = append(site.sm.Pages, tifa.SitemapPage{Language: "zh-CN", Path: "/store/journal/new", Title: "New one"})
	}, "/llms.txt", "[New one]")
	step(func() { site.sm.Pages[0].Title = "A changed" }, "/llms-full.txt", "# A changed\n")
	step(func() { site.sm.Pages[0].Title = "A again" }, "/store/journal/a.md", "# A again\n")
	step(func() { site.sm.Pages[0].Modified = time.Date(2026, 11, 3, 1, 2, 3, 0, time.UTC) }, "/sitemap.xml", "<lastmod>2026-11-03T01:02:03Z</lastmod>")
	step(func() { site.sm.Pages[0].Description = "Fresh words" }, "/llms.txt", ": Fresh words\n")
	step(func() { site.sm.Name = "Other shop" }, "/llms.txt", "# Other shop\n")
	step(func() { site.sm.Summary = "A summary." }, "/llms-full.txt", "> A summary.\n")
	step(func() { site.sm.Pages[0].Content = files.entry("zh-CN", "b", "text of b\n") }, "/store/journal/a.md", "text of b\n")
	step(func() { site.sm.Exclude = []string{"/projects", "/store/cart"} }, "/sitemap.xml", "<loc>https://shop.example.com/store</loc>")
	if w := get(s, "/sitemap.xml"); strings.Contains(w.Body.String(), "/store/cart<") {
		t.Errorf("the page excluded is in the sitemap: %s", w.Body.String())
	}
	// The same page in the other language is another answer.
	if w := get(s, "/en-US/store/journal/hi.md"); !strings.Contains(w.Body.String(), "text of hi") {
		t.Errorf("GET hi.md: %s", w.Body.String())
	}
	site.sm.Pages[1].Language, site.sm.Pages[1].Path = "zh-CN", "/store/journal/hi"
	if w := get(s, "/en-US/store/journal/hi.md"); w.Code != 404 {
		t.Errorf("GET hi.md after it left en-US: %d", w.Code)
	}
}

func TestSiteFilesHaveAnETagAnd304(t *testing.T) {
	s, site, files, log := noteSite(t, nil)
	for _, p := range []string{"/sitemap.xml", "/llms.txt", "/llms-full.txt", "/store/journal/a.md"} {
		w := get(s, p)
		tag := w.Header().Get("ETag")
		if !strings.HasPrefix(tag, `W/"`) {
			t.Fatalf("GET %s: ETag %q", p, tag)
		}
		callsBefore, readsBefore, logsBefore := site.calls.Load(), files.reads.Load(), log.count()
		w = get(s, p, "If-None-Match", tag)
		if w.Code != 304 || w.Body.Len() != 0 {
			t.Errorf("GET %s with its ETag: %d %q", p, w.Code, w.Body.String())
		}
		if site.calls.Load() != callsBefore+1 {
			t.Errorf("GET %s with its ETag: the function ran %d times", p, site.calls.Load()-callsBefore)
		}
		if files.reads.Load() != readsBefore || log.count() != logsBefore {
			t.Errorf("GET %s with its ETag made the answer again", p)
		}
		if got := get(s, p, "If-None-Match", `"other"`); got.Code != 200 || got.Header().Get("ETag") != tag {
			t.Errorf("GET %s with another ETag: %d %q", p, got.Code, got.Header().Get("ETag"))
		}
		site.sm.Pages[0].Title += "!"
		if got := get(s, p, "If-None-Match", tag); got.Code != 200 || got.Header().Get("ETag") == tag {
			t.Errorf("GET %s with the ETag of the old list: %d %q", p, got.Code, got.Header().Get("ETag"))
		}
	}
	// Two files of one list have two ETags, and an answer that is no 200 has none.
	a, b := get(s, "/llms.txt").Header().Get("ETag"), get(s, "/llms-full.txt").Header().Get("ETag")
	if a == b {
		t.Errorf("llms.txt and llms-full.txt share the ETag %s", a)
	}
	if w := get(s, "/store/journal/nope.md"); w.Code != 404 || w.Header().Get("ETag") != "" {
		t.Errorf("a 404: %d %v", w.Code, w.Header())
	}
}

// bigText is a Markdown text that gzip makes smaller and that is over gzhttp's minimum size.
func bigText() string { return strings.Repeat("A line of text for the page.\n", 200) }

func TestSiteFilesAreKeptInTheFormTheMemoKeeps(t *testing.T) {
	s, site, files, _ := noteSite(t, nil)
	site.sm.Pages[0].Content = files.entry("zh-CN", "a", bigText())
	var compressed atomic.Int32
	onCompressPage = func() { compressed.Add(1) }
	defer func() { onCompressPage = nil }()
	for _, p := range []string{"/llms-full.txt", "/store/journal/a.md"} {
		compressed.Store(0)
		plain := get(s, p)
		if plain.Code != 200 || plain.Header().Get("Content-Encoding") != "" || plain.Header().Get("Content-Length") != strconv.Itoa(plain.Body.Len()) || plain.Body.Len() < 5000 {
			t.Fatalf("GET %s identity: %d %v", p, plain.Code, plain.Header())
		}
		for range 2 {
			gz := get(s, p, "Accept-Encoding", "gzip")
			n := gz.Body.Len()
			if gz.Header().Get("Content-Encoding") != "gzip" || n >= plain.Body.Len() || decoded(t, gz) != plain.Body.String() || gz.Header().Get("Content-Length") != strconv.Itoa(n) {
				t.Errorf("GET %s gzip: %v, %d bytes of %d", p, gz.Header(), n, plain.Body.Len())
			}
			sameHeaders(t, p, plain.Header(), gz.Header(), "Content-Type", "Link", "Vary")
			if gz.Header().Get("Vary") == "" || gz.Header().Get("Cache-Control") != plain.Header().Get("Cache-Control") {
				t.Errorf("GET %s gzip: %v", p, gz.Header())
			}
		}
		if again := get(s, p); again.Body.String() != plain.Body.String() || again.Header().Get("Content-Length") != plain.Header().Get("Content-Length") {
			t.Errorf("GET %s identity from the memo: %d bytes", p, again.Body.Len())
		}
		if compressed.Load() != 1 {
			t.Errorf("GET %s: compressed %d times", p, compressed.Load())
		}
	}
}

func TestSiteFilesAnswerHEAD(t *testing.T) {
	s, site, files, _ := noteSite(t, nil)
	site.sm.Pages[0].Content = files.entry("zh-CN", "a", bigText())
	for _, p := range []string{"/llms-full.txt", "/store/journal/a.md", "/sitemap.xml"} {
		for _, enc := range []string{"", "gzip"} {
			var hdr []string
			if enc != "" {
				hdr = []string{"Accept-Encoding", enc}
			}
			// The first request makes the answer, the second one finds it.
			for round := range 2 {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("HEAD", p, nil)
				for i := 0; i < len(hdr); i += 2 {
					r.Header.Set(hdr[i], hdr[i+1])
				}
				s.ServeHTTP(w, r)
				// A HEAD is answered uncompressed whatever it accepts, with the length of the body.
				want := get(s, p)
				// The recorder, unlike net/http, does not drop the body of a HEAD.
				if w.Code != 200 || w.Header().Get("Content-Type") != want.Header().Get("Content-Type") || w.Header().Get("Content-Length") != want.Header().Get("Content-Length") || w.Header().Get("Content-Encoding") != "" {
					t.Errorf("HEAD %s %q round %d: %d %v, GET has %v", p, enc, round, w.Code, w.Header(), want.Header())
				}
			}
		}
	}
	// A HEAD that starts the answer leaves it for the GET.
	s, site, files, _ = noteSite(t, nil)
	files.entry("zh-CN", "a", bigText())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("HEAD", "/llms-full.txt", nil))
	if st, _ := s.memo.stats(); w.Code != 200 || st.Entries != 1 || site.calls.Load() != 1 {
		t.Errorf("HEAD: %d, %+v", w.Code, st)
	}
}

// Only a 200 is kept: made-up URLs cannot fill the memo, and an error is not remembered.
func TestOnlyAnAnswerIsKept(t *testing.T) {
	s, site, _, _ := noteSite(t, nil)
	entries := func() int { st, _ := s.memo.stats(); return st.Entries }
	for _, p := range []string{"/store/journal/zzz.md", "/en-US/store/journal/a.md", "/sitemap-9.xml", "/sitemap-1.xml"} {
		calls := site.calls.Load()
		for range 3 {
			if w := get(s, p); w.Code != 404 {
				t.Errorf("GET %s: %d", p, w.Code)
			}
		}
		if site.calls.Load()-calls != 3 || entries() != 0 {
			t.Errorf("GET %s: %d function calls, %d entries", p, site.calls.Load()-calls, entries())
		}
	}
	site.err = http.ErrAbortHandler
	for range 2 {
		if w := get(s, "/llms.txt"); w.Code != 500 {
			t.Errorf("an error of the function: %d", w.Code)
		}
	}
	site.err = nil
	if w := get(s, "/llms.txt"); w.Code != 200 || entries() != 1 {
		t.Errorf("after the error: %d, %d entries", w.Code, entries())
	}
	// A page that shows up is there at once.
	site.sm.Pages = append(site.sm.Pages, tifa.SitemapPage{Language: "zh-CN", Path: "/store/journal/zzz", Title: "Z"})
	if w := get(s, "/store/journal/zzz.md"); w.Code != 404 {
		t.Errorf("a page without Content has no .md: %d", w.Code)
	}
}

func TestRequestsForTheSameSiteFileShareOneComputation(t *testing.T) {
	for _, size := range []int64{config.Default().SSR.MemoSize, 0} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			cfg := config.Default()
			cfg.SSR.MemoSize = size
			s, site, files, log := noteSite(t, &cfg)
			files.gate = make(chan struct{})
			const n = 6
			answers := make([]*httptest.ResponseRecorder, n)
			var wg sync.WaitGroup
			for i := range n {
				wg.Add(1)
				go func() { defer wg.Done(); answers[i] = get(s, "/llms-full.txt") }()
			}
			until(t, "the requests to wait", func() bool { return waiting(s) == n })
			close(files.gate)
			wg.Wait()
			for i, w := range answers {
				if w.Code != 200 || w.Body.String() != answers[0].Body.String() || !strings.Contains(w.Body.String(), "text of hi") {
					t.Errorf("request %d: %d %s", i, w.Code, w.Body.String())
				}
			}
			if site.calls.Load() != n || files.reads.Load() != 2 || log.count() != 1 {
				t.Errorf("%d function calls, %d reads, %d computations for %d requests", site.calls.Load(), files.reads.Load(), log.count(), n)
			}
			if st, _ := s.memo.stats(); st.Misses != 1 || st.Shared != n-1 {
				t.Errorf("memo %+v", st)
			}
		})
	}
}

func TestSiteFilesAreMadeForEachRequestWithoutMemo(t *testing.T) {
	cfg := config.Default()
	cfg.SSR.MemoSize = 0
	s, site, files, log := noteSite(t, &cfg)
	for range 3 {
		if w := get(s, "/llms-full.txt"); w.Code != 200 {
			t.Fatalf("%d", w.Code)
		}
	}
	if site.calls.Load() != 3 || files.reads.Load() != 6 || log.count() != 3 {
		t.Errorf("%d function calls, %d reads, %d computations", site.calls.Load(), files.reads.Load(), log.count())
	}
	if st, used := s.memo.stats(); st.Entries != 0 || used != 0 {
		t.Errorf("memo %+v %d", st, used)
	}
	// The ETag and the 304 work as they do for a page.
	tag := get(s, "/llms.txt").Header().Get("ETag")
	if w := get(s, "/llms.txt", "If-None-Match", tag); tag == "" || w.Code != 304 {
		t.Errorf("304 without a memo: %q %d", tag, w.Code)
	}
}

func TestSitemapInSeveralPartsIsKeptPerPart(t *testing.T) {
	defer func(n int) { sitemapURLs = n }(sitemapURLs)
	sitemapURLs = 2
	s, site, _, log := noteSite(t, nil)
	var bodies []string
	for _, p := range []string{"/sitemap.xml", "/sitemap-1.xml", "/sitemap-2.xml"} {
		first := get(s, p)
		again := get(s, p)
		if first.Code != 200 || again.Body.String() != first.Body.String() || first.Header().Get("ETag") != again.Header().Get("ETag") {
			t.Errorf("GET %s: %d", p, first.Code)
		}
		bodies = append(bodies, first.Body.String())
	}
	if bodies[0] == bodies[1] || bodies[1] == bodies[2] || !strings.Contains(bodies[0], "<sitemapindex") {
		t.Errorf("the parts: %q", bodies)
	}
	if site.calls.Load() != 6 || log.count() != 1 {
		t.Errorf("%d function calls, the list was checked %d times", site.calls.Load(), log.count())
	}
	if st, _ := s.memo.stats(); st.Entries != 3 {
		t.Errorf("memo %+v", st)
	}
}

// The files of one list check its pages once, whichever file asks first. A file whose answer is not
// in the memo yet, such as the .md of each post, reuses them, and a changed list is checked again.
func TestFilesOfOneListCheckItsPagesOnce(t *testing.T) {
	s, site, files, log := noteSite(t, nil)
	files.entry("zh-CN", "b", "text of b\n")
	site.sm.Pages = append(site.sm.Pages, tifa.SitemapPage{Language: "zh-CN", Path: "/store/journal/b", Title: "B", Content: files.entry("zh-CN", "b", "text of b\n")})
	for _, p := range []string{"/store/journal/a.md", "/store/journal/b.md", "/llms.txt", "/sitemap.xml", "/llms-full.txt", "/store/journal/zzz.md", "/sitemap-4.xml"} {
		get(s, p)
	}
	if log.count() != 1 {
		t.Errorf("the pages of the zh-CN list were checked %d times", log.count())
	}
	// Another language has a list of its own.
	get(s, "/en-US/store/journal/hi.md")
	get(s, "/en-US/store/journal/hi.md")
	if log.count() != 2 {
		t.Errorf("the pages of the en-US list were checked, in all %d times", log.count())
	}
	// A changed list is checked again by the next file, whichever it is, and its new pages are there.
	site.sm.Pages[1].Title = "Hi, changed"
	site.sm.Pages = append(site.sm.Pages, tifa.SitemapPage{Language: "zh-CN", Path: "/store/journal/c", Title: "C", Content: files.entry("zh-CN", "c", "text of c\n")})
	if w := get(s, "/store/journal/c.md"); w.Code != 200 || !strings.Contains(w.Body.String(), "text of c") || log.count() != 3 {
		t.Errorf("a .md of the new list: %d %q, checked %d times", w.Code, w.Body.String(), log.count())
	}
	if w := get(s, "/store/journal/b.md"); w.Code != 200 || log.count() != 3 {
		t.Errorf("another .md of the new list: %d, checked %d times", w.Code, log.count())
	}
	// Only the list of the language of the file is replaced.
	if w := get(s, "/en-US/store/journal/hi.md"); !strings.Contains(w.Body.String(), "# Hi, changed") || log.count() != 4 {
		t.Errorf("the en-US list after the change: %q, checked %d times", w.Body.String(), log.count())
	}
	// With the memo off nothing is kept: every request checks.
	cfg := config.Default()
	cfg.SSR.MemoSize = 0
	s, _, _, log = noteSite(t, &cfg)
	for range 2 {
		get(s, "/store/journal/a.md")
		get(s, "/llms.txt")
	}
	if log.count() != 4 {
		t.Errorf("without a memo the pages were checked %d times for 4 requests", log.count())
	}
}

// The key of a list is the key of a file without the file.
func TestListKeyLeavesTheFileOut(t *testing.T) {
	sm := tifa.Sitemap{Name: "x", Pages: []tifa.SitemapPage{{Path: "/a", Title: "A"}}}
	if siteListKey("b", "l", sm) != siteListKey("b", "l", sm) || siteFileKey("k", "/a.md") == siteFileKey("k", "/b.md") || siteFileKey("k", "/a.md") == siteFileKey("j", "/a.md") {
		t.Error("the keys do not follow their input")
	}
	sm2 := sm
	sm2.Pages = []tifa.SitemapPage{{Path: "/a", Title: "B"}}
	if siteListKey("b", "l", sm) == siteListKey("b", "l", sm2) || siteListKey("b", "l", sm) == siteListKey("b", "m", sm) || siteListKey("b", "l", sm) == siteListKey("c", "l", sm) {
		t.Error("the key of a list lacks part of the list")
	}
}

// An OnRequest that rewrites the path decides nothing: the file is the one the request asked for.
func TestRewrittenPathDoesNotChangeTheFile(t *testing.T) {
	app := siteApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Request().URL.Path == "/llms.txt" {
			c.Request().URL.Path = "/sitemap.xml"
		}
		return next(c)
	}
	s := newSiteServer(t, app, siteDist())
	s.sitemap = (&theSite{sm: tifa.Sitemap{Name: "Shop", Exclude: []string{"/projects", "/store/journal/{post}"}}}).fn
	rewritten := get(s, "/llms.txt")
	if rewritten.Code != 200 || !strings.HasPrefix(rewritten.Body.String(), "# Shop") {
		t.Fatalf("GET /llms.txt: %d %s", rewritten.Code, rewritten.Body.String())
	}
	if w := get(s, "/sitemap.xml"); w.Code != 200 || !strings.Contains(w.Body.String(), "<urlset") || w.Header().Get("ETag") == rewritten.Header().Get("ETag") {
		t.Errorf("GET /sitemap.xml after it: %d %s", w.Code, w.Body.String())
	}
}

// If-None-Match is for an answer that is a 200. A file that does not exist stays a 404 without an
// ETag, whatever the request holds, and * matches a file that does.
func TestPreconditionNeverTurnsA404Into304(t *testing.T) {
	s, _, _, _ := noteSite(t, nil)
	for _, p := range []string{"/sitemap-2.xml", "/store/journal/zzz.md", "/en-US/store/journal/a.md"} {
		for _, inm := range []string{"*", `W/"anything"`, `"x", *`} {
			if w := get(s, p, "If-None-Match", inm); w.Code != 404 || w.Header().Get("ETag") != "" {
				t.Errorf("GET %s with If-None-Match %s: %d %v", p, inm, w.Code, w.Header())
			}
		}
	}
	for _, p := range []string{"/sitemap.xml", "/llms.txt", "/llms-full.txt", "/store/journal/a.md"} {
		w := get(s, p, "If-None-Match", "*")
		if w.Code != 304 || w.Header().Get("ETag") == "" || w.Body.Len() != 0 {
			t.Errorf("GET %s with If-None-Match *: %d %v", p, w.Code, w.Header())
		}
	}
}
