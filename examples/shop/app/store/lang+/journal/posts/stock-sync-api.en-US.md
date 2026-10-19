Our shop floor is in Hangzhou, but the web shop kept its stock in a table of its own. When the shop sold the last kettle on a Saturday, the web shop still said "in stock", and on Monday we called customers to apologise. This year we put both on one stock count, through the open API of the shop admin. Here is how, for other owners with a shop floor.

> The API is still in public beta and fields may change. The examples below use version `2026-10`.

## Before you start: get a token

Under "Settings → Open API" in the admin, create an app with the `products:read` and `inventory:write` scopes. Saving it shows a token that starts with `shk_`. **It is shown once**, so put it in your password manager right away.

Send it in a header with every request:

```bash
curl https://api.shanhai.example/v1/products?limit=2 \
  -H "Authorization: Bearer shk_live_8f2c1e" \
  -H "Accept: application/json"
```

The answer is a page of products:

```json
{
  "data": [
    {
      "id": "pour-over-kettle",
      "title": "Morning Pour-Over Kettle",
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

When `next` is not empty, pass it as the `cursor` parameter to get the next page. Do not build offsets yourself: a cursor holds steady while products come and go.

## Step one: pull everything once

The till on the shop floor is a small program we wrote in Go. When it starts, it pulls every product of the web shop and maps each SKU to a local barcode:

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

A full pull once a night is enough. The webhook below covers the rest of the day.

## Step two: sell one, take one off

For each piece sold on the shop floor, the till calls the adjust endpoint. It sends the **change**, not the count left, so two sales at the same time do not overwrite each other:

```bash
curl -X POST https://api.shanhai.example/v1/inventory/adjust \
  -H "Authorization: Bearer shk_live_8f2c1e" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: pos-2026-10-08-0042" \
  -d '{"sku": "PK-MIST", "delta": -1, "reason": "store_sale"}'
```

`Idempotency-Key` matters. The shop's network is flaky and the till retries after a timeout, and one key takes effect only once in 24 hours.

| Field | Type | Meaning |
| --- | --- | --- |
| `sku` | string | The SKU of the variant |
| `delta` | integer | Positive to add stock, negative to take it off |
| `reason` | string | `store_sale`, `return`, `restock` or `correction` |
| `note` | string | Optional, up to 200 characters |

## Step three: catch the web shop's changes with a webhook

The other way round, the shop floor must know when the web shop sells. Enter an address in the app settings and subscribe to `inventory.updated`. For every change the admin sends a POST:

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

Always check the signature, or anyone can write stock into your till. It is an HMAC-SHA256 of the body, in the `X-Shanhai-Signature` header:

```ts
import { createHmac, timingSafeEqual } from 'node:crypto'

export function verify(body: string, header: string, secret: string): boolean {
  const expected = createHmac('sha256', secret).update(body).digest('hex')
  const given = header.replace(/^sha256=/, '')
  if (given.length !== expected.length) return false
  return timingSafeEqual(Buffer.from(given), Buffer.from(expected))
}
```

On an event we do one thing: set the local stock to the event's `stock`. Events can arrive out of order, so we compare `occurredAt` first and drop older ones.

## What tripped us up

1. **Time zones.** Every time in the API is UTC. The shop's daily report cuts days at Beijing time, and its first version counted sales after 8 pm on the next day.
2. **Rate limits.** Each token gets at most 10 requests a second. With `limit` at 100, a few hundred products come down in seconds: do not fetch them one at a time.
3. **Sets.** A set of coasters is one SKU on the web and two single pieces on the shop floor. In the end we split it on the web too and sell the set as a bundle.

```yaml
# Configuration of the till
sync:
  base: https://api.shanhai.example
  token_env: SHANHAI_TOKEN
  full_pull: "0 3 * * *"   # every night at 3 am
  webhook:
    listen: ":8443"
    secret_env: SHANHAI_WEBHOOK_SECRET
```

The gap between the two counts went from a dozen pieces a week to none. If you are doing the same, leave a comment or write to us.
