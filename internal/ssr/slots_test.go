package ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// ScriptString writes what seroval writes for a string in a script. The cases are the texts of the
// router test in internal/codegen, with the writing that test finds in the page.
func TestScriptString(t *testing.T) {
	for _, c := range []struct{ name, text, want string }{
		{"plain", `{"a":1}`, `{\"a\":1}`},
		{"empty", ``, ``},
		{"quote and backslash", `{"quote":"a \"b\"","slash":"C:\\temp"}`, `{\"quote\":\"a \\\"b\\\"\",\"slash\":\"C:\\\\temp\"}`},
		{"script end", `{"tag":"</script><!--"}`, `{\"tag\":\"\x3C/script>\x3C!--\"}`},
		{"line and paragraph separators", "x\u2028y\u2029z", `x\u2028y\u2029z`},
		{"line breaks", "a\nb\rc\td\be\ff", `a\nb\rc\td\be\ff`},
		{"other control characters stay", "a\x00b\x01c\x1fd\x7fe", "a\x00b\x01c\x1fd\x7fe"},
		{"surrogate pair escapes", `{"emoji":"\ud83d\ude00"}`, `{\"emoji\":\"\\ud83d\\ude00\"}`},
		{"accent and Chinese", `{"a":"é","b":"林夏"}`, `{\"a\":\"é\",\"b\":\"林夏\"}`},
		{"other characters that start with E2", "€ “quoted” \u2027\u202a", "€ “quoted” \u2027\u202a"},
		{"E2 80 cut short at the end", "a\u2028", `a\u2028`},
		{"bytes that are no UTF-8", "a\xffb\xc3\"", "a\ufffdb\ufffd\\\""},
		{"a cut separator", "a\xe2\x80", "a\ufffd\ufffd"},
		{"a lone surrogate written as bytes", "a\xed\xa0\x80b", "a\ufffd\ufffd\ufffdb"},
		{"a lone surrogate written as an escape stays text", `{"s":"\ud800 \udfff\ud83d"}`, `{\"s\":\"\\ud800 \\udfff\\ud83d\"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ScriptString(c.text); got != c.want {
				t.Errorf("ScriptString(%q) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// Whatever the characters of a text are, what ScriptString writes holds none that could end a
// script element or a JavaScript string, and reads back as the text.
func TestScriptStringReadsBack(t *testing.T) {
	var all strings.Builder
	for r := rune(0); r < 0x3000; r++ {
		if r >= 0xD800 && r < 0xE000 {
			continue
		}
		all.WriteRune(r)
	}
	all.WriteString("😀</script><!-- -->")
	text := all.String()
	written := ScriptString(text)
	for _, bad := range []string{"<", "\u2028", "\u2029", "\n", "\r"} {
		if strings.Contains(written, bad) {
			t.Errorf("the written text has %q", bad)
		}
	}
	if got := unscript(t, written); got != text {
		t.Errorf("reads back as another text: %.60q", got)
	}
}

// unscript reads the inside of a JavaScript string literal the way JavaScript does, for the
// escapes ScriptString writes.
func unscript(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case '"', '\\':
			b.WriteByte(s[i])
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'x':
			var n int
			for _, c := range s[i+1 : i+3] {
				n = n*16 + strings.IndexRune("0123456789abcdef", c|0x20)
			}
			b.WriteRune(rune(n))
			i += 2
		case 'u':
			var n int
			for _, c := range s[i+1 : i+5] {
				n = n*16 + strings.IndexRune("0123456789abcdef", c|0x20)
			}
			b.WriteRune(rune(n))
			i += 4
		default:
			t.Fatalf("escape \\%c in %q", s[i], s)
		}
	}
	return b.String()
}

// call is how seroval writes the data of a Load that the tifa.data adapter takes: the slot is the
// whole argument, inside the quotes. It is the only place a slot may be.
const call = `$_TSR.t.get("tifa.data")("`

func TestFillSlots(t *testing.T) {
	const nonce = "00112233445566778899aabbccddeeff"
	slot := "~" + nonce
	number := func(n int) string { return fmt.Sprintf("%s.%d.", slot, n) }
	texts := []string{`{"a":"</script>"}`, `[1,2]`, "x\xe2\x80\xa8y"}
	t.Run("fills each slot with the escaped text of its number", func(t *testing.T) {
		html := `<p>text</p><script data-tsr-stream-part="">$R[1]=` + call + number(2) + `");$R[2]=` + call + number(1) + `");$R[3]=` + call + number(0) + `")</script>`
		got, err := FillSlots(html, nonce, texts)
		if err != nil {
			t.Fatal(err)
		}
		wantHTML := `<p>text</p><script data-tsr-stream-part="">$R[1]=` + call + `x\` + `u2028y");$R[2]=` + call + `[1,2]");$R[3]=` + call + `{\"a\":\"\x3C/script>\"}")</script>`
		if got != wantHTML {
			t.Errorf("got  %s\nwant %s", got, wantHTML)
		}
	})
	t.Run("a page without slots comes back as it is", func(t *testing.T) {
		html := `<p>no data</p>`
		got, err := FillSlots(html, nonce, texts)
		if err != nil || got != html {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("a slot of another nonce is data and stays", func(t *testing.T) {
		other := strings.Repeat("f", 32)
		html := `<p>~` + other + `.0.</p><div title="~` + other + `.0."></div><script>` + call + `~` + other + `.0.")</script>`
		got, err := FillSlots(html, nonce, texts)
		if err != nil || got != html {
			t.Errorf("got %q, %v", got, err)
		}
	})
	for _, c := range []struct{ name, tail string }{
		{"a nonce with no slot after it", slot + `")</script>`},
		{"a nonce with no number", slot + `..")</script>`},
		{"a number that is not closed", slot + `.1")</script>`},
		{"a number with a letter", slot + `.1x.")</script>`},
		{"a number with no text", number(3) + `")</script>`},
		{"a number too large", slot + `.99999999999.")</script>`},
		{"a good slot and then a bad one", number(0) + `");` + call + slot + `!")</script>`},
		{"a nonce at the end", slot},
	} {
		t.Run("fails for "+c.name, func(t *testing.T) {
			html := `<script>` + call + c.tail
			if got, err := FillSlots(html, nonce, texts); !errors.Is(err, ErrSlot) {
				t.Errorf("got %q, %v, want ErrSlot", got, err)
			}
		})
	}
	// A real slot is filled where seroval writes it, as the whole string argument of the adapter's
	// call, inside a script element that runs. Anywhere else the text could end a quote, an attribute or
	// an element that the escape of a JavaScript string does not stop, so the page is not served.
	for _, c := range []struct{ name, html string }{
		{"an attribute", `<div title="` + number(0) + `"></div>`},
		{"an attribute in the form of the call", `<div title='` + call + number(0) + `")'></div>`},
		{"the text of the page", `<p>` + number(0) + `</p>`},
		{"the text of the page in the form of the call", `<p>` + call + number(0) + `")</p>`},
		{"a comment", `<!-- ` + call + number(0) + `") -->`},
		{"after a script ended", `<script>1</script><p>` + call + number(0) + `")</p>`},
		{"a script, not as a number", `<script>var x = ` + number(0) + `;</script>`},
		{"a script, in a string of another call", `<script>f("` + number(0) + `")</script>`},
		{"a script, in another adapter", `<script>$_TSR.t.get("tifa.other")("` + number(0) + `")</script>`},
		{"a script, with text around it in the string", `<script>` + call + `a` + number(0) + `")</script>`},
		{"a script, with the string going on", `<script>` + call + number(0) + `b")</script>`},
		{"a script, with single quotes", `<script>$_TSR.t.get('tifa.data')('` + number(0) + `')</script>`},
		{"a script that is data", `<script type="application/json">` + call + number(0) + `")</script>`},
		{"the end of the page", `<script>1</script>` + number(0)},
	} {
		t.Run("fails for a real slot in "+c.name, func(t *testing.T) {
			if got, err := FillSlots(c.html, nonce, texts); !errors.Is(err, ErrSlot) {
				t.Errorf("got %q, %v, want ErrSlot", got, err)
			}
		})
	}
	t.Run("the form seroval writes is filled with and without the reference before it", func(t *testing.T) {
		for _, html := range []string{
			`<script>` + call + number(1) + `")</script>`,
			`<script data-tsr-stream-part="">a;$R[4]=` + call + number(1) + `");b</script>`,
			`<p>x</p><script nonce="n">$_TSR.p(()=>$_TSR.router=($R=>$R[0]={matches:$R[1]=[{l:$R[2]=` + call + number(1) + `")}]})($R["tsr"]))</script>`,
		} {
			got, err := FillSlots(html, nonce, texts)
			if err != nil || !strings.Contains(got, call+`[1,2]")`) || strings.Contains(got, nonce) {
				t.Errorf("%s: got %q, %v", html, got, err)
			}
		}
	})
	t.Run("the nonce is 32 hex digits and new each time", func(t *testing.T) {
		a, b := NewNonce(), NewNonce()
		if len(a) != 32 || a == b || strings.Trim(a, "0123456789abcdef") != "" {
			t.Errorf("nonces %q and %q", a, b)
		}
	})
}

// slotsBundle writes a slot for each text the render input numbers, as seroval does for the data of
// a Load, and copies the URL and the first text of the input into the page as text, as a component
// copies data. It keeps the nonce of every render it saw.
const slotsBundle = `globalThis.seen = []
globalThis.startRender = function (json, ...texts) {
  const input = JSON.parse(json)
  seen.push(input.nonce)
  const slots = Object.values(input.data).map((r) => '$_TSR.t.get("tifa.data")("~' + input.nonce + '.' + r.text + '.")').join(';')
  globalThis.renderResult = '<script>' + slots + '</script><p>' + input.nonce.length + ' ' + new Set(seen).size + '/' + seen.length + ' ' + seen.includes('caller') + '</p><b>' + input.url + '</b><i>' + (texts[0] ?? '') + '</i>'
}
`

// Render hands the VM a nonce, and the page it returns has the texts in place of the slots.
func TestRenderFillsTheSlotsOfTheBundle(t *testing.T) {
	p := newTestPool(t, slotsBundle, testConfig(1))
	texts := []string{`{"name":"a \"quoted\" word","tag":"</script>"}`, "[\"line\xe2\x80\xa8separator\xe2\x80\xa9\"]"}
	in := Input{URL: "/a", Data: json.RawMessage(`{"app/a":{"status":200,"text":0},"app/b":{"status":200,"text":1}}`), Texts: texts}
	html, err := p.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	want := `<script>` + call + ScriptString(texts[0]) + `");` + call + ScriptString(texts[1]) + `")</script><p>32 1/1 false</p><b>/a</b><i>` + texts[0] + `</i>`
	if html != want {
		t.Errorf("html = %s\nwant   %s", html, want)
	}
}

// Data that spells a slot cannot choose the text that fills it: the nonce comes after the Loads
// ran, and is new for each render. The slots here are whole and in the form of the call, in the URL,
// in the text of a Load and in the text of a component, with the mark and with a nonce that is not
// the render's.
func TestDataCannotForgeASlot(t *testing.T) {
	p := newTestPool(t, slotsBundle, testConfig(1))
	forged := "~" + strings.Repeat("0", 32) + ".0."
	for _, c := range []struct {
		name string
		in   Input
		want []string
	}{
		{"the URL", Input{URL: forged, Data: json.RawMessage(`{"app/a":{"status":200,"text":0}}`), Texts: []string{`"secret"`}}, []string{"<b>" + forged + "</b>"}},
		{"the text of a Load", Input{URL: "/a", Data: json.RawMessage(`{"app/a":{"status":200,"text":0},"app/b":{"status":200,"text":1}}`), Texts: []string{`{"x":"` + forged + `"}`, `"secret"`}}, []string{`<i>{"x":"` + forged + `"}</i>`}},
		{"a component", Input{URL: "/" + call + forged + `")`, Data: json.RawMessage(`{"app/a":{"status":200,"text":0}}`), Texts: []string{`"secret"`}}, []string{"<b>/" + call + forged + `")</b>`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			html, err := p.Render(context.Background(), c.in)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range c.want {
				if !strings.Contains(html, want) {
					t.Errorf("the forged slot was filled or changed, want %s in %s", want, html)
				}
			}
		})
	}
}

// copyBundle is an SSR bundle that copies the real slot of the render, which it holds in the input,
// to the place its URL names, as a component that read the serialization of the adapter would.
const copyBundle = `globalThis.startRender = function (json, ...texts) {
  const input = JSON.parse(json)
  const slot = '~' + input.nonce + '.0.'
  const call = '$_TSR.t.get("tifa.data")("'
  const where = {
    '/attribute': '<div title="' + slot + '"></div>',
    '/attribute-call': "<div title='" + call + slot + "\")'></div>",
    '/text': '<p>' + slot + '</p>',
    '/other-script': '<script>var x = ' + slot + ';</script>',
    '/other-string': '<script>f("' + slot + '")</script>',
    '/data-script': '<script type="application/json">' + call + slot + '")</script>',
    '/good': '<script>' + call + slot + '")</script>',
  }[input.url]
  globalThis.renderResult = '<p>page</p>' + where
}
`

// A component that copies the real slot of its render into an attribute, the text of the page or a
// place of a script that is no string of the adapter's call must not get the text of the data
// filled in there, which would write the escapes of a JavaScript string where HTML reads them
// differently. The page is not served.
func TestRenderFailsForARealSlotCopiedAway(t *testing.T) {
	p := newTestPool(t, copyBundle, testConfig(1))
	in := func(url string) Input {
		return Input{URL: url, Data: json.RawMessage(`{"app/a":{"status":200,"text":0}}`), Texts: []string{`{"x":" onmouseover=alert(1) x=\"","y":"</script>"}`}}
	}
	for _, url := range []string{"/attribute", "/attribute-call", "/text", "/other-script", "/other-string", "/data-script"} {
		if html, err := p.Render(context.Background(), in(url)); !errors.Is(err, ErrSlot) || html != "" {
			t.Errorf("%s: html %q, err %v, want ErrSlot", url, html, err)
		}
	}
	html, err := p.Render(context.Background(), in("/good"))
	if err != nil || !strings.Contains(html, call+`{\"x\":\" onmouseover=alert(1) x=\\\"\",\"y\":\"\x3C/script>\"}")`) {
		t.Errorf("/good: html %q, err %v", html, err)
	}
}

// Every render draws its own nonce, and one a caller puts into the Input is not used.
func TestRenderDrawsANonceForEachRender(t *testing.T) {
	p := newTestPool(t, slotsBundle, testConfig(1))
	var html string
	for range 5 {
		var err error
		html, err = p.Render(context.Background(), Input{URL: "/a", Data: json.RawMessage(`{}`), Nonce: "caller"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(html, "<p>32 5/5 false</p>") {
		t.Errorf("html = %s, want 5 different nonces of 32 digits and not the caller's", html)
	}
}

// A page that holds the mark and the nonce anywhere but in a whole slot is not served.
func TestRenderFailsForANonceThatIsNoSlot(t *testing.T) {
	p := newTestPool(t, `globalThis.startRender = function (json) {
  globalThis.renderResult = '<p>~' + JSON.parse(json).nonce + '</p>'
}
`, testConfig(1))
	_, err := p.Render(context.Background(), Input{URL: "/a", Data: json.RawMessage(`{}`)})
	if !errors.Is(err, ErrSlot) {
		t.Fatalf("err = %v, want ErrSlot", err)
	}
}

// BenchmarkFillSlots is what Render does after the VM for a page with the data of a table of 1,000
// rows (92 KB of JSON text) and one of a few KB, the sizes of the pages of examples/shop. The slot is
// in the hydration script after the page, where the router writes it, or in the middle of the page,
// where the search for the script that holds it has furthest to go.
func BenchmarkFillSlots(b *testing.B) {
	// filler is HTML as React writes it, with all the digits a nonce can start with.
	const filler = `<div class="card-0123 flex items-center"><a href="/orders/SH-1042">Order 1042 of 2026-10-31</a><span>Paid, ready to ship: 3 items, total 1,689.00</span></div>`
	table := "[" + strings.Repeat(`{"id":"3f2b8c1e-5d7a-4c9e-8f21-0a1b2c3d4e5f","ref":"8c1e5d7a-4c9e-8f21-0a1b-2c3d4e5f3f2b"},`, 1000) + `{"id":"x","ref":"y"}]`
	orders := `{"orders":[` + strings.Repeat(`{"id":"SH-1042","customer":"林夏","note":"a \"b\""},`, 100) + `{}]}`
	for _, c := range []struct {
		name   string
		text   string
		before int // copies of filler before the script
		after  int // and after it
	}{
		{"92KB text in a page of 200KB", table, 1300, 0},
		{"6KB text in a page of 130KB", orders, 850, 0},
		{"6KB text in a page of 1MB", orders, 6500, 0},
		{"6KB text in the middle of a page of 221KB", orders, 700, 700},
	} {
		b.Run(c.name, func(b *testing.B) {
			nonce := NewNonce()
			html := strings.Repeat(filler, c.before) + `<script data-tsr-stream-part="">self.$R=1;$_TSR.router=($R=>$R[0]={l:$R[4]=` + call + `~` + nonce + `.0.")})($R["tsr"]);$_TSR.e()</script>` + strings.Repeat(filler, c.after)
			b.SetBytes(int64(len(html) + len(c.text)))
			for b.Loop() {
				if _, err := FillSlots(html, nonce, []string{c.text}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	b.Run("NewNonce", func(b *testing.B) {
		for b.Loop() {
			NewNonce()
		}
	})
}
