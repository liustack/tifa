package core

import "log/slog"

// Logger is where the framework logs: slog.Default as it is now. It is read on every call, so
// slog.SetDefault in OnStart takes effect.
func Logger() *slog.Logger { return slog.Default() }
