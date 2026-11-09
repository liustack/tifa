package compare_test

import (
	"testing"
	"time"

	"github.com/liustack/tifa/test/compare"
)

// An instant in an answer (the time an order was placed) is compared by how far it is from the
// request that made it, to the second: the Go shop and the port answer at different times, but an
// order placed two days ago is two days ago for both, and one that is off by hours is not.
func TestStoreInstantsAreComparedFromTheRequest(t *testing.T) {
	at := time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	answer := func(at time.Time, offset time.Duration, jitter time.Duration) [][]compare.StoreResult {
		return [][]compare.StoreResult{{{Status: 200, At: at.UnixMilli(), Body: map[string]any{
			"placedAt": at.Add(offset + jitter).Format(time.RFC3339Nano),
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
		return [][]compare.StoreResult{{{Status: 200, At: at.UnixMilli(), Body: map[string]any{"placedAt": at.Add(offset).Format(time.RFC3339Nano)}}}}
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
			{Status: 303, At: order.UnixMilli(), Location: "/zh-CN/store/checkout?placed=SH-1043"},
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

// A placed order is read back with its delivery, which is a day of the store and so the same whenever
// it is read, as is a quote for the cart that is read at the same time.
func TestStorePlacedDeliveryIsMeasuredFromTheOrder(t *testing.T) {
	order := time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	const day = 24 * time.Hour
	run := func(readAfter time.Duration, quote time.Duration) [][]compare.StoreResult {
		read := order.Add(readAfter)
		return [][]compare.StoreResult{{
			{Status: 303, At: order.UnixMilli(), Location: "/zh-CN/store/checkout?placed=SH-1043"},
			{Status: 200, At: read.UnixMilli(), Body: map[string]any{
				"placed": map[string]any{
					"placedAt": order.Add(30 * time.Millisecond).Format(time.RFC3339Nano),
					"delivery": map[string]any{"earliest": order.Add(2 * day).Format(time.RFC3339Nano), "latest": order.Add(3 * day).Format(time.RFC3339Nano)},
				},
				"deliveries": []any{map[string]any{"earliest": time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC).Add(quote).Format(time.RFC3339Nano)}},
			}},
		}}
	}
	scenarios := []compare.StoreScenario{{Name: "s", Steps: []compare.StoreStep{{Method: "POST", URL: "/x"}, {Method: "GET", URL: "/y"}}}}
	diff := func(want, got [][]compare.StoreResult) []string {
		compare.NormalizeStore(want)
		compare.NormalizeStore(got)
		return compare.DiffStore(scenarios, want, got, 10)
	}
	// One server read the order 1 s after it was placed and the other 9 s after: the same order, and the same quote.
	if d := diff(run(time.Second, 0), run(9*time.Second, 0)); len(d) > 0 {
		t.Errorf("the same order differs: %v", d)
	}
	// A quote for another day is not the same.
	if d := diff(run(time.Second, 0), run(9*time.Second, day)); len(d) == 0 {
		t.Error("a quote that is a day off passed")
	}
}

// A delivery date is a day of the store, noon in its zone, the same on both servers all day, so it is
// compared as it is. Two runs at different times of the day are the same, a day off is not.
func TestStoreDeliveryDatesAreComparedAsTheyAre(t *testing.T) {
	noon := time.Date(2026, 11, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	answer := func(at time.Time, earliest time.Time) [][]compare.StoreResult {
		return [][]compare.StoreResult{{{Status: 200, At: at.UnixMilli(), Body: map[string]any{
			"delivery": map[string]any{"earliest": earliest.Format(time.RFC3339Nano), "latest": earliest.Format(time.RFC3339Nano)},
		}}}}
	}
	scenarios := []compare.StoreScenario{{Name: "s", Steps: []compare.StoreStep{{Method: "GET", URL: "/x"}}}}
	diff := func(want, got [][]compare.StoreResult) []string {
		compare.NormalizeStore(want)
		compare.NormalizeStore(got)
		return compare.DiffStore(scenarios, want, got, 10)
	}
	at := time.Date(2026, 11, 8, 17, 0, 0, 0, time.UTC)
	// The same noon, written in two zones, asked at two times of the day.
	if d := diff(answer(at, noon), answer(at.Add(5*time.Hour+7*time.Minute), noon.UTC())); len(d) > 0 {
		t.Errorf("the same delivery date differs: %v", d)
	}
	if d := diff(answer(at, noon), answer(at, noon.Add(24*time.Hour))); len(d) == 0 {
		t.Error("a delivery date a day off passed")
	}
}

// A run is asked for again only when every difference is a delivery date.
func TestOnlyDeliveryDateDiffsAreRetried(t *testing.T) {
	date := "s, step 2 GET /y: /deliveries/0/earliest: Go has \"@day 1\", Node \"@day 2\""
	other := "s, step 2 GET /y: /cart/count: Go has 1, Node 2"
	for _, c := range []struct {
		name  string
		diffs []string
		want  bool
	}{
		{"none", nil, false},
		{"dates", []string{date, date}, true},
		{"a date and something else", []string{date, other}, false},
		{"something else", []string{other}, false},
		{"as many as the limit", []string{date, date}, false},
	} {
		limit := 25
		if c.name == "as many as the limit" {
			limit = 2
		}
		if got := compare.OnlyDeliveryDateDiffs(c.diffs, limit); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
