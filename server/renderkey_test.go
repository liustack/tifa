package server

import (
	"reflect"
	"testing"

	"github.com/liustack/tifa/internal/ssr"
)

// The HTML of a page is a function of ssr.Input, the SSR bundle and the render clock. The render
// cache keys a page by all of them, so a field of the input that changes the HTML must change the
// key. This test fails when a field is added to ssr.Input without being put in the key or on the
// list of fields that cannot reach the HTML of a cached page.
func TestRenderKeyCoversEveryFieldOfTheInput(t *testing.T) {
	// Fields that are not in the key, and why that is safe.
	notKeyed := map[string]string{
		"Nonce":     "Render draws it for each render and fills the slots it names before the page leaves, so no trace of it stays in the HTML",
		"Deadline":  "a time budget for the render, not a part of the page",
		"PageTexts": "the texts of Texts with markers in place of their tifa.HTML values, which follow from those texts",
		"Error":     "set only to render a page again after a failed render, whose answer is never cached or tagged",
	}
	base := ssr.Input{BuildID: "b", URL: "/p", Data: []byte(`{}`), Texts: []string{"t"}, Preload: "p", Session: []byte(`{"signedIn":false}`), SessionKey: "k", SessionHead: "h"}
	want := renderKey(base, "bundle")
	if got := renderKey(base, "bundle"); got != want {
		t.Fatalf("the key of the same input changes: %s then %s", want, got)
	}
	if renderKey(base, "other bundle") == want {
		t.Error("another SSR bundle gives the same key")
	}
	typ := reflect.TypeOf(base)
	for i := range typ.NumField() {
		field := typ.Field(i)
		if why, ok := notKeyed[field.Name]; ok {
			if why == "" {
				t.Errorf("%s: say why it can stay out of the key", field.Name)
			}
			continue
		}
		changed := base
		v := reflect.ValueOf(&changed).Elem().Field(i)
		switch v.Kind() {
		case reflect.String:
			v.SetString(v.String() + "x")
		case reflect.Slice:
			switch v.Type().Elem().Kind() {
			case reflect.Uint8:
				v.SetBytes(append(append([]byte(nil), v.Bytes()...), ' ', '1'))
			case reflect.String:
				v.Set(reflect.Append(v, reflect.ValueOf("more")))
			default:
				t.Fatalf("%s: teach the test to change a %s", field.Name, v.Type())
			}
		default:
			t.Fatalf("%s: teach the test to change a %s", field.Name, v.Type())
		}
		if renderKey(changed, "bundle") == want {
			t.Errorf("a change of %s leaves the key as it was", field.Name)
		}
	}
	// A text moved to the end of the previous one is another page.
	a, b := base, base
	a.Texts, b.Texts = []string{"ab", "c"}, []string{"a", "bc"}
	if renderKey(a, "bundle") == renderKey(b, "bundle") {
		t.Error("two splits of the same text give the same key")
	}
}
