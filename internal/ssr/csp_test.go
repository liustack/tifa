package ssr

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// The marks of the render come out of the page, which is then what the render writes without
// them, and each marked script and style gives the hash of its content as the browser reads it:
// line breaks as \n and NUL as U+FFFD. An empty element, such as a script with src, gives none, and
// the same content gives one hash.
func TestTakeMarks(t *testing.T) {
	const nonce = "0123456789abcdef0123456789abcdef"
	m := ` nonce="` + nonce + `"`
	page := `<html><head><script` + m + `>a()` + "\r\n" + `b()` + "\r" + `</script>` +
		`<style` + m + ` media="screen">p{}</style>` +
		`<script` + m + ` type="module" src="/assets/a.js"></script>` +
		`<script>notMarked()</script>` +
		`<script` + m + ` data-x="a>b">a()` + "\n" + `b()` + "\n" + `</script>` +
		`</head><body><script` + m + `>c("` + "\x00" + `")</SCRIPT></body></html>`
	got, err := takeMarks(page, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.ReplaceAll(page, m, ""); got.HTML != want {
		t.Errorf("html\n%q\nwant\n%q", got.HTML, want)
	}
	if want := []string{sha("a()\nb()\n"), sha("c(\"�\")")}; !slices.Equal(got.Scripts, want) {
		t.Errorf("scripts %q, want %q", got.Scripts, want)
	}
	if want := []string{sha("p{}")}; !slices.Equal(got.Styles, want) {
		t.Errorf("styles %q, want %q", got.Styles, want)
	}
	if got, err := takeMarks("<p>no marks</p>", nonce); err != nil || got.HTML != "<p>no marks</p>" || got.Scripts != nil || got.Styles != nil {
		t.Errorf("a page without marks: %+v, %v", got, err)
	}
}

// A mark anywhere but right after <script or <style, or on an element that does not end, is not
// served: tifa would not know what the mark stands for. Nor is the nonce outside a mark.
func TestTakeMarksFailsForAStrayMark(t *testing.T) {
	const nonce = "0123456789abcdef0123456789abcdef"
	m := ` nonce="` + nonce + `"`
	for _, page := range []string{
		`<link` + m + ` rel="preload">`,
		`<script id="a"` + m + `>a()</script>`,
		`<p>` + m + `</p>`,
		`<script` + m + `>a()`,
		`<style` + m + `>p{}</script>`,
		`<script` + m + ` src="/a.js"`,
		`<script` + m + `>a()</script><p>` + nonce + `</p>`,
		`<script nonce='` + nonce + `'>a()</script>`,
	} {
		if got, err := takeMarks(page, nonce); !errors.Is(err, ErrMark) || got.HTML != "" {
			t.Errorf("%s: %+v, %v", page, got, err)
		}
	}
}

// InlineHashes reads a document Go wrote: every inline script that runs and every style, in any
// case of the tag, and no script with src or of a JSON type.
func TestInlineHashes(t *testing.T) {
	doc := `<!DOCTYPE html><html><head><script>a()</script><SCRIPT type="module">import "x"</Script>` +
		`<script type="module" src="/@vite/client"></script><script src='/a.js' >ignored()</script>` +
		`<script type="application/json" id="tifa-data">{"a":1}</script><script type="application/ld+json">{}</script>` +
		`<script type="importmap">{"imports":{}}</script><style>p{}</style><style></style><scripts>no</scripts>` +
		`<script>a()</script></head><body></body></html>`
	scripts, styles := InlineHashes(doc)
	if want := []string{sha("a()"), sha(`import "x"`), sha(`{"imports":{}}`)}; !slices.Equal(scripts, want) {
		t.Errorf("scripts %q, want %q", scripts, want)
	}
	if want := []string{sha("p{}")}; !slices.Equal(styles, want) {
		t.Errorf("styles %q, want %q", styles, want)
	}
}

// markBundle writes the marks of the render on a script and a style when the input asks for CSP,
// with a slot in the script, and on nothing otherwise.
const markBundle = `globalThis.startRender = function (json, ...texts) {
  const input = JSON.parse(json)
  const mark = input.csp ? ' nonce="' + input.nonce + '"' : ''
  const slots = Object.values(input.data).map((r) => '$_TSR.t.get("tifa.data")("~' + input.nonce + '.' + r.text + '.")').join(';')
  globalThis.renderResult = '<style' + mark + '>p{}</style><p>page</p><script' + mark + '>' + slots + '</script>'
}
`

// With CSP, RenderPage hands back the page without the marks and the hashes of the marked
// elements as the browser gets them, the data of the slots filled in. Without it the page has no
// marks and no hashes.
func TestRenderPageHashesTheMarkedElements(t *testing.T) {
	p := newTestPool(t, markBundle, testConfig(1))
	in := Input{URL: "/a", Data: json.RawMessage(`{"app/a":{"status":200,"text":0}}`), Texts: []string{`{"a":"</script>"}`}}
	want := `<style>p{}</style><p>page</p><script>` + call + ScriptString(in.Texts[0]) + `")</script>`
	plain, err := p.RenderPage(context.Background(), in)
	if err != nil || plain.HTML != want || plain.Scripts != nil || plain.Styles != nil {
		t.Fatalf("without CSP: %+v, %v", plain, err)
	}
	in.CSP = true
	page, err := p.RenderPage(context.Background(), in)
	if err != nil || page.HTML != want {
		t.Fatalf("with CSP: %+v, %v", page, err)
	}
	if want := []string{sha(call + ScriptString(in.Texts[0]) + `")`)}; !slices.Equal(page.Scripts, want) {
		t.Errorf("scripts %q, want %q", page.Scripts, want)
	}
	if want := []string{sha("p{}")}; !slices.Equal(page.Styles, want) {
		t.Errorf("styles %q, want %q", page.Styles, want)
	}
	if html, err := p.Render(context.Background(), in); err != nil || html != want {
		t.Errorf("Render: %q, %v", html, err)
	}
}

// BenchmarkTakeMarks is what RenderPage adds after the VM with CSP, for a page of the size of the
// pages of examples/shop: the marks of a head script, a head style, the scroll restoration script
// in the root and two hydration scripts, the last holding 6 KB of data.
func BenchmarkTakeMarks(b *testing.B) {
	const filler = `<div class="card-0123 flex items-center"><a href="/orders/SH-1042">Order 1042 of 2026-10-31</a><span>Paid, ready to ship: 3 items, total 1,689.00</span></div>`
	nonce := NewNonce()
	m := ` nonce="` + nonce + `"`
	data := strings.Repeat(`{\"id\":\"SH-1042\",\"customer\":\"林夏\",\"note\":\"a \\\"b\\\"\"},`, 100)
	page := `<!DOCTYPE html><html><head><meta charset="utf-8"><!--tifa-head--><script` + m + `>window.a=1</script><style` + m + `>.a{color:red}</style><!--/tifa-head--></head><body><div id="tifa-root">` +
		`<script` + m + `>(function(){var s=sessionStorage.getItem("tsr-scroll");if(s)scrollTo(0,0)})();document.currentScript.remove()</script>` +
		strings.Repeat(filler, 850) + `</div><script` + m + ` data-tsr-stream-part="">self.$R=1;$_TSR.router=($R=>$R[0]={l:` + call + data + `")})($R["tsr"]);$_TSR.e()</script>` +
		`<script` + m + `>$_TSR.h()</script></body></html>`
	b.SetBytes(int64(len(page)))
	for b.Loop() {
		if _, err := takeMarks(page, nonce); err != nil {
			b.Fatal(err)
		}
	}
}
