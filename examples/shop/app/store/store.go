// Package store holds what every page of the storefront shares: the languages it is offered in,
// the currency and time zone of each, and text kept in both languages. The storefront is the
// customer side of the shop studio, at /store/{lang}.
package store

import (
	"math"

	"github.com/liustack/tifa"
)

// Locale is one language of the storefront. Pages format money in Currency and times in
// TimeZone, with the language Code, so the server render and every browser write the same text.
type Locale struct {
	Code     string `json:"code"`
	Currency string `json:"currency"`
	TimeZone string `json:"timeZone"`
	// Other is the code of the other language, for the language switch.
	Other string `json:"other"`
}

var locales = []Locale{
	{Code: "zh-CN", Currency: "CNY", TimeZone: "Asia/Shanghai", Other: "en-US"},
	{Code: "en-US", Currency: "USD", TimeZone: "America/Los_Angeles", Other: "zh-CN"},
}

// FindLocale returns the locale of a code from the URL, or tifa.ErrNotFound.
func FindLocale(code string) (Locale, error) {
	for _, l := range locales {
		if l.Code == code {
			return l, nil
		}
	}
	return Locale{}, tifa.ErrNotFound
}

// Text is a text in both languages of the storefront: Chinese, then English.
type Text [2]string

// In returns the text in the language of l.
func (t Text) In(l Locale) string {
	if l.Code == "en-US" {
		return t[1]
	}
	return t[0]
}

// yuanPerDollar is the fixed rate the storefront shows dollar prices at. Prices are kept in
// yuan, and a real shop would set its dollar prices itself.
const yuanPerDollar = 7.1

// Price turns an amount in fen (cents of a yuan) into the minor unit of the locale's currency:
// fen for zh-CN, cents of a dollar for en-US, rounded up to the next 10 cents so that prices end
// in 0.
func Price(fen int, l Locale) int {
	if l.Currency == "CNY" {
		return fen
	}
	return int(math.Ceil(float64(fen)/yuanPerDollar/10)) * 10
}
