// Package receipt formats an amount and a time in Chinese, with the locale and the time zone
// in its data, so the server render and the browser agree.
package receipt

import "github.com/liustack/tifa"

type Receipt struct {
	Amount   float64 `json:"amount"`
	PaidAt   string  `json:"paidAt"`
	Locale   string  `json:"locale"`
	TimeZone string  `json:"timeZone"`
}

func Load(c *tifa.Ctx) (Receipt, error) {
	return Receipt{Amount: 1234.5, PaidAt: "2025-08-16T20:05:09Z", Locale: "zh-CN", TimeZone: "Asia/Shanghai"}, nil
}
