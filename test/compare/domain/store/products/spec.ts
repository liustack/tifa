// Ported from products/spec.go: the specification tab of a product.
import { inLocale } from '../store.ts'
import type { Locale } from '../store.ts'
import { colorName } from './catalogue.ts'
import type { ProductRecord } from './catalogue.ts'
import { packing, warranty } from './specData.ts'

export interface SpecRow {
  label: string
  value: string
}

// SpecGroup is a titled list of facts about a product.
export interface SpecGroup {
  title: string
  rows: SpecRow[]
}

// specGroups arranges what is known of a product into the three groups of the specification tab.
// number is the place of the product in the catalogue, from 1.
export function specGroups(p: ProductRecord, l: Locale, number: number): SpecGroup[] {
  const sep = l.code === 'en-US' ? ', ' : '、'
  const t = (zh: string, en: string) => inLocale([zh, en], l)
  const row = (zh: string, en: string, value: string): SpecRow => ({ label: t(zh, en), value })
  const basic: SpecGroup = { title: t('基本信息', 'The basics'), rows: p.specs.map((s) => ({ label: inLocale(s[0], l), value: inLocale(s[1], l) })) }
  basic.rows.push(
    row('材质', 'Materials', p.materials.map((m) => inLocale(m, l)).join(sep)),
    row('颜色', 'Colours', p.colors.map((id) => colorName(id, l)).join(sep)),
  )
  if (p.sizes.length > 0) basic.rows.push(row('尺码', 'Sizes', p.sizes.join(sep)))
  basic.rows.push(row('目录编号', 'Catalogue number', 'No. ' + String(number).padStart(3, '0')))
  const ship: SpecGroup = {
    title: t('包装与发货', 'Packing and shipping'),
    rows: [
      row('包装', 'Packed in', inLocale(packing[p.category], l)),
      row('发货地', 'Ships from', t('杭州市余杭区，山海仓', 'Shanhai warehouse, Yuhang, Hangzhou')),
      row('发货时效', 'Dispatch', t('现货 48 小时内发出', 'In stock items leave within 48 hours')),
      row('快递', 'Carrier', t('顺丰、中通，按地区选择', 'SF Express or ZTO, by region')),
      row('礼盒', 'Gift wrapping', t('可选，附手写卡片', 'Optional, with a handwritten card')),
    ],
  }
  const care: SpecGroup = {
    title: t('保修与售后', 'Warranty and returns'),
    rows: [
      row('保修', 'Warranty', inLocale(warranty[p.category], l)),
      row('退换', 'Returns', t('收到后七天内可无理由退换，定制商品除外', 'Seven days from delivery, except custom orders')),
      row('发票', 'Invoice', t('电子发票，下单时在备注里写抬头', 'Electronic invoice, write the title in the order notes')),
      row('客服', 'Support', t('9:00 到 21:00，在线回复', '9:00 to 21:00 China time, online')),
    ],
  }
  return [basic, ship, care]
}
