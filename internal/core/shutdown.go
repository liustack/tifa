package core

import "context"

type shutdownKey struct{}

// WithShutdown returns ctx carrying done, which the server closes when it starts to shut down. The
// server makes it the base of the context of every request it serves.
func WithShutdown(ctx context.Context, done <-chan struct{}) context.Context {
	return context.WithValue(ctx, shutdownKey{}, done)
}

// ShuttingDown returns the channel of WithShutdown that ctx carries, nil when it carries none.
func ShuttingDown(ctx context.Context) <-chan struct{} {
	done, _ := ctx.Value(shutdownKey{}).(<-chan struct{})
	return done
}
