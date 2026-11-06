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
