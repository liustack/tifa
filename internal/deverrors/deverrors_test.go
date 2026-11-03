package deverrors

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func requestErr(path string) Request {
	return Request{Method: "GET", URL: path, Status: 500, Message: "Internal Server Error", Error: "boom at " + path}
}

// The same key counts up and keeps the first entry where it was, a new key goes after the others.
func TestRecentCountsRepeats(t *testing.T) {
	r := new(Requests)
	r.Add(requestErr("/a"))
	r.Add(requestErr("/b"))
	r.Add(requestErr("/a"))
	r.Add(requestErr("/a"))
	got := r.List()
	if len(got) != 2 || got[0].URL != "/a" || got[0].Count != 3 || got[1].URL != "/b" || got[1].Count != 1 {
		t.Fatalf("list = %+v", got)
	}
	// Another status of the same request is another error.
	other := requestErr("/a")
	other.Status = 503
	r.Add(other)
	if got := r.List(); len(got) != 3 || got[2].Status != 503 || got[2].Count != 1 {
		t.Fatalf("list = %+v", got)
	}
}

// At most Max different errors stay: the oldest go. Clear empties the list.
func TestRecentKeepsTheLatest(t *testing.T) {
	r := new(Requests)
	for i := range Max + 5 {
		r.Add(requestErr(fmt.Sprintf("/%d", i)))
	}
	got := r.List()
	if len(got) != Max || got[0].URL != "/5" || got[Max-1].URL != fmt.Sprintf("/%d", Max+4) {
		t.Fatalf("kept %d, from %s to %s", len(got), got[0].URL, got[len(got)-1].URL)
	}
	// List returns a copy: counting up later does not change it.
	r.Add(requestErr("/5"))
	if got[0].Count != 1 {
		t.Fatal("List shares its entries with the list")
	}
	r.Clear()
	if got := r.List(); len(got) != 0 || got == nil {
		t.Fatalf("after Clear: %#v", got)
	}
}

// Browser errors count up by kind, message and stack, as the panel of the page does: the same
// error with another component stack or on another page is the same entry.
func TestBrowserKey(t *testing.T) {
	r := new(BrowserErrors)
	r.Add(BrowserReport{Kind: "uncaught", Title: "Uncaught error", Message: "boom", Stack: "Error: boom\n    at a", Text: "Error: boom\n    at a", URL: "http://127.0.0.1:9527/a"})
	r.Add(BrowserReport{Kind: "uncaught", Title: "Uncaught error", Message: "boom", Stack: "Error: boom\n    at a", Text: "Error: boom\n    at a\n\nComponent stack:\n    at B", URL: "http://127.0.0.1:9527/b"})
	r.Add(BrowserReport{Kind: "rejection", Title: "Unhandled rejection", Message: "boom", Stack: "Error: boom\n    at a", Text: "Error: boom\n    at a", URL: "http://127.0.0.1:9527/a"})
	got := r.List()
	if len(got) != 2 || got[0].Count != 2 || got[0].URL != "http://127.0.0.1:9527/a" || got[1].Kind != "rejection" {
		t.Fatalf("list = %+v", got)
	}
}

func TestRecentIsSafeForConcurrentUse(t *testing.T) {
	r := new(Renders)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 200 {
				r.Add(Render{Method: "GET", URL: fmt.Sprintf("/%d", (i+j)%30), Text: "differs"})
				if j%50 == 0 {
					r.List()
				}
				if j == 100 && i == 0 {
					r.Clear()
				}
			}
		})
	}
	wg.Wait()
	if n := len(r.List()); n > Max {
		t.Fatalf("%d entries", n)
	}
}

// The JSON of a report names every field, also when it is empty, so a reader can rely on it.
func TestReportJSON(t *testing.T) {
	data, err := json.Marshal(Report{}.Normal())
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"failure":null,"requests":[],"browser":[],"renders":[]}` {
		t.Fatalf("empty report = %s", got)
	}
	if !(Report{}).Empty() {
		t.Fatal("an empty report is not Empty")
	}
	full := Report{
		Failure:  &Failure{Title: "Build failed", Error: "x"},
		Requests: []Request{{Method: "GET", URL: "/", Status: 500, Message: "Internal Server Error", Error: "e", Count: 1}},
		Browser:  []Browser{{Kind: "recoverable", Title: "Hydration mismatch", URL: "http://h/", Text: "t", Count: 2}},
		Renders:  []Render{{Method: "GET", URL: "/", Text: "t", Count: 1}},
	}
	data, err = json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"failure":{"title":"Build failed","error":"x"}`,
		`"requests":[{"method":"GET","url":"/","status":500,"message":"Internal Server Error","error":"e","count":1}]`,
		`"browser":[{"kind":"recoverable","title":"Hydration mismatch","url":"http://h/","text":"t","count":2}]`,
		`"renders":[{"method":"GET","url":"/","text":"t","count":1}]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no %s in %s", want, data)
		}
	}
	for _, r := range []Report{{Failure: &Failure{}}, {Requests: full.Requests}, {Browser: full.Browser}, {Renders: full.Renders}} {
		if r.Empty() {
			t.Errorf("%+v is Empty", r)
		}
	}
}
