package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func nodePath(project, order string) string { return "/compare-write/" + project + "/" + order }

func TestReadScenarioHasNoWrites(t *testing.T) {
	for n := range 50 {
		steps := ReadScenario.Steps(nodePath, n)
		if len(steps) != 1 || !steps[0].Timed || steps[0].Page != n%len(Pages) {
			t.Fatalf("read %d: %+v", n, steps)
		}
	}
}

// timed returns the one timed step of a unit.
func timed(t *testing.T, steps []Step) Step {
	t.Helper()
	var found []Step
	for _, s := range steps {
		if s.Timed {
			found = append(found, s)
		}
	}
	if len(found) != 1 || !steps[len(steps)-1].Timed {
		t.Fatalf("a unit needs exactly one timed step, last: %+v", steps)
	}
	return found[0]
}

func TestReadWriteScenario(t *testing.T) {
	var writes []int
	seen := map[string]bool{}
	size := -1
	for n := range 300 {
		steps := ReadWriteScenario.Steps(nodePath, n)
		read := timed(t, steps)
		if read.Page != n%len(Pages) || read.Method != "GET" || read.Path != Pages[read.Page].URL {
			t.Fatalf("read %d is %+v", n, read)
		}
		if len(steps) == 1 {
			continue
		}
		if len(steps) != 2 {
			t.Fatalf("read %d has %d steps", n, len(steps))
		}
		w := steps[0]
		writes = append(writes, n)
		if read.Page != 2 && read.Page != 3 {
			t.Errorf("a write before page %d, whose data it does not change", read.Page)
		}
		wantPath := map[int]string{2: "/compare-write/studio/SH-1042", 3: "/compare-write/weekend/WK-0201"}[read.Page]
		if w.Method != "PATCH" || w.Path != wantPath || w.Timed {
			t.Errorf("write %+v, want PATCH %s untimed", w, wantPath)
		}
		body := string(w.Body)
		if seen[body] {
			t.Errorf("write %d repeats an earlier body: %s", n, body)
		}
		seen[body] = true
		if size >= 0 && len(body) != size && read.Page == 2 {
			t.Errorf("body sizes differ: %d and %d", len(body), size)
		}
		if read.Page == 2 {
			size = len(body)
		}
		if !strings.Contains(body, `"customer":"林夏"`) && read.Page == 2 {
			t.Errorf("the write changes the customer: %s", body)
		}
		if utf8.RuneCountInString(body) > 200 {
			t.Errorf("body too long: %s", body)
		}
	}
	// one write for each 10 reads, to within the distance to the next order page
	if len(writes) < 28 || len(writes) > 30 {
		t.Errorf("%d writes in 300 reads, want about 30", len(writes))
	}
	for i := 1; i < len(writes); i++ {
		if d := writes[i] - writes[i-1]; d < 6 || d > 14 {
			t.Errorf("writes at %d and %d are %d reads apart", writes[i-1], writes[i], d)
		}
	}
	// Tifa's own route is the write path when the spec says so.
	steps := ReadWriteScenario.Steps(func(project, order string) string { return "/projects/" + project + "/orders/" + order }, 2)
	if steps[0].Path != "/projects/studio/orders/SH-1042" {
		t.Errorf("write path %s", steps[0].Path)
	}
}

func TestWriteEachScenario(t *testing.T) {
	seen := map[string]bool{}
	for n := range 40 {
		steps := WriteEachScenario.Steps(nodePath, n)
		read := timed(t, steps)
		if len(steps) != 2 || (read.Page != 2 && read.Page != 3) || read.Page != 2+n%2 {
			t.Fatalf("read %d: %+v", n, steps)
		}
		if seen[string(steps[0].Body)] {
			t.Errorf("write %d repeats a body", n)
		}
		seen[string(steps[0].Body)] = true
	}
}

func TestClientAsksForTheEncodingAndCountsTheBytes(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Header.Get("Accept-Encoding")
		if asked == "gzip" {
			w.Header().Set("Content-Encoding", "gzip")
		}
		w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	for _, ae := range []string{"gzip", "identity"} {
		res, err := newClient(srv.URL, ae).do(context.Background(), Step{Method: "GET", Path: "/", Timed: true})
		if err != nil {
			t.Fatal(err)
		}
		if asked != ae || res.Bytes != 10 {
			t.Errorf("%s: server saw %q, client counted %d bytes", ae, asked, res.Bytes)
		}
		if want := map[string]string{"gzip": "gzip", "identity": ""}[ae]; res.Encoding != want {
			t.Errorf("%s: encoding %q, want %q", ae, res.Encoding, want)
		}
	}
	// A server that compresses when it was asked for no compression is an error.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write([]byte("x"))
	}))
	defer bad.Close()
	if _, err := newClient(bad.URL, "identity").do(context.Background(), Step{Method: "GET", Path: "/"}); err == nil {
		t.Error("a compressed answer to identity was accepted")
	}
}

func TestAllPagesAreTheSixPagesAndEveryOrder(t *testing.T) {
	all := AllPages()
	if len(all) != 6+54-2 {
		t.Fatalf("%d pages, want the 4 lists and stats pages and 54 order pages", len(all))
	}
	seen := map[string]bool{}
	for i, p := range all {
		if seen[p.URL] {
			t.Errorf("%s twice", p.URL)
		}
		seen[p.URL] = true
		if i < len(Pages) && p != Pages[i] {
			t.Errorf("page %d is %+v, want %+v", i, p, Pages[i])
		}
	}
	for _, url := range []string{"/projects/studio/orders/SH-1001", "/projects/weekend/orders/WK-0190", "/projects/studio/orders/SH-1042"} {
		if !seen[url] {
			t.Errorf("no page %s", url)
		}
	}
}
