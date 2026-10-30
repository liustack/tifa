package ssr

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// argumentsBundle leaves in its HTML what startRender was called with, as JavaScript got it: the
// JSON of the input without its nonce, which the page may hold only in a slot, each text after it,
// and how many arguments there were.
const argumentsBundle = `globalThis.startRender = function (json, ...texts) {
  json = json.replace(/,"nonce":"[0-9a-f]{32}"/, '')
  globalThis.renderResult = '<pre>' + JSON.stringify({ json, texts, count: arguments.length }) + '</pre>'
}
`

type arguments struct {
	JSON  string   `json:"json"`
	Texts []string `json:"texts"`
	Count int      `json:"count"`
}

func renderArguments(t *testing.T, p *Pool, in Input) arguments {
	t.Helper()
	html, err := p.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var got arguments
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(html, "<pre>"), "</pre>")), &got); err != nil {
		t.Fatalf("%v: %s", err, html)
	}
	return got
}

// The JSON text of the data of each Load reaches startRender as an argument of its own, as Go
// wrote it. It is neither escaped into the JSON of the input nor read out of it: the input holds
// the statuses and, for each text, the number of its argument.
func TestRenderGivesTheTextsToStartRenderAsArguments(t *testing.T) {
	p := newTestPool(t, argumentsBundle, testConfig(1))
	texts := []string{
		`{"name":"a \"quoted\" word","path":"C:\\temp\\x"}`,
		"[\"line\u2028separator\u2029\",\"</script><!--\",\"\\u2028\"]",
		`{"emoji":"\ud83d\ude00","accent":"é","zero":"\u0000"}`,
		`"just a string"`,
		"",
		strings.Repeat(`{"k":["v","</script>"]},`, 20000),
	}
	in := Input{BuildID: "b1", URL: "/orders/1", Data: json.RawMessage(`{"app/a":{"status":200,"text":0},"app/b":{"status":200,"text":1}}`), Texts: texts, Preload: "<link>"}
	got := renderArguments(t, p, in)
	if got.Count != 1+len(texts) {
		t.Fatalf("startRender got %d arguments, want %d", got.Count, 1+len(texts))
	}
	if !reflect.DeepEqual(got.Texts, texts) {
		for i := range texts {
			if i >= len(got.Texts) || got.Texts[i] != texts[i] {
				t.Errorf("text %d: got %.80q, want %.80q", i, got.Texts[i], texts[i])
			}
		}
	}
	// What the input holds is the JSON of Input without its texts. Go escapes <, > and & in it.
	want, err := json.Marshal(Input{BuildID: "b1", URL: "/orders/1", Data: in.Data, Preload: "<link>"})
	if err != nil {
		t.Fatal(err)
	}
	if got.JSON != string(want) {
		t.Errorf("the input is %s, want %s", got.JSON, want)
	}
	if strings.Contains(got.JSON, "quoted") || strings.Contains(got.JSON, "separator") {
		t.Errorf("the input holds a text: %s", got.JSON)
	}
}

// A render without data has no arguments after the input, and the texts of one render stay out of
// the next one on the same VM.
func TestRenderPassesNoTextsWhenThereAreNone(t *testing.T) {
	p := newTestPool(t, argumentsBundle, testConfig(1))
	first := renderArguments(t, p, Input{URL: "/a", Data: json.RawMessage(`{"app/a":{"status":200,"text":0}}`), Texts: []string{`{"a":1}`, `{"b":2}`}})
	if first.Count != 3 {
		t.Fatalf("startRender got %d arguments, want 3", first.Count)
	}
	for _, in := range []Input{
		{URL: "/b", Data: json.RawMessage(`{}`)},
		{URL: "/c", Data: json.RawMessage(`{"app/a":{"status":404}}`), Texts: []string{}},
	} {
		got := renderArguments(t, p, in)
		if got.Count != 1 || len(got.Texts) != 0 {
			t.Errorf("%s: startRender got %d arguments and the texts %q, want only the input", in.URL, got.Count, got.Texts)
		}
	}
}

// Go bytes that are no UTF-8, which a MarshalJSON method may write into the text of a Load's data,
// reach JavaScript as U+FFFD for each of them, as they did inside the JSON of the input.
func TestRenderReplacesBytesThatAreNoUTF8InTexts(t *testing.T) {
	p := newTestPool(t, argumentsBundle, testConfig(1))
	bad := "{\"k\":\"a\xffb\xc3\"}"
	viaInput, err := json.Marshal(map[string]string{"text": bad})
	if err != nil {
		t.Fatal(err)
	}
	var want struct{ Text string }
	if err := json.Unmarshal(viaInput, &want); err != nil {
		t.Fatal(err)
	}
	got := renderArguments(t, p, Input{URL: "/a", Data: json.RawMessage(`{}`), Texts: []string{bad}})
	if len(got.Texts) != 1 || got.Texts[0] != want.Text {
		t.Errorf("text = %q, want %q", got.Texts, want.Text)
	}
}
