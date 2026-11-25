package server

import (
	"reflect"
	"testing"
	"time"

	"github.com/liustack/tifa"
	internalcontent "github.com/liustack/tifa/internal/content"
)

// fill gives every field of v, and every field of the structs in it, a value other than the zero
// one. A slice gets one element, and an entry a body.
func fill(t *testing.T, v reflect.Value) {
	t.Helper()
	switch {
	case v.Type() == reflect.TypeFor[time.Time]():
		v.Set(reflect.ValueOf(time.Date(2026, 11, 26, 1, 2, 3, 4, time.UTC)))
	case v.Type() == reflect.TypeFor[internalcontent.Entry]():
		v.Set(reflect.ValueOf(siteEntry{internalcontent.NewHandle(&internalcontent.Body{Collection: "c", Language: "l", ID: "i", File: "f.html"})}))
	case v.Kind() == reflect.String:
		v.SetString("x")
	case v.Kind() == reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(t, v.Index(0))
	case v.Kind() == reflect.Struct:
		for i := range v.NumField() {
			fill(t, v.Field(i))
		}
	default:
		t.Fatalf("the key test cannot fill a %s: teach it, and siteKey", v.Type())
	}
}

// zeroEach calls check for each field of v and of the structs below it, after the field is set to
// its zero value, and restores it.
func zeroEach(v reflect.Value, name string, check func(name string)) {
	for i := range v.NumField() {
		f, field := v.Field(i), v.Type().Field(i)
		saved := reflect.New(f.Type()).Elem()
		saved.Set(f)
		f.SetZero()
		check(name + field.Name)
		f.Set(saved)
		switch {
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.Struct:
			zeroEach(f.Index(0), name+field.Name+".", check)
		}
	}
}

// A field that tifa.Sitemap or tifa.SitemapPage gets, and that siteKey does not hold, would let a
// change of it show an old answer. Every field is set, then each one cleared in turn: the key has
// to change.
func TestSiteKeyCoversEveryFieldOfTheSitemap(t *testing.T) {
	var sm tifa.Sitemap
	fill(t, reflect.ValueOf(&sm).Elem())
	base := siteKey("b", "/llms.txt", "en", sm)
	if base != siteKey("b", "/llms.txt", "en", sm) {
		t.Fatal("the key is not the same for the same list")
	}
	zeroEach(reflect.ValueOf(&sm).Elem(), "Sitemap.", func(name string) {
		if siteKey("b", "/llms.txt", "en", sm) == base {
			t.Errorf("%s is not in the key", name)
		}
	})
	// The parts of the entry that stand for it in the build.
	entry := func(c, l, id, file string) tifa.Sitemap {
		return tifa.Sitemap{Pages: []tifa.SitemapPage{{Content: siteEntry{internalcontent.NewHandle(&internalcontent.Body{Collection: c, Language: l, ID: id, File: file})}}}}
	}
	same := siteKey("b", "f", "", entry("c", "l", "i", "f.html"))
	for name, other := range map[string]tifa.Sitemap{
		"collection": entry("c2", "l", "i", "f.html"), "language": entry("c", "l2", "i", "f.html"),
		"id": entry("c", "l", "i2", "f.html"), "file": entry("c", "l", "i", "g.html"),
		"no entry": {Pages: []tifa.SitemapPage{{}}}, "an entry of no collection": {Pages: []tifa.SitemapPage{{Content: siteEntry{}}}},
	} {
		if siteKey("b", "f", "", other) == same {
			t.Errorf("the %s of an entry is not in the key", name)
		}
	}
	// The rest of the input.
	for name, key := range map[string]string{
		"build": siteKey("b2", "/llms.txt", "en", sm), "file": siteKey("b", "/llms-full.txt", "en", sm), "language": siteKey("b", "/llms.txt", "fr", sm),
	} {
		if key == base {
			t.Errorf("the %s is not in the key", name)
		}
	}
	// Moving text from one string to the next is another list.
	a := tifa.Sitemap{Name: "ab", Summary: "c"}
	b := tifa.Sitemap{Name: "a", Summary: "bc"}
	if siteKey("", "", "", a) == siteKey("", "", "", b) {
		t.Error("the key does not tell where a string ends")
	}
}
