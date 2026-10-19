package server

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/gzhttp"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

// compressed wraps h so that text answers go out with gzip or zstd to clients that accept
// them, the way gzhttp does by default: zstd when the client takes both alike, gzip at its
// default level, from 1024 bytes on, every answer marked Vary: Accept-Encoding, and nothing
// done to HEAD or to an answer that already has a Content-Encoding or a Content-Range. zstd
// runs at its default level, as in Caddy and Traefik: at gzhttp's fastest, the pages of the
// example shop come out 3% to 11% larger than with gzip. Only the types of compressible are
// compressed, a strong ETag of a compressed answer turns weak and Vary keeps Accept-Encoding
// (outgoing), and a Flush before the body sends the answer uncompressed (earlyFlush).
func compressed(h http.Handler) http.Handler {
	wrap, err := gzhttp.NewWrapper(gzhttp.ContentTypeFilter(compressible), gzhttp.ZstdCompressionLevel(int(zstd.SpeedDefault)))
	if err != nil {
		panic(err) // only options gzhttp rejects fail, and these are fixed
	}
	inner := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(&earlyFlush{ResponseWriter: w}, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner(&outgoing{ResponseWriter: w}, r)
	})
}

// compressible reports whether an answer of this Content-Type is worth compressing: text and
// the text formats, and fonts and icons stored without compression. The list is a short form
// of the default of Caddy's encode, which takes it from Cloudflare. Other images, WOFF fonts,
// audio, video and archives are compressed already. The files tifa build compresses
// (precompressed in internal/bundle) follow this list.
func compressible(contentType string) bool {
	t, _, _ := strings.Cut(contentType, ";")
	t = strings.ToLower(strings.TrimSpace(t))
	return strings.HasPrefix(t, "text/") || strings.HasSuffix(t, "+json") || strings.HasSuffix(t, "+xml") ||
		slices.Contains(compressibleTypes, t)
}

var compressibleTypes = []string{
	"application/javascript", "application/x-javascript", "application/json", "application/xml",
	"application/wasm", "application/x-protobuf", "application/vnd.ms-fontobject",
	"font/ttf", "font/otf", "image/x-icon", "image/vnd.microsoft.icon",
}

// quality returns the q an Accept-Encoding header gives coding, held between 0 and 1, and 0 when
// it does not name it. Like gzhttp, it does not read * as a coding.
func quality(header, coding string) float64 {
	for _, item := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(item, ";")
		if !strings.EqualFold(strings.TrimSpace(name), coding) {
			continue
		}
		q := 1.0
		for _, param := range strings.Split(params, ";") {
			if v, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(param)), "q="); ok {
				q, _ = strconv.ParseFloat(v, 64)
				q = min(max(q, 0), 1)
			}
		}
		return q
	}
	return 0
}

// pageEncoding picks the encoding compressed gives an answer to r, with the comparisons of
// gzhttp's selectEncoding, so a q of NaN goes the same way: none for HEAD, the one of zstd and
// gzip that is taken, else the higher q, and zstd when they are alike.
func pageEncoding(r *http.Request) string {
	if r.Method == http.MethodHead {
		return ""
	}
	header := r.Header.Get("Accept-Encoding")
	gz, zs := quality(header, "gzip"), quality(header, "zstd")
	switch {
	case zs <= 0:
		if gz > 0 {
			return "gzip"
		}
		return ""
	case gz <= 0:
		if zs > 0 {
			return "zstd"
		}
		return ""
	case gz > zs:
		return "gzip"
	}
	return "zstd"
}

// The encoders of compressPage, at the levels and window of compressed. The zstd encoder takes
// several pages at once.
var (
	pageGzip = sync.Pool{New: func() any {
		w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression) // fails only on a bad level
		return w
	}}
	pageZstd = must(zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithWindowSize(128<<10)))
)

// compressPage gives the gzip and zstd forms of a page for the HTML cache, so a hit is sent as
// stored: compressing a cached page again on each hit took more CPU than the hit itself. A page
// under gzhttp's minimum size gets none, as compressed does not compress it.
func compressPage(html string) (gz, zs []byte) {
	if len(html) < gzhttp.DefaultMinSize {
		return nil, nil
	}
	var buf bytes.Buffer
	w := pageGzip.Get().(*gzip.Writer)
	w.Reset(&buf)
	io.WriteString(w, html) // a bytes.Buffer does not fail
	w.Close()
	pageGzip.Put(w)
	return buf.Bytes(), pageZstd.EncodeAll([]byte(html), nil)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// earlyFlush sits between the handler and gzhttp. gzhttp picks the encoding from the first bytes
// of the body, and a Flush before them does nothing, so a stream that sends its header and then
// waits, as server-sent events do, would hold the header back. A Flush before any byte of the
// body sends the answer uncompressed instead: No-Gzip-Compression and an empty Write make gzhttp
// write the header as it is, and gzhttp removes No-Gzip-Compression before it does.
type earlyFlush struct {
	http.ResponseWriter
	body bool // a byte of the body was written, or the answer went out uncompressed
}

func (w *earlyFlush) Write(b []byte) (int, error) {
	if len(b) > 0 {
		w.body = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *earlyFlush) FlushError() error {
	if !w.body {
		w.body = true
		w.Header().Set(gzhttp.HeaderNoCompression, "1")
		if _, err := w.ResponseWriter.Write(nil); err != nil {
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *earlyFlush) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// outgoing sits between gzhttp and the connection and settles the header as it goes out:
//   - When gzhttp compresses an answer it sets Content-Encoding before it writes the header, and
//     a strong ETag then turns weak, as nginx does: a strong ETag promises the same bytes, and the
//     compressed bytes are others. If-None-Match compares weakly, so the weak ETag still gets 304.
//   - gzhttp adds Vary: Accept-Encoding before the handler runs, so a handler that sets Vary
//     itself replaces it. It is added back unless Vary names it already or is *.
type outgoing struct {
	http.ResponseWriter
	sent bool // the header went out
}

func (w *outgoing) settle() {
	if w.sent {
		return
	}
	w.sent = true
	h := w.Header()
	if etag := h.Get("ETag"); strings.HasPrefix(etag, `"`) && h.Get("Content-Encoding") != "" {
		h.Set("ETag", "W/"+etag)
	}
	for _, v := range h.Values("Vary") {
		for _, field := range strings.Split(v, ",") {
			if field = strings.TrimSpace(field); field == "*" || strings.EqualFold(field, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

func (w *outgoing) WriteHeader(code int) {
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.settle()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *outgoing) Write(b []byte) (int, error) {
	w.settle()
	return w.ResponseWriter.Write(b)
}

// gzhttp flushes and hijacks through http.Flusher and http.Hijacker on the writer below it.

func (w *outgoing) Flush() {
	w.settle()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *outgoing) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *outgoing) Unwrap() http.ResponseWriter { return w.ResponseWriter }
