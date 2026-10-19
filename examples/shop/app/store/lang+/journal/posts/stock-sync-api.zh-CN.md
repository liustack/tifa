山海的线下门店在杭州，网店的库存却一直放在另一张表里。周末门店卖掉最后一只手冲壶，网店还挂着“有货”，周一就要给客人打电话道歉。今年我们把两边接到了同一份库存上，用的就是店铺后台的开放接口。这篇写下整个过程，给同样有门店的店主参考。

> 接口目前还在公开测试，字段可能还会调整。下面的例子都基于 `2026-10` 版本。

## 准备：拿到令牌

在后台的“设置 → 开放接口”里新建一个应用，勾选 `products:read`、`inventory:write` 两个权限。保存后会得到一个以 `shk_` 开头的令牌，**只显示一次**，请马上存到密码管理器里。

每个请求都在请求头里带上它：

```bash
curl https://api.shanhai.example/v1/products?limit=2 \
  -H "Authorization: Bearer shk_live_8f2c1e" \
  -H "Accept: application/json"
```

返回的是分页后的商品列表：

```json
{
  "data": [
    {
      "id": "pour-over-kettle",
      "title": "晨间手冲壶",
      "variants": [
        { "sku": "PK-MIST", "color": "mist", "stock": 23 },
        { "sku": "PK-CHAR", "color": "charcoal", "stock": 0 }
      ],
      "updatedAt": "2026-10-08T09:12:44Z"
    }
  ],
  "next": "eyJvZmZzZXQiOjJ9"
}
```

`next` 不为空时，把它原样放进 `cursor` 参数就能取下一页。不要自己拼 offset，商品增删时游标比偏移量稳定。

## 第一步：全量拉一次

门店的收银系统是我们自己写的一个小程序，用 Go。启动时先把网店的全部商品拉一遍，建立 SKU 和本地条码的对应关系：

```go
type Variant struct {
	SKU   string `json:"sku"`
	Color string `json:"color"`
	Stock int    `json:"stock"`
}

type page struct {
	Data []struct {
		ID       string    `json:"id"`
		Variants []Variant `json:"variants"`
	} `json:"data"`
	Next string `json:"next"`
}

func pullAll(ctx context.Context, token string) (map[string]Variant, error) {
	out := map[string]Variant{}
	cursor := ""
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", base+"/v1/products?limit=100&cursor="+cursor, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		var p page
		err = json.NewDecoder(res.Body).Decode(&p)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, prod := range p.Data {
			for _, v := range prod.Variants {
				out[v.SKU] = v
			}
		}
		if p.Next == "" {
			return out, nil
		}
		cursor = p.Next
	}
}
```

全量拉取每天凌晨跑一次就够了，平时靠下面的 webhook。

## 第二步：卖出一件，就扣一件

门店每卖出一件，收银程序调用一次扣减接口。注意这里传的是**变化量**而不是剩余数量，这样两边同时卖出时不会互相覆盖：

```bash
curl -X POST https://api.shanhai.example/v1/inventory/adjust \
  -H "Authorization: Bearer shk_live_8f2c1e" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: pos-2026-10-08-0042" \
  -d '{"sku": "PK-MIST", "delta": -1, "reason": "store_sale"}'
```

`Idempotency-Key` 很重要。门店的网络不稳定，收银程序超时后会重试，同一个键在 24 小时内只会生效一次。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `sku` | string | 变体的 SKU |
| `delta` | integer | 正数入库，负数出库 |
| `reason` | string | `store_sale`、`return`、`restock` 或 `correction` |
| `note` | string | 可选，最多 200 字 |

## 第三步：用 webhook 接住网店的变化

反过来，网店卖出时门店也要知道。在应用设置里填一个地址，订阅 `inventory.updated` 事件。每次库存变动，后台会发一个 POST：

```json
{
  "event": "inventory.updated",
  "sku": "PK-MIST",
  "stock": 21,
  "delta": -2,
  "source": "web_order",
  "occurredAt": "2026-10-08T10:03:12Z"
}
```

一定要校验签名，否则谁都能往你的门店系统里写库存。签名是请求体的 HMAC-SHA256，放在 `X-Shanhai-Signature` 头里：

```ts
import { createHmac, timingSafeEqual } from 'node:crypto'

export function verify(body: string, header: string, secret: string): boolean {
  const expected = createHmac('sha256', secret).update(body).digest('hex')
  const given = header.replace(/^sha256=/, '')
  if (given.length !== expected.length) return false
  return timingSafeEqual(Buffer.from(given), Buffer.from(expected))
}
```

收到事件后，我们只做一件事：把本地库存设成事件里的 `stock`。事件可能乱序到达，所以先比较 `occurredAt`，旧的事件直接丢掉。

## 踩过的坑

1. **时区。** 接口里的时间都是 UTC。门店的日报按北京时间切日，第一版报表把晚上八点以后的销售算到了第二天。
2. **限流。** 每个令牌每秒最多 10 个请求。全量拉取时 `limit` 开到 100，几百个商品几秒钟就拉完了，不要一个一个查。
3. **套装商品。** 一套杯垫在网店是一个 SKU，门店却是两个单品。我们最后在网店也拆成了单品，再用“组合”功能卖套装。

```yaml
# 收银程序的配置
sync:
  base: https://api.shanhai.example
  token_env: SHANHAI_TOKEN
  full_pull: "0 3 * * *"   # 每天凌晨三点
  webhook:
    listen: ":8443"
    secret_env: SHANHAI_WEBHOOK_SECRET
```

现在两边的库存差异从每周十几件降到了零。如果你也在做类似的事情，欢迎在评论区留言，或者直接给我们写信。
