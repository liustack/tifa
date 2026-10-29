// The carriers of examples/shop/app/projects/project+/orders/shipping.go. The rules for shipping an
// order and changing its recipient belong to the handlers, which the comparison does not run.

export interface Carrier {
  code: string
  name: string
  days: string
  feeCents: number
}

export const carriers: Carrier[] = [
  { code: 'SF', name: '顺丰速运', days: '1 到 2 天', feeCents: 1200 },
  { code: 'JD', name: '京东物流', days: '1 到 2 天', feeCents: 1000 },
  { code: 'ZTO', name: '中通快递', days: '2 到 3 天', feeCents: 600 },
  { code: 'YTO', name: '圆通速递', days: '2 到 3 天', feeCents: 550 },
  { code: 'EMS', name: '中国邮政 EMS', days: '3 到 5 天', feeCents: 1000 },
]

export function carrierName(code: string): string {
  return carriers.find((c) => c.code === code)?.name ?? ''
}
