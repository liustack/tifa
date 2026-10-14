// Package tifa is the API that application code imports: the request context, errors and Layout data reuse.
package tifa

import "github.com/liustack/tifa/internal/core"

// Ctx wraps one request and its response. Load, the method handlers and OnRequest receive it.
type Ctx = core.Ctx

// Error is a failure that is safe to show to the client. Returned from Load, a method handler or OnRequest,
// it is answered with its own status and message without calling OnError. OnError also returns it.
type Error = core.Error

// ErrNotFound makes the server answer 404 without calling OnError.
var ErrNotFound = core.ErrNotFound

// Use returns the result of a Layout function for this request, the Layout of the caller's
// directory or of a directory above it: tifa.Use(c, project.Layout).
// Each function runs at most once per request, a second caller waits for the first run.
func Use[T any](c *Ctx, load func(*Ctx) (T, error)) (T, error) { return core.Use(c, load) }
