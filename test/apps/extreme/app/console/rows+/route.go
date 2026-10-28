// Package rows is a busy day in a shop's admin, sized after the react-admin demo and a long
// GitHub issue: the dashboard of the day with its charts, an order table of rows rows, and a
// support ticket with 300 comments written in Markdown.
package rows

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type NavItem struct {
	Label string `json:"label"`
	Icon  string `json:"icon"`
	Count int    `json:"count"`
}

type NavGroup struct {
	Title string    `json:"title"`
	Items []NavItem `json:"items"`
}

type Notice struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	At     string `json:"at"`
	Unread bool   `json:"unread"`
}

type KPI struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Kind  string  `json:"kind"` // money, count or percent
	Delta float64 `json:"delta"`
	Spark []int   `json:"spark"`
}

type Day struct {
	Date       string `json:"date"`
	RevenueFen int    `json:"revenueFen"`
	Orders     int    `json:"orders"`
	Refunds    int    `json:"refunds"`
}

type Series struct {
	Name   string `json:"name"`
	Values []int  `json:"values"`
}

type Share struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type Item struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type Order struct {
	ID        string   `json:"id"`
	Customer  string   `json:"customer"`
	Email     string   `json:"email"`
	Items     []Item   `json:"items"`
	AmountFen int      `json:"amountFen"`
	Status    string   `json:"status"`
	Payment   string   `json:"payment"`
	Carrier   string   `json:"carrier"`
	Tracking  string   `json:"tracking"`
	City      string   `json:"city"`
	CreatedAt string   `json:"createdAt"`
	Tags      []string `json:"tags"`
	Risk      string   `json:"risk"` // low, medium or high
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
}

type Event struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"` // comment, labeled, assigned, referenced, closed or reopened
	Author    string     `json:"author"`
	Login     string     `json:"login"`
	At        string     `json:"at"`
	Body      string     `json:"body"`
	Edited    bool       `json:"edited"`
	Reactions []Reaction `json:"reactions"`
}

type Ticket struct {
	Number       int      `json:"number"`
	Title        string   `json:"title"`
	State        string   `json:"state"`
	Opened       string   `json:"opened"`
	Author       string   `json:"author"`
	Labels       []Label  `json:"labels"`
	Assignees    []string `json:"assignees"`
	Milestone    string   `json:"milestone"`
	Participants []string `json:"participants"`
	Events       []Event  `json:"events"`
}

type Audit struct {
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	At     string `json:"at"`
}

type Console struct {
	Rows     int    `json:"rows"`
	TimeZone string `json:"timeZone"`
	Today    string `json:"today"`
	Shop     string `json:"shop"`
	User     string `json:"user"`
	Login    string `json:"login"`

	Nav     []NavGroup `json:"nav"`
	Notices []Notice   `json:"notices"`
	KPIs    []KPI      `json:"kpis"`

	Days       []Day     `json:"days"`
	Channels   []Series  `json:"channels"`
	Funnel     []Share   `json:"funnel"`
	Categories []Share   `json:"categories"`
	Heat       []int     `json:"heat"`
	Scatter    []float64 `json:"scatter"` // amount and item count of each order in turn

	Orders []Order `json:"orders"`
	Ticket Ticket  `json:"ticket"`
	Audit  []Audit `json:"audit"`
}

const comments = 300

func Load(c *tifa.Ctx) (Console, error) {
	n, err := seed.Size(c.Param("rows"), 1, 20_000)
	if err != nil {
		return Console{}, err
	}
	return Build(n), nil
}

// Build makes the admin page with an order table of n rows.
func Build(n int) Console {
	r := seed.New("console", n)
	out := Console{Rows: n, TimeZone: "Asia/Shanghai", Today: seed.Base.Format(time.DateOnly), Shop: "山海工作室", User: "林夏", Login: "linxia"}
	out.Nav = []NavGroup{
		{"概览", []NavItem{{"今日看板", "gauge", 0}, {"实时数据", "activity", 0}, {"经营报告", "chart", 3}}},
		{"订单", []NavItem{{"全部订单", "receipt", 1284}, {"待发货", "package", 37}, {"售后与退款", "undo", 12}, {"发票", "file", 5}}},
		{"商品", []NavItem{{"商品列表", "box", 0}, {"库存", "warehouse", 8}, {"分类与标签", "tags", 0}, {"评价管理", "star", 21}}},
		{"顾客", []NavItem{{"顾客列表", "users", 0}, {"会员与积分", "crown", 0}, {"工单", "inbox", 9}}},
		{"营销", []NavItem{{"优惠券", "ticket", 0}, {"活动", "megaphone", 2}, {"内容与日志", "pen", 0}}},
		{"设置", []NavItem{{"店铺设置", "store", 0}, {"员工与权限", "shield", 0}, {"开放接口", "plug", 0}}},
	}
	for i := range 20 {
		out.Notices = append(out.Notices, Notice{Title: seed.Pick(r, noticeTitles), Body: seed.Pick(r, noticeBodies), At: seed.Ago(r, 2), Unread: i < 7})
	}
	for _, k := range []struct {
		label, kind string
		base        float64
	}{{"今日销售额", "money", 4823600}, {"订单数", "count", 268}, {"访客", "count", 9341}, {"转化率", "percent", 0.0287}, {"客单价", "money", 17998}, {"退款率", "percent", 0.012}, {"待发货", "count", 37}, {"好评率", "percent", 0.962}} {
		kp := KPI{Label: k.label, Kind: k.kind, Value: k.base, Delta: math.Round((r.Float64()-0.4)*200) / 1000}
		for range 30 {
			kp.Spark = append(kp.Spark, 40+r.IntN(60))
		}
		out.KPIs = append(out.KPIs, kp)
	}
	for d := 89; d >= 0; d-- {
		orders := 180 + r.IntN(140) + int(60*math.Sin(float64(d)/7*2*math.Pi))
		out.Days = append(out.Days, Day{Date: seed.Base.AddDate(0, 0, -d).Format(time.DateOnly), Orders: orders, RevenueFen: orders * (15000 + r.IntN(6000)), Refunds: r.IntN(12)})
	}
	for _, ch := range []string{"小程序", "网店", "门店", "直播"} {
		s := Series{Name: ch}
		for range 30 {
			s.Values = append(s.Values, 20+r.IntN(120))
		}
		out.Channels = append(out.Channels, s)
	}
	out.Funnel = []Share{{"访问", 9341}, {"浏览商品", 6120}, {"加入购物车", 1488}, {"提交订单", 402}, {"支付成功", 268}}
	out.Categories = []Share{{"器皿", 38}, {"织物", 24}, {"文具", 17}, {"香氛", 13}, {"户外", 8}}
	for range 7 * 26 {
		out.Heat = append(out.Heat, r.IntN(60))
	}
	for range 400 {
		items := 1 + r.IntN(6)
		out.Scatter = append(out.Scatter, math.Round(float64(items)*(60+r.Float64()*300)), float64(items))
	}
	for i := range n {
		out.Orders = append(out.Orders, order(r, i))
	}
	out.Ticket = ticket(r)
	for range 30 {
		out.Audit = append(out.Audit, Audit{Actor: seed.Name(r), Action: seed.Pick(r, auditActions), Target: fmt.Sprintf("SH-%06d", 300000+r.IntN(n+1000)), At: seed.Ago(r, 1)})
	}
	return out
}

var (
	statuses = []string{"pending", "paid", "paid", "shipped", "shipped", "delivered", "refunding", "closed"}
	payments = []string{"微信支付", "支付宝", "银行卡", "花呗分期"}
	tagPool  = []string{"加急", "礼品", "大客户", "首单", "开票", "预售", "会员"}
	risks    = []string{"low", "low", "low", "low", "medium", "high"}
)

func order(r *rand.Rand, i int) Order {
	o := Order{
		ID: fmt.Sprintf("SH-%06d", 300000+i), Customer: seed.Name(r), Status: seed.Pick(r, statuses), Payment: seed.Pick(r, payments),
		Carrier: seed.Pick(r, seed.Carriers), Tracking: fmt.Sprintf("SF%013d", r.Int64N(1e13)), City: seed.Pick(r, seed.Cities),
		CreatedAt: seed.Ago(r, 1), Risk: seed.Pick(r, risks),
	}
	o.Email = fmt.Sprintf("%s@example.com", seed.Login(r))
	for range 1 + r.IntN(3) {
		p := seed.Pick(r, seed.Products)
		q := 1 + r.IntN(3)
		o.Items = append(o.Items, Item{p.Slug, p.Zh, q})
		o.AmountFen += q * p.PriceFen
	}
	for range r.IntN(3) {
		o.Tags = append(o.Tags, seed.Pick(r, tagPool))
	}
	return o
}

var labels = []Label{{"退款争议", "#a3352b"}, {"物流", "#3e6e8e"}, {"顺丰", "#6e9c95"}, {"高优先级", "#b3843a"}, {"需要财务", "#5b6876"}}

func ticket(r *rand.Rand) Ticket {
	t := Ticket{
		Number: 4821, Title: "双十一预售订单批量退款后，部分订单重复退款且物流状态未回写", State: "open",
		Opened: seed.Base.AddDate(0, 0, -6).Format(time.RFC3339), Author: "沈清和", Labels: labels,
		Assignees: []string{"林夏", "许知秋"}, Milestone: "十一月对账",
	}
	people := make([][2]string, 24)
	for i := range people {
		people[i] = [2]string{seed.Name(r), seed.Login(r)}
		t.Participants = append(t.Participants, people[i][0])
	}
	at := seed.Base.AddDate(0, 0, -6)
	for i := 0; i < comments; {
		at = at.Add(time.Duration(5+r.IntN(40)) * time.Minute)
		who := seed.Pick(r, people)
		e := Event{ID: fmt.Sprintf("e%d", len(t.Events)), Author: who[0], Login: who[1], At: at.Format(time.RFC3339)}
		if r.IntN(4) == 0 {
			e.Kind = seed.Pick(r, []string{"labeled", "assigned", "referenced", "closed", "reopened"})
			e.Body = seed.Pick(r, eventNotes[e.Kind])
		} else {
			e.Kind = "comment"
			e.Body = comment(r, i, people)
			e.Edited = r.IntN(6) == 0
			for _, em := range []string{"👍", "🎉", "👀", "❤️", "🚀"} {
				if r.IntN(3) == 0 {
					e.Reactions = append(e.Reactions, Reaction{em, 1 + r.IntN(9)})
				}
			}
			i++
		}
		t.Events = append(t.Events, e)
	}
	return t
}

// comment writes the Markdown of comment i: a reply that may quote, list, show a query or a
// payload, or a table of the affected orders.
func comment(r *rand.Rand, i int, people [][2]string) string {
	var b strings.Builder
	if i%7 == 3 {
		fmt.Fprintf(&b, "> %s\n\n", seed.Pick(r, replyLines))
	}
	fmt.Fprintf(&b, "@%s %s\n\n", seed.Pick(r, people)[1], seed.Sentence(r, replyLines, 2))
	switch i % 6 {
	case 0:
		for range 3 {
			fmt.Fprintf(&b, "- [%s] `SH-%06d` %s\n", seed.Pick(r, []string{" ", "x"}), 300000+r.IntN(5000), seed.Pick(r, replyLines))
		}
	case 1:
		fmt.Fprintf(&b, "```sql\nSELECT id, refund_status, refunded_fen FROM orders\nWHERE batch_id = 'presale-1111' AND refunded_fen > amount_fen\nORDER BY id LIMIT %d;\n```\n", 20+r.IntN(80))
	case 2:
		b.WriteString("| 订单 | 应退 | 实退 |\n| --- | ---: | ---: |\n")
		for range 3 {
			fen := 10000 + r.IntN(90000)
			fmt.Fprintf(&b, "| SH-%06d | ¥%d.%02d | ¥%d.%02d |\n", 300000+r.IntN(5000), fen/100, fen%100, fen*2/100, fen*2%100)
		}
	case 3:
		fmt.Fprintf(&b, "```json\n{ \"order\": \"SH-%06d\", \"refund\": { \"status\": \"succeeded\", \"attempts\": %d }, \"carrier\": \"SF\" }\n```\n", 300000+r.IntN(5000), 2+r.IntN(3))
	default:
		fmt.Fprintf(&b, "**%s** %s\n", seed.Pick(r, replyLines), seed.Pick(r, replyLines))
	}
	return b.String()
}

var (
	noticeTitles = []string{"新订单", "退款申请", "库存预警", "差评提醒", "工单回复", "提现到账"}
	noticeBodies = []string{"SH-301204 已付款，等待发货。", "顾客申请退款 ¥328.00。", "晨间手冲壶库存低于 10 件。", "一条 2 星评价需要回复。", "工单 #4821 有新回复。", "¥12,480.00 已到账。"}
	auditActions = []string{"修改了收件地址", "标记为已发货", "发起退款", "添加备注", "导出订单", "修改了价格"}
	replyLines   = []string{
		"我这边查了支付回调日志，同一个退款单号回调了两次。", "建议先冻结这批订单的二次退款入口。", "财务那边今天能对完账吗？",
		"物流状态没回写是因为顺丰推送的签名校验失败。", "已经和顺丰的对接人确认，他们昨晚换了密钥。", "补偿脚本跑完了，日志在附件里。",
		"这 37 单我手动核对过，金额都对上了。", "能不能加一个幂等键，防止下次重放？", "预售尾款和定金是两笔支付，退款时要分开处理。",
		"已经给受影响的顾客发了短信说明。", "我先把回调重试关掉，观察一小时。", "看起来是队列积压导致的重复消费。",
	}
	eventNotes = map[string][]string{
		"labeled":    {"添加了标签 退款争议", "添加了标签 需要财务", "移除了标签 物流"},
		"assigned":   {"指派给了 许知秋", "指派给了 林夏"},
		"referenced": {"在提交 3f9c2e1 中引用了这个工单", "在工单 #4830 中引用了这个工单"},
		"closed":     {"关闭了这个工单"},
		"reopened":   {"重新打开了这个工单"},
	}
)
