package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

// A handler that fails after it wrote part of the answer must not end the answer as if it were
// whole. The client has to see an error, over a chunked answer and over one with Content-Length,
// with and without compression.
func TestFailureAfterPartialAnswerCutsTheConnection(t *testing.T) {
	part := strings.Repeat("partial answer ", 200)
	for _, compress := range []bool{false, true} {
		for _, length := range []bool{false, true} {
			cfg := config.Default()
			cfg.Server.Compress = compress
			var reported atomic.Int32
			s := newServer(cfg, Hooks{OnError: func(c *tifa.Ctx, err error) tifa.Error {
				reported.Add(1)
				return tifa.Error{Status: 500, Message: "failed"}
			}})
			s.handle("GET", "/stream", func(c *tifa.Ctx) error {
				c.SetHeader("Content-Type", "text/plain")
				if length {
					c.SetHeader("Content-Length", "1000000")
				}
				if _, err := io.WriteString(c.Writer(), part); err != nil {
					return err
				}
				if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
					return err
				}
				return errors.New("model failed")
			})
			srv := httptest.NewServer(s)
			res, err := srv.Client().Get(srv.URL + "/stream")
			if err != nil {
				t.Fatalf("compress=%v length=%v: %v", compress, length, err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			srv.Close()
			if err == nil {
				t.Errorf("compress=%v length=%v: read %d bytes and a clean end, want an error", compress, length, len(body))
			}
			if n := reported.Load(); n != 1 {
				t.Errorf("compress=%v length=%v: OnError heard %d times, want 1", compress, length, n)
			}
		}
	}
}
