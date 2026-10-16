package ssr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
