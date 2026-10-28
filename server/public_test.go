package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/liustack/tifa/internal/config"
)

// publicDist is a dist the way tifa build writes it for an app with public files: build.json
// names the content hash of each file under public, and the embedded files carry no
// modification time, so the hash is the only validator they have.
func publicDist() fstest.MapFS {
	return fstest.MapFS{
		"build.json":        {Data: []byte(`{"buildId":"p1","public":{"robots.txt":"0a1b2c","notes.txt":"d4e5f6","old.txt":"0a1b2c"}}`)},
		"public/robots.txt": {Data: []byte("User-agent: *")},
		"public/notes.txt":  {Data: []byte(strings.Repeat("a note of the shop\n", 100))},
		// A file the build did not hash, such as one added by hand to a dist directory.
		"public/loose.txt": {Data: []byte("loose")},
	}
}

func publicServer(t *testing.T, compress bool) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.Server.Compress = compress
	s := New(cfg, Hooks{})
	if err := s.setFront(publicDist(), nil); err != nil {
		t.Fatal(err)
	}
	return s
}

// The files of the public folder are embedded in the program with no modification time, so
// without an ETag a browser that revalidates (Cache-Control: no-cache) never gets a 304 and
// downloads the whole file again on every visit.
func TestPublicFilesAnswerConditionalRequestsWith304(t *testing.T) {
	for _, compress := range []bool{false, true} {
		s := publicServer(t, compress)
		w := get(s, "/robots.txt")
		etag := w.Header().Get("ETag")
		if w.Code != 200 || etag != `"0a1b2c"` || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("compress %v: %d %v", compress, w.Code, w.Header())
		}
		w = get(s, "/robots.txt", "If-None-Match", etag)
		if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != etag || w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("compress %v, matching ETag: %d %v %q", compress, w.Code, w.Header(), w.Body.String())
		}
		for _, header := range []string{`"zzz"`, `"0a1b2c`, ``} {
			if w := get(s, "/robots.txt", "If-None-Match", header); w.Code != 200 || w.Body.String() != "User-agent: *" {
				t.Errorf("compress %v, If-None-Match %q: %d %q", compress, header, w.Code, w.Body.String())
			}
		}
		// A list and a weak form match too, as a proxy or gzhttp may have weakened the ETag.
		for _, header := range []string{`"zzz", "0a1b2c"`, `W/"0a1b2c"`, `*`} {
			if w := get(s, "/robots.txt", "If-None-Match", header); w.Code != 304 {
				t.Errorf("compress %v, If-None-Match %q: %d", compress, header, w.Code)
			}
		}
		// A name in build.json that has no file is a 404, not a 304.
		if w := get(s, "/old.txt", "If-None-Match", etag); w.Code != 404 {
			t.Errorf("compress %v, a hash without a file: %d", compress, w.Code)
		}
	}
}

// The ETag of a file that goes out compressed is weak, as nginx does, since a strong ETag
// promises the same bytes, and Vary names the encoding. The 304 of the same validator works for
// the compressed and the plain form.
func TestPublicFileETagsWithCompression(t *testing.T) {
	s := publicServer(t, true)
	plain := get(s, "/notes.txt")
	if plain.Header().Get("ETag") != `"d4e5f6"` || plain.Header().Get("Content-Encoding") != "" || !varies(plain, "Accept-Encoding") {
		t.Fatalf("plain: %v", plain.Header())
	}
	zipped := get(s, "/notes.txt", "Accept-Encoding", "gzip")
	if zipped.Header().Get("Content-Encoding") != "gzip" || zipped.Header().Get("ETag") != `W/"d4e5f6"` || !varies(zipped, "Accept-Encoding") {
		t.Fatalf("gzip: %v", zipped.Header())
	}
	for _, accept := range []string{"", "gzip", "zstd"} {
		for _, etag := range []string{plain.Header().Get("ETag"), zipped.Header().Get("ETag")} {
			w := get(s, "/notes.txt", "Accept-Encoding", accept, "If-None-Match", etag)
			if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("Content-Encoding") != "" {
				t.Errorf("Accept-Encoding %q, If-None-Match %s: %d %v", accept, etag, w.Code, w.Header())
			}
		}
	}
	// With server.compress off the file keeps its strong ETag, whatever the client accepts.
	off := get(publicServer(t, false), "/notes.txt", "Accept-Encoding", "gzip")
	if off.Header().Get("ETag") != `"d4e5f6"` || off.Header().Get("Content-Encoding") != "" {
		t.Errorf("compress off: %v", off.Header())
	}
}

func TestPublicFilesHEADAndMissingFiles(t *testing.T) {
	s := publicServer(t, true)
	head := func(target string, header ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("HEAD", target, nil)
		for i := 0; i < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := head("/robots.txt"); w.Code != 200 || w.Header().Get("ETag") != `"0a1b2c"` || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %v", w.Code, w.Header())
	}
	if w := head("/robots.txt", "If-None-Match", `"0a1b2c"`); w.Code != 304 {
		t.Errorf("HEAD with the ETag: %d", w.Code)
	}
	// A file the build did not hash goes out without a validator, and is not a 304.
	if w := get(s, "/loose.txt", "If-None-Match", `"0a1b2c"`); w.Header().Get("ETag") != "" || w.Body.String() != "loose" {
		t.Errorf("file without a hash: %d %v", w.Code, w.Header())
	}
	for _, target := range []string{"/gone.txt", "/public/robots.txt"} {
		if w := get(s, target, "If-None-Match", "*"); w.Code != 404 || w.Header().Get("ETag") != "" {
			t.Errorf("%s: %d %v", target, w.Code, w.Header())
		}
	}
}

// The files of the browser bundle have the hash in their names and keep their headers.
func TestBundleFilesCarryNoETag(t *testing.T) {
	s := publicServer(t, true)
	files := publicDist()
	files["assets/app-1.js"] = &fstest.MapFile{Data: []byte("console.log(1)")}
	if err := s.setFront(files, nil); err != nil {
		t.Fatal(err)
	}
	w := get(s, "/assets/app-1.js")
	if w.Header().Get("ETag") != "" || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("asset: %v", w.Header())
	}
}
