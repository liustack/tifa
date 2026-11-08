package compare_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/liustack/tifa/test/compare"
)

// The delivery dates of the TypeScript port are the same as the ones in Go (the cases of
// TestDeliveryDatesAcrossAClockChange in examples/shop/app/store/cart): a calendar day of the store
// carried as noon in its zone, also where the clock changes. The table of regions on a product
// page adds whole 24 hour steps to the earliest date and shows the day in the store zone.
func TestPortDeliveryDatesAreCalendarDaysOfTheStore(t *testing.T) {
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	la, _ := time.LoadLocation("America/Los_Angeles")
	sh, _ := time.LoadLocation("Asia/Shanghai")
	type input struct {
		Name string `json:"name"`
		Now  int64  `json:"now"`
		Lang string `json:"lang"`
		Way  int    `json:"way"`
	}
	cases := []struct {
		input
		earliest, latest string
	}{
		{input{"LA next day over the spring change", time.Date(2026, 3, 7, 23, 30, 0, 0, la).UnixMilli(), "en-US", 1}, "2026-03-08T19:00:00Z", "2026-03-08T19:00:00Z"},
		{input{"LA on the day of the spring change", time.Date(2026, 3, 8, 12, 0, 0, 0, la).UnixMilli(), "en-US", 1}, "2026-03-09T19:00:00Z", "2026-03-09T19:00:00Z"},
		{input{"LA standard over the spring change", time.Date(2026, 3, 5, 23, 30, 0, 0, la).UnixMilli(), "en-US", 0}, "2026-03-08T19:00:00Z", "2026-03-10T19:00:00Z"},
		{input{"LA next day over the autumn change", time.Date(2026, 11, 1, 0, 30, 0, 0, la).UnixMilli(), "en-US", 1}, "2026-11-02T20:00:00Z", "2026-11-02T20:00:00Z"},
		{input{"LA standard over the autumn change", time.Date(2026, 10, 30, 0, 30, 0, 0, la).UnixMilli(), "en-US", 0}, "2026-11-02T20:00:00Z", "2026-11-04T20:00:00Z"},
		{input{"LA standard from the 29th", time.Date(2026, 10, 29, 12, 0, 0, 0, la).UnixMilli(), "en-US", 0}, "2026-11-01T20:00:00Z", "2026-11-03T20:00:00Z"},
		{input{"Shanghai at the last millisecond of a day", time.Date(2026, 11, 8, 23, 59, 59, 999000000, sh).UnixMilli(), "zh-CN", 1}, "2026-11-09T04:00:00Z", "2026-11-09T04:00:00Z"},
		{input{"Shanghai at the first instant of the next day", time.Date(2026, 11, 9, 0, 0, 0, 0, sh).UnixMilli(), "zh-CN", 1}, "2026-11-10T04:00:00Z", "2026-11-10T04:00:00Z"},
	}
	var inputs []input
	for _, c := range cases {
		inputs = append(inputs, c.input)
	}
	in, _ := json.Marshal(inputs)
	// Region dates: from the earliest date of standard delivery, steps of 24 hours (east 0, north 1,
	// south 1, west 2, far 3), written as the month and the day in Los Angeles.
	regions := `[
	  {"now": ` + itoa(time.Date(2026, 10, 29, 12, 0, 0, 0, la).UnixMilli()) + `, "want": ["11-01", "11-02", "11-02", "11-03", "11-04"]},
	  {"now": ` + itoa(time.Date(2026, 3, 5, 12, 0, 0, 0, la).UnixMilli()) + `, "want": ["03-08", "03-09", "03-09", "03-10", "03-11"]}
	]`
	script := `
import { getDeliveries } from './domain/store/cart/cart.ts'
import { findLocale } from './domain/store/store.ts'
const inputs = ` + string(in) + `
const regions = ` + regions + `
const cart = { subtotalCents: 0, discountCents: 0, freeShippingCents: 1 }
const out = inputs.map((c) => {
  const l = findLocale(c.lang)
  const d = getDeliveries(cart, c.now, l)[c.way]
  return [d.earliest, d.latest]
})
const la = new Intl.DateTimeFormat('en-US', { timeZone: 'America/Los_Angeles', month: '2-digit', day: '2-digit' })
const l = findLocale('en-US')
const days = regions.map((r) => {
  const first = Date.parse(getDeliveries(cart, r.now, l)[0].earliest)
  return [0, 1, 1, 2, 3].map((n) => la.format(first + n * 86400000).replace('/', '-'))
})
console.log(JSON.stringify({ out, days }))
`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = filepath.Join(".")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, b)
	}
	var got struct {
		Out  [][2]string `json:"out"`
		Days [][]string  `json:"days"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	for i, c := range cases {
		if got.Out[i][0] != c.earliest || got.Out[i][1] != c.latest {
			t.Errorf("%s: %v, want %s and %s", c.Name, got.Out[i], c.earliest, c.latest)
		}
	}
	want := [][]string{{"11-01", "11-02", "11-02", "11-03", "11-04"}, {"03-08", "03-09", "03-09", "03-10", "03-11"}}
	for i := range want {
		for j := range want[i] {
			if got.Days[i][j] != want[i][j] {
				t.Errorf("region dates %d: %v, want %v", i, got.Days[i], want[i])
				break
			}
		}
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
