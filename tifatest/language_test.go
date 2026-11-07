package tifatest

import "testing"

func TestCtxHasTheLanguageOfItsPath(t *testing.T) {
	site := []string{"zh-CN", "en-US"}
	for _, tc := range []struct {
		languages []string
		path, want string
	}{
		{site, "/en-US/store/mine", "en-US"},
		{site, "/en-US", "en-US"},
		{site, "/store/mine", "zh-CN"},
		{site, "/zh-CN/store", "zh-CN"},
		{site, "/fr/store", "zh-CN"},
		{site, "/en-USA/store", "zh-CN"},
		{site, "/", "zh-CN"},
		{nil, "/en-US/store", ""},
	} {
		if got := languageOf(tc.languages, tc.path); got != tc.want {
			t.Errorf("languageOf(%q, %q) = %q, want %q", tc.languages, tc.path, got, tc.want)
		}
	}
}
