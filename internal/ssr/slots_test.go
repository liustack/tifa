package ssr

import (
	"context"
	"encoding/json"
	"errors"
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

func TestFillSlots(t *testing.T) {
	const nonce = "00112233445566778899aabbccddeeff"
	slot := "~" + nonce
	texts := []string{`{"a":"</script>"}`, `[1,2]`, "x\u2028y"}
	t.Run("fills each slot with the escaped text of its number", func(t *testing.T) {
		html := `<p>` + slot + `.0.</p><script>f("` + slot + `.2.","` + slot + `.1.","` + slot + `.0.")</script>`
		got, err := FillSlots(html, nonce, texts)
		if err != nil {
			t.Fatal(err)
		}
		wantHTML := `<p>` + `{\"a\":\"\x3C/script>\"}` + `</p><script>f("x\u2028y","[1,2]","{\"a\":\"\x3C/script>\"}")</script>`
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
		html := `<p>~` + other + `.0.</p>`
		got, err := FillSlots(html, nonce, texts)
		if err != nil || got != html {
			t.Errorf("got %q, %v", got, err)
		}
	})
	for _, c := range []struct{ name, html string }{
		{"a nonce with no slot after it", `<p>` + slot + `</p>`},
		{"a nonce with no number", `<p>` + slot + `..</p>`},
		{"a number that is not closed", `<p>` + slot + `.1</p>`},
		{"a number with a letter", `<p>` + slot + `.1x.</p>`},
		{"a number with no text", `<p>` + slot + `.3.</p>`},
		{"a number too large", `<p>` + slot + `.99999999999.</p>`},
		{"a good slot and then a bad one", `<p>` + slot + `.0.` + slot + `!</p>`},
		{"a nonce at the end", `<p>` + slot},
	} {
		t.Run("fails for "+c.name, func(t *testing.T) {
			if got, err := FillSlots(c.html, nonce, texts); !errors.Is(err, ErrSlot) {
				t.Errorf("got %q, %v, want ErrSlot", got, err)
			}
		})
	}
	t.Run("the nonce is 32 hex digits and new each time", func(t *testing.T) {
		a, b := NewNonce(), NewNonce()
		if len(a) != 32 || a == b || strings.Trim(a, "0123456789abcdef") != "" {
			t.Errorf("nonces %q and %q", a, b)
		}
	})
}

// slotsBundle writes a slot for each text the render input numbers, in a string of a script, as the
// bundle of tifa does for the data of a Load, and copies the URL of the input into the page, as a
// component copies data. It keeps the nonce of every render it saw.
const slotsBundle = `globalThis.seen = []
globalThis.startRender = function (json, ...texts) {
  const input = JSON.parse(json)
  seen.push(input.nonce)
  const slots = Object.values(input.data).map((r) => '"~' + input.nonce + '.' + r.text + '."').join(',')
  globalThis.renderResult = '<script>f(' + slots + ')</script><p>' + input.nonce.length + ' ' + new Set(seen).size + '/' + seen.length + ' ' + seen.includes('caller') + '</p><b>' + input.url + '</b>'
}
`

// Render hands the VM a nonce, and the page it returns has the texts in place of the slots.
func TestRenderFillsTheSlotsOfTheBundle(t *testing.T) {
	p := newTestPool(t, slotsBundle, testConfig(1))
	texts := []string{`{"name":"a \"quoted\" word","tag":"</script>"}`, "[\"line\u2028separator\u2029\"]"}
	in := Input{URL: "/a", Data: json.RawMessage(`{"app/a":{"status":200,"text":0},"app/b":{"status":200,"text":1}}`), Texts: texts}
	html, err := p.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	want := `<script>f("` + ScriptString(texts[0]) + `","` + ScriptString(texts[1]) + `")</script><p>32 1/1 false</p><b>/a</b>`
	if html != want {
		t.Errorf("html = %s\nwant   %s", html, want)
	}
}

// Data that spells a slot cannot choose the text that fills it: the nonce comes after the Loads
// ran, and is new for each render.
func TestDataCannotForgeASlot(t *testing.T) {
	p := newTestPool(t, slotsBundle, testConfig(1))
	forged := strings.Repeat("0", 32) + ".0."
	in := Input{URL: forged, Data: json.RawMessage(`{"app/a":{"status":200,"text":0}}`), Texts: []string{`"secret"`}}
	html, err := p.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<b>"+forged+"</b>") || strings.Contains(html, "<b>\"secret\"") {
		t.Errorf("the forged slot was filled or changed: %s", html)
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
// rows (92 KB of JSON text) and one of a few KB, the sizes of the pages of examples/shop.
func BenchmarkFillSlots(b *testing.B) {
	// filler is HTML as React writes it, with all the digits a nonce can start with.
	const filler = `<div class="card-0123 flex items-center"><a href="/orders/SH-1042">Order 1042 of 2026-10-31</a><span>Paid, ready to ship: 3 items, total 1,689.00</span></div>`
	table := "[" + strings.Repeat(`{"id":"3f2b8c1e-5d7a-4c9e-8f21-0a1b2c3d4e5f","ref":"8c1e5d7a-4c9e-8f21-0a1b-2c3d4e5f3f2b"},`, 1000) + `{"id":"x","ref":"y"}]`
	orders := `{"orders":[` + strings.Repeat(`{"id":"SH-1042","customer":"林夏","note":"a \"b\""},`, 100) + `{}]}`
	for _, c := range []struct {
		name  string
		text  string
		pages int // copies of filler around the slot
	}{
		{"92KB text in a page of 200KB", table, 1300},
		{"6KB text in a page of 130KB", orders, 850},
		{"6KB text in a page of 1MB", orders, 6500},
	} {
		b.Run(c.name, func(b *testing.B) {
			nonce := NewNonce()
			html := strings.Repeat(filler, c.pages) + `<script>f("~` + nonce + `.0.")</script>` + strings.Repeat(filler, 100)
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
