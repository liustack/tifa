package tifa

import (
	"testing"
	"time"
)

// A test binary keeps the defaults, whatever its flags and environment.
var (
	testURL     = String("test.url", "app.db", "")
	testHosts   = Strings("test.hosts", []string{"a", "b"}, "")
	testWorkers = Int("test.workers", 4, "")
	testOpen    = Bool("test.open", true, "")
	testIdle    = Duration("test.idle", time.Minute, "")
	testLimit   = Size("test.limit", 1<<20, "")
)

func TestSettingsKeepTheirDefaultsInTests(t *testing.T) {
	if testURL != "app.db" || len(testHosts) != 2 || testWorkers != 4 || !testOpen || testIdle != time.Minute || testLimit != 1<<20 {
		t.Fatal(testURL, testHosts, testWorkers, testOpen, testIdle, testLimit)
	}
}
