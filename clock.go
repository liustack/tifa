package tifa

import (
	"context"
	"time"

	"github.com/liustack/tifa/internal/core"
)

// WithRenderClock returns a context whose server render reads now for Date,
// Temporal.Now and Intl's current time. nil restores the live clock.
// Set it before rendering, for example in OnRequest with c.SetContext.
// The clock runs on the rendering goroutine and must not block. If requests
// share it, it must be safe for concurrent calls. It does not change Go's
// time.Now, deadlines, or module initialization during VM prewarming.
// Cached pages may skip rendering or share another request's render. The
// clock is not a cache key. Pass displayed times through Load data instead.
func WithRenderClock(ctx context.Context, now func() time.Time) context.Context {
	return core.WithRenderClock(ctx, now)
}
