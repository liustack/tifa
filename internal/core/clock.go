package core

import (
	"context"
	"time"
)

type renderClockKey struct{}

// WithRenderClock carries the clock to the render that consumes ctx.
func WithRenderClock(ctx context.Context, now func() time.Time) context.Context {
	return context.WithValue(ctx, renderClockKey{}, now)
}

// RenderClock returns nil for the live clock.
func RenderClock(ctx context.Context) func() time.Time {
	now, _ := ctx.Value(renderClockKey{}).(func() time.Time)
	return now
}
