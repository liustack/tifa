// Package keys renders a settings page of keys translated messages and 5000 formatted numbers,
// dates, plurals, relative times and lists in eight locales. The messages come with the data, as
// a backend loads them for i18next, in Chinese with English as the fallback.
package keys

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strconv"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Page struct {
	Keys      int    `json:"keys"`
	IntlCalls int    `json:"intlCalls"`
	Version   string `json:"version"`
	// Messages holds the messages of each language by group and key.
	Messages map[string]map[string]map[string]string `json:"messages"`
	Groups   []string                                `json:"groups"`
	Users    []string                                `json:"users"`
	Now      string                                  `json:"now"`
}

const (
	intlCalls = 5000
	perGroup  = 100
)

func Load(c *tifa.Ctx) (Page, error) {
	n, err := seed.Size(c.Param("keys"), 1, 50_000)
	if err != nil {
		return Page{}, err
	}
	r := seed.New("i18n", n)
	p := Page{Keys: n, IntlCalls: intlCalls, Now: seed.Base.Format("2006-01-02T15:04:05Z"), Messages: map[string]map[string]map[string]string{"zh-CN": {}, "en-US": {}}}
	for range 40 {
		p.Users = append(p.Users, seed.Name(r))
	}
	for i := range n {
		group := fmt.Sprintf("g%03d", i/perGroup)
		if i%perGroup == 0 {
			p.Groups = append(p.Groups, group)
			p.Messages["zh-CN"][group] = map[string]string{}
			p.Messages["en-US"][group] = map[string]string{}
		}
		key := "k" + strconv.Itoa(i)
		zhText, enText := message(r, i)
		// Every tenth Chinese message is missing, so i18next falls back to English.
		if i%10 != 9 {
			p.Messages["zh-CN"][group][key] = zhText
		}
		if i%6 == 2 {
			p.Messages["en-US"][group][key+"_one"] = "One " + enText
			p.Messages["en-US"][group][key+"_other"] = "{{count}} " + enText
			if i%10 != 9 {
				delete(p.Messages["zh-CN"][group], key)
				p.Messages["zh-CN"][group][key+"_other"] = "{{count}} 件" + zhText
			}
		} else {
			p.Messages["en-US"][group][key] = enText
		}
	}
	h := fnv.New64a()
	fmt.Fprint(h, n)
	p.Version = strconv.FormatUint(h.Sum64(), 36)
	return p, nil
}

var (
	zhPhrases = []string{
		"开启后，订单发货时会自动通知顾客，", "这项设置只对本店铺生效，", "修改会在下一次同步时生效，", "如果关闭，已经排队的消息仍会发出，",
		"退款金额会原路退回顾客的支付账户，", "库存低于安全线时，商品会自动下架，", "我们会保留最近九十天的操作记录，", "管理员可以随时撤销这项授权，",
		"同一顾客每天最多收到三条提醒。", "请先确认收件地址再打印面单。", "节假日期间发货时间可能顺延。", "优惠券不能和会员价叠加使用。",
	}
	enPhrases = []string{
		"When on, customers hear from you as soon as an order ships, ", "this setting applies to this shop only, ", "changes apply at the next sync, ",
		"messages already queued still go out, ", "refunds go back to the customer's original payment, ", "products below the safety stock are hidden, ",
		"we keep the activity log for ninety days, ", "an admin can revoke this access at any time. ", "a customer gets at most three reminders a day. ",
		"check the address before printing the label. ", "shipping may take longer over holidays. ", "coupons do not stack with member prices. ",
	}
)

// message returns the Chinese and English text of message i: plain, with a name, plural, nested,
// with a number, with an amount or with a date, in turn.
func message(r *rand.Rand, i int) (string, string) {
	zh, en := seed.Sentence(r, zhPhrases, 5), seed.Sentence(r, enPhrases, 4)
	switch i % 6 {
	case 1:
		return "{{name}}，" + zh, "{{name}}, " + en
	case 3:
		return zh + "（$t(g000.k0)）", en + " ($t(g000.k0))"
	case 4:
		return zh + "已处理 {{val, number}} 条。", en + " {{val, number}} handled."
	case 5:
		return zh + "合计 {{amount, currency(CNY)}}。", en + " Total {{amount, currency(CNY)}}."
	}
	return zh, en
}
