package config

import (
	"math"
	"strings"
	"testing"
)

// Go reads GOMEMLIMIT, GOGC and GOMAXPROCS from the environment before the program reads .env, so
// the program takes them from .env itself. The environment wins, and Go has applied it already.
func TestGoRuntimeFromDotenv(t *testing.T) {
	dir := dotenvDir(t, "GOMEMLIMIT=150MiB\nGOGC=50\nGOMAXPROCS=1\n")
	g, err := Read(nil, nil, dir).GoRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if g.MemoryLimit == nil || *g.MemoryLimit != 150<<20 || g.GCPercent == nil || *g.GCPercent != 50 || g.MaxProcs == nil || *g.MaxProcs != 1 {
		t.Fatalf("%+v", g)
	}
	g, err = Read(nil, []string{"GOMEMLIMIT=1GiB", "GOGC=200", "GOMAXPROCS=4"}, dir).GoRuntime()
	if err != nil || g.MemoryLimit != nil || g.GCPercent != nil || g.MaxProcs != nil {
		t.Fatalf("the environment wins: %+v, %v", g, err)
	}
	g, err = Read(nil, nil, t.TempDir()).GoRuntime()
	if err != nil || g.MemoryLimit != nil || g.GCPercent != nil || g.MaxProcs != nil {
		t.Fatalf("no .env: %+v, %v", g, err)
	}
}

// The values Go takes: a byte count with B, KiB, MiB, GiB or TiB or none, and off.
func TestGoRuntimeValues(t *testing.T) {
	limits := map[string]int64{"1048576": 1 << 20, "512B": 512, "64KiB": 64 << 10, "150MiB": 150 << 20, "2GiB": 2 << 30, "1TiB": 1 << 40, "off": math.MaxInt64}
	for v, want := range limits {
		g, err := Read(nil, nil, dotenvDir(t, "GOMEMLIMIT="+v+"\n")).GoRuntime()
		if err != nil || g.MemoryLimit == nil || *g.MemoryLimit != want {
			t.Errorf("GOMEMLIMIT=%s: %v, %v", v, g.MemoryLimit, err)
		}
	}
	g, err := Read(nil, nil, dotenvDir(t, "GOGC=off\n")).GoRuntime()
	if err != nil || g.GCPercent == nil || *g.GCPercent != -1 {
		t.Errorf("GOGC=off: %v, %v", g.GCPercent, err)
	}
	for _, line := range []string{"GOMEMLIMIT=150MB", "GOMEMLIMIT=1.5GiB", "GOMEMLIMIT=-1", "GOMEMLIMIT=99999999999TiB", "GOGC=fast", "GOMAXPROCS=0", "GOMAXPROCS=two"} {
		_, err := Read(nil, nil, dotenvDir(t, line+"\n")).GoRuntime()
		name, _, _ := strings.Cut(line, "=")
		if err == nil || !strings.Contains(err.Error(), ".env "+name) {
			t.Errorf("%s: %v", line, err)
		}
	}
}
