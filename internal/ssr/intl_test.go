package ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// exprBundle renders the value of each expression in data.exprs, one per line in a <pre>.
const exprBundle = `globalThis.startRender = function (json) {
  const exprs = JSON.parse(json).data.exprs
  globalThis.renderResult = '<pre>' + exprs.map((e) => String(new Function('return ' + e)())).join('\n') + '</pre>'
}`

// renderExprs renders exprs in p and returns their values.
func renderExprs(t *testing.T, p *Pool, exprs ...string) []string {
	t.Helper()
	data, err := json.Marshal(map[string][]string{"exprs": exprs})
	if err != nil {
		t.Fatal(err)
	}
	html, err := p.Render(context.Background(), Input{BuildID: "b1", URL: "/", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(html, "<pre>"), "</pre>"), "\n")
}

func TestRenderFormatsWithIntl(t *testing.T) {
	p := newTestPool(t, exprBundle, testConfig(1))
	for _, tc := range []struct{ expr, want string }{
		{`new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY'}).format(1234.5)`, "¥1,234.50"},
		// 20:05 UTC is the next day in Shanghai.
		{`new Date(Date.UTC(2025, 7, 16, 20, 5, 9)).toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'})`, "2025/8/17"},
		{`new Date(Date.UTC(2025, 7, 16, 6, 5, 9)).toLocaleString('zh-CN', {timeZone: 'Asia/Shanghai', dateStyle: 'full', timeStyle: 'short'})`, "2025年8月16日星期六 14:05"},
		{`(1234567.891).toLocaleString('de-DE')`, "1.234.567,891"},
		{`new Intl.PluralRules('ru').select(2)`, "few"},
	} {
		if got := renderExprs(t, p, tc.expr)[0]; got != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestIntlDefaultsToEnglishAndUTC(t *testing.T) {
	p := newTestPool(t, exprBundle, testConfig(1))
	for _, tc := range []struct{ expr, want string }{
		{`new Intl.DateTimeFormat().resolvedOptions().locale`, "en-US"},
		{`new Intl.DateTimeFormat().resolvedOptions().timeZone`, "UTC"},
		{`new Date(Date.UTC(2025, 7, 16, 20, 5, 9)).toLocaleString()`, "8/16/2025, 8:05:09 PM"},
		{`(1234.5).toLocaleString()`, "1,234.5"},
	} {
		if got := renderExprs(t, p, tc.expr)[0]; got != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

// The local time of a page's Date is UTC whatever zone the process has, so a page renders the
// same on a laptop in Shanghai and on a server in UTC. The Go code of the app keeps the zone of
// the process. The child process starts in Asia/Shanghai, as a developer's machine would.
func TestLocalTimeIsUTCAndGoKeepsItsZone(t *testing.T) {
	if os.Getenv("TIFA_TEST_ZONE") == "1" {
		p := newTestPool(t, exprBundle, testConfig(1))
		got := renderExprs(t, p,
			`new Date(2026, 6, 1).getTimezoneOffset()`,
			`new Date(0).getHours()`,
			`new Date(0).toString()`,
			`new Date(Date.UTC(2025, 7, 16, 20, 5, 9)).toLocaleString()`)
		_, offset := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local).Zone()
		fmt.Printf("js=%s go=%d\n", strings.Join(got, "|"), offset)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestLocalTimeIsUTCAndGoKeepsItsZone$", "-test.count=1", "-test.v", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "TIFA_TEST_ZONE=1", "TZ=Asia/Shanghai")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	want := "js=0|0|Thu Jan 01 1970 00:00:00 GMT+0000|8/16/2025, 8:05:09 PM go=28800"
	if !strings.Contains(string(out), want) {
		t.Fatalf("want %s in:\n%s", want, out)
	}
}

// A VM that replaces a failed one has Intl too: it lives on each VM's global object.
func TestReplacedVMHasIntl(t *testing.T) {
	p := newTestPool(t, exprBundle, testConfig(1))
	data, _ := json.Marshal(map[string][]string{"exprs": {"null.x"}})
	if _, err := p.Render(context.Background(), Input{BuildID: "b1", URL: "/", Data: data}); err == nil {
		t.Fatal("want the render to fail")
	}
	if got := renderExprs(t, p, `new Intl.NumberFormat('en-US').format(1e6)`)[0]; got != "1,000,000" {
		t.Fatalf("got %q", got)
	}
	if s := p.stats(); s.created != 2 {
		t.Fatalf("want the failed VM replaced, got %+v", s)
	}
}

// intlBundle formats on lines 2 to 5, each mapped to its own file: a number without a locale, a
// date without a timeZone, calls that pass both, and plural rules without a locale. Line 6 is
// a call from inside a package, which a page calls on line 7.
var intlBundle = strings.Join([]string{
	"(function () {",
	"  function price(n) { return n.toLocaleString() }",
	"  function day(d) { return d.toLocaleDateString('zh-CN') }",
	"  function fine() { return [new Intl.NumberFormat('en-US').format(1), new Date(0).toLocaleString('en-US', {timeZone: 'UTC'}), Intl.DateTimeFormat('zh-CN', {timeZone: 'Asia/Shanghai', timeStyle: 'short'}).format(0), (1).toLocaleString('de')].join() }",
	"  function plural() { return new Intl.PluralRules().select(1) }",
	"  function lib() { return new Intl.DateTimeFormat().format(0) }",
	"  function page() { return lib() }",
	"  globalThis.startRender = function (json) {",
	"    globalThis.renderResult = '<p>' + [price(1234.5), day(new Date(0)), fine(), plural(), page()].join('|') + '</p>'",
	"  }",
	"})()",
}, "\n")

func intlMap() []byte {
	return sourceMapOf(
		mapping{1, 0, "app/price.tsx", 2, 4},
		mapping{2, 0, "app/day.tsx", 4, 2},
		mapping{3, 0, "app/fine.tsx", 0, 0},
		mapping{4, 0, "app/plural.tsx", 6, 0},
		mapping{5, 0, "node_modules/datelib/index.js", 9, 1},
		mapping{6, 0, "app/page.tsx", 1, 2},
		mapping{8, 0, "app/render.tsx", 0, 0},
	)
}

// Under tifa dev a call that leaves the locale or, for a date, the time zone to the default is
// logged once, with its place in the sources and what to do. The render does not change.
func TestDevWarnsAboutIntlDefaults(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	defer swap(&logf, func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	})()
	p, err := NewPool(intlBundle, intlMap(), testConfig(2), true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	want := "<p>1,234.5|1970/1/1|1,1/1/1970, 12:00:00 AM,08:00,1|one|1/1/1970</p>"
	// Both VMs render twice, and each place is still logged once.
	for range 4 {
		html, err := p.Render(context.Background(), Input{BuildID: "b1", URL: "/", Data: []byte("{}")})
		if err != nil {
			t.Fatal(err)
		}
		if html != want {
			t.Fatalf("got %s, want %s", html, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 4 {
		t.Fatalf("want 4 warnings, got %d:\n%s", len(logs), strings.Join(logs, "\n"))
	}
	for i, want := range []string{
		"Number.prototype.toLocaleString without a locale at app/price.tsx:3:5",
		"Date.prototype.toLocaleDateString without a timeZone at app/day.tsx:5:3",
		"Intl.PluralRules without a locale at app/plural.tsx:7:1",
		"Intl.DateTimeFormat without a locale and a timeZone at node_modules/datelib/index.js:10:2",
	} {
		if !strings.Contains(logs[i], want) {
			t.Errorf("warning %d lacks %q:\n%s", i, want, logs[i])
		}
		for _, advice := range []string{"en-US and UTC", "hydration", "timeZone"} {
			if !strings.Contains(logs[i], advice) {
				t.Errorf("warning %d lacks %q:\n%s", i, advice, logs[i])
			}
		}
	}
	// The stack names the page that called the package.
	if !strings.Contains(logs[3], "app/page.tsx:2:3") {
		t.Errorf("the warning lacks the caller of the package:\n%s", logs[3])
	}
	t.Log(logs[3])
}

// A built program does not check.
func TestProductionDoesNotWarnAboutIntlDefaults(t *testing.T) {
	defer swap(&logf, func(format string, args ...any) { t.Errorf("logged "+format, args...) })()
	p, err := NewPool(intlBundle, intlMap(), testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Render(context.Background(), Input{BuildID: "b1", URL: "/", Data: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
}
