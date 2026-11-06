// The choice of the storefront's language from Accept-Language, as the Go code makes it with
// golang.org/x/text/language (examples/shop/app/store/store.go, Preferred):
//
//	tags, _, _ := language.ParseAcceptLanguage(acceptLanguage)
//	_, i, confidence := matcher.Match(tags...)
//	if confidence == language.No { return locales[0] }
//
// ParseAcceptLanguage reads the header entry by entry. One entry it cannot read (a tag of the
// package does not know or write well, a weight that is not a number) makes it refuse the whole
// header, and the answer is the first locale. The matcher is not ported. langdata.gen.ts holds
// what it answers for a single tag, made from the package by TestLanguageData, and this module
// picks among the tags of a header the way the matcher does: the best confidence first, then a language
// written as the locale's (zh for zh-CN), then its region, then the first of the header (the entries
// are sorted by weight, the first of equal weights first). A tag that matches exactly but for its
// region stops tags of another language after it from taking the answer. TestStoreDomainMatchesGo
// runs about 2,500 headers through both to keep this equal.
//
// What is not the same as in the package: a tag with several extensions or variants is read for
// its validity but its extensions are not checked one by one, and a three letter region after a
// script is not read.
import { languages, matchClasses, matching, regions, scripts, variants } from './langdata.gen.ts'

// grandfathered are the old tags that the package reads as a tag of today: the language and the region.
const grandfathered = new Map<string, [string, string]>([
  ['art-lojban', ['jbo', '']], ['i-ami', ['ami', '']], ['i-bnn', ['bnn', '']], ['i-hak', ['hak', '']], ['i-klingon', ['tlh', '']],
  ['i-lux', ['lb', '']], ['i-navajo', ['nv', '']], ['i-pwn', ['pwn', '']], ['i-tao', ['tao', '']], ['i-tay', ['tay', '']],
  ['i-tsu', ['tsu', '']], ['no-bok', ['nb', '']], ['no-nyn', ['nn', '']], ['sgn-be-fr', ['sfb', '']], ['sgn-be-nl', ['vgt', '']],
  ['sgn-ch-de', ['sgg', '']], ['zh-guoyu', ['cmn', '']], ['zh-hakka', ['hak', '']], ['zh-min-nan', ['nan', '']], ['zh-xiang', ['hsn', '']],
  ['cel-gaulish', ['xtg', '']], ['en-gb-oed', ['en', 'GB']], ['i-default', ['en', '']], ['i-enochian', ['und', '']],
  ['i-mingo', ['see', '']], ['zh-min', ['nan', '']],
])

// The words ParseAcceptLanguage takes for a language when they are not a tag.
const acceptFallback: Record<string, string> = { english: 'en', deutsch: 'de', italian: 'it', french: 'fr', '*': 'und' }

interface Tag {
  lang: string
  script: string
  region: string
}

const alnum = /^[a-z0-9]+$/
const alpha = /^[a-z]+$/
const digits = /^[0-9]+$/

/** parseTag is language.Parse: a tag, or undefined where the package returns an error. */
function parseTag(entry: string): Tag | undefined {
  const s = entry.toLowerCase().replaceAll('_', '-')
  const old = grandfathered.get(s)
  if (old) return { lang: old[0], script: '', region: old[1] }
  const tokens = s.split('-')
  if (tokens.some((t) => t.length < 1 || t.length > 8 || !alnum.test(t))) return undefined
  let i = 0
  const tag: Tag = { lang: 'und', script: '', region: '' }
  if (tokens[0].length === 1) {
    // Only private use may start a tag: x- and at least one more subtag.
    return tokens[0] === 'x' && tokens.length > 1 ? tag : undefined
  }
  if (tokens[0].length >= 4) return undefined
  if (!languages.has(tokens[0])) return undefined
  tag.lang = tokens[0]
  i++
  // <lang>-<extlang> is the language of the extlang.
  while (i < tokens.length && tokens[i].length === 3 && alpha.test(tokens[i])) {
    if (!languages.has(tokens[i])) return undefined
    if (tokens[i] !== 'und') tag.lang = tokens[i]
    i++
  }
  if (i < tokens.length && tokens[i].length === 4 && alpha.test(tokens[i])) {
    const script = tokens[i][0].toUpperCase() + tokens[i].slice(1)
    if (!scripts.has(script)) return undefined
    tag.script = script
    i++
  }
  if (i < tokens.length && (tokens[i].length === 2 || tokens[i].length === 3)) {
    const t = tokens[i]
    const region = t.toUpperCase()
    if (!(t.length === 2 && alpha.test(t)) && !(t.length === 3 && digits.test(t))) return undefined
    if (!regions.has(region)) return undefined
    tag.region = region
    i++
  }
  while (i < tokens.length && tokens[i].length >= 4) {
    if (!variants.has(tokens[i])) return undefined
    i++
  }
  // Extensions: a singleton and subtags of two or more letters, and private use to the end.
  while (i < tokens.length) {
    if (tokens[i].length !== 1) return undefined
    const singleton = tokens[i++]
    const start = i
    while (i < tokens.length && tokens[i].length >= (singleton === 'x' ? 1 : 2)) i++
    if (i === start) return undefined
    if (singleton === 'x') break
  }
  if (i < tokens.length) return undefined
  return tag
}

/** parseFloat32 is strconv.ParseFloat(s, 32): a number, or undefined where Go returns an error. */
function parseFloat32(s: string): number | undefined {
  const m = /^([+-]?)(inf|infinity|nan)$/i.exec(s)
  if (m) return /nan/i.test(m[2]) ? NaN : m[1] === '-' ? -Infinity : Infinity
  let value: number
  if (/^[+-]?0[xX]/.test(s)) {
    const h = /^([+-]?)0[xX]([0-9a-fA-F_]*)(?:\.([0-9a-fA-F_]*))?[pP]([+-]?[0-9_]+)$/.exec(s)
    if (!h) return undefined
    const whole = h[2].replaceAll('_', '')
    const frac = (h[3] ?? '').replaceAll('_', '')
    if (whole + frac === '') return undefined
    value = (parseInt(whole + frac, 16) / 16 ** frac.length) * 2 ** Number(h[4].replaceAll('_', ''))
    if (h[1] === '-') value = -value
  } else {
    // Go also takes underscores between digits.
    if (/_/.test(s) && /(^|[^0-9])_|_($|[^0-9])/.test(s)) return undefined
    const plain = s.replaceAll('_', '')
    if (!/^[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?$/.test(plain)) return undefined
    value = Number(plain)
  }
  const f = Math.fround(value)
  if (!Number.isFinite(f) && Number.isFinite(value)) return undefined // out of the range of a float32
  return f
}

const consume = (s: string, c: string) => (s !== '' && s[0] === c ? s.slice(1).trim() : '')

/** parseAcceptLanguage is language.ParseAcceptLanguage: the tags by weight, or none where it refuses the header. */
export function parseAcceptLanguage(header: string): Tag[] {
  if (header.split('-').length - 1 > 1000) return []
  const list: Array<{ tag: Tag; q: number }> = []
  let rest = header
  while (rest !== '') {
    const comma = rest.indexOf(',')
    let entry: string
    if (comma >= 0) {
      entry = rest.slice(0, comma).trim()
      rest = rest.slice(comma + 1).trim()
    } else {
      entry = rest.trim()
      rest = ''
    }
    if (entry === '') continue
    const semi = entry.indexOf(';')
    let weight = ''
    if (semi >= 0) {
      weight = entry.slice(semi + 1).trim()
      entry = entry.slice(0, semi).trim()
    }
    let tag = parseTag(entry)
    if (!tag) {
      const lang = Object.hasOwn(acceptFallback, entry) ? acceptFallback[entry] : undefined
      if (!lang) return []
      tag = { lang, script: '', region: '' }
    }
    let q = 1
    if (weight !== '') {
      const parsed = parseFloat32(consume(consume(weight, 'q'), '='))
      if (parsed === undefined) return []
      if (parsed <= 0) continue
      q = parsed
    }
    list.push({ tag, q })
  }
  // sort.Stable on "greater weight first". An insertion sort does the same for a short list.
  for (let i = 1; i < list.length; i++) {
    for (let j = i; j > 0 && list[j].q > list[j - 1].q; j--) [list[j], list[j - 1]] = [list[j - 1], list[j]]
  }
  return list.map((e) => e.tag)
}

const rank: Record<string, number> = { E: 3, H: 2, L: 1 }

/** matchOne is what the matcher answers for one tag: the index of the locale, its confidence, and
 * whether the language is pinned: the match is exact but for the region, so a tag of another
 * language after it cannot take the answer. It is undefined for no match. */
function matchOne(t: Tag): { index: number; conf: string; pinned: boolean } | undefined {
  const id = matching.get(`${t.lang} ${t.script}`)
  if (id === undefined) return undefined
  const fields = matchClasses[id].split(' ')
  let answer = fields[0]
  if (t.region !== '') for (const f of fields.slice(1)) if (f.startsWith(t.region + '=')) answer = f.slice(t.region.length + 1)
  if (answer === '-') return undefined
  return { index: Number(answer[0]), conf: answer[1], pinned: answer[2] === 'P' || answer[1] === 'E' }
}

/** preferredIndex is the index of the locale (0 zh-CN, 1 en-US) the header asks for, 0 for none. */
export function preferredIndex(acceptLanguage: string): number {
  const tags = parseAcceptLanguage(acceptLanguage)
  let best: { index: number; conf: string; origLang: boolean; origReg: boolean } | undefined
  let pinnedLang: string | undefined
  for (const [i, tag] of tags.entries()) {
    if (pinnedLang !== undefined && tag.lang !== pinnedLang) continue
    const m = matchOne(tag)
    if (!m) continue
    // A tag pins its language when no tag after it has the same one.
    if (m.pinned && !tags.slice(i + 1).some((t) => t.lang === tag.lang)) pinnedLang = tag.lang
    // Of two answers of the same confidence the matcher keeps the one whose language was written
    // as the language of the locale (zh for zh-CN), then the one whose region is the locale's.
    const cand = { index: m.index, conf: m.conf, origLang: tag.lang === (m.index === 0 ? 'zh' : 'en'), origReg: tag.region === (m.index === 0 ? 'CN' : 'US') }
    const beaten =
      !best ||
      rank[cand.conf] > rank[best.conf] ||
      (rank[cand.conf] === rank[best.conf] && (cand.origLang !== best.origLang ? cand.origLang : cand.origReg !== best.origReg ? cand.origReg : false))
    if (beaten) best = cand
    // An exact answer ends the search.
    if (best!.conf === 'E') break
  }
  return best ? best.index : 0
}
