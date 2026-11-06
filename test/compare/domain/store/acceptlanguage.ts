// The choice of the storefront's language from Accept-Language, as the Go code makes it with
// golang.org/x/text/language (examples/shop/app/store/store.go, Preferred):
//
//	tags, _, _ := language.ParseAcceptLanguage(acceptLanguage)
//	_, i, confidence := matcher.Match(tags...)
//	if confidence == language.No { return locales[0] }
//
// ParseAcceptLanguage reads the header entry by entry. One entry it cannot read (a tag of the
// package does not know or write well, a weight that is not a number) makes it refuse the whole
// header, and the answer is the first locale. Then the matcher chooses among the tags (language/match.go:
// getBest and bestMatch.update, ported here with their tie-breakers). Its data, which the package keeps
// in tables it does not export, is in langdata.gen.ts, written from the package by TestLanguageData:
// the supported tags of each language, what the matcher makes of a wanted tag (its maximized script and
// region), the region groups and the paradigm locales. TestStoreDomainMatchesGo compares the port with
// Go on about 36,000 headers.
import { langCanonical, languages, matchHeaders, matchRegion, paradigm, profileClasses, profileEntries, regionAlpha3, regionCanonical, regionGroups, regions, scripts, variants } from './langdata.gen.ts'

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
const acceptFallback: Record<string, string> = { english: 'en', deutsch: 'de', italian: 'it', french: 'fr', '*': 'mul' }

const canon = (lang: string) => langCanonical.get(lang) ?? lang

interface Tag {
  /** lang is the language as the package keeps it: chi is zh. */
  lang: string
  /** written is the language as it was written, which the table of the matcher is made by. */
  written: string
  script: string
  region: string
  /** variant says the tag has a variant. */
  variant: boolean
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
    tag.variant = true
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
        const inner: Tag = { lang: canon(tokens[i]), written: tokens[i], script: '', region: '', variant: false }
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
  if (old) return { lang: canon(old[0]), written: old[0], script: '', region: old[1], variant: false }
  const tokens = s.split('-')
  if (tokens.some((t) => t.length < 1 || t.length > 8 || !alnum.test(t))) return undefined
  const tag: Tag = { lang: 'und', written: 'und', script: '', region: '', variant: false }
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
      tag = { lang: canon(lang), written: lang, script: '', region: '', variant: false }
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

// ---- The matcher (language/match.go): Match, getBest and bestMatch.update, with the data of the
// package in langdata.gen.ts.

// The confidences of the package.
const No = 0
const Low = 1
const High = 2
const Exact = 3

/** Have is a supported tag in the index of the matcher (haveTag). */
interface Have {
  index: number
  lang: string
  script: string
  region: string
  conf: number
  maxScript: string
  maxRegion: string
  altScript: string
  nextMax: number
}

const headers = new Map<string, Have[]>()
for (const [lang, text] of matchHeaders) {
  headers.set(
    lang,
    text.split(' ').map((entry) => {
      const f = entry.split(',')
      return { index: Number(f[0]), lang: f[1], script: f[2], region: f[3], conf: Number(f[4]), maxScript: f[5], maxRegion: f[6], altScript: f[7], nextMax: Number(f[8]) }
    }),
  )
}

const groupOf = (region: string) => regionGroups.get(region) ?? 0
const regionRows = matchRegion.map((row) => {
  const f = row.split(' ')
  return { lang: f[0], script: f[1], group: Number(f[2]), distance: Number(f[3]) }
})
const paradigmRows = paradigm.map((row) => row.split(' '))

const defaultDistance = 4

/** regionGroupDist is the distance between two regions by their CLDR groups, and whether it is the default. */
function regionGroupDist(a: string, b: string, script: string, lang: string): { dist: number; same: boolean } {
  const aGroup = groupOf(a) << 1
  const bGroup = groupOf(b) << 1
  for (const ri of regionRows) {
    if (ri.lang === lang && (ri.script === '' || ri.script === script)) {
      const group = 1 << (ri.group & ~0x80)
      if ((ri.group & 0x80) === 0) {
        if ((aGroup & bGroup & group) !== 0) return { dist: ri.distance, same: ri.distance === defaultDistance }
      } else if (((aGroup | bGroup) & group) === 0) {
        return { dist: ri.distance, same: ri.distance === defaultDistance }
      }
    }
  }
  return { dist: defaultDistance, same: true }
}

const isParadigmLocale = (lang: string, region: string) => paradigmRows.some((e) => e[0] === lang && (region === e[1] || region === e[2]))

/** Profile is what the matcher makes of a wanted tag before it compares it (see profileOf). */
interface Profile {
  lang: string
  script: string
  region: string
  maxScript: string
  maxRegion: string
  header: string
}

const languageClasses = new Map<string, { none: number; rest: number }>()
const scriptClasses = new Map<string, number>()
for (const entry of profileEntries) {
  const [key, a, b] = entry.split(':')
  if (b !== undefined) languageClasses.set(key, { none: Number(a), rest: Number(b) })
  else scriptClasses.set(key, Number(a))
}

const canonicalRegion = (r: string) => regionCanonical.get(r) ?? r

function expand(fields: string, ref: Profile | undefined, script: string, region: string): Profile | undefined {
  if (fields === '-') return undefined
  const f = fields.split('/')
  const typedRegion = canonicalRegion(region)
  return {
    lang: f[0] === '~' ? (ref as Profile).lang : f[0],
    script: f[1] === '=' ? script : f[1],
    region: f[2] === '=' ? typedRegion : f[2],
    maxScript: f[3] === '=' ? script : f[3] === '~' ? (ref as Profile).maxScript : f[3],
    maxRegion: f[4] === '=' ? typedRegion : f[4] === '^' ? (ref as Profile).maxRegion : f[4],
    header: f[5] === '~' ? (ref as Profile).header : f[5],
  }
}

/**
 * profileOf is what the matcher does with a wanted tag before comparing it: no profile where it skips
 * the tag (no supported tag is listed for its language), else the tag with the region as
 * canonicalization leaves it, its maximized script and region, and the language its supported tags are
 * listed under.
 */
function profileOf(tag: Tag): Profile | undefined {
  const language = languageClasses.get(tag.written)
  if (!language) return undefined
  const id = tag.script === '' ? language.none : (scriptClasses.get(`${tag.written} ${tag.script}`) ?? language.rest)
  const [baseFields, ...exceptions] = profileClasses[id].split(' ')
  const base = expand(baseFields, undefined, tag.script, '')
  if (tag.region === '') return base
  const exception = exceptions.find((e) => e.startsWith(tag.region + '='))
  if (exception) return expand(exception.slice(tag.region.length + 1), base, tag.script, tag.region)
  if (!base) return undefined
  // A region with nothing special about it: the profile with no region, with the region written.
  return { ...base, region: canonicalRegion(tag.region), maxRegion: canonicalRegion(tag.region) }
}

/** Want is a wanted tag as update sees it. */
interface Want {
  lang: string
  script: string
  region: string
  variant: boolean
}

/** Best is the accumulated best match (bestMatch). */
interface Best {
  have?: Have
  want?: Want
  conf: number
  pinnedRegion: string
  pinLanguage: boolean
  sameRegionGroup: boolean
  origLang: boolean
  origReg: boolean
  paradigmReg: boolean
  regGroupDist: number
  origScript: boolean
}

/** update is bestMatch.update: it makes the new pair the best match if it is a better one. */
function update(m: Best, have: Have, tag: Want, maxScript: string, maxRegion: string, pin: boolean): void {
  let c = have.conf
  if (c < m.conf) return
  if (m.pinLanguage && tag.lang !== m.want?.lang) return
  if (tag.lang === m.want?.lang && m.sameRegionGroup) {
    const { same } = regionGroupDist(m.pinnedRegion, have.maxRegion, have.maxScript, m.want.lang)
    if (!same) return
  }
  if (c === Exact && have.maxScript === maxScript) m.pinLanguage = pin
  const equalsRest = have.script === tag.script && have.region === tag.region && !tag.variant
  if (equalsRest) {
    // nothing
  } else if (have.maxScript !== maxScript) {
    if (Low < m.conf || have.altScript !== maxScript) return
    c = Low
  } else if (have.maxRegion !== maxRegion) {
    if (High < c) c = High
  }

  let beaten = false
  if (c !== m.conf) {
    if (c < m.conf) return
    beaten = true
  }
  const origLang = have.lang === tag.lang && tag.lang !== ''
  if (!beaten && m.origLang !== origLang) {
    if (m.origLang) return
    beaten = true
  }
  const origReg = have.region === tag.region && tag.region !== ''
  if (!beaten && m.origReg !== origReg) {
    if (m.origReg) return
    beaten = true
  }
  const { dist: regGroupDist, same: sameGroup } = regionGroupDist(have.maxRegion, maxRegion, maxScript, tag.lang)
  if (!beaten && m.regGroupDist !== regGroupDist) {
    if (regGroupDist > m.regGroupDist) return
    beaten = true
  }
  const paradigmReg = isParadigmLocale(tag.lang, have.maxRegion)
  if (!beaten && m.paradigmReg !== paradigmReg) {
    if (!paradigmReg) return
    beaten = true
  }
  const origScript = have.script === tag.script && tag.script !== ''
  if (!beaten && m.origScript !== origScript) {
    if (m.origScript) return
    beaten = true
  }
  if (beaten) {
    m.have = have
    m.want = tag
    m.conf = c
    m.pinnedRegion = maxRegion
    m.sameRegionGroup = sameGroup
    m.origLang = origLang
    m.origReg = origReg
    m.paradigmReg = paradigmReg
    m.origScript = origScript
    m.regGroupDist = regGroupDist
  }
}

/** langID is the language of a tag as the package keeps it, '' for none (und). */
function langID(tag: Tag): string {
  const profile = profileOf(tag)
  if (profile) return profile.lang
  return tag.written === 'und' ? '' : tag.lang
}

/** getBest is matcher.getBest: the supported tag that matches the tags best, and the confidence. */
function getBest(tags: Tag[]): { have?: Have; conf: number } {
  const best: Best = { conf: No, pinnedRegion: '', pinLanguage: false, sameRegionGroup: false, origLang: false, origReg: false, paradigmReg: false, regGroupDist: 0, origScript: false }
  const ids = tags.map(langID)
  for (const [i, tag] of tags.entries()) {
    const profile = profileOf(tag)
    if (!profile) continue
    const w: Want = { lang: profile.lang, script: profile.script, region: profile.region, variant: tag.variant }
    const list = headers.get(profile.header)
    if (!list) continue
    // A tag pins its language when no tag after it has the same one.
    const pin = !ids.slice(i + 1).some((id) => id === w.lang)
    for (const start of list) {
      let have = start
      update(best, have, w, profile.maxScript, profile.maxRegion, pin)
      if (best.conf === Exact) {
        while (have.nextMax !== 0) {
          have = list[have.nextMax]
          update(best, have, w, profile.maxScript, profile.maxRegion, pin)
        }
        return { have: best.have, conf: best.conf }
      }
    }
  }
  if (best.conf <= No) return { conf: No }
  return { have: best.have, conf: best.conf }
}

/** preferredIndex is the index of the locale (0 zh-CN, 1 en-US) the header asks for, 0 for none. */
export function preferredIndex(acceptLanguage: string): number {
  const { have, conf } = getBest(parseAcceptLanguage(acceptLanguage))
  return have && conf > No ? have.index : 0
}
