package devserver

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type eventStream struct {
	r    *bufio.Reader
	body io.Closer
}

func connectEvents(t *testing.T, url string) *eventStream {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	return &eventStream{bufio.NewReader(resp.Body), resp.Body}
}

// next returns the next status event, failing the test after 5s.
func (s *eventStream) next(t *testing.T) status {
	t.Helper()
	type result struct {
		st  status
		err error
	}
	got := make(chan result, 1)
	go func() {
		var event, data string
		for {
			line, err := s.r.ReadString('\n')
			if err != nil {
				got <- result{err: err}
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && event == "status":
				var st status
				got <- result{st, json.Unmarshal([]byte(data), &st)}
				return
			}
		}
	}()
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.st
	case <-time.After(5 * time.Second):
		s.body.Close()
		t.Fatal("no status event within 5s")
		return status{}
	}
}

func TestEventsSendTheCurrentStatusFirst(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(h.close)
	failed := status{Version: "v1", Title: "Build failed", Error: "go build: undefined: x"}
	h.publish(failed)
	s := connectEvents(t, srv.URL)
	if got := s.next(t); got != failed {
		t.Fatalf("first event %+v", got)
	}
	h.publish(status{Version: "v2"})
	if got := s.next(t); got != (status{Version: "v2"}) {
		t.Fatalf("second event %+v", got)
	}
}

func TestEventsReachEveryConnection(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(h.close)
	a, b := connectEvents(t, srv.URL), connectEvents(t, srv.URL)
	a.next(t)
	b.next(t)
	h.publish(status{Version: "v2"})
	if got := a.next(t); got.Version != "v2" {
		t.Fatalf("a got %+v", got)
	}
	if got := b.next(t); got.Version != "v2" {
		t.Fatalf("b got %+v", got)
	}
}

func TestEventsEndWhenTheHubCloses(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s := connectEvents(t, srv.URL)
	s.next(t)
	h.close()
	if _, err := io.ReadAll(s.r); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
}
