package server

import (
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

// jsonApp has a page whose Load returns the value of the query parameter v from values, with SSR
// and without.
func jsonApp(values map[string]any) App {
	load := func(c *tifa.Ctx) (any, error) { return values[c.Query("v")], nil }
	return App{
		Routes: []Route{
			{ID: "app/page", Path: "/p", Page: true, SSR: true, Load: load},
			{ID: "app/plain", Path: "/plain", Page: true, SSR: false, Load: load},
		},
	}
}

type emptyObject struct{}

type withLists struct {
	Items []string       `json:"items"`
	Tags  map[string]int `json:"tags"`
	Raw   []byte         `json:"raw"`
}

// The data of a Load is written as json v2 writes it: a nil slice is [], a nil map is {}, a nil
// []byte is "", and map keys are sorted. The render gets that text, and navigation data carries it.
func TestLoadDataIsJSONv2(t *testing.T) {
	values := map[string]any{
		"lists":  withLists{},
		"sorted": map[string]int{"b": 2, "a": 1, "c": 3},
	}
	s, r := newFrontServer(t, jsonApp(values))
	cases := map[string]string{
		"lists":  `{"items":[],"tags":{},"raw":""}`,
		"sorted": `{"a":1,"b":2,"c":3}`,
	}
	for v, want := range cases {
		if w := get(s, "/p?v="+v); w.Code != 200 {
			t.Fatalf("%s: %d %s", v, w.Code, w.Body.String())
		}
		r.mu.Lock()
		texts := r.inputs[len(r.inputs)-1].Texts
		r.mu.Unlock()
		if len(texts) != 1 || texts[0] != want {
			t.Errorf("%s: the render got %q, want %s", v, texts, want)
		}
		w := getData(s, "/p?v="+v, "b1", "1")
		if body := w.Body.String(); !strings.Contains(body, `"data":`+want) {
			t.Errorf("%s: navigation data %s lacks %s", v, body, want)
		}
	}
}

// json v2 leaves out a field with omitempty when its value is empty JSON: {}, [], "" or null. The
// data of a Load is kept whatever it is, so a Load whose value is empty JSON still has data.
func TestEmptyLoadDataIsKept(t *testing.T) {
	values := map[string]any{
		"object": emptyObject{},
		"array":  []int{},
		"string": "",
	}
	s, _ := newFrontServer(t, jsonApp(values))
	want := map[string]string{"object": `{}`, "array": `[]`, "string": `""`}
	for v, data := range want {
		w := getData(s, "/p?v="+v, "b1", "1")
		if body := w.Body.String(); !strings.Contains(body, `{"status":200,"data":`+data+`}`) {
			t.Errorf("%s: navigation data %s, want the data %s", v, body, data)
		}
		w = get(s, "/plain?v="+v)
		if body := w.Body.String(); !strings.Contains(body, `"app/plain":{"status":200,"json":"`+strings.ReplaceAll(data, `"`, `\"`)+`"}`) {
			t.Errorf("%s: the page without SSR carries %s, want the data %s", v, body, data)
		}
	}
}

// A page without SSR carries its data in a script element. The data of the app is written without
// HTML escapes, so the document writes <, >, & and U+2028 and U+2029 as escapes: no text of the app
// can end the element.
func TestPageWithoutSSREscapesItsDataForHTML(t *testing.T) {
	values := map[string]any{"hostile": map[string]string{"x": "</script><!--<script>&\u2028\u2029"}}
	s, _ := newFrontServer(t, jsonApp(values))
	w := get(s, "/plain?v=hostile")
	body := w.Body.String()
	start := strings.Index(body, `<script type="application/json" id="tifa-data">`)
	if w.Code != 200 || start < 0 {
		t.Fatalf("%d %s", w.Code, body)
	}
	data := body[start+len(`<script type="application/json" id="tifa-data">`):]
	end := strings.Index(data, "</script>")
	if end < 0 || data[end:] != "</script></body></html>" {
		t.Fatalf("the data ends its script early: %s", data)
	}
	data = data[:end]
	for _, bad := range []string{"<", ">", "&", "\u2028", "\u2029"} {
		if strings.Contains(data, bad) {
			t.Errorf("the data holds %q: %s", bad, data)
		}
	}
	if want := "\\u003c/script\\u003e\\u003c!--\\u003cscript\\u003e\\u0026\\u2028\\u2029"; !strings.Contains(data, want) {
		t.Errorf("the data %s lacks %s", data, want)
	}
}
