package core

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrNotFound is an expected result. The server answers 404 without calling OnError.
var ErrNotFound = errors.New("tifa: not found")

// Error is a failure that is safe to show to the client. The server answers it as is, without calling OnError.
type Error struct {
	Status  int
	Message string
}

func (e Error) Error() string { return fmt.Sprintf("%d %s", e.Status, e.Message) }

func publicError(status int, message string) Error {
	if message == "" {
		message = http.StatusText(status)
	}
	return Error{Status: status, Message: message}
}

// RedirectError is returned by Ctx.Redirect. The server performs the redirect without calling OnError.
type RedirectError struct {
	URL  string
	Code int
}

func (e *RedirectError) Error() string { return fmt.Sprintf("tifa: redirect %d to %s", e.Code, e.URL) }
