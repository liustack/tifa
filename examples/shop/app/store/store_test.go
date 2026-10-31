package store

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

func TestPricesFollowTheLocale(t *testing.T) {
	zh, _ := FindLocale("zh-CN")
	en, _ := FindLocale("en-US")
	if zh.Currency != "CNY" || en.Currency != "USD" || zh.Other != "en-US" || en.Other != "zh-CN" {
		t.Fatalf("locales %+v %+v", zh, en)
	}
	// Yuan stay as they are. Dollars are the yuan at 7.1, rounded up to the next 10 cents.
	for _, tc := range []struct{ fen, cny, usd int }{{32800, 32800, 4620}, {7100, 7100, 1000}, {7101, 7101, 1010}, {0, 0, 0}} {
		if got := Price(tc.fen, zh); got != tc.cny {
			t.Errorf("Price(%d, zh-CN) = %d, want %d", tc.fen, got, tc.cny)
		}
		if got := Price(tc.fen, en); got != tc.usd {
			t.Errorf("Price(%d, en-US) = %d, want %d", tc.fen, got, tc.usd)
		}
	}
	if _, err := FindLocale("fr-FR"); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("unknown locale: %v", err)
	}
	if (Text{"壶", "Kettle"}).In(en) != "Kettle" || (Text{"壶", "Kettle"}).In(zh) != "壶" {
		t.Error("Text.In picks the wrong language")
	}
}

// A visitor of /store goes to the storefront in the language their browser prefers, Chinese when
// it prefers none of the store's.
func TestPreferredFollowsAcceptLanguage(t *testing.T) {
	for header, want := range map[string]string{
		"":                              "zh-CN",
		"en-US,en;q=0.9":                "en-US",
		"en-GB":                         "en-US",
		"en":                            "en-US",
		"zh-CN,zh;q=0.9,en;q=0.8":       "zh-CN",
		"fr-FR,fr;q=0.9,en-US;q=0.8":    "en-US",
		"de-DE":                         "zh-CN",
		"zh-TW":                         "zh-CN",
		"not a language header ;;; q=x": "zh-CN",
	} {
		if got := Preferred(header).Code; got != want {
			t.Errorf("Preferred(%q) = %s, want %s", header, got, want)
		}
	}
}

func TestStoreEntryRedirectsToALanguage(t *testing.T) {
	r := httptest.NewRequest("GET", "/store", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	c, w := tifatest.NewCtx(t, r)
	if err := GET(c); err == nil || err.Error() != "tifa: redirect 302 to /store/en-US" {
		t.Errorf("got %v, want a redirect to /store/en-US", err)
	}
	if w.Header().Get("Vary") != "Accept-Language" {
		t.Errorf("Vary %q", w.Header().Get("Vary"))
	}
}
