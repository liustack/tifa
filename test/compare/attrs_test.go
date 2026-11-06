package compare_test

import (
	"reflect"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

func TestParseAttrsKeepsWhatAReaderFollowsOrFills(t *testing.T) {
	page := `<html lang="en-US"><head><title>x</title><meta name="description" content="d"><meta charset="utf-8">` +
		`<link rel="stylesheet" href="/a.css"><link rel="canonical" href="/c"><script>var a = '<a href="/no">'</script></head><body>` +
		`<a class="k" href="/store/en-US">Home</a><img src="/i.avif" srcset="/i.avif 480w, /j.avif 1200w" alt="A cup">` +
		`<template><a href="/hidden">h</a></template><input type="checkbox" name="n" checked value="1"><input type="text" value="">` +
		`<select name="s"><option value="a">A</option><option value="b" selected>B</option></select><button type="submit" disabled>Go</button></body></html>`
	want := []string{
		"html lang=en-US", "meta name=description", "meta content=d", "link rel=canonical", "link href=/c",
		"a href=/store/en-US", "img src=/i.avif", "img srcset=/i.avif 480w, /j.avif 1200w", "img alt=A cup",
		"input type=checkbox", "input name=n", "input value=1", "input checked=true",
		"input type=text", "input value=",
		"select name=s", "option value=a", "option value=b", "option selected=true",
		"button type=submit", "button disabled=true",
	}
	if got := compare.ParseAttrs([]byte(page)); !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestDiffAttrsNamesTheFirstDifferences(t *testing.T) {
	if d := diffAttrs([]string{"a href=/x"}, []string{"a href=/x"}); d != "" {
		t.Errorf("equal lists differ: %s", d)
	}
	if d := diffAttrs([]string{"a href=/x", "html lang=en-US"}, []string{"a href=/y"}); d == "" {
		t.Error("lists that differ in a link and in length are equal")
	}
}
