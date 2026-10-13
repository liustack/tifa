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
// app restarts. A larger body waits until the app listens, then streams to it once.
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
		Transport:     &retryTransport{base: http.DefaultTransport.(*http.Transport).Clone(), addr: appAddr, wait: wait, down: down},
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
	addr string // where the app listens
	wait time.Duration
	down func() error
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, replayable, err := bufferBody(req)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(t.wait)
	if !replayable {
		// The body can go out only once: wait until the app listens before sending it.
		if err := t.waitApp(req, deadline); err != nil {
			req.Body.Close()
			return nil, err
		}
		resp, err := t.base.RoundTrip(req)
		if err != nil && notListening(err) {
			return nil, fmt.Errorf("the app stopped listening before the request was sent, and a body over %d MiB cannot be sent again: %w", maxReplayBody>>20, err)
		}
		return resp, err
	}
	var delay time.Duration
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
		if err := t.pause(req, deadline, &delay, err); err != nil {
			return nil, err
		}
	}
}

// waitApp waits until a connection to the app succeeds.
func (t *retryTransport) waitApp(req *http.Request, deadline time.Time) error {
	var delay time.Duration
	for {
		var d net.Dialer
		conn, err := d.DialContext(req.Context(), "tcp", t.addr)
		if err == nil {
			return conn.Close()
		}
		if err := t.pause(req, deadline, &delay, err); err != nil {
			return err
		}
	}
}

// pause waits before the next attempt, after failed found no app listening. It returns an error
// when waiting is pointless: the app is down until the next build, wait passed, or the browser left.
func (t *retryTransport) pause(req *http.Request, deadline time.Time, delay *time.Duration, failed error) error {
	if t.down != nil {
		if err := t.down(); err != nil {
			return err
		}
	}
	if time.Now().After(deadline) {
		return fmt.Errorf("the app did not accept connections within %s: %w", t.wait, failed)
	}
	if testHookRetry != nil {
		testHookRetry()
	}
	*delay = min(max(2**delay, 10*time.Millisecond), 200*time.Millisecond)
	select {
	case <-req.Context().Done():
		return req.Context().Err()
	case <-time.After(*delay):
		return nil
	}
}

// notListening reports whether err happened while connecting, before anything was sent.
func notListening(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// bufferBody reads a body of up to maxReplayBody bytes into memory so it can be sent again.
// A larger body is left to stream. When its length is unknown, req.Body then yields the bytes
// read so far and the rest.
func bufferBody(req *http.Request) (body []byte, replayable bool, err error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, true, nil
	}
	if req.ContentLength > maxReplayBody {
		return nil, false, nil // known to be too large, leave it unread
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
