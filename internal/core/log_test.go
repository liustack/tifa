package core

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func textLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}}))
}

// The default is read at each call, so slog.SetDefault made after startup counts.
func TestLoggerFollowsTheDefault(t *testing.T) {
	old := slog.Default()
	defer slog.SetDefault(old)
	var buf bytes.Buffer
	slog.SetDefault(textLogger(&buf))
	Logger().Info("first")
	var buf2 bytes.Buffer
	slog.SetDefault(textLogger(&buf2))
	Logger().Info("second")
	if !strings.Contains(buf.String(), "first") || strings.Contains(buf.String(), "second") || !strings.Contains(buf2.String(), "second") {
		t.Fatalf("first handler %q, second %q", buf.String(), buf2.String())
	}
}
