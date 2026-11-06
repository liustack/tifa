package compare_test

import (
	"testing"
	"time"

	"github.com/liustack/tifa/test/compare"
)

// An instant in an answer is compared by how far it is from the request that made it, to the
// second: the Go shop and the port answer at different times, but a delivery that is two days away
// is two days away for both, and one that is off by hours is not.
func TestStoreInstantsAreComparedFromTheRequest(t *testing.T) {
	at := time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	answer := func(at time.Time, offset time.Duration, jitter time.Duration) [][]compare.StoreResult {
		return [][]compare.StoreResult{{{Status: 200, At: at.UnixMilli(), Body: map[string]any{
			"earliest": at.Add(offset + jitter).Format(time.RFC3339Nano), "placedAt": at.Add(jitter).Format(time.RFC3339Nano),
		}}}}
	}
	scenarios := []compare.StoreScenario{{Name: "s", Steps: []compare.StoreStep{{Method: "GET", URL: "/x"}}}}
	diff := func(want, got [][]compare.StoreResult) []string {
		compare.NormalizeStore(want)
		compare.NormalizeStore(got)
		return compare.DiffStore(scenarios, want, got, 10)
	}
	day := 24 * time.Hour
	// A later run with a few milliseconds of delay in the request is the same.
	if d := diff(answer(at, 2*day, 3*time.Millisecond), answer(at.Add(37*time.Minute), 2*day, 41*time.Millisecond)); len(d) > 0 {
		t.Errorf("the same instants differ: %v", d)
	}
	// Hours off, or a day off, is not.
	if d := diff(answer(at, 2*day, 0), answer(at.Add(time.Hour), 2*day+3*time.Hour, 0)); len(d) == 0 {
		t.Error("an instant that is three hours off passed")
	}
	if d := diff(answer(at, 2*day, 0), answer(at, 3*day, 0)); len(d) == 0 {
		t.Error("an instant that is a day off passed")
	}
}

// Two runs that answer 20 ms apart in the same second boundary are the same: the offsets are
// compared with a tolerance, not rounded first and then compared exactly.
func TestStoreInstantsAreComparedWithATolerance(t *testing.T) {
	at := time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	answer := func(offset time.Duration) [][]compare.StoreResult {
		return [][]compare.StoreResult{{{Status: 200, At: at.UnixMilli(), Body: map[string]any{"earliest": at.Add(offset).Format(time.RFC3339Nano)}}}}
	}
	scenarios := []compare.StoreScenario{{Name: "s", Steps: []compare.StoreStep{{Method: "GET", URL: "/x"}}}}
	diff := func(a, b time.Duration) []string {
		want, got := answer(a), answer(b)
		compare.NormalizeStore(want)
		compare.NormalizeStore(got)
		return compare.DiffStore(scenarios, want, got, 10)
	}
	day := 24 * time.Hour
	for _, c := range [][2]time.Duration{{2*day + 490*time.Millisecond, 2*day + 510*time.Millisecond}, {2*day + 990*time.Millisecond, 2*day + 1010*time.Millisecond}, {2 * day, 2*day + 200*time.Millisecond}} {
		if d := diff(c[0], c[1]); len(d) > 0 {
			t.Errorf("%v against %v: %v", c[0], c[1], d)
		}
	}
	for _, c := range [][2]time.Duration{{2 * day, 2*day + time.Second}, {2 * day, 2*day + 3*time.Hour}, {2 * day, 3 * day}} {
		if d := diff(c[0], c[1]); len(d) == 0 {
			t.Errorf("%v against %v passed", c[0], c[1])
		}
	}
}

// The time an order was placed is read back later, and it is measured from the request that made
// the order, not from the request that reads it.
func TestStorePlacedAtIsMeasuredFromTheOrder(t *testing.T) {
	order := time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	run := func(readAfter time.Duration, placed time.Duration) [][]compare.StoreResult {
		return [][]compare.StoreResult{{
			{Status: 303, At: order.UnixMilli(), Location: "/store/zh-CN/checkout?placed=SS-ABCDEFGHIJ"},
			{Status: 200, At: order.Add(readAfter).UnixMilli(), Body: map[string]any{"placedAt": order.Add(placed).Format(time.RFC3339Nano)}},
		}}
	}
	scenarios := []compare.StoreScenario{{Name: "s", Steps: []compare.StoreStep{{Method: "POST", URL: "/x"}, {Method: "GET", URL: "/y"}}}}
	diff := func(want, got [][]compare.StoreResult) []string {
		compare.NormalizeStore(want)
		compare.NormalizeStore(got)
		return compare.DiffStore(scenarios, want, got, 10)
	}
	// One server read it 1 s after the order, the other 9 s after: the same order.
	if d := diff(run(time.Second, 40*time.Millisecond), run(9*time.Second, 55*time.Millisecond)); len(d) > 0 {
		t.Errorf("the same order differs: %v", d)
	}
	if d := diff(run(time.Second, 40*time.Millisecond), run(time.Second, 3*time.Hour)); len(d) == 0 {
		t.Error("an order placed three hours later passed")
	}
}
