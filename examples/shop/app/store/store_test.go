package store

import (
	"errors"
	"testing"

	"github.com/liustack/tifa"
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
