// The carriers of examples/shop/app/projects/project+/orders/shipping.go. The rules for shipping an
// order and changing its recipient belong to the handlers, which the comparison does not run.

export interface Carrier {
  code: string
  name: string
  days: string
  feeCents: number
}

export const carriers: Carrier[] = [
  { code: 'SF', name: 'SF Express', days: '1 to 2 days', feeCents: 1200 },
  { code: 'JD', name: 'JD Logistics', days: '1 to 2 days', feeCents: 1000 },
  { code: 'ZTO', name: 'ZTO Express', days: '2 to 3 days', feeCents: 600 },
  { code: 'YTO', name: 'YTO Express', days: '2 to 3 days', feeCents: 550 },
  { code: 'EMS', name: 'China Post EMS', days: '3 to 5 days', feeCents: 1000 },
]

export function carrierName(code: string): string {
  return carriers.find((c) => c.code === code)?.name ?? ''
}
