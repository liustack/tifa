// Package kb is a long engineering handbook in Markdown, about kb kilobytes, with one highlighted
// code block for every 4 KB: a migration guide or a changelog of the kind docs sites render whole.
package kb

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Doc struct {
	Kilobytes int    `json:"kilobytes"`
	Blocks    int    `json:"blocks"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

func Load(c *tifa.Ctx) (Doc, error) {
	kb, err := seed.Size(c.Param("kb"), 4, 50_000)
	if err != nil {
		return Doc{}, err
	}
	return Build(kb), nil
}

// Build writes a handbook of about kb kilobytes with kb/4 code blocks.
func Build(kb int) Doc {
	r := seed.New("markdown", kb)
	d := Doc{Kilobytes: kb, Blocks: kb / 4, Title: "订单服务迁移手册"}
	var b strings.Builder
	b.WriteString("订单服务从单体拆到独立部署之后，接口、数据和告警都要跟着搬。本手册按模块列出每一步，配上能直接复制的代码。\n\n")
	section := kb << 10 / d.Blocks
	for i := range d.Blocks {
		start := b.Len()
		lang := langs[i%len(langs)]
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, seed.Pick(r, headings))
		paragraph(&b, r)
		if i%3 == 0 {
			table(&b, r)
		}
		fmt.Fprintf(&b, "```%s\n%s```\n\n", lang, snippets[lang])
		if i%5 == 0 {
			fmt.Fprintf(&b, "> %s\n\n", seed.Sentence(r, zh, 3))
		}
		for b.Len()-start < section {
			fmt.Fprintf(&b, "### %s\n\n", seed.Pick(r, subheadings))
			paragraph(&b, r)
			list(&b, r)
		}
	}
	d.Body = b.String()
	return d
}

func paragraph(b *strings.Builder, r *rand.Rand) {
	fmt.Fprintf(b, "%s 调用 `%s` 之前先确认 **%s**，详见 [接口约定](https://docs.example.com/orders#%d)。%s\n\n",
		seed.Sentence(r, zh, 3), seed.Pick(r, idents), seed.Pick(r, idents), r.IntN(100), seed.Sentence(r, en, 2))
}

func list(b *strings.Builder, r *rand.Rand) {
	for range 4 {
		fmt.Fprintf(b, "- %s `%s`\n", seed.Sentence(r, zh, 1), seed.Pick(r, idents))
	}
	b.WriteString("\n")
}

func table(b *strings.Builder, r *rand.Rand) {
	b.WriteString("| 字段 | 类型 | 说明 |\n| --- | --- | --- |\n")
	for range 5 {
		fmt.Fprintf(b, "| `%s` | %s | %s |\n", seed.Pick(r, idents), seed.Pick(r, types), seed.Sentence(r, zh, 1))
	}
	b.WriteString("\n")
}

var (
	langs       = []string{"go", "typescript", "sql", "bash", "json", "yaml", "css", "python"}
	headings    = []string{"拆分订单表", "迁移支付回调", "重放历史事件", "切换库存扣减", "灰度发布", "回滚预案", "告警与看板", "压测与容量", "对账脚本", "清理旧接口"}
	subheadings = []string{"注意事项", "检查清单", "常见问题", "背景", "做法", "验证", "回滚"}
	idents      = []string{"order_id", "placedAt", "refund.status", "inventory.reserve", "X-Request-Id", "retry_after", "carrier_code", "amount_fen", "idempotency_key", "shipments[]"}
	types       = []string{"string", "int64", "bool", "timestamp", "decimal(12,2)", "jsonb"}
	zh          = []string{
		"新服务上线前，旧接口保留两周。", "每次迁移都先在影子库跑一遍。", "金额一律以分为单位存整数，", "时间统一存 UTC，展示时再换时区，",
		"重试要带幂等键，", "消息可能乱序到达，", "回调失败时按指数退避重试，", "库存扣减必须在同一事务里完成。",
		"对账在每天凌晨三点跑，", "告警阈值按过去七天的 P99 定，", "灰度按店铺 ID 取模放量，", "回滚只需要切回旧的路由表。",
	}
	en = []string{
		" Keep the old endpoint until every client has moved.", " Retries must carry the same idempotency key.", " Never compare money as floats.",
		" Log the request ID on both sides.", " A missing carrier code is a bug, not a default.", " Replays must be safe to run twice.",
	}
	snippets = map[string]string{
		"go": `// ReserveStock holds the items of an order until it is paid or expires.
func ReserveStock(ctx context.Context, tx *sql.Tx, order Order) error {
	for _, item := range order.Items {
		res, err := tx.ExecContext(ctx,
			"UPDATE stock SET reserved = reserved + $1 WHERE sku = $2 AND on_hand - reserved >= $1",
			item.Quantity, item.SKU)
		if err != nil {
			return fmt.Errorf("reserve %s: %w", item.SKU, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrOutOfStock{SKU: item.SKU}
		}
	}
	return nil
}
`,
		"typescript": `export async function refund(orderId: string, amountFen: number, key: string): Promise<Refund> {
  const res = await fetch(` + "`/api/orders/${orderId}/refunds`" + `, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    body: JSON.stringify({ amountFen }),
  })
  if (res.status === 409) return (await res.json()) as Refund // already refunded
  if (!res.ok) throw new RefundError(res.status, await res.text())
  return (await res.json()) as Refund
}
`,
		"sql": `-- Orders paid but not shipped for more than two days, by carrier.
SELECT o.carrier_code, count(*) AS late, max(now() - o.paid_at) AS oldest
FROM orders o
LEFT JOIN shipments s ON s.order_id = o.id
WHERE o.status = 'paid'
  AND s.id IS NULL
  AND o.paid_at < now() - interval '2 days'
GROUP BY o.carrier_code
ORDER BY late DESC;
`,
		"bash": `#!/usr/bin/env bash
set -euo pipefail
# Replay the events of one day into the new service, 500 at a time.
day=${1:?usage: replay.sh 2026-10-27}
for page in $(seq 0 200); do
  curl -fsS "https://orders.internal/events?day=$day&page=$page&size=500" \
    | jq -c '.events[]' \
    | while read -r ev; do echo "$ev" | ./bin/replay --target new; done
done
`,
		"json": `{
  "id": "SH-1042",
  "status": "paid",
  "amountFen": 65600,
  "items": [
    { "sku": "PK-MIST", "quantity": 2, "priceFen": 32800 }
  ],
  "shipping": { "carrier": "SF", "city": "杭州", "eta": "2026-10-30" },
  "paidAt": "2026-10-27T08:12:44Z"
}
`,
		"yaml": `service: orders
replicas: 3
resources:
  requests: { cpu: 250m, memory: 256Mi }
  limits: { cpu: "1", memory: 512Mi }
rollout:
  strategy: canary
  steps: [5, 25, 50, 100]
  pauseBetween: 15m
alerts:
  - name: refund-latency
    expr: histogram_quantile(0.99, rate(refund_seconds_bucket[5m])) > 2
`,
		"css": `.order-row[data-status="refunding"] {
  border-left: 2px solid var(--destructive);
  background: color-mix(in oklab, var(--destructive) 6%, transparent);
}
@media (max-width: 640px) {
  .order-row { grid-template-columns: 1fr auto; }
  .order-row .carrier { display: none; }
}
`,
		"python": `def reconcile(day: date, ledger: Ledger, gateway: Gateway) -> list[Mismatch]:
    """Match the payments of one day against the gateway's settlement file."""
    ours = {p.id: p.amount_fen for p in ledger.payments(day)}
    theirs = {s.payment_id: s.amount_fen for s in gateway.settlements(day)}
    return [
        Mismatch(pid, ours.get(pid), theirs.get(pid))
        for pid in ours.keys() | theirs.keys()
        if ours.get(pid) != theirs.get(pid)
    ]
`,
	}
)
