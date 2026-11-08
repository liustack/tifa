package server

import (
	"net/http/httptest"
	"runtime"
	"testing"
	"weak"

	"github.com/liustack/tifa/internal/ssr"
)

// A reader that goes back to the pool lets go of the page it read, so a page the cache dropped is
// not kept by the pool, where the cache does not count it. sync.Pool keeps what it holds through
// one collection, so a page the pool still pointed at would outlive it.
func TestPooledReadersLetGoOfThePage(t *testing.T) {
	html, _ := bigPage(ssr.Input{URL: "/stats"})
	gz := compressPage(html)
	held := weak.Make(&gz[0])
	for _, write := range []func(*gunzip) error{
		func(r *gunzip) error { return unzipTo(httptest.NewRecorder(), r) },
		func(r *gunzip) error { return zstdTo(httptest.NewRecorder(), r) },
	} {
		r, err := gunzipper(gz)
		if err != nil {
			t.Fatal(err)
		}
		if err := write(r); err != nil {
			t.Fatal(err)
		}
	}
	gz = nil
	runtime.GC()
	if held.Value() != nil {
		t.Error("a pooled reader still holds the page")
	}
}
