package ssr

import (
	"context"
	"testing"
	"time"
)

func TestHostTemporal(t *testing.T) {
	for _, tc := range []struct{ name, expr, want string }{
		{"global", `typeof Temporal`, "object"},
		// Separate reads of a live clock can straddle a millisecond. Include the first
		// lazy installation in the bracket rather than requiring identical samples.
		{"clock", `(() => { const before = Date.now(); const now = Temporal.Now.instant().epochMilliseconds; return before <= now && now <= Date.now() })()`, "true"},
		{"zone", `Temporal.Now.timeZoneId()`, "UTC"},
		{"zoned now", `Temporal.Now.zonedDateTimeISO().timeZoneId`, "UTC"},
		{"locale", `Temporal.PlainDate.from('2026-10-22').toLocaleString('en-US', {dateStyle: 'long'})`, "October 22, 2026"},
		{"date bridge", `new Date('2026-10-22T12:34:56.789Z').toTemporalInstant().toString()`, "2026-10-22T12:34:56.789Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderExprs(t, hostPool(t), attempt(tc.expr))[0]; got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestTemporalClockAdvancesInReusedVM(t *testing.T) {
	p := hostPool(t)
	expr := `(() => { const before = Date.now(); const now = Temporal.Now.instant().epochMilliseconds; const previous = globalThis.previousTemporalNow; globalThis.previousTemporalNow = now; return before <= now && now <= Date.now() && (previous === undefined || now > previous) })()`
	for range 2 {
		if got := renderExprs(t, p, expr)[0]; got != "true" {
			t.Fatalf("Temporal.Now must follow the live Date clock: %s", got)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestTemporalPageRenders(t *testing.T) {
	const source = `
const day = Temporal.PlainDate.from('2026-10-22');
globalThis.startRender = function () {
  Promise.resolve().then(() => {
    globalThis.renderResult = '<time>' + day.add({days: 1}).toLocaleString('en-US', {dateStyle: 'long'}) + '</time>';
  });
}`
	for _, dev := range []bool{false, true} {
		p, err := NewPool(source, nil, testConfig(1), dev)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.Close() })
		for range 2 {
			html, err := p.Render(context.Background(), Input{URL: "/", Data: []byte("{}")})
			if err != nil || html != "<time>October 23, 2026</time>" {
				t.Fatalf("dev=%t: html=%q err=%v", dev, html, err)
			}
		}
	}
}

func TestReplacedVMHasTemporal(t *testing.T) {
	p := hostPool(t)
	if _, err := p.Render(context.Background(), Input{URL: "/", Data: []byte(`{"exprs":["null.x"]}`)}); err == nil {
		t.Fatal("want the render to fail")
	}
	if got := renderExprs(t, p, `Temporal.PlainDate.from('2026-10-22').toString()`)[0]; got != "2026-10-22" {
		t.Fatalf("replacement rendered %q", got)
	}
	if s := p.stats(); s.created != 2 {
		t.Fatalf("want a replacement VM, got %+v", s)
	}
}
