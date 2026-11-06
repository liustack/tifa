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
// compares them with Go on many headers.
//
// Not ported: the matcher itself, whose answer for a single tag is read from the table, and its
// last tie-breakers (the distance between region groups), which no header of the comparisons has
// needed. About 10,000 headers, most of them generated from the grammar of a tag and of a weight with
// parts cut and put in at random, keep the port equal to the package (TestStoreDomainMatchesGo).
import { langCanonical, languages, matchClasses, matching, regionAlpha3, regions, scripts, variants } from './langdata.gen.ts'

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

const canon = (lang: string) => langCanonical.get(lang) ?? lang

interface Tag {
  /** lang is the language as the package keeps it: chi is zh. */
  lang: string
  /** written is the language as it was written, which the table of the matcher is made by. */
  written: string
  script: string
  region: string
}

const alnum = /^[a-z0-9]+$/
const alpha = /^[a-z]+$/
const digits = /^[0-9]+$/

/** region is the region a subtag names, as the package reads it, or undefined for an unknown one. */
function readRegion(token: string): string | undefined {
  if (token.length === 3 && alpha.test(token)) return regionAlpha3.get(token.toUpperCase())
  if (!(token.length === 2 && alpha.test(token)) && !(token.length === 3 && digits.test(token))) return undefined
  return regions.has(token.toUpperCase()) ? token.toUpperCase() : undefined
}

/**
 * readTagBody reads what follows the language in a tag, from tokens[i]: the script, the region and
 * the variants. With extlang it reads the extlangs before them too, as Parse does for a tag of its own
 * (and not for the tag in a transform extension). It returns false where the package returns an error.
 */
function readTagBody(tokens: string[], start: number, tag: Tag, extlang: boolean): { ok: boolean; next: number } {
  let i = start
  while (extlang && i < tokens.length && tokens[i].length === 3 && alpha.test(tokens[i])) {
    if (!languages.has(tokens[i])) return { ok: false, next: i }
    if (tokens[i] !== 'und') {
      tag.lang = canon(tokens[i])
      tag.written = tokens[i]
    }
    i++
  }
  if (i < tokens.length && tokens[i].length === 4 && alpha.test(tokens[i])) {
    const script = tokens[i][0].toUpperCase() + tokens[i].slice(1)
    if (!scripts.has(script)) return { ok: false, next: i }
    tag.script = script
    i++
  }
  if (i < tokens.length && (tokens[i].length === 2 || tokens[i].length === 3)) {
    const region = readRegion(tokens[i])
    if (!region) return { ok: false, next: i }
    tag.region = region
    i++
  }
  while (i < tokens.length && tokens[i].length >= 4) {
    if (!variants.has(tokens[i])) return { ok: false, next: i }
    i++
  }
  return { ok: true, next: i }
}

/**
 * readExtensions is parseExtensions of the package: the extensions and private use from tokens[i],
 * which are checked and, apart from the language of a transform extension, not used. It returns false
 * where the package returns an error.
 */
function readExtensions(tokens: string[], start: number): boolean {
  let i = start
  while (i < tokens.length && tokens[i].length === 1) {
    const singleton = tokens[i]
    const first = i
    i++
    const minLength = (from: number, min: number) => {
      while (i < tokens.length && tokens[i].length >= min) i++
      return i > from
    }
    if (singleton === 'u') {
      // Attributes, then keys with their types. A key given twice with other types is refused.
      while (i < tokens.length && tokens[i].length > 2) i++
      const keys = new Map<string, string>()
      while (i < tokens.length && tokens[i].length === 2) {
        const key = tokens[i++]
        const types: string[] = []
        while (i < tokens.length && tokens[i].length > 2) types.push(tokens[i++])
        const value = types.join('-')
        if (keys.has(key) && keys.get(key) !== value) return false
        keys.set(key, value)
      }
    } else if (singleton === 't') {
      if (i < tokens.length && (tokens[i].length === 2 || tokens[i].length === 3) && alpha.test(tokens[i][1])) {
        if (!languages.has(tokens[i])) return false
        const inner: Tag = { lang: canon(tokens[i]), written: tokens[i], script: '', region: '' }
        const body = readTagBody(tokens, i + 1, inner, false)
        if (!body.ok) return false
        i = body.next
      }
      while (i < tokens.length && tokens[i].length === 2 && !alpha.test(tokens[i][1])) {
        i++
        minLength(i, 3)
      }
    } else if (singleton === 'x') {
      minLength(i, 1)
    } else {
      minLength(i, 2)
    }
    // The extension is the singleton and its subtags with their hyphens: at least 4 characters, and 3 for private use.
    let length = 0
    for (let k = first; k < i; k++) length += tokens[k].length
    length += i - first - 1
    if (length < 3 || (singleton !== 'x' && length < 4)) return false
    if (singleton === 'x') break
  }
  return true
}

/** parseTag is language.Parse: a tag, or undefined where the package returns an error. */
function parseTag(entry: string): Tag | undefined {
  const s = entry.toLowerCase().replaceAll('_', '-')
  const old = grandfathered.get(s)
  if (old) return { lang: canon(old[0]), written: old[0], script: '', region: old[1] }
  const tokens = s.split('-')
  if (tokens.some((t) => t.length < 1 || t.length > 8 || !alnum.test(t))) return undefined
  const tag: Tag = { lang: 'und', written: 'und', script: '', region: '' }
  if (tokens[0].length === 1) {
    // Only private use may start a tag: x- and at least one more subtag.
    return tokens[0] === 'x' && readExtensions(tokens, 0) ? tag : undefined
  }
  if (tokens[0].length >= 4) return undefined
  if (!languages.has(tokens[0])) return undefined
  tag.lang = canon(tokens[0])
  tag.written = tokens[0]
  const body = readTagBody(tokens, 1, tag, true)
  if (!body.ok) return undefined
  if (body.next < tokens.length) {
    // What follows must be extensions: another kind of subtag is a syntax error.
    if (tokens[body.next].length !== 1) return undefined
    if (!readExtensions(tokens, body.next)) return undefined
  }
  return tag
}

const hexDigit = /[0-9a-fA-F]/

/**
 * parseFloat32 is strconv.ParseFloat(s, 32): a number, or undefined where Go returns an error. That
 * is a syntax error, and a finite number that does not fit a float32 (ErrRange), but not an explicit
 * inf, which is a number, nor a number so small that it is zero.
 */
function parseFloat32(s: string): number | undefined {
  // inf, infinity and nan, in any case. Only inf takes a sign.
  const special = /^([+-]?)(inf|infinity)$/i.exec(s)
  if (special) return special[1] === '-' ? -Infinity : Infinity
  if (/^nan$/i.test(s)) return NaN
  let i = 0
  let negative = false
  if (s[i] === '+' || s[i] === '-') negative = s[i++] === '-'
  const hex = s.length > i + 2 && s[i] === '0' && (s[i + 1] === 'x' || s[i + 1] === 'X')
  if (hex) i += 2
  const digitOK = (c: string) => (hex ? hexDigit.test(c) : c >= '0' && c <= '9')
  let mantissa = ''
  let fraction = ''
  let sawDot = false
  let sawDigits = false
  let underscores = false
  for (; i < s.length; i++) {
    const c = s[i]
    if (c === '_') {
      underscores = true
    } else if (c === '.') {
      if (sawDot) break
      sawDot = true
    } else if (digitOK(c)) {
      sawDigits = true
      if (sawDot) fraction += c
      else mantissa += c
    } else break
  }
  if (!sawDigits) return undefined
  let exponent = 0
  let sawExponent = false
  if (i < s.length && s[i].toLowerCase() === (hex ? 'p' : 'e')) {
    i++
    sawExponent = true
    let expNegative = false
    if (s[i] === '+' || s[i] === '-') expNegative = s[i++] === '-'
    if (i >= s.length || !((s[i] >= '0' && s[i] <= '9') || s[i] === '_')) return undefined
    let sawExponentDigits = false
    let e = 0
    for (; i < s.length && ((s[i] >= '0' && s[i] <= '9') || s[i] === '_'); i++) {
      if (s[i] === '_') {
        underscores = true
        continue
      }
      sawExponentDigits = true
      if (e < 10000) e = e * 10 + Number(s[i])
    }
    if (!sawExponentDigits) return undefined
    exponent = expNegative ? -e : e
  }
  if (i !== s.length) return undefined
  if (hex && !sawExponent) return undefined
  if (underscores && !underscoreOK(s)) return undefined
  let value: number | undefined
  if (hex) value = hexToFloat32(mantissa + fraction, exponent - 4 * fraction.length)
  else value = decimalToFloat32(mantissa + fraction, exponent - fraction.length)
  // A number that is not zero or infinite in the text and is infinite as a float32 is out of range.
  if (value === undefined || !Number.isFinite(value)) return undefined
  return negative ? -value : value
}

/**
 * hexToFloat32 is the float32 nearest to digits (hexadecimal, a mantissa) times 2 to the power
 * exponent, ties to even, or undefined where it is too large for a float32. The mantissa and the
 * exponent are joined in a BigInt and rounded once, so that neither one alone being out of the range
 * of a double matters: 0x0p1024 is 0, and 0x1 followed by 256 zeros and p-1024 is 1.
 */
function hexToFloat32(digits: string, exponent: number): number | undefined {
  return roundFloat32(digits === '' ? 0n : BigInt('0x' + digits), exponent)
}

/**
 * decimalToFloat32 is the float32 nearest to digits (decimal) times 10 to the power exponent, or
 * undefined where it is too large. It is not Math.fround(Number(text)), which rounds twice and
 * turns a number just below the middle of two float32 into the middle of them, and then up.
 */
function decimalToFloat32(digits: string, exponent: number): number | undefined {
  const trimmed = digits.replace(/^0+/, '')
  if (trimmed === '') return 0
  // Far above the largest float32 (3.4e38) or far below half the smallest (7e-46).
  const magnitude = trimmed.length + exponent
  if (magnitude > 40) return undefined
  if (magnitude < -50) return 0
  const n = BigInt(trimmed)
  if (exponent >= 0) return roundFloat32(n * 10n ** BigInt(exponent), 0)
  // n over 10^k, as a binary number with 30 bits at least and one more bit that says the rest is not zero.
  const divisor = 10n ** BigInt(-exponent)
  const shift = Math.max(0, divisor.toString(2).length + 30 - n.toString(2).length)
  const scaled = n << BigInt(shift)
  const quotient = scaled / divisor
  const sticky = scaled % divisor === 0n ? 0n : 1n
  return roundFloat32((quotient << 1n) | sticky, -shift - 1)
}

/** roundFloat32 is the float32 nearest to mantissa times 2 to the power exponent, ties to even, or undefined where it is too large. */
function roundFloat32(mantissa: bigint, exponent: number): number | undefined {
  if (mantissa === 0n) return 0
  const length = mantissa.toString(2).length
  // The value is below 2 to the power top. A float32 keeps 24 bits from 2^-126 up, fewer below it
  // (the subnormals, down to 2^-149).
  const top = length + exponent
  if (top > 128) return undefined
  const bits = top - 1 >= -126 ? 24 : top + 149
  if (bits < 0) return 0
  const shift = length - bits
  let kept = mantissa
  let scale = exponent
  if (shift > 0) {
    const s = BigInt(shift)
    kept = mantissa >> s
    const rest = mantissa & ((1n << s) - 1n)
    const half = 1n << (s - 1n)
    if (rest > half || (rest === half && (kept & 1n) === 1n)) kept += 1n
    scale += shift
  }
  const result = Number(kept) * 2 ** scale
  return result >= 2 ** 128 ? undefined : result
}

/** underscoreOK is the check of strconv: an underscore sits between digits, or after a base prefix. */
function underscoreOK(s: string): boolean {
  let i = 0
  if (s[0] === '+' || s[0] === '-') i = 1
  let saw = '^'
  let hex = false
  if (s.length - i >= 2 && s[i] === '0' && 'box'.includes(s[i + 1].toLowerCase())) {
    i += 2
    saw = '0'
    hex = s[i - 1].toLowerCase() === 'x'
  }
  for (; i < s.length; i++) {
    const c = s[i]
    if ((c >= '0' && c <= '9') || (hex && hexDigit.test(c))) {
      saw = '0'
      continue
    }
    if (c === '_') {
      if (saw !== '0') return false
      saw = '_'
      continue
    }
    if (saw === '_') return false
    saw = '!'
  }
  return saw !== '_'
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
      tag = { lang: canon(lang), written: lang, script: '', region: '' }
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
  const id = matching.get(`${t.written} ${t.script}`)
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
