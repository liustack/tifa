package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/config"
)

// GOMEMLIMIT, GOGC and GOMAXPROCS in .env take effect, for serve and for every other command, and
// an invalid one stops the program. Go has read the environment already, so .env is applied here.
func TestGoRuntimeSettingsOfDotenvApply(t *testing.T) {
	oldLimit, oldGC, oldProcs := debug.SetMemoryLimit(-1), debug.SetGCPercent(-1), runtime.GOMAXPROCS(0)
	debug.SetGCPercent(oldGC)
	t.Cleanup(func() { debug.SetMemoryLimit(oldLimit); debug.SetGCPercent(oldGC); runtime.GOMAXPROCS(oldProcs) })

	dir := t.TempDir()
	write := func(env string) {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func() error {
		var out bytes.Buffer
		return runSources(context.Background(), App{}, config.Read([]string{"version"}, nil, dir), &out, &out, nil, nil)
	}
	write("GOMEMLIMIT=150MiB\nGOGC=50\nGOMAXPROCS=1\n")
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if got := debug.SetMemoryLimit(-1); got != 150<<20 {
		t.Errorf("memory limit %d", got)
	}
	if got := debug.SetGCPercent(oldGC); got != 50 {
		t.Errorf("GOGC %d", got)
	}
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("GOMAXPROCS %d", got)
	}
	// ssr.vms follows GOMAXPROCS, the one of .env too.
	var out bytes.Buffer
	if err := runSources(context.Background(), App{}, config.Read([]string{"config"}, nil, dir), &out, &out, nil, nil); err != nil || !strings.Contains(out.String(), "vms: 1\n") {
		t.Errorf("%v\n%s", err, out.String())
	}
	write("GOMEMLIMIT=150MB\n")
	if err := run(); err == nil || !strings.Contains(err.Error(), ".env GOMEMLIMIT") {
		t.Errorf("got %v", err)
	}
}
