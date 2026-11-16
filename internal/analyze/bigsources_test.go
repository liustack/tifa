package analyze

import (
	"slices"
	"strings"
	"testing"
)

// The build warns where the code itself shows that a number above 2^53 goes into a field that a
// page reads, which Tifa would refuse at run time: the direct forms only.
func TestWarnsOfBigNumbersPutIntoFields(t *testing.T) {
	m, err := analyzeApp(t, "bigsources")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	want := []struct{ at, field, source string }{
		{"app/api/route.go:20", "Public.Number", "math/rand.Uint64"},
		{"app/api/route.go:22", "Mine.ID", "time.Time.UnixNano"},
		{"app/page/route.go:47", "Row.Nano", "time.Time.UnixNano"},
		{"app/page/route.go:48", "Row.Rand", "math/rand/v2.Uint64"},
		{"app/page/route.go:49", "Row.Hash", "Sum64"},
		{"app/page/route.go:50", "Row.Max", "the constant 9223372036854775807"},
		{"app/page/route.go:51", "Row.Shift", "the constant 1152921504606846976"},
		{"app/page/route.go:52", "Row.Lit", "the constant 18446744073709551615"},
		{"app/page/route.go:53", "Row.Converted", "Sum64"},
		{"app/page/route.go:54", "Row.Local", "math/rand.Uint64"},
		{"app/page/route.go:55", "Row.Bytes", "binary.BigEndian.Uint64 of bytes from crypto/rand"},
		{"app/page/route.go:56", "Row.Func", "Sum64String"},
		{"app/page/route.go:57", "Row.V1", "math/rand.Int63"},
		{"app/page/route.go:58", "Pair.A", "time.Time.UnixNano"},
		{"app/page/route.go:58", "Pair.B", "math/rand/v2.Uint64"},
		{"app/page/route.go:61", "Row.Assigned", "time.Time.UnixNano"},
	}
	var got []string
	for _, w := range m.Warnings {
		got = append(got, w.String())
	}
	if len(got) != len(want) {
		t.Fatalf("got %d warnings, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i, w := range want {
		for _, part := range []string{w.at + ":", w.field + " ", w.source, "will be refused at run time", `json:",string"`, "UnixMilli"} {
			if !strings.Contains(got[i], part) {
				t.Errorf("warning %d lacks %q:\n%s", i, part, got[i])
			}
		}
	}
}

// What looks alike stays quiet: the small time units, a bounded random number, a float64, a
// tagged field, a struct that no page reads, an id from a database row and a constant below 2^53.
func TestBigNumberLookAlikesStayQuiet(t *testing.T) {
	m, err := analyzeApp(t, "bigquiet")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !slices.Equal(m.Warnings, nil) {
		t.Fatalf("warnings:\n%v", m.Warnings)
	}
}
