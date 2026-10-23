package ssr

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

const clockBundle = `
const loaded = Date.now();
const dateNow = Date.now;
const f = new Intl.DateTimeFormat('en-US', {timeZone:'UTC', dateStyle:'full', timeStyle:'long'});
const instant = Temporal.Now.instant;
globalThis.startRender = function (input) {
 const data = JSON.parse(input);
 if (data.url === '/fail') throw new Error('replace this VM');
 const first = dateNow();
 Promise.resolve().then(() => Promise.resolve()).then(() => {
  globalThis.renderResult = '<p>' + [first, new Date().getTime(), instant().epochMilliseconds, Date() === new Date(dateNow()).toString(), f.format() === f.format(dateNow()), loaded].join('|') + '</p>';
 });
};`

func renderClock(t *testing.T, p *Pool, at time.Time) {
	t.Helper()
	ctx := tifa.WithRenderClock(context.Background(), func() time.Time { return at })
	got, err := p.Render(ctx, Input{URL: "/", Data: []byte("{}")})
	prefix := fmt.Sprintf("<p>%d|%d|%d|true|true|", at.UnixMilli(), at.UnixMilli(), at.UnixMilli())
	if err != nil || !strings.HasPrefix(got, prefix) {
		t.Fatalf("render = %q, %v, want %s", got, err, prefix)
	}
	loaded, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(got, prefix), "</p>"), 10, 64)
	if err != nil || loaded < time.Now().Add(-time.Minute).UnixMilli() || loaded > time.Now().UnixMilli() {
		t.Fatalf("module initialization must use the live clock: %s", got)
	}
}

func TestRenderClock(t *testing.T) {
	p := newTestPool(t, clockBundle, testConfig(1))
	for _, sec := range []int64{0, 1700000000, -1} {
		renderClock(t, p, time.Unix(sec, 123456789))
	}
	got, err := p.Render(context.Background(), Input{URL: "/", Data: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	ms, err := strconv.ParseInt(strings.Split(strings.TrimPrefix(got, "<p>"), "|")[0], 10, 64)
	if err != nil || ms < time.Now().Add(-time.Minute).UnixMilli() {
		t.Fatalf("clock leaked: %s", got)
	}
	ctx := tifa.WithRenderClock(context.Background(), func() time.Time { return time.Unix(1, 0) })
	if _, err := p.Render(ctx, Input{URL: "/fail"}); err == nil {
		t.Fatal("want failed render")
	}
	renderClock(t, p, time.Unix(1800000000, 987654321))
	if s := p.stats(); s.created != 2 {
		t.Fatalf("want replaced VM, got %+v", s)
	}
}

func TestRenderClockConcurrent(t *testing.T) {
	p := newTestPool(t, clockBundle, testConfig(4))
	// Subtests finish before the enclosing test's cleanup closes the pool.
	for i := range 16 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for j := range 3 {
				renderClock(t, p, time.Unix(int64(i*100+j), 123456789))
			}
		})
	}
}
