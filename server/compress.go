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
// them, the way gzhttp does by default except for the choice: gzip when the client takes both alike,
// zstd only when it ranks zstd higher or takes no gzip, gzip at its
// default level, from 1024 bytes on, every answer marked Vary: Accept-Encoding, and nothing
// done to HEAD or to an answer that already has a Content-Encoding or a Content-Range. zstd
// runs at its default level, as in Caddy and Traefik: at gzhttp's fastest, the pages of the
// example shop come out 3% to 11% larger than with gzip. Only the types of compressible are
// compressed, a strong ETag of a compressed answer turns weak and Vary keeps Accept-Encoding
// (outgoing), and a Flush before the body sends the answer uncompressed (earlyFlush).
func compressed(h http.Handler) http.Handler {
	wrap, err := gzhttp.NewWrapper(gzhttp.ContentTypeFilter(compressible), gzhttp.ZstdCompressionLevel(int(zstd.SpeedDefault)), gzhttp.PreferZstd(false))
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
//
// Server-sent events (text/event-stream) are left out: the stream stays open, and its compressor
// with it, about 1 MiB with gzip and 0.6 MiB with zstd per open stream, to save a few bytes of
// small messages.
func compressible(contentType string) bool {
	t, _, _ := strings.Cut(contentType, ";")
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "text/event-stream" {
		return false
	}
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
// gzip that is taken, else the higher q, and gzip when they are alike.
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
	case zs > gz:
		return "zstd"
	}
	return "gzip"
}

// The encoders and readers of the page cache, at the levels and window of compressed. The zstd
// encoders are made for a stream, with one goroutine each.
var (
	pageGzip = sync.Pool{New: func() any {
		w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression) // fails only on a bad level
		return w
	}}
	pageZstd = sync.Pool{New: func() any {
		w, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithWindowSize(128<<10), zstd.WithEncoderConcurrency(1))
		if err != nil {
			panic(err) // only options zstd rejects fail, and these are fixed
		}
		return w
	}}
	pageGunzip sync.Pool // of *gzip.Reader
	pageCopy   = sync.Pool{New: func() any { b := make([]byte, 32<<10); return &b }}
)

// onCompressPage is called, when a test sets it, each time compressPage compresses a page.
var onCompressPage func()

// compressPage gives the gzip form of a page for the HTML cache. The cache keeps this form alone,
// and a hit sends it as it is, or unzips it for a client that takes no gzip. It returns nil for a
// page under gzhttp's minimum size, where compressed does not compress, and for one that gzip does
// not make smaller. The form is copied into a slice of its own size, since the buffer it was made
// in is larger and the cache counts what it keeps.
func compressPage(html string) []byte {
	if len(html) < gzhttp.DefaultMinSize {
		return nil
	}
	if onCompressPage != nil {
		onCompressPage()
	}
	var buf bytes.Buffer
	w := pageGzip.Get().(*gzip.Writer)
	w.Reset(&buf)
	io.WriteString(w, html) // a bytes.Buffer does not fail
	w.Close()
	pageGzip.Put(w)
	if buf.Len() >= len(html) {
		return nil
	}
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out
}

// gunzipper opens the gzip form of a page. The caller closes it with release.
func gunzipper(gz []byte) (r *gzip.Reader, err error) {
	if r, _ = pageGunzip.Get().(*gzip.Reader); r == nil {
		return gzip.NewReader(bytes.NewReader(gz))
	}
	if err = r.Reset(bytes.NewReader(gz)); err != nil {
		pageGunzip.Put(r)
		return nil, err
	}
	return r, nil
}

// unzipTo writes the HTML of a page kept as gzip to w, through a pooled reader and buffer, so a
// hit allocates next to nothing. Errors that come from w are told apart from the page's own.
func unzipTo(w io.Writer, r *gzip.Reader) error {
	buf := pageCopy.Get().(*[]byte)
	defer pageCopy.Put(buf)
	_, err := io.CopyBuffer(w, r, *buf)
	pageGunzip.Put(r)
	return err
}

// zstdTo writes the HTML of a page kept as gzip to w as zstd, the way unzipTo writes it as it is.
func zstdTo(w io.Writer, r *gzip.Reader) error {
	enc := pageZstd.Get().(*zstd.Encoder)
	enc.Reset(w)
	err := unzipTo(enc, r)
	if cerr := enc.Close(); err == nil {
		err = cerr
	}
	enc.Reset(nil)
	pageZstd.Put(enc)
	return err
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
