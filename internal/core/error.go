package core

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// ErrNotFound is an expected result. The server answers 404 without calling OnError.
var ErrNotFound = errors.New("tifa: not found")

// ErrSSRBusy means the render queue is full or its wait budget expired.
var ErrSSRBusy = errors.New("ssr: render queue busy")

// Error is a failure that is safe to show to the client. The server answers it as is, without calling OnError.
type Error struct {
	Status  int
	Message string
}

func (e Error) Error() string { return fmt.Sprintf("%d %s", e.Status, e.Message) }

// Fields is a message per input field, by its json name, for a form to show next to each field.
// The server answers it with 422 without calling OnError, and an answer in JSON carries it as
// "fields". It is a separate type so that Error stays comparable.
type Fields map[string]string

// Err returns f, or nil when f has no entries. An empty Fields is still a non-nil error, so a
// validator that fills a Fields ends with it:
//
//	return fields.Err()
func (f Fields) Err() error {
	if len(f) == 0 {
		return nil
	}
	return f
}

func (f Fields) Error() string {
	names := slices.Sorted(maps.Keys(f))
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + ": " + f[name]
	}
	return "tifa: invalid fields: " + strings.Join(parts, ", ")
}

func publicError(status int, message string) Error {
	if message == "" {
		message = http.StatusText(status)
	}
	return Error{Status: status, Message: message}
}

// RedirectError is what Redirect returns. The server performs the redirect without calling OnError.
type RedirectError struct {
	URL    string
	Status int
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("tifa: redirect %d to %s", e.Status, e.URL)
}

// Redirect returns an error that makes the server redirect to url with status. Return it from Load,
// a method handler or OnRequest. It panics when status is not 3xx.
func Redirect(url string, status int) error {
	if status < 300 || status > 399 {
		panic(fmt.Sprintf("tifa: Redirect to %s with non-3xx status %d", url, status))
	}
	return &RedirectError{URL: url, Status: status}
}
