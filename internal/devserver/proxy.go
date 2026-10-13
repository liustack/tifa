package devserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// maxReplayBody is the largest request body the proxy keeps so it can send it again while the
// app restarts. A larger body streams to the app once, and fails if the app is not listening.
const maxReplayBody = 8 << 20

// testHookRetry lets tests see each attempt that found no app listening.
var testHookRetry func()

// proxy forwards requests to the app at appAddr. While the app restarts, a request waits for up
// to wait until the app listens again. down reports why no app will come, such as a failed
// build with no older app to fall back on: the proxy then answers at once. down may be nil.
func proxy(appAddr string, wait time.Duration, down func() error) http.Handler {
	target := &url.URL{Scheme: "http", Host: appAddr}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
			r.SetXForwarded()
		},
		Transport:     &retryTransport{base: http.DefaultTransport.(*http.Transport).Clone(), wait: wait, down: down},
		FlushInterval: -1, // pass streamed responses, such as the app's own SSE, through right away
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, context.Canceled) {
				return // the browser went away
			}
			unavailable(w, http.StatusBadGateway, err)
		},
	}
}

// unavailable answers with a page that shows err and loads the dev client, so the browser shows
// the build state and reloads once the app is back.
func unavailable(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html>
<meta charset="utf-8">
<title>tifa dev</title>
<script src="%s"></script>
<pre style="white-space: pre-wrap">%s</pre>
`, clientPath, html.EscapeString(err.Error()))
}

type retryTransport struct {
	base http.RoundTripper
	wait time.Duration
	down func() error
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, replayable, err := bufferBody(req)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(t.wait)
	delay := 10 * time.Millisecond
	for {
		attempt := req
		if body != nil {
			attempt = req.Clone(req.Context())
			attempt.Body = io.NopCloser(bytes.NewReader(body))
			attempt.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		}
		resp, err := t.base.RoundTrip(attempt)
		if err == nil || !notListening(err) {
			return resp, err
		}
		if !replayable {
			return nil, fmt.Errorf("the app is not accepting connections, and the request body is larger than %d MiB so it cannot be sent again: %w", maxReplayBody>>20, err)
		}
		if t.down != nil {
			if downErr := t.down(); downErr != nil {
				return nil, downErr
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the app did not accept connections within %s: %w", t.wait, err)
		}
		if testHookRetry != nil {
			testHookRetry()
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
		delay = min(2*delay, 200*time.Millisecond)
	}
}

// notListening reports whether err happened while connecting, before anything was sent.
func notListening(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// bufferBody reads a body of up to maxReplayBody bytes into memory so it can be sent again.
// A larger body is left to stream: req.Body then yields the bytes read so far and the rest.
func bufferBody(req *http.Request) (body []byte, replayable bool, err error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, true, nil
	}
	orig := req.Body
	body, err = io.ReadAll(io.LimitReader(orig, maxReplayBody+1))
	if err != nil {
		orig.Close()
		return nil, false, err
	}
	if len(body) > maxReplayBody {
		req.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), orig), orig}
		return nil, false, nil
	}
	orig.Close()
	return body, true, nil
}
