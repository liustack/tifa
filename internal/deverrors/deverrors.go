// Package deverrors holds the errors tifa dev keeps for `tifa errors`: their kinds, their JSON
// form, and the lists that keep the latest of each kind and count repeats. The app under
// server.DevMain keeps its request errors and render warnings with them, and tifa dev the errors
// of the browser.
package deverrors

import (
	"slices"
	"strconv"
	"sync"
)

// Path is where tifa dev answers `tifa errors` (GET) and takes the errors pages report (POST), and
// where the app under server.DevMain answers tifa dev with its own (GET) and forgets them (DELETE).
// tifa dev never forwards a browser request under /__tifa/dev/ to the app.
const Path = "/__tifa/dev/errors"

// PIDHeader is the header in which tifa dev names its process ID when it answers at Path, so that
// `tifa errors` knows the answer comes from the tifa dev that .tifa/dev.lock names.
const PIDHeader = "X-Tifa-Dev-Pid"

// Max is how many different errors each list keeps, as the panel of the page does.
const Max = 20

// Failure is what the overlay of every page shows: a build that failed, an app that did not start,
// stopped or did not load the SSR bundle.
type Failure struct {
	Title string `json:"title"`
	Error string `json:"error"`
}

// Request is an unexpected error of a request, which went to OnError: the request, the status
// and the public message it got, and the error with its stack trace in the source files.
type Request struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Status  int    `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error"`
	Count   int    `json:"count"`
}

// Render is a page whose two renders of the same data differed.
type Render struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Text   string `json:"text"`
	Count  int    `json:"count"`
}

// Browser is an error of the browser as the panel of the page shows it. Kind is "recoverable"
// (a hydration mismatch or another error React recovered from, Title says which), "uncaught" or
// "rejection". URL is the page that reported it first.
type Browser struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// BrowserReport is what a page sends for each error it shows. Message and Stack make the key, as
// in the panel. Text is what the panel shows.
type BrowserReport struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
	Text    string `json:"text"`
	URL     string `json:"url"`
}

// App is what the app under server.DevMain answers at Path.
type App struct {
	Requests []Request `json:"requests"`
	Renders  []Render  `json:"renders"`
}

// Report is what tifa dev answers at Path: the errors since the last build started. Failure is
// nil when everything builds and runs.
type Report struct {
	Failure  *Failure  `json:"failure"`
	Requests []Request `json:"requests"`
	Browser  []Browser `json:"browser"`
	Renders  []Render  `json:"renders"`
}

// Empty reports whether r has no error.
func (r Report) Empty() bool {
	return r.Failure == nil && len(r.Requests) == 0 && len(r.Browser) == 0 && len(r.Renders) == 0
}

// Normal returns r with empty lists in place of nil ones, so that its JSON has [] for them.
func (r Report) Normal() Report {
	r.Requests = orEmpty(r.Requests)
	r.Browser = orEmpty(r.Browser)
	r.Renders = orEmpty(r.Renders)
	return r
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Requests keeps the latest request errors. The same request, status and error counts up. The
// zero value is empty and ready to use, and it is safe for concurrent use.
type Requests struct{ recent[Request] }

func (r *Requests) Add(e Request) {
	r.add(e.Method+"\n"+e.URL+"\n"+strconv.Itoa(e.Status)+"\n"+e.Error, e)
}

// Renders keeps the latest pages whose renders differed, by request: the text names the values
// that differed, which change from one render to the next.
type Renders struct{ recent[Render] }

func (r *Renders) Add(e Render) { r.add(e.Method+"\n"+e.URL, e) }

// BrowserErrors keeps the latest errors of the browser, by kind, message and stack.
type BrowserErrors struct{ recent[Browser] }

func (r *BrowserErrors) Add(e BrowserReport) {
	r.add(e.Kind+"\n"+e.Message+"\n"+e.Stack, Browser{Kind: e.Kind, Title: e.Title, URL: e.URL, Text: e.Text})
}

type counted[T any] interface{ withCount(int) T }

func (e Request) withCount(n int) Request { e.Count = n; return e }
func (e Render) withCount(n int) Render   { e.Count = n; return e }
func (e Browser) withCount(n int) Browser { e.Count = n; return e }

// recent keeps the first entry of the last Max keys, oldest first, with the number of times each
// key was added since.
type recent[T counted[T]] struct {
	mu      sync.Mutex
	entries []entry[T]
}

type entry[T any] struct {
	key string
	v   T
	n   int
}

func (r *recent[T]) add(key string, v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.entries {
		if r.entries[i].key == key {
			r.entries[i].n++
			return
		}
	}
	r.entries = append(r.entries, entry[T]{key: key, v: v, n: 1})
	if len(r.entries) > Max {
		r.entries = slices.Delete(r.entries, 0, len(r.entries)-Max)
	}
}

// List returns the entries, oldest first, never nil.
func (r *recent[T]) List() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]T, len(r.entries))
	for i, e := range r.entries {
		list[i] = e.v.withCount(e.n)
	}
	return list
}

// Clear forgets every entry.
func (r *recent[T]) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}
