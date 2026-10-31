// The Vite plugin of tifa. tifa writes it into .tifa/vite/plugin.js of an application, and the
// application's vite.config.ts uses it through .tifa/vite.js. It tells Vite where tifa's
// generated entries are, resolves the modules tifa generates, and builds the SSR bundle that
// QuickJS runs: one ES module file with every dependency inside, no imports and no Node API.
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { builtinModules, createRequire } from 'node:module'

// devBase is where the Vite dev server serves modules under tifa dev. tifa dev sends every
// request under it to Vite and every other one to the application.
export const devBase = '/__tifa/vite/'

const clientOnlyStub = '\0tifa-client-only'
const propertyState = '\0tifa-core-js-property-state'
const routeModule = /^\.\.?\/(?:.*\/)?\+(?:route|layout)(?:\.js|\.ts)?$/
const clientSource = /\.client(?:\.[jt]sx?)?$/
const reactServerEntry = /[/\\]react-dom[/\\]server\.edge\.js$/
const reactStringRenderer = /[/\\]react-dom[/\\]cjs[/\\]react-dom-server-legacy\.browser\.production\.js$/
// React's own production file, which has createElement and cloneElement, see nativeCopyProps.
const reactModule = /[/\\]react[/\\]cjs[/\\]react\.production\.js$/
// React's production JSX runtime, which has jsx and jsxs, see inlineElements.
const reactJsxRuntime = /[/\\]react[/\\]cjs[/\\]react-jsx-runtime\.production\.js$/
// The hash of @emotion/hash 0.9, which emotion takes of every style it serializes, and the
// dist files that have it. internal/ssr gives the VMs the same hash in Go as __tifa_murmur2__.
const emotionHash = /[/\\]@emotion[/\\]hash[/\\]dist[/\\]emotion-hash\.(?:esm|cjs\.prod|cjs\.dev)\.js$/
const murmur2Source = `function murmur2(str) {
  // 'm' and 'r' are mixing constants generated offline.
  // They're not really 'magic', they just happen to work well.
  // const m = 0x5bd1e995;
  // const r = 24;
  // Initialize the hash
  var h = 0; // Mix 4 bytes at a time into the hash

  var k,
      i = 0,
      len = str.length;

  for (; len >= 4; ++i, len -= 4) {
    k = str.charCodeAt(i) & 0xff | (str.charCodeAt(++i) & 0xff) << 8 | (str.charCodeAt(++i) & 0xff) << 16 | (str.charCodeAt(++i) & 0xff) << 24;
    k =
    /* Math.imul(k, m): */
    (k & 0xffff) * 0x5bd1e995 + ((k >>> 16) * 0xe995 << 16);
    k ^=
    /* k >>> r: */
    k >>> 24;
    h =
    /* Math.imul(k, m): */
    (k & 0xffff) * 0x5bd1e995 + ((k >>> 16) * 0xe995 << 16) ^
    /* Math.imul(h, m): */
    (h & 0xffff) * 0x5bd1e995 + ((h >>> 16) * 0xe995 << 16);
  } // Handle the last few bytes of the input array


  switch (len) {
    case 3:
      h ^= (str.charCodeAt(i + 2) & 0xff) << 16;

    case 2:
      h ^= (str.charCodeAt(i + 1) & 0xff) << 8;

    case 1:
      h ^= str.charCodeAt(i) & 0xff;
      h =
      /* Math.imul(h, m): */
      (h & 0xffff) * 0x5bd1e995 + ((h >>> 16) * 0xe995 << 16);
  } // Do a few final mixes of the hash to ensure the last few
  // bytes are well-incorporated.


  h ^= h >>> 13;
  h =
  /* Math.imul(h, m): */
  (h & 0xffff) * 0x5bd1e995 + ((h >>> 16) * 0xe995 << 16);
  return ((h ^ h >>> 15) >>> 0).toString(36);
}
`
// The main files of seroval, which TanStack Router writes the data of a page with, and the
// directory of any of its modules.
const serovalModule = /[/\\]node_modules[/\\]seroval[/\\]dist[/\\]index\.c?js$/
const serovalPackage = /^(.*[/\\]node_modules[/\\]seroval)[/\\]/
// The length from which seroval escapes a string with JSON.stringify, when that gives the same.
const serovalLength = 'const MIN_JSON_STRINGIFY_LENGTH = 64;'
// seroval's parseObject and parseObjectAsync take an array as an array in their first line, and
// ask the plugins further down. Each entry has the name of the function, that first line, and the
// forms the plugin knows: the call of the plugins further down, and the first line that asks the
// plugins first. From 1.7.0 seroval passes parsePlugin the plugins to ask, those for plain objects
// or the others, and an array gets the others.
const serovalArrays = [
  [
    'parseObject',
    '\tif (Array.isArray(current)) return parseArray(ctx, depth, id, current);\n',
    [
      [
        '\tconst parsed = parsePlugin(ctx, depth, id, current);\n',
        '\tif (Array.isArray(current)) return parsePlugin(ctx, depth, id, current) || parseArray(ctx, depth, id, current);\n',
      ],
      [
        '\tconst parsed = parsePlugin(ctx, depth, id, current, getObjectPlugins(ctx.base, current, currentClass));\n',
        '\tif (Array.isArray(current)) return parsePlugin(ctx, depth, id, current, ctx.base.plugins) || parseArray(ctx, depth, id, current);\n',
      ],
    ],
  ],
  [
    'parseObjectAsync',
    '\tif (Array.isArray(current)) return parseArray$1(ctx, depth, id, current);\n',
    [
      [
        '\tconst parsed = await parsePlugin$1(ctx, depth, id, current);\n',
        '\tif (Array.isArray(current)) return await parsePlugin$1(ctx, depth, id, current) || parseArray$1(ctx, depth, id, current);\n',
      ],
      [
        '\tconst parsed = await parsePlugin$1(ctx, depth, id, current, getObjectPlugins(ctx.base, current, currentClass));\n',
        '\tif (Array.isArray(current)) return await parsePlugin$1(ctx, depth, id, current, ctx.base.plugins) || parseArray$1(ctx, depth, id, current);\n',
      ],
    ],
  ],
]
// lucide-react builds every icon it renders again from the icon's data and props, with object and
// array spreads that take long in QuickJS. The tifa plugin caches what it built for an icon with
// the same props, see lucideCache. lucideFiles are the files of lucide-react 1.42 to
// 1.61 the cache depends on, each with the start of the base64 SHA-256 of its text with the
// version taken out of its license comment (see lucideText).
const lucideModule = /[/\\]lucide-react[/\\]dist[/\\]esm[/\\](Icon|createLucideIcon)\.mjs$/
const lucideFiles = {
  'Icon.mjs': 'fSlEepanC8e4JvCZ',
  'createLucideIcon.mjs': 'EBpPPIf8R/bnOJe7',
  'shared/src/build/buildLucideIconForReact.mjs': 'kCsZISFzunSDGldZ',
  'shared/src/build/buildLucideIconNode.mjs': '2ghtU8h+rJ6hPYjs',
  'shared/src/build/defaultAttributes.mjs': 'NvQmyXYwf1MBgSgm',
  'shared/src/utils/mergeClasses.mjs': 'VAuAw++Ix73S/b7C',
  'shared/src/utils/hasA11yProp.mjs': 'gN3CMtFD/5HmcRru',
}
const lucideParams = `    const hasAccessibleProp = Boolean(children) || hasA11yProp(rest);
    const [name, svgAttributes, builtIconNode = []] = buildLucideIconForReact(icon, {
      color: color ?? contextColor,
      width: width ?? size ?? contextSize,
      height: height ?? size ?? contextSize,
      strokeWidth: strokeWidth ?? contextStrokeWidth,
      absoluteStrokeWidth: absoluteStrokeWidth ?? contextAbsoluteStrokeWidth,
      nonScalingStroke: nonScalingStroke ?? contextNonScalingStroke,
      className: mergeClasses(contextClass, className),
      hasA11yProp: hasAccessibleProp,
      attributes: rest
    });
`
const lucidePaths = '        ...builtIconNode.map(([tag, attrs]) => createElement(tag, attrs)),\n'
const lucideIconData =
  '  const iconData = typeof iconDataOrName === "string" ? toLucideIconData(iconDataOrName, iconNode, aliases) : iconDataOrName;\n'
// lucideCacheSource is the cache, added at the end of Icon.mjs. tifaFindIcon looks up what
// buildLucideIconForReact built for icon with the values of the props that went into it: each prop
// lucide reads, the two class names mergeClasses joins, whether the icon has an accessible name,
// and each prop lucide passes on to the svg, in their order. It caches only icons
// createLucideIcon made, whose data lucide never changes, and only when every value is a
// primitive, which it compares as Object.is does: an object or a function prop may change between
// renders. It finds nothing while a page has replaced a built-in that lucide builds an icon with.
// Each icon keeps the last lucideCacheSize ways it was built. tifaKeepIcon caches what a render
// that found nothing built, and the elements of the icon's paths, which React does not change.
const lucideCacheSize = 16
const lucideCacheSource = `
const tifaLucideIcons = new WeakSet();
const tifaIconCache = new WeakMap();
let tifaPending = null;
const tifaArray = Array.prototype, tifaString = String.prototype;
const tifaArrayIterator = Object.getPrototypeOf([][Symbol.iterator]());
const tifaFilter = tifaArray.filter, tifaMap = tifaArray.map, tifaReduce = tifaArray.reduce, tifaIndexOf = tifaArray.indexOf, tifaJoin = tifaArray.join;
const tifaValues = tifaArray[Symbol.iterator], tifaNext = tifaArrayIterator.next, tifaTrim = tifaString.trim, tifaSplit = tifaString.split;
const tifaSplitter = tifaString[Symbol.split], tifaEntries = Object.entries, tifaNumber = Number, tifaBoolean = Boolean, tifaSymbols = Object.getOwnPropertySymbols;
function tifaBuiltinsKept() {
  return tifaArray.filter === tifaFilter && tifaArray.map === tifaMap && tifaArray.reduce === tifaReduce && tifaArray.indexOf === tifaIndexOf &&
    tifaArray.join === tifaJoin && tifaArray[Symbol.iterator] === tifaValues && tifaArrayIterator.next === tifaNext && tifaString.trim === tifaTrim &&
    tifaString.split === tifaSplit && tifaString[Symbol.split] === tifaSplitter && Object.entries === tifaEntries && Number === tifaNumber &&
    Boolean === tifaBoolean && Object.getOwnPropertySymbols === tifaSymbols;
}
function tifaPrimitive(v) {
  return v === null || (typeof v !== "object" && typeof v !== "function" && typeof v !== "symbol");
}
function tifaIs(a, b) {
  return a === b ? a !== 0 || 1 / a === 1 / b : a !== a && b !== b;
}
function tifaSameRest(entry, rest) {
  const keys = entry.keys, values = entry.values;
  let n = 0;
  for (const key in rest) {
    if (n === keys.length || keys[n] !== key || !tifaIs(values[n], rest[key])) return false;
    n++;
  }
  return n === keys.length;
}
function tifaFindIcon(icon, color, width, height, strokeWidth, absoluteStrokeWidth, nonScalingStroke, contextClass, className, a11y, rest) {
  tifaPending = null;
  if (!tifaLucideIcons.has(icon) || !tifaBuiltinsKept() || tifaSymbols(rest).length !== 0) return void 0;
  const cache = tifaIconCache.get(icon);
  if (cache !== void 0) {
    const list = cache.list;
    for (let i = 0; i < list.length; i++) {
      const at = (cache.last + i) % list.length, e = list[at];
      if (tifaIs(e.color, color) && tifaIs(e.width, width) && tifaIs(e.height, height) && tifaIs(e.strokeWidth, strokeWidth) &&
        tifaIs(e.absoluteStrokeWidth, absoluteStrokeWidth) && tifaIs(e.nonScalingStroke, nonScalingStroke) && tifaIs(e.contextClass, contextClass) &&
        tifaIs(e.className, className) && e.a11y === a11y && tifaSameRest(e, rest)) {
        cache.last = at;
        return e;
      }
    }
  }
  if (!(tifaPrimitive(color) && tifaPrimitive(width) && tifaPrimitive(height) && tifaPrimitive(strokeWidth) && tifaPrimitive(absoluteStrokeWidth) &&
    tifaPrimitive(nonScalingStroke) && tifaPrimitive(contextClass) && tifaPrimitive(className))) return void 0;
  const keys = [], values = [];
  let n = 0;
  for (const key in rest) {
    const value = rest[key];
    if (!tifaPrimitive(value)) return void 0;
    keys[n] = key;
    values[n] = value;
    n++;
  }
  tifaPending = { icon, color, width, height, strokeWidth, absoluteStrokeWidth, nonScalingStroke, contextClass, className, a11y, keys, values, built: null, kids: null };
  return void 0;
}
function tifaKeepIcon(built, kids) {
  const e = tifaPending;
  if (e === null) return kids;
  tifaPending = null;
  let cache = tifaIconCache.get(e.icon);
  if (cache === void 0) tifaIconCache.set(e.icon, (cache = { list: [], next: 0, last: 0 }));
  e.icon = null;
  e.built = built;
  e.kids = kids;
  if (cache.list.length < ${lucideCacheSize}) {
    cache.list[cache.list.length] = e;
  } else {
    cache.list[cache.next] = e;
    cache.next = (cache.next + 1) % ${lucideCacheSize};
  }
  return kids;
}
export { tifaLucideIcons };
`
// Slot of @radix-ui/react-slot merges the props of the Slot into those of its child with
// mergeProps on every render of a component that uses asChild. For each prop of the child it tests a
// regexp, and it builds the class name with filter and join. The tifa plugin changes mergeProps in
// dist/index.mjs and dist/index.js, see radixMergeProps. radixMergePropsSource is mergeProps of
// react-slot 1.4 to 1.5 and radixMergePropsOldSource the one of 1.2.1 to 1.3, which does not merge
// aria-describedby.
const radixSlotModule = /[/\\]@radix-ui[/\\]react-slot[/\\]dist[/\\]index\.m?js$/
const radixHandlerTest = '    const isHandler = /^on[A-Z]/.test(propName);\n'
const radixClassJoin = '      overrideProps[propName] = [slotPropValue, childPropValue].filter(Boolean).join(" ");\n'
const radixMergePropsOldSource = `function mergeProps(slotProps, childProps) {
  const overrideProps = { ...childProps };
  for (const propName in childProps) {
    const slotPropValue = slotProps[propName];
    const childPropValue = childProps[propName];
${radixHandlerTest}    if (isHandler) {
      if (slotPropValue && childPropValue) {
        overrideProps[propName] = (...args) => {
          const result = childPropValue(...args);
          slotPropValue(...args);
          return result;
        };
      } else if (slotPropValue) {
        overrideProps[propName] = slotPropValue;
      }
    } else if (propName === "style") {
      overrideProps[propName] = { ...slotPropValue, ...childPropValue };
    } else if (propName === "className") {
${radixClassJoin}    }
  }
  return { ...slotProps, ...overrideProps };
}
`
const radixMergePropsSource = radixMergePropsOldSource.replace(
  '    }\n  }\n  return',
  `    } else if (propName === "aria-describedby") {
      overrideProps[propName] = concatAriaDescribedby(childPropValue, slotPropValue);
    }
  }
  return`,
)
// react-i18next gives each call of useTranslation a copy of the whole i18n object, which takes about
// 40 property descriptors a call in QuickJS and runs for every component that translates. The tifa
// plugin changes dist/es/useTranslation.js and dist/commonjs/useTranslation.js, see
// reactI18nextCopy, and leaves the rest. i18nWrapperSource is the function that makes the copy.
const reactI18nextModule = /[/\\]react-i18next[/\\]dist[/\\](?:es|commonjs)[/\\]useTranslation\.js$/
const i18nWrapperSource = `  const createI18nWrapper = original => {
    const descriptors = Object.getOwnPropertyDescriptors(original);
    if (descriptors.__original) delete descriptors.__original;
    const wrapper = Object.create(Object.getPrototypeOf(original), descriptors);
    if (!Object.prototype.hasOwnProperty.call(wrapper, '__original')) {
      try {
        Object.defineProperty(wrapper, '__original', {
          value: original,
          writable: false,
          enumerable: false,
          configurable: false
        });
      } catch (_) {}
    }
    return wrapper;
  };
`
// i18nWrapperLists is how many objects reactI18nextCopy keeps a list of keys for.
const i18nWrapperLists = 8
// i18nWrapperHelpers is what reactI18nextCopy adds to the end of the module. A list holds the own
// keys of an object that tifaWrapperKeep found to have only plain properties: strings for names,
// data, writable, enumerable and configurable, and a prototype that is neither null nor
// Object.prototype. tifaWrapperCopy gives a copy of the object when its keys,
// prototype and room for new keys are the ones of the list, and nothing otherwise.
const i18nWrapperHelpers = `
var tifaWrapperLists = [], tifaWrapperOwnKeys = Reflect.ownKeys, tifaWrapperGetProto = Object.getPrototypeOf, tifaWrapperSetProto = Object.setPrototypeOf;
var tifaWrapperExtensible = Object.isExtensible, tifaWrapperDefine = Object.defineProperty, tifaWrapperObject = Object.prototype;
function tifaWrapperCopy(original) {
  let list;
  for (let i = 0; i < tifaWrapperLists.length; i++) if (tifaWrapperLists[i].original === original) { list = tifaWrapperLists[i]; break; }
  if (list === undefined) return;
  const keys = tifaWrapperOwnKeys(original), expected = list.keys, n = expected.length;
  if (keys.length !== n || tifaWrapperGetProto(original) !== list.proto || !tifaWrapperExtensible(original)) return;
  for (let i = 0; i < n; i++) if (keys[i] !== expected[i]) return;
  const wrapper = tifaWrapperSetProto({ ...original }, list.proto);
  tifaWrapperDefine(wrapper, '__original', { value: original, writable: false, enumerable: false, configurable: false });
  return wrapper;
}
function tifaWrapperKeep(original, descriptors) {
  const lists = tifaWrapperLists;
  for (let i = 0; i < lists.length; i++) if (lists[i].original === original) { lists.splice(i, 1); break; }
  const proto = tifaWrapperGetProto(original);
  if (proto === null || proto === tifaWrapperObject) return;
  const keys = tifaWrapperOwnKeys(descriptors);
  for (let i = 0; i < keys.length; i++) {
    const key = keys[i], d = descriptors[key];
    if (typeof key !== 'string' || key === '__original' || !('value' in d) || !d.writable || !d.enumerable || !d.configurable) return;
  }
  if (lists.length === ${i18nWrapperLists}) lists.shift();
  lists.push({ original, keys, proto });
}
`
// i18next translates with code that copies and searches more than a translation needs, which takes
// long in QuickJS. The tifa plugin changes two of its functions in dist/esm/i18next.js and
// dist/cjs/i18next.js, see i18nextFaster, and leaves the rest. i18nextFixedT is the end of the
// function getFixedT returns, which copies all options of i18next into selectorOpts on every call,
// though only a key or key prefix that is a function reads them. i18nextObjectPath is RegExpCache
// and the start of looksLikeObjectPath, which builds the source of the same regexp from the
// separators on every call.
const i18nextModule = /[/\\]i18next[/\\]dist[/\\](?:esm|cjs)[/\\]i18next\.js$/
const i18nextFixedT = `      const selectorOpts = {
        ...this.options,
        ...o
      };
      if (Array.isArray(scopeNs) && !explicitCallNs) selectorOpts.ns = scopeNs;
      if (typeof o.keyPrefix === 'function') o.keyPrefix = keysFromSelector(o.keyPrefix, selectorOpts);
      const keySeparator = this.options.keySeparator || '.';
      let resultKey;
      if (o.keyPrefix && Array.isArray(key)) {
        resultKey = key.map(k => {
          if (typeof k === 'function') k = keysFromSelector(k, selectorOpts);
          return \`\${o.keyPrefix}\${keySeparator}\${k}\`;
        });
      } else {
        if (typeof key === 'function') key = keysFromSelector(key, selectorOpts);
        resultKey = o.keyPrefix ? \`\${o.keyPrefix}\${keySeparator}\${key}\` : key;
      }
      return this.t(resultKey, o);
`
const i18nextObjectPath = `class RegExpCache {
  constructor(capacity) {
    this.capacity = capacity;
    this.regExpMap = new Map();
    this.regExpQueue = [];
  }
  getRegExp(pattern) {
    const regExpFromCache = this.regExpMap.get(pattern);
    if (regExpFromCache !== undefined) {
      return regExpFromCache;
    }
    const regExpNew = new RegExp(pattern);
    if (this.regExpQueue.length === this.capacity) {
      this.regExpMap.delete(this.regExpQueue.shift());
    }
    this.regExpMap.set(pattern, regExpNew);
    this.regExpQueue.push(pattern);
    return regExpNew;
  }
}
const chars = [' ', ',', '?', '!', ';'];
const looksLikeObjectPathRegExpCache = new RegExpCache(20);
const looksLikeObjectPath = (key, nsSeparator, keySeparator) => {
  nsSeparator = nsSeparator || '';
  keySeparator = keySeparator || '';
  const possibleChars = chars.filter(c => !nsSeparator.includes(c) && !keySeparator.includes(c));
  if (possibleChars.length === 0) return true;
  const r = looksLikeObjectPathRegExpCache.getRegExp(\`(\${possibleChars.map(c => c === '?' ? '\\\\?' : c).join('|')})\`);
`
// i18nextCacheSource is what the tifa plugin adds to i18next for build.i18next_cache, see
// i18nextCache. It is one line, which goes before the line that makes i18next's default
// instance, and so after all classes of i18next and before any instance exists.
//
// tifaI18nDebts holds a number for each key of t: a call the cache does not answer adds
// i18nextCacheMiss, one it answers takes i18nextCacheHit off, down to -i18nextCacheOff. From
// i18nextCacheOff on, calls of the key skip the cache and take 1 off each, so one call in
// i18nextCacheMiss + 1 tries the cache again.
const i18nextCacheSize = 1 << 20
const i18nextCacheMiss = 32
const i18nextCacheHit = 128
const i18nextCacheOff = 64 * i18nextCacheMiss
const i18nextCacheKeys = 4096
const i18nextCacheSource = `
var tifaI18nCaches = new WeakMap(), tifaI18nBytes = 0, tifaI18nEffects = 0, tifaI18nIds = new WeakMap(), tifaI18nLastId = 0;
var tifaI18nDebts = new Map(), tifaI18nFormatters = new WeakSet(), tifaI18nFound;
var tifaI18nIsArray = Array.isArray, tifaI18nPrototypeOf = Object.getPrototypeOf, tifaI18nObject = Object.prototype;
function tifaI18nClear() { tifaI18nEffects++; tifaI18nCaches = new WeakMap(); tifaI18nBytes = 0; }
function tifaI18nPart(v) {
  switch (typeof v) {
    case 'string': return 's' + v.length + ':' + v;
    case 'number': return (v === 0 && 1 / v < 0 ? 'n-0' : 'n' + v) + ';';
    case 'boolean': return v ? 'T' : 'F';
    case 'undefined': return 'U';
    case 'bigint': return 'b' + v + ';';
    case 'object':
      if (v === null) return 'N';
      if (tifaI18nIsArray(v)) {
        let s = 'a' + v.length + ';';
        for (let i = 0; i < v.length; i++) { const e = v[i]; if (typeof e !== 'string') return; s += e.length + ':' + e; }
        return s;
      }
  }
}
function tifaI18nObjectPart(o) {
  const proto = tifaI18nPrototypeOf(o);
  if (proto !== tifaI18nObject && proto !== null) return;
  let k = 'o', p;
  for (const name in o) { if ((p = tifaI18nPart(o[name])) === undefined) return; k += name.length + ':' + name + p; }
  return k + ';';
}
function tifaI18nKey(i18n, options, fixedT, keyPrefix, key, opts, rest) {
  let k, p;
  if ((k = tifaI18nPart(fixedT.lng)) === undefined) return;
  if (!fixedT.lng) { if ((p = tifaI18nPart(i18n.translator.language)) === undefined) return; k += p; }
  if ((p = tifaI18nPart(fixedT.lngs)) === undefined) return; k += p;
  if ((p = tifaI18nPart(fixedT.ns)) === undefined) return; k += p;
  if ((p = tifaI18nPart(fixedT.keyPrefix)) === undefined) return; k += p;
  if ((p = tifaI18nPart(keyPrefix)) === undefined) return; k += p;
  k += key.length + ':' + key;
  const format = i18n.services.i18nFormat, formats = format && format.formats;
  if (typeof formats === 'object' && formats !== null) {
    let f = tifaI18nIds.get(formats);
    if (f === undefined) tifaI18nIds.set(formats, f = ++tifaI18nLastId);
    k += 'f' + f + ';';
  }
  const defaults = options.interpolation.defaultVariables;
  if (defaults) { if (typeof defaults !== 'object' || (p = tifaI18nObjectPart(defaults)) === undefined) return; k += p; }
  if (typeof opts === 'object') {
    if (opts === null) return k + 'N';
    if ((p = tifaI18nObjectPart(opts)) === undefined) return;
    return k + p;
  }
  if ((p = tifaI18nPart(opts)) === undefined) return; k += 'p' + p;
  for (let i = 0; i < rest.length; i++) { if ((p = tifaI18nPart(rest[i])) === undefined) return; k += p; }
  return k;
}
function tifaI18nFind(i18n, fixedT, keyPrefix, key, opts, rest) {
  if (typeof key !== 'string') return;
  const options = i18n.options;
  if (options.isClone || options.missingInterpolationHandler) return;
  let debt = tifaI18nDebts.get(key);
  if (debt === undefined) { if (tifaI18nDebts.size >= ${i18nextCacheKeys}) tifaI18nDebts.clear(); tifaI18nDebts.set(key, debt = { n: 0 }); }
  else if (debt.n >= ${i18nextCacheOff}) { debt.n--; return; }
  if (!(i18n.services.formatter instanceof Formatter)) return;
  const k = tifaI18nKey(i18n, options, fixedT, keyPrefix, key, opts, rest), cache = tifaI18nCaches.get(i18n);
  if (k !== undefined && cache !== undefined) {
    const hit = cache.get(k);
    if (hit !== undefined) { debt.n = debt.n > ${i18nextCacheHit - i18nextCacheOff} ? debt.n - ${i18nextCacheHit} : ${-i18nextCacheOff}; tifaI18nFound = hit; return null; }
  }
  debt.n += ${i18nextCacheMiss};
  return k;
}
function tifaI18nKeep(i18n, key, effects, result) {
  if (effects === tifaI18nEffects && typeof result === 'string') {
    const bytes = 2 * (key.length + result.length) + 80;
    if (tifaI18nBytes + bytes > ${i18nextCacheSize}) { tifaI18nCaches = new WeakMap(); tifaI18nBytes = 0; }
    if (bytes > ${i18nextCacheSize}) return result;
    let cache = tifaI18nCaches.get(i18n);
    if (cache === undefined) { tifaI18nCaches.set(i18n, cache = new Map()); tifaI18nFormatters.add(i18n.services.formatter); }
    cache.set(key, result); tifaI18nBytes += bytes;
  }
  return result;
}
function tifaI18nWrap(object, names, clears) {
  for (const name of names) {
    const f = object[name];
    object[name] = function (...args) {
      const clear = clears(this);
      if (clear) tifaI18nClear(); else tifaI18nEffects++;
      try { return f.apply(this, args); } finally { if (clear) tifaI18nClear(); else tifaI18nEffects++; }
    };
  }
}
tifaI18nWrap(ResourceStore.prototype, ['addNamespaces', 'removeNamespaces', 'addResource', 'addResources', 'addResourceBundle', 'removeResourceBundle'], () => true);
tifaI18nWrap(Formatter.prototype, ['init', 'add', 'addCached'], (formatter) => tifaI18nFormatters.has(formatter));
var tifaI18nOwn = new WeakSet();
function tifaI18nCount(formatter) {
  const formats = formatter.formats;
  if (typeof formats !== 'object' || formats === null) return;
  for (const name in formats) {
    const f = formats[name];
    if (typeof f !== 'function' || tifaI18nOwn.has(f)) continue;
    const counted = function (...args) { tifaI18nEffects++; return f.apply(this, args); };
    tifaI18nOwn.add(counted);
    formats[name] = counted;
  }
}
var tifaI18nFormatterInit = Formatter.prototype.init, tifaI18nAdd = Formatter.prototype.add, tifaI18nAddCached = Formatter.prototype.addCached;
Formatter.prototype.init = function (...args) {
  try { return tifaI18nFormatterInit.apply(this, args); } finally { const formats = this.formats; if (typeof formats === 'object' && formats !== null) for (const name in formats) if (typeof formats[name] === 'function') tifaI18nOwn.add(formats[name]); }
};
Formatter.prototype.add = function (...args) { try { return tifaI18nAdd.apply(this, args); } finally { tifaI18nCount(this); } };
Formatter.prototype.addCached = function (...args) { try { return tifaI18nAddCached.apply(this, args); } finally { tifaI18nCount(this); } };
tifaI18nWrap(postProcessor, ['addPostProcessor'], () => true);
var tifaI18nHandle = postProcessor.handle;
postProcessor.handle = function (...args) { tifaI18nEffects++; return tifaI18nHandle.apply(this, args); };
tifaI18nWrap(I18n.prototype, ['loadNamespaces'], () => true);
tifaI18nWrap(I18n.prototype, ['init', 'reloadResources', 'use', 'setDefaultNamespace', 'loadLanguages'], (i18n) => !i18n.options.isClone && tifaI18nCaches.delete(i18n));
var tifaI18nForward = Logger.prototype.forward;
Logger.prototype.forward = function (...args) { tifaI18nEffects++; return tifaI18nForward.apply(this, args); };
`
// i18nextCacheHead, i18nextCacheReturn and i18nextCacheInstance are the lines i18nextCache
// changes, each once in i18next, and i18nextCacheMethods the methods i18nextCacheSource and the
// changed lines need, by the line that starts their class or object.
const i18nextCacheHead = '    const fixedT = (key, opts, ...rest) => {\n'
const i18nextCacheReturn = '      return this.t(resultKey, o);\n    };\n'
const i18nextCacheInstance = '\nconst instance = I18n.createInstance();\n'
const i18nextCacheMethods = {
  'class Logger {': ['forward'],
  'class ResourceStore extends EventEmitter {': ['addNamespaces', 'removeNamespaces', 'addResource', 'addResources', 'addResourceBundle', 'removeResourceBundle'],
  'class Translator extends EventEmitter {': ['changeLanguage', 'translate'],
  'class Formatter {': ['init', 'add', 'addCached', 'format'],
  'var postProcessor = {': ['addPostProcessor', 'handle'],
  'class I18n extends EventEmitter {': ['init', 'loadResources', 'reloadResources', 'use', 'changeLanguage', 'setDefaultNamespace', 'loadNamespaces', 'loadLanguages', 'cloneInstance', 'getFixedT'],
}
// createElement and cloneElement of React in its production file, as React 19.3 has them. Each
// copies the keys of its config into the props of the element with a for-in loop that asks
// hasOwnProperty.call about each key and skips key, __self and __source (and a ref that is
// undefined, in cloneElement). The loop takes about 250 ns for each key in QuickJS. nativeCopyProps
// asks __tifa_copy_props, which internal/ssr defines in each VM, before the loop. That function is
// the loop written in C in the engine, and it answers false, having changed nothing, for any config
// where the loop and it could differ, which then goes through the loop as before. The plugin
// changes a function only when its whole text is the one below, so that a change anywhere in it
// (the order of the steps, the key, the defaults, the element it makes) leaves it as React has it.
const propsCreate = `exports.createElement = function (type, config, children) {
  var propName,
    props = {},
    key = null;
  if (null != config)
    for (propName in (void 0 !== config.key && (key = "" + config.key), config))
      hasOwnProperty.call(config, propName) &&
        "key" !== propName &&
        "__self" !== propName &&
        "__source" !== propName &&
        (props[propName] = config[propName]);
  var childrenLength = arguments.length - 2;
  if (1 === childrenLength) props.children = children;
  else if (1 < childrenLength) {
    for (var childArray = Array(childrenLength), i = 0; i < childrenLength; i++)
      childArray[i] = arguments[i + 2];
    props.children = childArray;
  }
  if (type && type.defaultProps)
    for (propName in ((childrenLength = type.defaultProps), childrenLength))
      void 0 === props[propName] &&
        (props[propName] = childrenLength[propName]);
  return ReactElement(type, key, props);
};
`
const propsClone = `exports.cloneElement = function (element, config, children) {
  if (null === element || void 0 === element)
    throw Error(
      "The argument must be a React element, but you passed " + element + "."
    );
  var props = assign({}, element.props),
    key = element.key;
  if (null != config)
    for (propName in (void 0 !== config.key && (key = "" + config.key), config))
      !hasOwnProperty.call(config, propName) ||
        "key" === propName ||
        "__self" === propName ||
        "__source" === propName ||
        ("ref" === propName && void 0 === config.ref) ||
        (props[propName] = config[propName]);
  var propName = arguments.length - 2;
  if (1 === propName) props.children = children;
  else if (1 < propName) {
    for (var childArray = Array(propName), i = 0; i < propName; i++)
      childArray[i] = arguments[i + 2];
    props.children = childArray;
  }
  return ReactElement(element.type, key, props);
};
`
// The loop of each function, and the code that takes its place. The code reads config.key first,
// as the head of the loop does, and then asks the engine. The loop that follows is the one React
// has, over the same config, for the VM that has no such function and for a config the function
// does not copy.
const propsCreateLoop = `  if (null != config)
    for (propName in (void 0 !== config.key && (key = "" + config.key), config))
      hasOwnProperty.call(config, propName) &&
        "key" !== propName &&
        "__self" !== propName &&
        "__source" !== propName &&
        (props[propName] = config[propName]);
`
const propsCloneLoop = `  if (null != config)
    for (propName in (void 0 !== config.key && (key = "" + config.key), config))
      !hasOwnProperty.call(config, propName) ||
        "key" === propName ||
        "__self" === propName ||
        "__source" === propName ||
        ("ref" === propName && void 0 === config.ref) ||
        (props[propName] = config[propName]);
`
const propsCreateNative = `  if (null != config)
    if (
      (void 0 !== config.key && (key = "" + config.key),
      !tifaCopyProps || !tifaCopyProps(config, props, hasOwnProperty, false))
    )
      for (propName in config)
        hasOwnProperty.call(config, propName) &&
          "key" !== propName &&
          "__self" !== propName &&
          "__source" !== propName &&
          (props[propName] = config[propName]);
`
const propsCloneNative = `  if (null != config)
    if (
      (void 0 !== config.key && (key = "" + config.key),
      !tifaCopyProps || !tifaCopyProps(config, props, hasOwnProperty, true))
    )
      for (propName in config)
        !hasOwnProperty.call(config, propName) ||
          "key" === propName ||
          "__self" === propName ||
          "__source" === propName ||
          ("ref" === propName && void 0 === config.ref) ||
          (props[propName] = config[propName]);
`
// What the code needs, taken when the module of React runs and added to its end, so that no line of
// the file moves. A VM with no __tifa_copy_props gets null and runs the loops.
const propsNative = `
var tifaCopyProps = "function" === typeof __tifa_copy_props ? __tifa_copy_props : null;
`
// The error handler of renderToString, which ignores the errors of the render.
const ignoredErrors = 'function onError() {}'
// React's HTML escape in its string renderer, with the regular expression that finds the first
// character to escape.
const escapeSource = 'matchHtmlRegExp = /["\'&<>]/;\nfunction escapeTextForBrowser(text) {'
// The function of React's string renderer that writes an attribute whose value is a string, the
// calls of it with a name that is a string in pushAttribute, the case of pushAttribute that calls it
// with the name it was given, the first test of isAttributeNameSafe and its call in the default branch
// of pushAttribute.
const stringAttributeSource = `function pushStringAttribute(target, name, value) {
  "function" !== typeof value &&
    "symbol" !== typeof value &&
    "boolean" !== typeof value &&
    target.push(" ", name, '="', escapeTextForBrowser(value), '"');
}`
const stringAttributeCall = /pushStringAttribute\(target, ("[^"\n]*"), value\);/g
const literalAttributePush = /target\.push\(" ", ("[^"\n]*"), '="', escapeTextForBrowser\(value\), '"'\)/g
const sharedStringCase = `    case "dir":
    case "role":
    case "viewBox":
    case "width":
    case "height":
      pushStringAttribute(target, name, value);`
const attributeNameTest = `function isAttributeNameSafe(attributeName) {
  if (hasOwnProperty.call(validatedAttributeNameCache, attributeName))
    return !0;`
const attributeNameCheck = 'if (((name = aliases.get(name) || name), isAttributeNameSafe(name))) {'
// The tests of the type of a value in pushAttribute that inlineAttributeCalls changes so that a string
// is let through by one question: the one of the cases src, href, action and formAction, the one of
// xlinkHref, and the switch of the default branch.
const urlTypeTest = `      if (
        null == value ||
        "function" === typeof value ||
        "symbol" === typeof value ||
        "boolean" === typeof value
      )
        break;`
const xlinkTypeTest = `    case "xlinkHref":
      if (
        "function" === typeof value ||
        "symbol" === typeof value ||
        "boolean" === typeof value
      )
        break;`
const typeSwitch = `          switch (typeof value) {
            case "function":
            case "symbol":
              return;
            case "boolean":`
// escapeCacheSize is how much the cache of escapes holds in each VM, in UTF-16 code units: the
// strings and their escapes, and escapeEntrySize more for each entry, which stands for the memory
// an entry of the Map takes besides its strings. So many short strings cannot take more memory
// than a few long ones. Full, the cache takes about 0.5 to 2 MB of VM memory, the most for strings
// of a little over 8,192 code units outside Latin-1.
const escapeCacheSize = 1 << 18
const escapeEntrySize = 64
// The loop of escapeTextForBrowser in React's string renderer that escapes a string one character
// at a time.
const escapeLoop = `  var match = matchHtmlRegExp.exec(text);
  if (match) {
    var html = "",
      index,
      lastIndex = 0;
    for (index = match.index; index < text.length; index++) {
      switch (text.charCodeAt(index)) {
        case 34:
          match = "&quot;";
          break;
        case 38:
          match = "&amp;";
          break;
        case 39:
          match = "&#x27;";
          break;
        case 60:
          match = "&lt;";
          break;
        case 62:
          match = "&gt;";
          break;
        default:
          continue;
      }
      lastIndex !== index && (html += text.slice(lastIndex, index));
      lastIndex = index + 1;
      html += match;
    }
    text = lastIndex !== index ? html + text.slice(lastIndex, index) : html;
  }
  return text;`
// The function of React's string renderer that puts an element on the component stack of a task,
// and the code in renderNodeDestructive that puts each node on it and takes it off again.
const stackPush = `function pushComponentStack(task) {
  var node = task.node;
  if ("object" === typeof node && null !== node)
    switch (node.$$typeof) {
      case REACT_ELEMENT_TYPE:
        task.componentStack = { parent: task.componentStack, type: node.type };
    }
}`
const stackAroundNode = `      (node = task.componentStack),
      pushComponentStack(task),
      retryNode(request, task),
      (task.componentStack = node));`
// The readers of component stacks in React's string renderer of React DOM 19.3, as the calls of
// getThrownInfo and the reads of a componentStack property: getThrownInfo makes the errorInfo for
// the error handler, and the reads hand the stack on from task to task and to it. Other versions
// may have as many readers at other places, so the counts are those of one version.
const stackReaders = '7 15'
// Where retryNode in React's string renderer takes the name of a component for its key path.
const keyPathName = `            name = getComponentNameFromType(type),
            keyOrIndex =`
// The readers of the names in key paths in React's string renderer of React DOM 19.3, as the calls
// of JSON.stringify with an array and the reads of [1] of a variable named keyPath or ...KeyPath:
// useActionState hashes a key path for its key, and the code that prerenders a page reads the
// names of the key paths of what it postpones.
const keyPathReaders = '2 4'
// Where React's string renderer makes a key path, and what the tifa plugin makes of each place so
// that it makes one only when tifaKeyPaths says so: for each element, for the children of an array
// that has a place of its own, and for the fallback of a Suspense boundary. The last place is the
// start of createRequest, which sets tifaKeyPaths from a global that the bundle has when a module
// has $$FORM_ACTION in its code.
const keyPathMakers = [
  ['          key = [task.keyPath, name, keyOrIndex];', '          key = tifaKeyPaths ? [task.keyPath, name, keyOrIndex] : null;'],
  ['    ((task.keyPath = [task.keyPath, "Fragment", childIndex]),', '    ((task.keyPath = tifaKeyPaths ? [task.keyPath, "Fragment", childIndex] : null),'],
  ['              [keyPath[0], "Suspense Fallback", keyPath[2]],', '              tifaKeyPaths ? [keyPath[0], "Suspense Fallback", keyPath[2]] : null,'],
  [
    '  formState\n) {\n  resumableState = new RequestInstance(',
    '  formState\n) {\n  tifaKeyPaths = true === globalThis.tifaFormActions, resumableState = new RequestInstance(',
  ],
]
// The uses of key paths in React's string renderer of React DOM 19.3 that the tifa plugin does not
// guard: the reads of [0] or [2] of a variable named keyPath or ...KeyPath, the assignments to a
// trackedPostpones that are not null, and the calls of createReplayTask. The string renderer makes
// its requests with createRequest, which sets trackedPostpones to null, and a task has a replay
// only when createReplayTask makes it, in the code that resumes a render and in the code that
// follows a trackedPostpones. So the reads of the key paths in the code that resumes or prerenders
// a page never run in renderToString, and the others are those of useActionState and of a
// Suspense fallback.
const keyPathUses = '11 0 2'
// The files of React's string renderer, by the SHA-256 of their text, for which the uses of key
// paths were read to the end, every read of a key path in its code and whether it can run in
// renderToString. The counts of keyPathUses cannot tell a read of a key path that is new, such as
// its length, or one through another name, so the plugin makes no key path only in these files.
// React DOM 19.3.1 differs from 19.3.0 in the line of its version alone.
const reviewedRenderers = new Map([
  ['5528f58d28f5e035dccbf0ebec7e593ed0b97cebae830b838756c27c100a8caa', 'react-dom 19.3.0'],
  ['a470866a7a27a7f4fa69ef04e9bfdea0514fc9defdcc48bda43516b9ae2dfadd', 'react-dom 19.3.1'],
])
// The files of React's production JSX runtime, by the SHA-256 of their text, for which the elements
// that jsx makes were read to the end, see inlineElements. React 19.3.1 has the file of 19.3.0.
const reviewedJsxRuntimes = new Map([['1e46f15002696985e80c61d47aaa30dabb03c954270a690f5f7dfbbacfa9002b', 'react 19.3.0 and 19.3.1']])
// guardedCases is the fewest string cases a switch needs for the tifa plugin to put a Set check in
// front of it, and switchCases the prefix of the names of those Sets.
const guardedCases = 8
const switchCases = 'tifaSwitchCases'
// The loops of flushSubtree in React's string renderer that hand the chunks of a segment, before
// each child and after the last, one by one to the destination.
const segmentLoops = `      for (var childIdx = 0; childIdx < segment.length; childIdx++) {
        for (r = segment[childIdx]; chunkIdx < r.index; chunkIdx++)
          destination.push(chunks[chunkIdx]);
        r = flushSegment(request, destination, r, hoistableState);
      }
      for (; chunkIdx < chunks.length - 1; chunkIdx++)
        destination.push(chunks[chunkIdx]);
      chunkIdx < chunks.length && (r = destination.push(chunks[chunkIdx]));`
// The packages whose CommonJS modules the ES modules of the SSR bundle import as namespaces of plain
// values, see plainNamespaces.
const plainPackages = new Set(['react', 'react-dom'])
// The helpers with which rolldown gives an ES module the exports of a CommonJS module as a
// namespace, white space collapsed: __copyProps defines a getter for each export. rolldown adds $
// and a number to the name of a helper when the bundle has another name like it.
const esmHelpers =
  'var __copyProps = (to, from, except, desc) => { if (from && typeof from === "object" || typeof from === "function") ' +
  '{ for (var keys = __getOwnPropNames(from), i = 0, n = keys.length, key; i < n; i++) { key = keys[i]; if ' +
  '(!__hasOwnProp.call(to, key) && key !== except) { __defProp(to, key, { get: ((k) => from[k]).bind(null, key), ' +
  'enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable }); } } } return to; }; var __toESM = (mod, ' +
  'isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(isNodeMode || !mod ' +
  '|| !mod.__esModule || !__hasOwnProp.call(mod, "default") ? __defProp(target, "default", { value: mod, ' +
  'enumerable: true }) : target, mod));'
// plainESM does what __toESM of esmHelpers does, with a property of each export's value in place of
// a getter. Its name is as long as __toESM, and the helpers it calls are those of __toESM, with
// their names in the bundle.
const plainESM =
  ' function __tifaV(mod, isNodeMode) { var target = mod != null ? __create(__getProtoOf(mod)) : {}; if (isNodeMode ' +
  '|| !mod || !mod.__esModule || !__hasOwnProp.call(mod, "default")) __defProp(target, "default", { value: mod, ' +
  'enumerable: true }); if (mod && typeof mod === "object" || typeof mod === "function") for (var keys = ' +
  '__getOwnPropNames(mod), i = 0, n = keys.length, key, desc; i < n; i++) { key = keys[i]; if ' +
  '(!__hasOwnProp.call(target, key)) __defProp(target, key, { value: mod[key], enumerable: !(desc = ' +
  '__getOwnPropDesc(mod, key)) || desc.enumerable }); } return target; }'
const nodeBuiltins = new Set(builtinModules)

// pollInterval is how often, in milliseconds, Vite's watcher looks at each file once it polls.
// Polling 10,000 files every second takes about 8% of one CPU core.
const pollInterval = 1000

// devState is what the plugin and tifa's Vite driver (js/vite/driver.mjs) share in the Node
// process of tifa dev. Vite bundles the config, this file with it, so they meet on globalThis.
// The driver sets it up, so outside tifa dev it is empty:
//   - pollFiles: the dev server's watcher is to poll, for a system that ran out of watches.
//   - changed(event, file): gets every event of the dev server's watcher.
//   - scanned(watcher): returns once the watcher has read the files it was given.
//   - restart(run): runs run, a restart of the dev server, so the driver knows when one runs.
//   - outOfWatches(err): the watcher ran out of watches or file descriptors.
function devState() {
  return (globalThis[Symbol.for('tifa.dev')] ??= {})
}

/**
 * tifa returns the plugins of a tifa application.
 *
 * settings.target is build.target of the project configuration, the browsers the browser bundle
 * runs in. settings.i18nextCache is build.i18next_cache, see ssrPlugin.
 */
export function tifa(settings = {}) {
  let root = ''
  const plugin = {
    name: 'tifa',
    config(config, { command }) {
      root = fs.realpathSync(path.resolve(config.root ?? '.'))
      const gen = path.join(root, '.tifa')
      const dist = path.join(gen, 'dist')
      return {
        base: command === 'serve' ? devBase : '/',
        appType: 'custom',
        publicDir: false,
        clearScreen: false,
        server: {
          watch: {
            // The executable and the build output change with every rebuild and are no modules.
            ignored: [path.join(root, 'bin') + '/**', dist + '/**'],
            ...(devState().pollFiles ? { usePolling: true, interval: pollInterval, binaryInterval: pollInterval } : {}),
          },
        },
        environments: {
          client: {
            build: {
              outDir: dist,
              emptyOutDir: false,
              assetsDir: 'assets',
              manifest: true,
              copyPublicDir: false,
              cssCodeSplit: false,
              target: settings.target ?? undefined,
              rolldownOptions: {
                input: path.join(gen, 'client.ts'),
                output: {
                  entryFileNames: 'assets/app-[hash].js',
                  chunkFileNames: 'assets/chunk-[hash].js',
                  assetFileNames: 'assets/[name]-[hash][extname]',
                  // Every page loads the entry and what it imports statically, so all of it goes
                  // into the entry script. Rolldown would otherwise split off what route chunks or
                  // the chunks they load share with it, React for one, and a page would wait for
                  // more files before it hydrates, with the same bytes in all.
                  codeSplitting: { groups: [{ name: 'app', tags: ['$initial'] }] },
                },
              },
            },
          },
          // An application may turn the source map off, which the config of a plugin would
          // override.
          ssr: ssrEnvironment(path.join(gen, 'server.ts'), dist, config.environments?.ssr?.build?.sourcemap),
        },
      }
    },
    configureServer(server) {
      const state = devState()
      server.watcher.on('all', (event, file) => state.changed?.(event, file))
      // A module is served once the watcher has read the files, so an edit after the module
      // was read is one the watcher sees. Vite restarts without waiting for the new watcher.
      if (state.scanned) server.middlewares.use((req, res, next) => state.scanned(server.watcher).then(() => next(), next))
      // Every restart goes through server.restart, from Vite after a change to its config as
      // well. A restart copies the new server into the old object, this wrapper with it.
      if (state.restart) {
        const restart = server.restart
        server.restart = (forceOptimize) => state.restart(() => restart(forceOptimize))
      }
      // chokidar reports a watch the system refused as an error and goes on without it.
      server.watcher.on('error', (err) => {
        if (['ENOSPC', 'EMFILE', 'ENFILE'].includes(err?.code)) state.outOfWatches?.(err)
      })
    },
  }
  return [plugin, generated(), lazyRoutes(), ssrPlugin(() => root, () => import('vite'), { i18nextCache: settings.i18nextCache })]
}

// lazyRoute is the prefix of the ID of a stand-in for a route module, see lazyRoutes.
const lazyRoute = '\0tifa-lazy-route:'

/**
 * lazyRoutes makes the browser bundle load every route module except the root Layout when its
 * route needs it. routes.tsx imports a route module with import(), or statically when it exports
 * head, so that the server render, whose bundle is one file, calls head from the module itself.
 * In the browser build this plugin hands such a static import a stand-in instead: the same
 * exports, made with lazyRouteComponent from an import() of the module, and a head that loads
 * the module and calls its head. TanStack Router accepts a head that returns a promise, and it
 * loads the chunk of a route before it runs head, so the module is there by then. The root
 * Layout stays in the entry script: the root route needs its NotFound, and the entries its
 * htmlAttributes. tifa dev serves each module on its own and keeps the static imports.
 *
 * environments are the Vite environments it applies to.
 */
export function lazyRoutes(environments = ['client']) {
  let routes = '' // .tifa/routes.tsx
  let rootLayout = ''
  let router // the ID TanStack Router resolves to from routes.tsx
  let unknownKind = false
  return {
    name: 'tifa:lazy-routes',
    // Before Vite's resolver, which would resolve the import to the module itself.
    enforce: 'pre',
    apply: 'build',
    applyToEnvironment: (env) => environments.includes(env.name),
    configResolved(config) {
      const root = fs.realpathSync(config.root)
      routes = path.join(root, '.tifa', 'routes.tsx')
      rootLayout = path.join(root, 'app', 'layout.tsx')
    },
    resolveId: {
      filter: { id: /^\.\.\/app\// },
      async handler(source, importer, options) {
        if (importer !== routes) return null
        if (options.kind === undefined) {
          // Without the kind of an import, a static one cannot be told from import().
          if (!unknownKind) {
            unknownKind = true
            this.warn(
              'tifa: this Vite does not say whether an import is static, so the route modules that export head ' +
                'are part of the entry script of the browser. tifa is tested with the Vite that tifa new writes into package.json',
            )
          }
          return null
        }
        if (options.kind !== 'import-statement') return null
        const resolved = await this.resolve(source, importer, { kind: options.kind, skipSelf: true })
        if (!resolved || resolved.external || resolved.id === rootLayout) return null
        router ??= await this.resolve('@tanstack/react-router', importer, { kind: options.kind, skipSelf: true })
        if (!router) this.error(`@tanstack/react-router does not resolve from ${importer}`)
        return lazyRoute + resolved.id
      },
    },
    load: {
      filter: { id: new RegExp('^' + lazyRoute) },
      handler(id) {
        return lazyRouteModule(router.id, id.slice(lazyRoute.length))
      },
    },
  }
}

// lazyRouteModule is the stand-in lazyRoutes gives routes.tsx for the route module module, with
// lazyRouteComponent imported from router. routes.tsx reads default, head, Error and NotFound of
// a route module (TestHeadModulesReadOnlyWhatTheirStandInExports in internal/codegen holds it to
// that). A stand-in for an export the module lacks is never read, and the bundler leaves it out.
export function lazyRouteModule(router, module) {
  return (
    `import { lazyRouteComponent } from ${JSON.stringify(router)}\n` +
    `const load = () => import(${JSON.stringify(module)})\n` +
    'export default lazyRouteComponent(load)\n' +
    'const error = /* @__PURE__ */ lazyRouteComponent(load, "Error")\n' +
    'const notFound = /* @__PURE__ */ lazyRouteComponent(load, "NotFound")\n' +
    'export { error as Error, notFound as NotFound }\n' +
    'export const head = (context) => load().then((module) => module.head(context))\n'
  )
}

// generated resolves ./+route and ./+layout, the imports that lead to what tifa generates. Other
// imports, @/ and tifa/client among them, resolve by the paths of the application's
// tsconfig.json, as they do for TypeScript.
export function generated() {
  let root = ''
  return {
    name: 'tifa:generated',
    config(config) {
      root = fs.realpathSync(path.resolve(config.root ?? '.'))
      return { resolve: { tsconfigPaths: true } }
    },
    resolveId: {
      filter: { id: routeModule },
      handler(source, importer) {
        if (!importer) return null
        return resolveRouteModule(this, root, source, importer)
      },
    },
  }
}

// ssrEnvironment is the Vite environment that builds the SSR bundle from entry into dir/ssr.js.
// The bundle is an ES module, which the server loads as one: a dependency may await at its top
// level, and its code runs in strict mode, as in the browser bundle. polyfillRequire: false keeps
// a require of a CommonJS module as it is, a call of the host's require, where it would become a
// require that rolldown makes with createRequire of Node. The syntax target is ES2022, which
// QuickJS runs as it is: a lower one only slows rendering, a higher one may emit syntax QuickJS
// lacks. Whoever builds it sets build.minify. sourcemap is build.sourcemap, hidden when
// undefined.
//
// Packages resolve to their main files, not their browser ones, unlike bundlers for Cloudflare
// Workers. A browser file often takes what the browser has (abort-controller takes
// AbortController from self, form-data takes FormData from window), and QuickJS has none of it,
// so renders that work with the main file would fail, with no build warning to point at them.
export function ssrEnvironment(entry, dir, sourcemap = 'hidden') {
  return {
    consumer: 'server',
    resolve: {
      noExternal: true,
      conditions: ['worker', 'import', 'module', 'default'],
      mainFields: ['module', 'main'],
      builtins: [],
    },
    define: {
      'process.env.NODE_ENV': '"production"',
      global: 'globalThis',
    },
    build: {
      outDir: dir,
      emptyOutDir: false,
      copyPublicDir: false,
      ssr: entry,
      target: 'es2022',
      // ssr.js.map names the sources of the places in the warnings of the build and in the stack
      // traces of render errors, which the server reads. hidden leaves the comment that points to
      // it out of ssr.js. The server needs no copy of the sources.
      sourcemap,
      rolldownOptions: {
        output: {
          format: 'esm',
          polyfillRequire: false,
          entryFileNames: 'ssr.js',
          codeSplitting: false,
          sourcemapExcludeSources: true,
        },
      },
    },
  }
}

// resolveRouteModule resolves ./+route and ./+layout the way the generated tsconfig lets
// TypeScript see them: rootDirs merges the application root and .tifa, so a relative import whose
// last element is +route or +layout, with or without .js or .ts, and which points into app/<dir>
// is .tifa/app/<dir>/+route.ts or +layout.ts, whatever the path in between.
function resolveRouteModule(ctx, root, source, importer) {
  const app = path.join(root, 'app')
  const gen = path.join(root, '.tifa')
  let dir = path.dirname(importer.split('?')[0])
  // A generated module imports its neighbours the same way, from .tifa/app.
  if (dir === gen || dir.startsWith(gen + path.sep)) dir = path.join(root, path.relative(gen, dir))
  const target = path.join(dir, source).replace(/\.(?:js|ts)$/, '')
  if (!target.startsWith(app + path.sep)) {
    ctx.error(`${source} is generated for directories under app/ only, ${importer} is outside`)
  }
  const rel = path.relative(root, target)
  const module = path.join(gen, rel + '.ts')
  if (!fs.existsSync(module)) {
    ctx.error(
      `${source}: ${path.dirname(rel).split(path.sep).join('/')} has no generated ${path.basename(rel)} module: ` +
        '+route exists for a page, +layout for a Layout with pages',
    )
  }
  return module
}

// ssrPlugin applies to the SSR build only. It replaces *.client modules with a stub, refuses Node
// APIs in page code, trims React's server entry to the renderer actually used, caches the HTML
// escapes of that renderer and makes them with replaceAll, joins the chunks it writes, puts Set
// checks in front of its long switches on strings, passes the errors renderToString ignores to the
// runtime, keeps the renderer from building component stacks and from putting the names of
// components into their key paths, gives the importers of React namespaces of plain values,
// switches core-js to its property-backed internal state, checks that the bundle is one file, gives
// the imports of Node modules stand-ins that throw when used, writes the calls of jsx of React that
// it knows as the objects they return, and warns of them and of the places
// where it may load a module at run time. Its source map names sources
// relative to the application root. It comes with serovalPlugin, which changes seroval. With
// settings.i18nextCache, build.i18next_cache, it also adds a cache to i18next, see i18nextCache.
export function ssrPlugin(getRoot, loadVite, settings = {}) {
  const reactTargets = new Map() // server.edge.js path to the renderer modules it imports
  let coreJS // real directory of the core-js package the application resolves, '' for none
  let coreJSState = false // whether the internal state module of coreJS is in the bundle
  let coreJSAdapted = false // whether it got the property-backed state
  let namesDropped = false // whether React's key paths have no names of components
  let keyPathsDropped = false // whether React makes key paths only when a module has $$FORM_ACTION
  let serverActions // the modules outside React DOM with $$FORM_ACTION in their code
  let lucide // folder of lucide-react's ES modules to whether the plugin knows its files
  let jsxRuntimes // the files of React's JSX runtime in the bundle to whether the plugin knows them
  const plugin = {
    name: 'tifa:ssr',
    applyToEnvironment: (env) => env.name === 'ssr',
    buildStart() {
      coreJS = findPackage(getRoot(), 'core-js')
      coreJSState = coreJSAdapted = false
      namesDropped = false
      keyPathsDropped = false
      serverActions = new Set()
      lucide = new Map()
      jsxRuntimes = new Map()
    },
    outputOptions(options) {
      const root = getRoot()
      return {
        ...options,
        // The places in the source map, relative to the application root.
        sourcemapPathTransform: (source, map) => path.relative(root, path.resolve(path.dirname(map), source)),
      }
    },
    resolveId: {
      order: 'pre',
      async handler(source, importer, extra) {
        const fromDependency = importer?.includes(`${path.sep}node_modules${path.sep}`)
        // On Node, the helpers of rolldown import createRequire, which polyfillRequire: false
        // leaves unused. Without side effects, the import goes with it.
        if (source === 'node:module' && importer === rolldownRuntime) {
          return { id: source, external: true, moduleSideEffects: false }
        }
        if (source === nodeHelper || source === asyncHooksHelper) return { id: source, moduleSideEffects: false }
        if (source.startsWith('node:') || nodeBuiltins.has(source)) {
          if (!fromDependency) this.error(`Node APIs are not supported in page code: ${source}`)
          // A dependency may import a Node module it never uses on this platform: an import gets
          // a stand-in, see nodeModuleStub. A require or an import() stays as it is, and the
          // check of the output finds it.
          if (extra?.kind === 'import-statement') {
            const code = this.getModuleInfo(importer)?.code ?? fs.readFileSync(importer, 'utf8')
            if (asyncHooks.has(source) && [...importsOf(code, source, (code) => this.parse(code))].every((name) => serverHooks.has(name))) {
              return { id: asyncHooksHelper, moduleSideEffects: false }
            }
            return nodeModuleStub(source, importer)
          }
          return { id: source, external: true, moduleSideEffects: false }
        }
        if (clientSource.test(source)) return clientOnlyStub
        if (importer && reactTargets.get(importer)?.includes(source)) {
          // Each renderer module only initializes its own engine. An unused engine need not run
          // for the other's API.
          return { id: path.join(path.dirname(importer), source), moduleSideEffects: false }
        }
        if (
          coreJS &&
          source === '../internals/weak-map-basic-detection' &&
          importer === path.join(coreJS, 'internals', 'internal-state.js')
        ) {
          coreJSAdapted = true
          return propertyState
        }
        return null
      },
    },
    transform: {
      filter: { code: /\$\$FORM_ACTION/ },
      handler(_, id) {
        if (packageName(id) !== 'react-dom') serverActions.add(path.relative(getRoot(), id))
        return null
      },
    },
    load(id) {
      if (id === nodeHelper) return { code: nodeHelperCode, map: { mappings: '' } }
      if (id === asyncHooksHelper) return { code: asyncHooksCode, map: { mappings: '' } }
      if (id.startsWith(nodeStub)) return nodeModuleCode(id, (code) => this.parse(code), this.getModuleInfo(nodeStubImporter(id)))
      if (id === clientOnlyStub) {
        return 'export default function ClientOnlyStub(){ throw new Error("Client-only module invoked during SSR") }'
      }
      // Select core-js's existing property-backed internal state. Do not replace the global
      // WeakMap or patch generated output.
      if (id === propertyState) return 'module.exports = false'
      if (coreJS && id === path.join(coreJS, 'internals', 'internal-state.js')) coreJSState = true
      const lucideFile = lucideModule.exec(id)
      if (lucideFile) {
        const dir = path.dirname(id)
        if (!lucide.has(dir)) {
          const unknown = Object.keys(lucideFiles).find((file) => lucideFileHash(path.join(dir, file)) !== lucideFiles[file])
          if (unknown) {
            this.warn(
              `lucide-react ${packageVersion(path.resolve(dir, '..', '..'))}: ${path.join(dir, unknown)} has a form the tifa ` +
                'plugin does not know, so the tifa plugin does not cache the icons lucide-react builds. Pages render the ' +
                'same, but icons take longer to render',
            )
          }
          lucide.set(dir, !unknown)
        }
        if (!lucide.get(dir)) return null
        return lucideCache(lucideFile[1], fs.readFileSync(id, 'utf8'))
      }
      if (radixSlotModule.test(id)) {
        const source = fs.readFileSync(id, 'utf8')
        const merged = radixMergeProps(source)
        if (!merged && source.includes('function mergeProps(')) {
          this.warn(
            `@radix-ui/react-slot ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has no mergeProps the tifa ` +
              'plugin knows, so Slot tests a regexp for each prop it merges and builds class names with filter and ' +
              'join. Pages render the same, but take longer to render',
          )
        }
        return merged ?? null
      }
      if (reactI18nextModule.test(id)) {
        const source = fs.readFileSync(id, 'utf8')
        const copied = reactI18nextCopy(source)
        if (!copied && source.includes('createI18nWrapper')) {
          this.warn(
            `react-i18next ${packageVersion(path.resolve(id, '..', '..', '..'))}: ${id} has no createI18nWrapper the ` +
              'tifa plugin knows, so useTranslation copies the whole i18n object on every call. Pages render the ' +
              'same, but take longer to render',
          )
        }
        return copied ?? null
      }
      if (i18nextModule.test(id)) {
        const i18next = `i18next ${packageVersion(path.resolve(id, '..', '..', '..'))}: ${id}`
        const original = fs.readFileSync(id, 'utf8')
        const { source, unknown } = i18nextFaster(original)
        if (unknown.includes('fixedT')) {
          this.warn(
            `${i18next} has no fixedT the tifa plugin knows, so the functions getFixedT returns copy all ` +
              'options on every call. Pages render the same, but translations take longer',
          )
        }
        if (unknown.includes('looksLikeObjectPath')) {
          this.warn(
            `${i18next} has no looksLikeObjectPath the tifa plugin knows, so it builds its regexp on every ` +
              'call. Pages render the same, but translations take longer',
          )
        }
        const faster = unknown.length === 2 ? null : source
        if (!settings.i18nextCache) return faster
        const cached = i18nextCache(faster ?? original)
        if (cached === undefined) {
          this.warn(
            `${i18next} has a form the tifa plugin does not know, so the tifa plugin does not cache its translations ` +
              'as build.i18next_cache, on by default, asks. Pages render the same, but translations take longer. ' +
              'i18next 25.5 to 26.5 is cached, and build.i18next_cache: false in tifa.yaml builds without this warning',
          )
        }
        return cached ?? faster
      }
      if (emotionHash.test(id)) {
        const source = fs.readFileSync(id, 'utf8')
        if (source.split(murmur2Source).length !== 2) {
          this.warn(
            `@emotion/hash ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has no murmur2 the tifa plugin knows, ` +
              'so emotion hashes its styles in JavaScript. Pages render the same, but styles take longer to serialize',
          )
          return null
        }
        return source.replace(murmur2Source, () => hashInGo(murmur2Source))
      }
      if (reactJsxRuntime.test(id)) {
        const known = reviewedJsxRuntimes.has(createHash('sha256').update(fs.readFileSync(id)).digest('hex'))
        jsxRuntimes.set(id, known)
        if (!known) {
          this.warn(
            `react ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has a jsx the tifa plugin does not know, so ` +
              'the tifa plugin does not write the elements of JSX as objects in place of calls of jsx. Pages render ' +
              'the same, but take longer to render',
          )
        }
        return null
      }
      if (reactModule.test(id)) {
        const react = `react ${packageVersion(path.resolve(id, '..', '..'))}: ${id}`
        const { source, missing } = nativeCopyProps(fs.readFileSync(id, 'utf8'))
        for (const name of missing) {
          this.warn(
            `${react} has no ${name} in the form the tifa plugin knows, so React copies the config of each ` +
              `element it makes with ${name} one key at a time. Pages render the same, but those elements ` +
              'take longer to make',
          )
        }
        return missing.length === 2 ? null : source
      }
      if (reactStringRenderer.test(id)) {
        const react = `react-dom ${packageVersion(path.resolve(id, '..', '..'))}: ${id}`
        let source = fs.readFileSync(id, 'utf8')
        const reviewed = reviewedRenderers.has(createHash('sha256').update(source).digest('hex'))
        const cached = cacheEscapes(source)
        if (cached) {
          source = cached
        } else {
          this.warn(
            `${react} has no matchHtmlRegExp and escapeTextForBrowser in the form the tifa plugin knows, so ` +
              'the tifa plugin does not cache the escapes of the HTML React writes or ask the engine to escape ' +
              'them. Pages render the same, but texts and attribute values take longer to render',
          )
        }
        const chained = chainEscapes(source)
        if (chained) {
          source = chained
        } else {
          this.warn(
            `${react} has no loop over the characters of a text in escapeTextForBrowser in the form the tifa ` +
              'plugin knows, so the tifa plugin does not escape them with replaceAll. Pages render the same, but ' +
              'texts and attribute values with & < > " or \' in them that the cache of escapes does not hold take ' +
              'longer to render',
          )
        }
        const joined = joinSegmentChunks(source)
        if (joined) {
          source = joined
        } else {
          this.warn(
            `${react} has no loops over the chunks of a segment in flushSubtree in the form the tifa plugin ` +
              'knows, so the tifa plugin does not join the chunks of a segment before it writes them. Pages ' +
              'render the same, but take longer to render',
          )
        }
        const inlined = inlineAttributeCalls(source)
        if (inlined) {
          source = inlined
        } else {
          this.warn(
            `${react} has no pushStringAttribute, pushAttribute and isAttributeNameSafe in the form the tifa ` +
              'plugin knows, so the tifa plugin does not write the common attributes without calling the ' +
              'functions that write one string attribute and check an attribute name. Pages render the same, ' +
              'but take longer to render',
          )
        }
        // A syntax tree goes only with the source it was parsed from: each change below parses
        // the source the changes before it left.
        const parse = (code) => this.parse(code)
        const read = keyPathNamesRead(parse(source))
        const unnamed = !read && dropKeyPathNames(source)
        if (unnamed) {
          source = unnamed
          namesDropped = true
          const used = reviewed ? keyPathsUsed(parse(source)) : 'is not a file of React DOM whose key paths the tifa plugin has read'
          const unkeyed = !used && dropKeyPaths(source)
          if (unkeyed) {
            source = unkeyed
            keyPathsDropped = true
          } else {
            this.warn(
              `${react} ${used || 'has no key paths in the form the tifa plugin knows'}, so the tifa plugin does not ` +
                'leave out the key paths of the elements. Pages render the same, but take longer to render',
            )
          }
        } else {
          this.warn(
            `${react} ${read || `has no "${keyPathName.trim().split('\n')[0]}" the tifa plugin knows`}, ` +
              'so the tifa plugin does not leave the names of components out of their key paths. Pages render the ' +
              'same, but take longer to render',
          )
        }
        const guarded = guardSwitches(source, parse)
        if (guarded.switches.length > 0) {
          source = guarded.code
        } else {
          this.warn(
            `${react} has no switch the tifa plugin can put a Set check in front of, so a name that matches ` +
              'none of the cases of a switch is compared with each of them. Pages render the same, but take ' +
              'longer to render',
          )
        }
        // renderToString leaves a Suspense boundary whose content threw to the browser, and its
        // error handler ignores the error. Give renderToString and renderToStaticMarkup the
        // onError option of React's other renderers, for the runtime to report the error. Each
        // call reports to its own onError, so a render inside a render keeps its errors.
        if (source.split(ignoredErrors).length !== 2) {
          this.warn(
            `${react} has no "${ignoredErrors}" the tifa plugin knows, so a server render that fails inside ` +
              'a Suspense boundary cannot say why',
          )
          return source
        }
        // React gives that handler the component stack of the error, which the handler drops.
        const stacksRead = componentStacksRead(parse(source))
        const unstacked = !stacksRead && dropComponentStacks(source)
        if (unstacked) {
          source = unstacked
        } else {
          this.warn(
            `${react} ${stacksRead || 'has no pushComponentStack in the form the tifa plugin knows'}, so the tifa ` +
              'plugin does not keep React from building component stacks. Pages render the ' +
              'same, but take longer to render',
          )
        }
        return (
          source.replace(ignoredErrors, 'function onError(error) { tifaOnError?.(error) }') +
          `
var tifaOnError
for (const name of ['renderToString', 'renderToStaticMarkup']) {
  const render = exports[name]
  exports[name] = function (children, options) {
    const outer = tifaOnError
    tifaOnError = options?.onError
    try {
      return render(children, options)
    } finally {
      tifaOnError = outer
    }
  }
}
`
        )
      }
      if (reactServerEntry.test(id)) {
        const source = fs.readFileSync(id, 'utf8')
        const trimmed = reactServerExports(this.parse(source))
        if (!trimmed) {
          this.warn(
            `react-dom ${packageVersion(path.dirname(id))}: ${id} has a form the tifa plugin does not know, ` +
              'so the SSR bundle keeps both React server renderers. Pages render the same, but the bundle is ' +
              'larger and each VM takes longer to start',
          )
          return null
        }
        reactTargets.set(id, trimmed.targets)
        return trimmed.code
      }
      return null
    },
    buildEnd() {
      // React writes the key of the state of useActionState with a server action from the key path
      // of the component, see dropKeyPathNames.
      if (namesDropped && serverActions.size > 0) {
        this.warn(
          `${[...serverActions].join(', ')}: has $$FORM_ACTION in its code, the property of a server action. tifa ` +
            'has no server actions, and the tifa plugin leaves the names of components out of their key paths in ' +
            'React, so if the module makes such an action, the key React writes into a form for the state of ' +
            'useActionState with it differs from the one React writes without the plugin',
        )
      }
      // core-js keeps internal state in WeakMaps whose values point back to their keys, which
      // QuickJS never frees, so the VMs would grow with every render.
      if (coreJSState && !coreJSAdapted) {
        this.warn(
          `core-js ${packageVersion(coreJS)} at ${coreJS}: internals/internal-state.js no longer loads ` +
            'internals/weak-map-basic-detection, so the tifa plugin cannot switch core-js from WeakMap to ' +
            'property-backed state. QuickJS does not free WeakMap entries whose values point back to their ' +
            'keys, so the memory of each VM may grow',
        )
      }
    },
    writeBundle(options, bundle) {
      // ssr.js and ssr.js.map come from one build. The output folder is kept between dev
      // builds, so a map this build did not write is of other code.
      if (!bundle['ssr.js.map']) fs.rmSync(path.join(options.dir, 'ssr.js.map'), { force: true })
    },
    renderChunk(code, chunk) {
      const plain = plainNamespaces(code, chunk.moduleIds)
      if (plain.warning) this.warn(plain.warning)
      // Only with a JSX runtime of which every file is known: with another, the elements might differ.
      const elements =
        jsxRuntimes.size > 0 && [...jsxRuntimes.values()].every(Boolean)
          ? inlineElements(plain.code, (source) => this.parse(source))
          : { code: plain.code }
      // The code keeps the lines it had, so the source map fits it as it is.
      return elements.code === code ? null : { code: elements.code, map: null }
    },
    async generateBundle(_, bundle) {
      const chunks = Object.values(bundle).filter((output) => output.type === 'chunk')
      if (chunks.length !== 1) this.error(`the SSR bundle has ${chunks.length} files, want 1`)
      const [chunk] = chunks
      const imports = chunk.code.match(/^(?:import|export)\b.*\bfrom\s*(["'][^"']*["'])/m)
      if (imports) this.error(`the SSR bundle imports ${imports[1]}, which QuickJS cannot load`)
      const stubs = nodeModulesLeft(chunk)
      if (stubs.length > 0) {
        this.warn(
          'the SSR bundle uses Node modules, which QuickJS does not have: in their place it has stand-ins that throw ' +
            'when a render uses them. The list is a hint, a render may never reach the code that uses them:\n' +
            stubs.join('\n'),
        )
      }
      const loads = await hostLoads(chunk, await loadVite(), (code) => this.parse(code))
      if (loads.length > 0) {
        this.warn(
          'the SSR bundle may load modules at run time, which QuickJS cannot do: a server render that ' +
            'reaches one of these places fails. The list is a hint, it may name code no render reaches ' +
            `and miss some:\n${loads.join('\n')}`,
        )
      }
      // The minifier ran before this hook, and would undo the rewrite if it ran after it. The file
      // ssr.js.map is an asset the bundler made from the map of the chunk before this hook, so it
      // is changed here to fit. A map inline in ssr.js is not used.
      const parse = (code) => this.parse(code)
      const asset = bundle[`${chunk.fileName}.map`]
      const map = asset && JSON.parse(typeof asset.source === 'string' ? asset.source : Buffer.from(asset.source).toString())
      const rewritten = rewriteStatements(chunk.code, parse, map?.mappings)
      if (rewritten.count > 0) {
        // The rewrite must give code that parses: the build fails here, not in the server.
        parse(rewritten.code)
        chunk.code = rewritten.code
        if (map) {
          map.mappings = rewritten.mappings
          asset.source = JSON.stringify(map)
        }
      }
      // With a module that has $$FORM_ACTION in its code, React makes its key paths, see dropKeyPaths.
      // The global is set on a line of its own before all the code, since a module may render when
      // it runs. The map gets an empty line to fit.
      if (keyPathsDropped && serverActions.size > 0) {
        chunk.code = `globalThis.tifaFormActions = true;\n${chunk.code}`
        parse(chunk.code)
        if (map) {
          map.mappings = `;${map.mappings}`
          asset.source = JSON.stringify(map)
        }
      }
    },
  }
  return [plugin, serovalPlugin(), prerenderPlugin()]
}

// rolldownRuntime is the ID of the module of rolldown's helpers.
const rolldownRuntime = '\0rolldown/runtime.js'

// nodeStub starts the IDs of the stand-ins of Node modules, see nodeModuleStub, and nodeHelper is
// the ID of the module they share.
const nodeStub = '\0tifa-node:'
const nodeHelper = '\0tifa-node'

// nodeModuleStub resolves an import of the Node module source by importer, a file of a dependency,
// to a stand-in module of its own: QuickJS has no Node modules. Each export of the stand-in throws
// when it is used: called, constructed, or its properties read or set. Merely importing a Node
// module, as a package does that uses it only on Node or only in code a render never reaches, does
// no harm, and a module the bundle runs only on an import() runs only then. The stand-in has no
// side effects, so the exports no code uses leave the bundle.
function nodeModuleStub(source, importer) {
  return { id: nodeStub + JSON.stringify([source, importer]), moduleSideEffects: false }
}

function nodeStubImporter(id) {
  return JSON.parse(id.slice(nodeStub.length))[1]
}

// nodeModuleCode returns the stand-in with the ID id, whose module importer imports as info, its
// module info, gives it, and which parse parses. It exports what Node exports from the module,
// asked of the Node that runs the build, and every other name importer imports from it, such as an
// API of a newer Node: rolldown fails on an import of a name a module does not export.
function nodeModuleCode(id, parse, info) {
  const [source, importer] = JSON.parse(id.slice(nodeStub.length))
  const names = new Set(Object.keys(nodeModuleExports(source)))
  for (const name of importsOf(info?.code ?? fs.readFileSync(importer, 'utf8'), source, parse)) names.add(name)
  names.delete('default')
  names.delete('*')
  const args = (name) => [source, packageName(importer), name].map((v) => JSON.stringify(v)).join(', ')
  let out = `import { nodeExport } from ${JSON.stringify(nodeHelper)}\n`
  if (asyncHooks.has(source)) {
    // The server has these, see asyncHooksCode. The other exports are stand-ins.
    out += `export { ${[...serverHooks].join(', ')} } from ${JSON.stringify(asyncHooksHelper)}\n`
    for (const name of serverHooks) names.delete(name)
  }
  let i = 0
  for (const name of names) {
    out += `const e${i} = /* @__PURE__ */ nodeExport(${args(name)})\nexport { e${i++} as ${JSON.stringify(name)} }\n`
  }
  out += `export default /* @__PURE__ */ nodeExport(${args('')})\n`
  // The stand-in comes from no source file: the places in it map to none.
  return { code: out, map: { mappings: '' } }
}

// asyncHooks are the names of node:async_hooks. Its exports serverHooks, AsyncLocalStorage and
// AsyncResource, are those of the server, which the tifa host makes (js/host/async-local-storage.ts)
// and asyncHooksHelper exports. An import of node:async_hooks that asks for nothing else gets
// asyncHooksHelper itself, and the build does not warn of it. The stand-in that any other import
// gets exports them too.
const asyncHooks = new Set(['node:async_hooks', 'async_hooks'])
const serverHooks = new Set(['AsyncLocalStorage', 'AsyncResource'])
const asyncHooksHelper = '\0tifa-node-async-hooks'
const asyncHooksCode = `const { AsyncLocalStorage, AsyncResource } = globalThis.__tifa_async_hooks__
export { AsyncLocalStorage, AsyncResource }
`

// importsOf returns the names code, parsed by parse, imports or re-exports from the module source,
// and * for a default or namespace import of it or an export * from it.
function importsOf(code, source, parse) {
  const names = new Set()
  for (const node of parse(code).body) {
    if (node.source?.value !== source) continue
    if (node.type === 'ExportAllDeclaration') names.add('*')
    if (node.type !== 'ImportDeclaration' && node.type !== 'ExportNamedDeclaration') continue
    for (const s of node.specifiers) {
      const name = node.type === 'ImportDeclaration' ? s.imported : s.local
      names.add(name ? (name.type === 'Identifier' ? name.name : name.value) : '*')
    }
  }
  return names
}

// nodeModuleExports returns the module object of the Node module source, of the Node that runs the
// build. Its own properties are the names the ES module exports.
function nodeModuleExports(source) {
  try {
    return process.getBuiltinModule?.(source) ?? createRequire(import.meta.url)(source)
  } catch {
    return {}
  }
}

// nodeHelperCode makes the exports of the stand-ins: a function whose every operation throws an
// error that says which export was used, of which Node module, imported by which package. The
// server's explanation of the error ends in the same words as the message (see internal/ssr).
const nodeHelperCode = `const traps = ['apply', 'construct', 'defineProperty', 'deleteProperty', 'get', 'getOwnPropertyDescriptor',
  'getPrototypeOf', 'has', 'isExtensible', 'ownKeys', 'preventExtensions', 'set', 'setPrototypeOf']
const named = new Set(['defineProperty', 'deleteProperty', 'get', 'getOwnPropertyDescriptor', 'has', 'set'])
function nodeModuleUsed(module, importers, name, trap, key) {
  const what = named.has(trap) && typeof key === 'string' ? (name ? name + '.' + key : key) : name || 'the default export'
  return new Error(what + ' of the Node module "' + module + '", imported by ' + importers + ', was used on the server, which has no Node modules')
}
export function nodeExport(module, importers, name) {
  const handler = {}
  for (const trap of traps) handler[trap] = (target, key) => { throw nodeModuleUsed(module, importers, name, trap, key) }
  return new Proxy(function () {}, handler)
}
`

// nodeModulesLeft returns the Node modules whose stand-ins are in chunk, one line for each, with the
// packages that import it.
function nodeModulesLeft(chunk) {
  const importers = new Map()
  for (const [id, module] of Object.entries(chunk.modules)) {
    if (!id.startsWith(nodeStub) || module.renderedLength === 0) continue
    const [source, importer] = JSON.parse(id.slice(nodeStub.length))
    if (!importers.has(source)) importers.set(source, new Set())
    importers.get(source).add(packageName(importer))
  }
  return [...importers].map(([source, names]) => `${JSON.stringify(source)}, imported by ${[...names].join(', ')}`)
}

// renderToStringImpl is the function of React DOM's string renderer that renderToString and
// renderToStaticMarkup call, as prerenderToString knows it.
const renderToStringImpl = `function renderToStringImpl(
  children,
  options,
  generateStaticMarkup,
  abortReason
) {
  var didFatal = !1,
    fatalError = null,
    result = "",
    readyToStream = !1;
  options = createResumableState(options ? options.identifierPrefix : void 0);
  children = createRequest(
    children,
    options,
    createRenderState(options, generateStaticMarkup),
    createFormatContext(0, null, 0, null),
    Infinity,
    onError,
    void 0,
    void 0,
    function () {
      readyToStream = !0;
    },
    void 0,
    void 0,
    void 0
  );
  children.flushScheduled = null !== children.destination;
  performWork(children);
  10 === children.status && (children.status = 11);
  null === children.trackedPostpones &&
    safelyEmitEarlyPreloads(children, 0 === children.pendingRootTasks);
  abort(children, abortReason);
  startFlowing(children, {
    push: function (chunk) {
      null !== chunk && (result += chunk);
      return !0;
    },
    destroy: function (error) {
      didFatal = !0;
      fatalError = error;
    }
  });
  if (didFatal && fatalError !== abortReason) throw fatalError;
  if (!readyToStream) throw Error(formatProdErrorMessage(426));
  return result;
}
`
// prerenderToStringSource is the function prerenderToString adds to the string renderer, next to
// renderToStringImpl, whose helpers it calls. It is what renderToStringImpl does, but it flushes the
// HTML once no task is left: a component that suspended, such as a React.lazy one, pings its task
// when its promise settles, and the string renderer renders the task again at once. The last task
// to finish flushes in a job of its own, outside the work loop that ran it. The signal of
// the options ends the render as renderToString ends it, with its reason, so the boundaries still
// waiting are left to the browser.
const prerenderToStringSource = `
function tifaPrerenderToString(children, options) {
  var abortReason =
    'The server used "renderToString" which does not support Suspense. If you intended for this Suspense boundary to render the fallback content on the server consider throwing an Error somewhere within the Suspense boundary. If you intended to have the server wait for the suspended component please switch to "renderToReadableStream" which supports Suspense on the server';
  return new Promise(function (resolve, reject) {
    var started = !1,
      allReady = !1,
      done = !1,
      readyToStream = !1,
      state = createResumableState(options ? options.identifierPrefix : void 0),
      request = createRequest(
        children,
        state,
        createRenderState(state, !1),
        createFormatContext(0, null, 0, null),
        Infinity,
        options && options.onError ? options.onError : onError,
        void 0,
        function () {
          allReady = !0;
          started && Promise.resolve().then(finish);
        },
        function () {
          readyToStream = !0;
        },
        void 0,
        void 0,
        void 0
      );
    function finish() {
      if (done) return;
      done = !0;
      var didFatal = !1,
        fatalError = null,
        result = "";
      abort(request, abortReason);
      startFlowing(request, {
        push: function (chunk) {
          null !== chunk && (result += chunk);
          return !0;
        },
        destroy: function (error) {
          didFatal = !0;
          fatalError = error;
        }
      });
      if (didFatal && fatalError !== abortReason) reject(fatalError);
      else if (!readyToStream) reject(Error(formatProdErrorMessage(426)));
      else resolve(result);
    }
    var signal = options && options.signal;
    signal && signal.addEventListener("abort", finish);
    request.flushScheduled = null !== request.destination;
    performWork(request);
    10 === request.status && (request.status = 11);
    null === request.trackedPostpones &&
      safelyEmitEarlyPreloads(request, 0 === request.pendingRootTasks);
    started = !0;
    (allReady || (signal && signal.aborted)) && finish();
  });
}
exports.prerenderToString = tifaPrerenderToString;
`

// prerenderPlugin applies to the SSR build only. It adds prerenderToString to React DOM's string
// renderer and its server entry: it renders as renderToString does and waits for the components
// that suspended, see prerenderToStringSource. The runtime renders pages with it. ssrPlugin
// includes it, after the changes it makes to the string renderer, and trims the server entry to
// an ES module that exports it. A server entry that keeps its CommonJS form gets the export here.
export function prerenderPlugin() {
  return {
    name: 'tifa:prerender',
    applyToEnvironment: (env) => env.name === 'ssr',
    transform: {
      filter: { id: [reactStringRenderer, reactServerEntry] },
      handler(code, id) {
        if (reactServerEntry.test(id)) {
          const string = /^exports\.renderToString = (\w+)\.renderToString;$/m.exec(code)
          return string ? `${code}\nexports.prerenderToString = ${string[1]}.prerenderToString;\n` : null
        }
        const waiting = prerenderToString(code)
        if (waiting) return { code: waiting, map: null }
        this.warn(
          `react-dom ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has no renderToStringImpl the tifa plugin ` +
            'knows, so the server does not wait for a component that suspends, such as a React.lazy one: a page with ' +
            'one fails to render on the server',
        )
        return { code: code + prerenderNotWaiting, map: null }
      },
    },
  }
}

// prerenderToString returns source, React DOM's string renderer, with prerenderToString, which
// renders as renderToString does and waits for the components that suspended (see
// prerenderToStringSource). It returns undefined when the renderer has no renderToStringImpl the
// plugin knows.
function prerenderToString(source) {
  if (source.split(renderToStringImpl).length !== 2) return undefined
  return source + prerenderToStringSource
}

// prerenderNotWaiting stands in for prerenderToString in a string renderer the plugin cannot add it
// to: it renders with renderToString, which does not wait.
const prerenderNotWaiting = `
exports.prerenderToString = function (children, options) {
  return new Promise(function (resolve) {
    resolve(exports.renderToString(children, options));
  });
};
`

// serovalPlugin applies to the SSR build only. It changes seroval, which TanStack Router writes
// the data of a page with, so that it escapes short strings with JSON.stringify and asks its
// plugins for arrays as it does for objects.
export function serovalPlugin() {
  let seroval // directory of the seroval package the bundle loads a module of, '' for none
  let serovalMain = false // whether the bundle loads a main file of seroval
  return {
    name: 'tifa:seroval',
    applyToEnvironment: (env) => env.name === 'ssr',
    buildStart() {
      seroval = ''
      serovalMain = false
    },
    load(id) {
      seroval ||= id.match(serovalPackage)?.[1] ?? ''
      if (serovalModule.test(id)) {
        serovalMain = true
        const name = `seroval ${packageVersion(path.resolve(id, '..', '..'))}: ${id}`
        let source = fs.readFileSync(id, 'utf8')
        // seroval escapes a string shorter than that length one character at a time, which is
        // slower in QuickJS than JSON.stringify from 2 code units on. Both give the same text:
        // seroval takes JSON.stringify only for a string without the characters it escapes
        // otherwise.
        if (source.split(serovalLength).length === 2) {
          source = source.replace(serovalLength, 'const MIN_JSON_STRINGIFY_LENGTH = 2;')
        } else {
          this.warn(
            `${name} has no "${serovalLength}" the tifa plugin knows, so seroval escapes strings shorter than ` +
              '64 code units one character at a time. Pages render the same, but the data of small pages takes ' +
              'longer to write',
          )
        }
        // The data of a route that is an array goes through the tifa.data adapter as the JSON text
        // Go wrote only when seroval asks its plugins for arrays. Otherwise seroval writes each of
        // its values, which takes several times as long in QuickJS. The other plugins of a page,
        // TanStack Router's and the one of RouteError, take errors and streams, which no array is,
        // so they write the same as before.
        const arrays = askPluginsForArrays(source)
        if (arrays.missing.length === 0) {
          source = arrays.source
        } else {
          this.warn(
            `${name} has no ${arrays.missing.join(' and no ')} in the form the tifa plugin knows, so the tifa ` +
              'plugin changes neither, and seroval takes an array as an array before it asks its plugins. Pages ' +
              'render the same, but the data of a route that is an array takes longer to write',
          )
        }
        return source
      }
      return null
    },
    buildEnd() {
      // seroval moved the code the plugin changes to another file.
      if (seroval && !serovalMain) {
        this.warn(
          `seroval ${packageVersion(seroval)} at ${seroval}: the SSR bundle loads neither dist/index.js nor ` +
            'dist/index.cjs, so the tifa plugin does not change seroval, which escapes strings shorter than 64 ' +
            'code units one character at a time and takes an array as an array before it asks its plugins. Pages ' +
            'render the same, but the data of small pages and of routes that are arrays takes longer to write',
        )
      }
    },
  }
}

// askPluginsForArrays returns the source of seroval's main file with parseObject and
// parseObjectAsync asking the plugins for an array before they take it as an array, and the names
// of the functions the source does not have in the form it knows. The source is of use only when
// none is missing, so that seroval does not write arrays one way at once and another way async.
// Each line stays where it was.
function askPluginsForArrays(source) {
  const missing = []
  for (const [fn, check, forms] of serovalArrays) {
    const form = forms.find(([plugins]) => source.split(plugins).length === 2)
    if (source.split(check).length !== 2 || !form) missing.push(fn)
    else source = source.replace(check, () => form[1])
  }
  return { source, missing }
}

// nativeCopyProps returns the source of React's production file with createElement and cloneElement
// asking __tifa_copy_props before they loop over the keys of their config, and the names of the
// functions that the source does not have in the form it knows. A function changes only when its
// whole text is the one of propsCreate or propsClone, and it changes in one place, its loop, so a
// function is either as the plugin knows it and changed, or left as React has it. __tifa_copy_props
// is the loop written in C in the engine: it does what the loop does, for a config where nothing
// can make the two differ, and answers false, having changed nothing, for any other (see
// docs/build.md). The lines stay where they were.
function nativeCopyProps(source) {
  const missing = []
  let out = source
  for (const [name, text, loop, native] of [
    ['createElement', propsCreate, propsCreateLoop, propsCreateNative],
    ['cloneElement', propsClone, propsCloneLoop, propsCloneNative],
  ]) {
    if (out.split(text).length !== 2 || text.split(loop).length !== 2) {
      missing.push(name)
      continue
    }
    out = out.replace(text, () => text.replace(loop, () => sameLines(loop, native)))
  }
  if (missing.length === 2) return { source, missing }
  return { source: out + propsNative, missing }
}

// cacheEscapes returns the source of React's string renderer with escapeTextForBrowser asking the
// engine first, and with a cache in front of it when the VM has no engine escape, or null when the
// source does not have it in the form it knows. React escapes every text and attribute value it
// writes. __tifa_escape, which internal/ssr defines in each VM, is that function written in C in
// the engine and gives the same text for every argument (the tests compare it with React's function
// on every UTF-16 code unit, among other things). escapeTextForBrowser becomes the engine's
// function itself when the VM has it, so a call of it is one call, and the function below
// otherwise, which is how the bundle runs where the VM has none. That is a variable and not a
// function declaration, so the plugin changes the source only when its name does not come before
// the declaration, which would call it before it holds the function.
//
// The function below is the cache. A string with nothing to escape costs one search of the regular
// expression, but one with & < > " or ' goes through a loop over each character, which takes long
// in QuickJS. Such strings are mostly the same from render to render, like the class names of
// Tailwind CSS ([&_svg]:size-4, has-[>svg]:px-3), so the cache keeps their escapes, keyed by the
// string. It holds escapeCacheSize code units, and when the next string does not fit it starts over
// empty: texts of the data, which differ in each request, fill it, and the strings every render
// writes come back at the next render. A string larger than the whole cache is not kept. The cache
// lives in the module, so each VM has its own. It searches with exec, which takes less time in
// QuickJS than test: test looks up exec and calls it.
function cacheEscapes(source) {
  if (source.split(escapeSource).length !== 2) return null
  // The name's first place in the source is the declaration's.
  if (source.indexOf('escapeTextForBrowser') !== source.indexOf(escapeSource) + escapeSource.indexOf('escapeTextForBrowser')) return null
  const cached = `matchHtmlRegExp = /["'&<>]/;
var tifaEscapes = new Map(),
  tifaEscapesSize = 0;
function tifaCachedEscapeText(text) {
  if ("string" !== typeof text) return tifaEscapeTextForBrowser(text);
  if (null === matchHtmlRegExp.exec(text)) return text;
  var escaped = tifaEscapes.get(text);
  if (void 0 === escaped) {
    escaped = tifaEscapeTextForBrowser(text);
    var size = text.length + escaped.length + ${escapeEntrySize};
    if (size <= ${escapeCacheSize}) {
      tifaEscapesSize + size > ${escapeCacheSize} && (tifaEscapes.clear(), (tifaEscapesSize = 0));
      tifaEscapes.set(text, escaped);
      tifaEscapesSize += size;
    }
  }
  return escaped;
}
var escapeTextForBrowser = "function" === typeof __tifa_escape ? __tifa_escape : tifaCachedEscapeText;
function tifaEscapeTextForBrowser(text) {`
  return source.replace(escapeSource, () => sameLines(escapeSource, cached))
}

// hashInGo returns the source of murmur2, source, asking __tifa_murmur2__ first, which hashes a
// string in Go, the same as the loop of murmur2 does. It gives undefined for anything else, and
// then murmur2 hashes as before. The lines stay where they were.
function hashInGo(source) {
  const [first, ...rest] = source.split('\n')
  return [
    first +
      ' var tifaHash = "function" === typeof __tifa_murmur2__ ? __tifa_murmur2__(str) : void 0; if (void 0 !== tifaHash) return tifaHash;',
    ...rest,
  ].join('\n')
}

// lucideFileHash returns the start of the base64 SHA-256 of the text of a file of lucide-react, with
// the version taken out of its license comment, or '' when there is no such file.
function lucideFileHash(file) {
  let text
  try {
    text = fs.readFileSync(file, 'utf8')
  } catch {
    return ''
  }
  text = text.replace(/^ \* @license lucide-react v\S+ - ISC$/m, ' * @license lucide-react - ISC')
  return createHash('sha256').update(text).digest('base64').slice(0, 16)
}

// lucideCache returns the source of Icon.mjs of lucide-react with the cache of lucideCacheSource,
// or of createLucideIcon.mjs that adds the data of each icon it makes to the icons the cache may
// cache. Icon looks in the cache before it builds an icon, and puts what it built there. Each line
// stays where it was. Both files are those of lucideFiles.
function lucideCache(file, source) {
  if (file === 'createLucideIcon') {
    return source
      .replace("import Icon from './Icon.mjs';", "import Icon, { tifaLucideIcons } from './Icon.mjs';")
      .replace(lucideIconData, () => lucideIconData.replace(/\n$/, ' tifaLucideIcons.add(iconData);\n'))
  }
  const params = lucideParams.split('\n')
  params[0] +=
    ' const tifaColor = color ?? contextColor, tifaWidth = width ?? size ?? contextSize, tifaHeight = height ?? size ?? contextSize,' +
    ' tifaStrokeWidth = strokeWidth ?? contextStrokeWidth, tifaAbsolute = absoluteStrokeWidth ?? contextAbsoluteStrokeWidth,' +
    ' tifaNonScaling = nonScalingStroke ?? contextNonScalingStroke; const tifaHit = tifaFindIcon(icon, tifaColor, tifaWidth,' +
    ' tifaHeight, tifaStrokeWidth, tifaAbsolute, tifaNonScaling, contextClass, className, hasAccessibleProp, rest);'
  params[1] = '    const tifaBuilt = tifaHit !== void 0 ? tifaHit.built : buildLucideIconForReact(icon, {'
  params[2] = '      color: tifaColor,'
  params[3] = '      width: tifaWidth,'
  params[4] = '      height: tifaHeight,'
  params[5] = '      strokeWidth: tifaStrokeWidth,'
  params[6] = '      absoluteStrokeWidth: tifaAbsolute,'
  params[7] = '      nonScalingStroke: tifaNonScaling,'
  params[11] += ' const [name, svgAttributes, builtIconNode = []] = tifaBuilt;'
  return (
    source
      .replace(lucideParams, () => params.join('\n'))
      .replace(
        lucidePaths,
        () =>
          '        ...(tifaHit !== void 0 ? tifaHit.kids : tifaKeepIcon(tifaBuilt, builtIconNode.map(([tag, attrs]) => createElement(tag, attrs)))),\n',
      ) + lucideCacheSource
  )
}

// i18nextFaster returns the source of dist/esm/i18next.js or dist/cjs/i18next.js of i18next with
// the changes below, and the names of the functions it does not find in a form it knows and
// leaves as they are. Each line stays where it was. It knows the forms of i18next 23 to 26.5.
// i18next before 25.9 has no selectorOpts, which fixedT then does not need to leave out.
//
// fixedT, the function getFixedT returns, copies the options into selectorOpts only when a key or
// a key prefix that is a function asks for them, at the first place one does. Up to that place it
// reads only o.keyPrefix and this.options.keySeparator and calls no code of the page, so the copy
// holds what the copy at the old place held. i18next 26.3 and later also call Array.isArray for
// the copy, so fixedT copies at the old place while a page has replaced Array.isArray, or before
// the end of the file has set tifaIsArray. A page can only tell when an option of i18next or of a
// call is a getter, which then runs later or not at all, and i18next makes both objects itself,
// with plain values.
//
// looksLikeObjectPath keeps the regexp RegExpCache gave it for the last two separators, when both
// are strings, and takes it again for the same two: the chars, and so the pattern, are the same.
// RegExpCache only drops a pattern when it is asked for another, and that call keeps the new one or
// forgets the kept one, so the cache would still give the kept regexp, which is not global and
// keeps no state between tests. While a page has replaced a built-in that making the pattern calls
// (filter, map and join of arrays, includes or indexOf of strings, get of maps), it makes the
// pattern as before.
function i18nextFaster(source) {
  const unknown = []
  const scoped = '      if (Array.isArray(scopeNs) && !explicitCallNs) selectorOpts.ns = scopeNs;\n'
  const prefixed = "      if (typeof o.keyPrefix === 'function') o.keyPrefix = keysFromSelector(o.keyPrefix, selectorOpts);\n"
  const unscoped = i18nextFixedT.replace(scoped, '')
  const fixedT = [i18nextFixedT, unscoped, unscoped.replace(prefixed, '')].find((known) => source.split(known).length === 2)
  if (fixedT) {
    const scopes = fixedT.includes(scoped)
    const lines = fixedT.split('\n')
    lines[0] = '      let tifaSelectorOpts; const selectorOpts = () => {'
    lines[1] =
      '        if (tifaSelectorOpts === undefined) { tifaSelectorOpts = { ...this.options, ...o };' +
      (scopes ? ' if (Array.isArray(scopeNs) && !explicitCallNs) tifaSelectorOpts.ns = scopeNs;' : '') +
      ' }'
    lines[2] = '        return tifaSelectorOpts;'
    lines[3] = scopes ? '      }; if (Array.isArray !== tifaIsArray) selectorOpts();' : '      };'
    if (scopes) lines[4] = ''
    for (let i = 4; i < lines.length; i++) lines[i] = lines[i].replace(', selectorOpts)', ', selectorOpts())')
    source = source.replace(fixedT, () => lines.join('\n'))
    if (scopes) source += '\nvar tifaIsArray = Array.isArray;\n'
  } else if (source.includes('selectorOpts')) {
    unknown.push('fixedT')
  }
  const filters = {
    includes: 'chars.filter(c => !nsSeparator.includes(c) && !keySeparator.includes(c))',
    indexOf: 'chars.filter(c => nsSeparator.indexOf(c) < 0 && keySeparator.indexOf(c) < 0)',
  }
  const find = Object.keys(filters).find(
    (name) => source.split(i18nextObjectPath.replace(filters.includes, filters[name])).length === 2,
  )
  if (find) {
    const known = i18nextObjectPath.replace(filters.includes, filters[find])
    const lines = known.split('\n')
    const at = lines.indexOf("const chars = [' ', ',', '?', '!', ';'];")
    lines[at] +=
      ` const tifaArray = Array.prototype, tifaString = String.prototype, tifaFilter = tifaArray.filter, tifaMap = tifaArray.map, tifaJoin = tifaArray.join, tifaFind = tifaString.${find}, tifaMapGet = Map.prototype.get; let tifaPathNs, tifaPathKs, tifaPathRegExp;`
    lines[at + 5] =
      `  const tifaKept = tifaPathRegExp !== undefined && nsSeparator === tifaPathNs && keySeparator === tifaPathKs && tifaArray.filter === tifaFilter && tifaArray.map === tifaMap && tifaArray.join === tifaJoin && tifaString.${find} === tifaFind && Map.prototype.get === tifaMapGet;` +
      ` const possibleChars = tifaKept ? chars : ${filters[find]};`
    lines[at + 7] =
      '  const r = tifaKept ? tifaPathRegExp : looksLikeObjectPathRegExpCache.getRegExp(`(${possibleChars.map(c => c === \'?\' ? \'\\\\?\' : c).join(\'|\')})`);' +
      " if (!tifaKept) { const kept = typeof nsSeparator === 'string' && typeof keySeparator === 'string'; tifaPathNs = nsSeparator; tifaPathKs = keySeparator; tifaPathRegExp = kept ? r : undefined; }"
    source = source.replace(known, () => lines.join('\n'))
  } else {
    unknown.push('looksLikeObjectPath')
  }
  return { source, unknown }
}

// i18nextCache returns the source of i18next with a cache of the strings that the functions
// getFixedT returns give, for build.i18next_cache, or undefined when the source does not have the
// parts it changes in a form it knows. It knows the forms of i18next 25.5 to 26.5. Each line
// stays where it was.
//
// The cache is a Map for each instance, from a key of the call to the string the call gave, in a
// WeakMap, so the strings an instance kept go with it. The key holds the language, namespace and
// key prefix of the function, which an app may change on it, the language of the instance when
// the function has none, the formats object of an i18nFormat module such as i18next-icu, the
// defaultVariables of the instance, and the key and the options of the call, when each is a
// string, number, boolean, bigint, null, undefined or array of strings, and the options are a
// plain object. A call with another value, such as a function or an object, translates as before
// and keeps nothing. So does a call during which i18next logs, as it does for each missing key
// before it calls missingKeyHandler and emits missingKey, so the handler and the listeners hear of
// every missing key, and a call that runs code of the app, a formatter added with add or addCached
// or a post-processor, which may read something of the request. Only strings are kept, so a call
// with returnObjects or returnDetails translates again too. Instances made by cloneInstance,
// instances with a missingInterpolationHandler and instances with a formatter module of their own
// keep nothing.
//
// The cache forgets all it holds when anything an app does through i18next's API could change a
// translation it holds: the methods of the store, loadNamespaces, adding a formatter to the
// formatter of an instance it holds translations of, and adding a post-processor. A backend loads
// resources through the store too. The other methods of an instance that change its resources,
// namespaces or options make it forget what that instance kept. changeLanguage and cloneInstance keep it, since the language is part of the key
// and clones keep nothing. It holds at most about i18nextCacheSize bytes, counting two bytes a
// character and 80 for each entry, and starts over when the next string would not fit. A key of t
// whose calls keep missing stops using the cache, see i18nextCacheSource. It assumes that the app
// changes resources and options, other than defaultVariables, only through i18next's API, and that
// the other functions it gives i18next, such as overloadTranslationOptionHandler, give the same
// result for the same input. When an app breaks either, pages may show translations from before
// the change.
function i18nextCache(source) {
  const once = (text) => source.split(text).length === 2
  if (!once(i18nextCacheHead) || !once(i18nextCacheReturn) || !once(i18nextCacheInstance)) return undefined
  for (const [head, names] of Object.entries(i18nextCacheMethods)) {
    const start = source.indexOf(`\n${head}\n`)
    if (start < 0 || !once(`\n${head}\n`)) return undefined
    const body = source.slice(start, source.indexOf('\n}', start + 1))
    if (!names.every((name) => body.includes(`\n  ${name}(`))) return undefined
  }
  const order = ['\n  getFixedT(', i18nextCacheHead, i18nextCacheReturn, i18nextCacheInstance].map((text) => source.indexOf(text))
  if (order.some((at, i) => i > 0 && at < order[i - 1])) return undefined
  const added = i18nextCacheSource.trim().replace(/\n\s*/g, ' ')
  return source
    .replace(
      i18nextCacheHead,
      () =>
        i18nextCacheHead.slice(0, -1) +
        ' const tifaKey = tifaI18nFind(this, fixedT, keyPrefix, key, opts, rest); if (tifaKey === null) return tifaI18nFound; const tifaEffects = tifaI18nEffects;\n',
    )
    .replace(i18nextCacheReturn, () => '      return tifaKey === undefined ? this.t(resultKey, o) : tifaI18nKeep(this, tifaKey, tifaEffects, this.t(resultKey, o));\n    };\n')
    .replace(i18nextCacheInstance, () => `\n${added} const instance = I18n.createInstance();\n`)
}

// reactI18nextCopy returns the source of react-i18next's useTranslation with the copy of the i18n
// object made from a list of the object's keys, or undefined when the source does not have
// createI18nWrapper in the form it knows. The copy is the one the function made: a new object with
// the prototype of the i18n object, its own properties in the same order with the values they have
// now, and __original, which the hook compares to tell which object a copy is of. Each call still
// makes its own copy, and the hook around it, with its refs, memos and the check of the language,
// stays as it was.
//
// What the function did with Object.getOwnPropertyDescriptors and Object.create, a spread and
// Object.setPrototypeOf do when every own property of the object is a string key with a data
// property that is writable, enumerable and configurable. The first call for an object finds out
// whether it is so, and keeps its keys and prototype in a list, for 8 objects at most, oldest
// first out. Later calls make the copy only when the object is extensible, has that prototype and
// those keys in that order, and run the function as it was for any other state. So values that
// changed copy as they are, and keys that were added or deleted make the function run as before and
// make a new list. Objects with an accessor, a symbol, a hidden or locked property, a key
// __original, and no prototype or Object.prototype for one, never get a list, and
// no object that is not extensible is copied from its list, which is what freezing or sealing an
// object does to its properties. The list does not see a property of the object change its
// attributes or become an accessor while its keys stay and it is extensible, and an i18n object
// is not a Proxy.
function reactI18nextCopy(source) {
  if (source.split(i18nWrapperSource).length !== 2) return undefined
  const lines = i18nWrapperSource.split('\n')
  lines[0] += ' const tifaCopy = tifaWrapperCopy(original); if (tifaCopy !== undefined) return tifaCopy;'
  lines[1] += ' tifaWrapperKeep(original, descriptors);'
  return source.replace(i18nWrapperSource, () => lines.join('\n')) + i18nWrapperHelpers
}

// radixMergeProps returns the source of react-slot with mergeProps testing the name of a prop for a
// handler (on and a capital letter) with the codes of its first three characters, and joining the
// two class names with a template, or undefined when the source does not have mergeProps in a form
// it knows. The rest of the function stays as it is, so the props come out with the same keys in the
// same order and the same values, and the handlers of the Slot and its child still run child
// first. /^on[A-Z]/ on a string is true when its first two characters are "on" and the third is one
// of A to Z, and [a, b].filter(Boolean).join(" ") is the string of a and b joined with a space when
// both are truthy, the string of the one that is, and the empty string when neither is, where a
// template turns the value to a string the way join does, so an object gives its toString and a
// symbol throws. The check is of the whole function, so a release that changes any part of it,
// such as a new prop it merges, is left alone with a warning. Each change stays on its line. A
// page that replaces RegExp.prototype.test, Array.prototype.filter or join does not change what
// mergeProps does, as it did before.
function radixMergeProps(source) {
  const known = [radixMergePropsSource, radixMergePropsOldSource].find((text) => source.split(text).length === 2)
  if (!known) return undefined
  const merged = known
    .replace(radixHandlerTest, () => '    const isHandler = propName.length > 2 && propName.charCodeAt(0) === 111 && propName.charCodeAt(1) === 110 && (propName.charCodeAt(2) - 65 >>> 0) < 26;\n')
    .replace(
      radixClassJoin,
      () => '      overrideProps[propName] = slotPropValue ? (childPropValue ? `${slotPropValue} ${childPropValue}` : `${slotPropValue}`) : childPropValue ? `${childPropValue}` : "";\n',
    )
  return source.replace(known, () => merged)
}

// chainEscapes returns the source of React's string renderer with escapeTextForBrowser escaping
// each of the five characters it escapes with one replaceAll, or null when the source does not
// have its loop over the characters in the form it knows. The loop handles one character at a
// time, which takes long in QuickJS, and replaceAll runs in the engine. Both give the same text:
// each character becomes its entity, & first, so the & of the other entities stays as it is, and
// no entity has any of the other four characters in it. A string without them comes back as it
// is, without a call of replaceAll.
function chainEscapes(source) {
  if (source.split(escapeLoop).length !== 2) return null
  const chained = `  null !== matchHtmlRegExp.exec(text) &&
    (text = text
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#x27;"));
  return text;`
  return source.replace(escapeLoop, () => sameLines(escapeLoop, chained))
}

// inlineAttributeCalls returns the source of React's string renderer with pushAttribute writing the
// attributes that go through pushStringAttribute without calling it, and checking the name of an
// attribute that isAttributeNameSafe has seen without calling that, or null when the source does not
// have these functions in the form it knows. pushAttribute writes every attribute of every element,
// and a call in QuickJS costs about as much as the work of the short function it makes. For a name
// that is a string, such as "class" for className, the chunk before the value is one string, ' class="',
// where pushStringAttribute pushed " ", the name and '="'. The chunks are pushed one by one as before,
// so there is no more joining of strings, which costs more than it saves. The text is the same:
// the inlined test is the body of pushStringAttribute, and isAttributeNameSafe starts with the lookup
// that is inlined. Each change stays on the lines it was on.
//
// React asks the type of the value of an attribute up to three times, whether it is a function, a
// symbol or a boolean, which it does not write. A string is the value of almost every attribute, so
// each of these tests asks first whether the value is a string, and writes it if it is, with one
// question where React asks three. A value of another type is asked the three questions after that
// one, and is written whatever its type if it is none of the three. The tests keep the type in no
// variable: one more local in pushAttribute costs every call of it about what the questions save.
function inlineAttributeCalls(source) {
  const calls = source.match(stringAttributeCall)?.length
  const pushes = source.match(literalAttributePush)?.length
  if (
    source.split(stringAttributeSource).length !== 2 ||
    source.split(sharedStringCase).length !== 2 ||
    source.split(attributeNameTest).length !== 2 ||
    source.split(attributeNameCheck).length !== 2 ||
    source.split(urlTypeTest).length !== 2 ||
    source.split(xlinkTypeTest).length !== 2 ||
    source.split(typeSwitch).length !== 2 ||
    calls !== 11 ||
    pushes !== 1
  ) {
    return null
  }
  const written = (name) => `("string" === typeof value || "function" !== typeof value && "symbol" !== typeof value && "boolean" !== typeof value) && target.push(' ${name.slice(1, -1)}="', escapeTextForBrowser(value), '"');`
  return source
    .replace(urlTypeTest, () => sameLines(urlTypeTest, `      if (
        null == value ||
        ("string" !== typeof value && ("function" === typeof value || "symbol" === typeof value || "boolean" === typeof value))
      )
        break;`))
    .replace(xlinkTypeTest, () => sameLines(xlinkTypeTest, `    case "xlinkHref":
      if ("string" !== typeof value && ("function" === typeof value || "symbol" === typeof value || "boolean" === typeof value))
        break;`))
    .replace(typeSwitch, () => sameLines(typeSwitch, `          if ("string" !== typeof value) switch (typeof value) {
            case "function":
            case "symbol":
              return;
            case "boolean":`))
    .replace(literalAttributePush, (all, name) => `target.push(' ${name.slice(1, -1)}="', escapeTextForBrowser(value), '"')`)
    .replace(stringAttributeCall, (all, name) => written(name))
    .replace(sharedStringCase, () => sharedStringCase.replace('pushStringAttribute(target, name, value);', `("string" === typeof value || "function" !== typeof value && "symbol" !== typeof value && "boolean" !== typeof value) && target.push(" ", name, '="', escapeTextForBrowser(value), '"');`))
    .replace(attributeNameCheck, () => 'if (((name = aliases.get(name) || name), hasOwnProperty.call(validatedAttributeNameCache, name) || isAttributeNameSafe(name))) {')
}

// dropComponentStacks returns the source of React's string renderer without the component stacks
// it builds, or null when the source does not have the code that builds them in the form it
// knows. React puts each element it renders on the component stack of its task, an object for
// each, which takes long in QuickJS. In the string renderer the stack only goes into the second
// argument of the error handler of renderToString, as errorInfo.componentStack, and the tifa
// plugin gives renderToString a handler that passes on only the error. React needs the stack only
// for that, so it can stop building it: pushComponentStack does nothing, and renderNodeDestructive
// no longer saves and restores the stack around each node. The call of retryNode stays on its
// line, for the stack traces of render errors. The plugin changes the source only when
// componentStacksRead finds no other readers of the stacks.
function dropComponentStacks(source) {
  if (source.split(stackPush).length !== 2 || source.split(stackAroundNode).length !== 2) return null
  return source
    .replace(stackPush, () => sameLines(stackPush, 'function pushComponentStack() {}'))
    .replace(stackAroundNode, () => '\n\n      retryNode(request, task)\n    );')
}

// componentStacksRead returns why program, React's string renderer, may read component stacks
// otherwise than the tifa plugin knows, or '' when it reads them only as stackReaders has it.
function componentStacksRead(program) {
  let calls = 0
  let reads = 0
  visitNodes(program, (node, parent, key) => {
    if (node.type === 'CallExpression' && node.callee.type === 'Identifier' && node.callee.name === 'getThrownInfo') calls++
    if (named(node, 'componentStack') && !(parent.type === 'AssignmentExpression' && key === 'left')) reads++
  })
  return `${calls} ${reads}` === stackReaders
    ? ''
    : `calls getThrownInfo ${calls} times and reads componentStack ${reads} times, which the tifa plugin does not know`
}

// dropKeyPathNames returns the source of React's string renderer without the names of components
// in their key paths, or null when the source does not take the name in the form it knows. For
// each element it renders, React gets the name of its component, which takes long in QuickJS, and
// puts it into the key path of the element. Of the code that reads the names, only one place runs
// in renderToString: useActionState hashes the key path into the key of its state, for an action
// with $$FORM_ACTION, which only a server action has. The others resume or prerender a page. tifa
// has no server actions, and the plugin warns of each module that sets $$FORM_ACTION. The plugin
// leaves the names out only when keyPathNamesRead finds no form state and no other readers of the
// names.
function dropKeyPathNames(source) {
  if (source.split(keyPathName).length !== 2) return null
  return source.replace(keyPathName, () => '            name = null,\n            keyOrIndex =')
}

// dropKeyPaths returns the source of React's string renderer, without the names of components in
// its key paths, that makes no key path unless tifaKeyPaths is true, or null when the source does
// not make its key paths in the form the plugin knows. React makes an array for each element it
// renders, for each place of the children of an array and for the fallback of each Suspense
// boundary. Of the code that reads them only two places run in renderToString: the hash in
// useActionState for an action with $$FORM_ACTION, and the key path of the fallback of a
// boundary. keyPathsUsed finds no other. So tifaKeyPaths is true only when the bundle has a module
// with $$FORM_ACTION in its code (the plugin sets globalThis.tifaFormActions then, see
// generateBundle), and the hash is the same as it is with every key path. Otherwise a key path is
// null, which the fallback of a boundary passes on and the other places only hand on.
function dropKeyPaths(source) {
  if (keyPathMakers.some(([from]) => source.split(from).length !== 2)) return null
  for (const [from, to] of keyPathMakers) source = source.replace(from, () => to)
  return `${source}\nvar tifaKeyPaths = false;\n`
}

// keyPathsUsed returns why program, React's string renderer, may use its key paths otherwise than
// in the places dropKeyPaths knows, or '' when it does not in renderToString: it uses them only as
// keyPathUses has it.
function keyPathsUsed(program) {
  let ends = 0
  let tracked = 0
  let replays = 0
  visitNodes(program, (node) => {
    if (
      node.type === 'MemberExpression' && node.computed && node.property.type === 'Literal' &&
      (node.property.value === 0 || node.property.value === 2) && node.object.type === 'Identifier' &&
      /(^k|K)eyPath$/.test(node.object.name)
    ) {
      ends++
    }
    if (
      node.type === 'AssignmentExpression' && named(node.left, 'trackedPostpones') &&
      !(node.right.type === 'Literal' && node.right.value === null)
    ) {
      tracked++
    }
    if (node.type === 'CallExpression' && node.callee.type === 'Identifier' && node.callee.name === 'createReplayTask') replays++
  })
  return `${ends} ${tracked} ${replays}` === keyPathUses
    ? ''
    : `reads [0] or [2] of a key path ${ends} times, sets trackedPostpones to something but null ${tracked} times and calls createReplayTask ${replays} times, which the tifa plugin does not know`
}

// keyPathNamesRead returns why program, React's string renderer, may read the names of components
// in their key paths, or '' when it does not in renderToString. It reads them only as
// keyPathReaders has it, and creates each request without a form state: RequestInstance and
// createRequest have a parameter formState, each new RequestInstance passes nothing or void 0 for
// it, or the formState of createRequest, and each call of createRequest passes nothing or void 0.
function keyPathNamesRead(program) {
  const params = {}
  const calls = { RequestInstance: [], createRequest: [] }
  let hashes = 0
  let names = 0
  const functions = [] // the names of the functions around the node, the innermost last
  const visit = (node) => {
    if (node.type === 'FunctionDeclaration' && node.id && node.id.name in calls) {
      params[node.id.name] = node.params.findIndex((param) => param.type === 'Identifier' && param.name === 'formState')
    }
    const callee = (node.type === 'NewExpression' || node.type === 'CallExpression') && node.callee.type === 'Identifier' && node.callee.name
    if (callee === 'RequestInstance' && node.type === 'NewExpression' || callee === 'createRequest' && node.type === 'CallExpression') {
      calls[callee].push({ node, inside: functions.at(-1) })
    }
    if (
      node.type === 'CallExpression' && named(node.callee, 'stringify') && node.callee.object.type === 'Identifier' &&
      node.callee.object.name === 'JSON' && node.arguments[0]?.type === 'ArrayExpression'
    ) {
      hashes++
    }
    if (
      node.type === 'MemberExpression' && node.computed && node.property.type === 'Literal' && node.property.value === 1 &&
      node.object.type === 'Identifier' && /(^k|K)eyPath$/.test(node.object.name)
    ) {
      names++
    }
    const fn = functionTypes.has(node.type)
    if (fn) functions.push(node.id?.name)
    for (const value of Object.values(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child)
      }
    }
    if (fn) functions.pop()
  }
  visit(program)
  const nothing = (arg) => arg === undefined || (arg.type === 'UnaryExpression' && arg.operator === 'void' && arg.argument.type === 'Literal')
  const passes = (name, ok) =>
    params[name] >= 0 &&
    calls[name].length > 0 &&
    calls[name].every(({ node, inside }) => node.arguments.every((arg) => arg.type !== 'SpreadElement') && ok(node.arguments[params[name]], inside))
  const fromCreateRequest = (arg, inside) => inside === 'createRequest' && arg.type === 'Identifier' && arg.name === 'formState'
  if (!passes('createRequest', nothing) || !passes('RequestInstance', (arg, inside) => nothing(arg) || fromCreateRequest(arg, inside))) {
    return 'may create a request with a form state'
  }
  return `${hashes} ${names}` === keyPathReaders
    ? ''
    : `calls JSON.stringify with an array ${hashes} times and reads [1] of a key path ${names} times, which the tifa plugin does not know`
}

// visitNodes calls visit with each node of the syntax tree node, its parent and the key of the
// parent that holds it.
function visitNodes(node, visit, parent = null, key = null) {
  if (parent) visit(node, parent, key)
  for (const [k, value] of Object.entries(node)) {
    for (const child of Array.isArray(value) ? value : [value]) {
      if (child && typeof child.type === 'string') visitNodes(child, visit, node, k)
    }
  }
}

// named reports whether node reads the property name of an object with a dot.
function named(node, name) {
  return node.type === 'MemberExpression' && !node.computed && node.property.type === 'Identifier' && node.property.name === name
}

// sameLines returns to, which replaces from, with as many lines as from, so the lines after it
// stay where they were: the places in React DOM that the stack traces of render errors give are
// those of the code the tifa plugin loads for it. A shorter to gets empty lines in front, so its
// last lines stay where the last lines of from were. Lines that to has beyond those of from go on
// its last line, which only works for code without line comments that ends each statement with a
// semicolon.
function sameLines(from, to) {
  const want = from.split('\n').length
  const lines = to.split('\n')
  if (lines.length > want) lines.splice(want - 1, Infinity, lines.slice(want - 1).map((line) => line.trim()).join(' '))
  return '\n'.repeat(want - lines.length) + lines.join('\n')
}

// joinSegmentChunks returns the source of React's string renderer with flushSubtree handing the
// chunks of a segment between two of its children to the destination at once, joined, or null
// when the source does not have that code in the form it knows. A page has thousands of chunks,
// and each call of the destination's push, which appends the chunk to the HTML, takes long in
// QuickJS. The renderer gives that function only the destination of renderToString, which skips a
// null chunk and always returns true, and its chunks are strings. join writes a null as an empty
// string, so the HTML stays the same.
function joinSegmentChunks(source) {
  if (source.split(segmentLoops).length !== 2) return null
  const joined = `      for (var childIdx = 0; childIdx < segment.length; childIdx++) {
        r = segment[childIdx];
        chunkIdx < r.index &&
          (destination.push(chunks.slice(chunkIdx, r.index).join("")),
          (chunkIdx = r.index));
        r = flushSegment(request, destination, r, hoistableState);
      }
      chunkIdx < chunks.length &&
        (r = destination.push(
          0 === chunkIdx ? chunks.join("") : chunks.slice(chunkIdx).join("")
        ));`
  return source.replace(segmentLoops, () => sameLines(segmentLoops, joined))
}

// guardSwitches puts a check of a Set in front of each switch of source, which it parses with
// parse, that compares a name with string cases only, at least guardedCases of them. QuickJS
// compares the name with one case after the other, so a name that matches none, such as most
// attributes and tags in React's string renderer, costs a comparison for each case. With the
// check it goes to the default clause at once:
//
//	{ if (tifaSwitchCases0.has(x)) switch (x) { cases } else switch (0) { default clause } }
//
// and without a default clause the else is left out. It returns the new source, and the switches
// it changed as {name, cases}, the name of the switch and the values of its cases. It changes a
// switch only where that does the same:
//
//   - The switch compares a parameter, a var or a function declared at the top of a function
//     around it, which reads the same twice. A global might be a property with a getter.
//   - Every case is a string literal. A switch compares with ===, Set.has with SameValueZero,
//     which are the same for a string, and no other value matches either.
//   - The default clause, if any, is the last, after a clause that ends in break, return, throw or
//     continue, so no case falls through into it. A break in it leaves the switch (0).
//   - No clause declares let, const, class or a function, whose scope is the whole switch.
//   - Inside a switch it changes, it leaves the switches as they are.
//
// The block keeps an if without else from taking the else of an if around the switch. The Sets are
// declared after the hashbang and the directives of source, so they exist before any of its code
// runs. Each line of source stays where it was. It changes nothing in a source that already names
// tifaSwitchCases.
export function guardSwitches(source, parse) {
  if (source.includes(switchCases)) return { code: source, switches: [] }
  const program = parse(source)
  const found = []
  const scopes = [] // the names each function around the node declares, the innermost last
  const visit = (node) => {
    const scope = functionTypes.has(node.type)
    if (scope) scopes.push(declaredNames(node))
    if (node.type === 'SwitchStatement' && guardable(node) && scopes.some((names) => names.has(node.discriminant.name))) {
      found.push(node)
    }
    for (const value of Object.values(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child)
      }
    }
    if (scope) scopes.pop()
  }
  visit(program)
  // The outermost ones, from the end of source, so the places of the others stay valid.
  const outer = found.filter((n) => !found.some((m) => m !== n && m.start <= n.start && n.end <= m.end))
  outer.sort((a, b) => b.start - a.start)
  const sets = []
  const switches = []
  let code = source
  for (const n of outer) {
    const set = switchCases + sets.length
    const x = n.discriminant.name
    const tests = n.cases.filter((c) => c.test !== null)
    const last = n.cases[n.cases.length - 1]
    const guarded =
      last.test === null
        ? `if (${set}.has(${x})) ${code.slice(n.start, last.start)}} else switch (0) { ${code.slice(last.start, n.end - 1)}}`
        : `if (${set}.has(${x})) ${code.slice(n.start, n.end)}`
    code = code.slice(0, n.start) + `{ ${guarded} }` + code.slice(n.end)
    sets.push(`var ${set} = new Set([${tests.map((c) => JSON.stringify(c.test.value)).join(', ')}]);`)
    switches.push({ name: x, cases: tests.map((c) => c.test.value) })
  }
  if (sets.length === 0) return { code: source, switches: [] }
  // On the line after a hashbang, which must come first, after a semicolon that ends the
  // directive before them, if any.
  const hashbang = source.startsWith('#!') ? /\r\n|[\n\r\u2028\u2029]|$/.exec(source) : null
  let at = hashbang ? hashbang.index + hashbang[0].length : 0
  for (const node of program.body) {
    if (typeof node.directive !== 'string') break
    at = node.end
  }
  return { code: code.slice(0, at) + ';' + sets.join(' ') + ' ' + code.slice(at), switches: switches.reverse() }
}

const functionTypes = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression'])

// declaredNames returns the names that the function node declares for all of its body: its
// parameters, its var variables anywhere outside the functions in it, and the functions declared
// at the top of its body. A let, a const, a class or a function in a block is seen only inside that
// block, and names in patterns are left out, which only leaves a switch on them as it is.
function declaredNames(fn) {
  const names = new Set()
  for (const param of fn.params) {
    const id = param.type === 'AssignmentPattern' ? param.left : param
    if (id.type === 'Identifier') names.add(id.name)
  }
  if (fn.body.type === 'BlockStatement') {
    for (const node of fn.body.body) if (node.type === 'FunctionDeclaration' && node.id) names.add(node.id.name)
  }
  const visit = (node) => {
    if (node.type === 'VariableDeclaration' && node.kind === 'var') {
      for (const d of node.declarations) if (d.id.type === 'Identifier') names.add(d.id.name)
    }
    if (functionTypes.has(node.type)) return
    for (const value of Object.values(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child)
      }
    }
  }
  visit(fn.body)
  return names
}

// guardable reports whether guardSwitches may put a Set check in front of the switch node.
function guardable(node) {
  const cases = node.cases
  const def = cases.findIndex((c) => c.test === null)
  const exits = (statements) =>
    ['BreakStatement', 'ReturnStatement', 'ThrowStatement', 'ContinueStatement'].includes(statements.at(-1)?.type)
  return (
    node.discriminant.type === 'Identifier' &&
    (def === -1 || (def === cases.length - 1 && def > 0 && exits(cases[def - 1].consequent))) &&
    cases.filter((c) => c.test !== null).length >= guardedCases &&
    cases.every((c) => c.test === null || (c.test.type === 'Literal' && typeof c.test.value === 'string')) &&
    cases.every((c) =>
      c.consequent.every(
        (s) =>
          !(s.type === 'VariableDeclaration' && s.kind !== 'var') &&
          s.type !== 'FunctionDeclaration' &&
          s.type !== 'ClassDeclaration',
      ),
    )
  )
}

// plainNamespaces returns code, a chunk of the SSR bundle before it is minified, with the ES
// modules that import a CommonJS module of plainPackages getting a namespace of plain values, and
// a warning when it cannot do that. ids are the modules of the chunk. rolldown gives an ES module
// the exports of a CommonJS module with __toESM(require_x()), a namespace with a getter for each
// export, so a page that calls React.createElement or a component that calls a hook of React
// through a namespace calls a getter each time, which takes long in QuickJS. React and React DOM
// set their exports once, when their modules run, and never change them, so plainESM gives the
// same namespace with each export's value: the same values, prototype, names and enumerability.
// It changes only the calls of __toESM whose module rolldown wrote under a region of those
// packages ("//#region path", from rolldown 1.2.15 "// path"), and finds the helpers and the calls by the form rolldown writes them in: when the form
// is another, it warns and changes nothing. The calls keep their length and plainESM goes at the
// end of the line of __toESM, which has no source, so every other place stays where it was.
export function plainNamespaces(code, ids) {
  const packages = [...new Set(ids.map(packageName).filter((name) => plainPackages.has(name)))].sort()
  if (packages.length === 0 || !/\b__toESM(?:\$\d+)?\(/.test(code)) return { code }
  const skip = (why) => ({
    code,
    warning:
      `${packages.join(', ')}: ${why}, so the tifa plugin does not give the modules that import them namespaces ` +
      'of plain values in place of getters. Pages render the same, but take longer to render',
  })
  const start = code.search(/\bvar __copyProps(?:\$\d+)? = /)
  const end = code.indexOf(' : target, mod));', start) + ' : target, mod));'.length
  const helpers = new Map() // the name of each helper to its name in the bundle
  const known =
    start >= 0 &&
    end > start &&
    code
      .slice(start, end)
      .replace(/\s+/g, ' ')
      .replace(/\b(__[A-Za-z]+)(\$\d+)?/g, (_, name, number = '') => {
        if ((helpers.get(name) ?? name + number) !== name + number) helpers.set(name, null)
        else helpers.set(name, name + number)
        return name
      }) === esmHelpers &&
    ![...helpers.values()].includes(null)
  if (!known) {
    return skip('rolldown writes __toESM, which gives an ES module the exports of a CommonJS module, in a form the tifa plugin does not know')
  }
  const toESM = helpers.get('__toESM')
  const plainName = '__tifaV' + toESM.slice('__toESM'.length)
  if (new RegExp(`\\b${plainName.replace('$', '\\$')}(?![\\w$])`).test(code)) return skip(`the SSR bundle already names ${plainName}`)
  const regions = new Map() // the name of each module's require function to its package
  for (const m of code.matchAll(/^\/\/(?:#region)? ([^\n]+)\n[\t ]*var (require_[\w$]+) = \/\* @__PURE__ \*\/ __commonJSMin(?:\$\d+)?\(/gm)) {
    regions.set(m[2], packageName(m[1]))
  }
  const plain = []
  for (const { index: at } of code.matchAll(new RegExp(`(?<![\\w$])${toESM.replace('$', '\\$')}\\(`, 'g'))) {
    const call = code.slice(at + toESM.length + 1, at + toESM.length + 200)
    const required = /^(require_[\w$]+)\(\)/.exec(call)?.[1]
    if (!required) {
      if (/^\(\/\* @__PURE__ \*\/ __commonJSMin(?:\$\d+)?\(/.test(call)) continue
      return skip(`rolldown calls __toESM in a form the tifa plugin does not know: ${toESM}(${call.split('\n')[0].slice(0, 40)}`)
    }
    if (!regions.has(required)) return skip(`the tifa plugin cannot tell which package ${required}() of the SSR bundle loads`)
    if (plainPackages.has(regions.get(required))) plain.push(at)
  }
  if (plain.length === 0) return { code }
  let changed = code
  for (const at of plain) changed = changed.slice(0, at) + plainName + changed.slice(at + plainName.length)
  const helper = plainESM.replace('__tifaV', plainName).replace(/\b__[A-Za-z]+\b/g, (name) => helpers.get(name) ?? name)
  return { code: changed.slice(0, end) + helper + changed.slice(end) }
}

// inlineElements returns code, a chunk of the SSR bundle before it is minified, with calls of jsx
// and jsxs of React's JSX runtime written as the objects they return. React 19.3 in production
// has one function for both. It returns { $$typeof, type, key, ref, props }: $$typeof is
// Symbol.for("react.transitional.element"), key is the third argument or the key of the config as
// a string, or null, ref is props.ref or null, and props is the config itself when it has no key.
// So a call
//
//   - whose config is an object literal with no spread, no computed name and no property named
//     key, ref or __proto__, which jsx would copy, read or treat apart,
//   - and which has no third argument, or a string, a number or a name for it,
//
// is the object { $$typeof: symbol, type: T, key: K, ref: null, props: { ... } }, with the same
// fields in the same order. A string or a number key becomes its text. A name becomes
// (void 0 !== name ? "" + name : null), as jsx turns it: undefined is no key and null is "null".
// The key of the object comes before the values of the config, which jsx evaluates first, so a
// name stays a call when the config assigns to it. A name that a call in the config changes is not
// seen. Any other call is left as it is.
//
// The calls are found by the variable that holds the runtime: one that is given only
// require_jsx_runtime(), which rolldown writes for react/jsx-runtime, or that module's wrapper
// written in its place, with __toESM or the function of plainNamespaces around it or not, where the
// wrapper requires the production file of React. The caller makes sure
// the text of that file is one the plugin knows. The symbol is taken once, in a variable declared
// before the first statement, on the line of the statement. Each line of code stays where it was.
// A jsx reads the config through its prototype, so a program that sets Object.prototype.ref or
// .key sees what jsx made but not the objects. parse gives the syntax tree of a source. It returns
// the code, how many calls it saw, how many it wrote as objects and how many of those had a key.
export function inlineElements(code, parse) {
  const symbol = '__tifaElement'
  const stats = { code, sites: 0, inlined: 0, keyed: 0 }
  const production = new Set() // the require functions of the production file of the runtime
  for (const m of code.matchAll(/^\/\/(?:#region)? ([^\n]*[/\\]react[/\\]cjs[/\\]react-jsx-runtime\.production\.js)\n/gm)) {
    const rest = code.slice(m.index + m[0].length)
    const declared = /\bvar (require_[\w$]+) = \/\* @__PURE__ \*\/ __commonJSMin(?:\$\d+)?\(/.exec(rest)
    if (declared && !/^\/\/(?:#region)? /m.test(rest.slice(0, declared.index)) && packageName(m[1]) === 'react') {
      production.add(declared[1])
    }
  }
  const runtimes = new Set() // the require functions of react/jsx-runtime
  const wrapper =
    /^\/\/(?:#region)? ([^\n]*[/\\]react[/\\]jsx-runtime\.js)\n[\t ]*var (require_[\w$]+) = \/\* @__PURE__ \*\/ __commonJSMin(?:\$\d+)?\(\(\(exports, module\) => \{\s*module\.exports = (require_[\w$]+)\(\);\s*\}\)\);/gm
  for (const m of code.matchAll(wrapper)) {
    if (packageName(m[1]) === 'react' && production.has(m[3])) runtimes.add(m[2])
  }
  if (production.size === 0 || new RegExp(`(?<![\\w$])${symbol}(?![\\w$])`).test(code)) return stats
  const program = parse(code)
  // require_jsx_runtime(), or the wrapper of react/jsx-runtime where rolldown wrote it in place, in
  // the only module that imports it: (__commonJSMin((exports, module) => { module.exports = require_x() }))()
  const loads = (node) => {
    if (node.type !== 'CallExpression' || node.arguments.length !== 0) return false
    if (node.callee.type === 'Identifier') return runtimes.has(node.callee.name)
    const inline = /^__commonJSMin(?:\$\d+)?\(\(\(exports, module\) => \{\s*module\.exports = (require_[\w$]+)\(\);\s*\}\)\)$/.exec(
      node.callee.type === 'CallExpression' ? code.slice(node.callee.start, node.callee.end) : '',
    )
    return inline !== null && production.has(inline[1])
  }
  const isRuntime = (node) =>
    loads(node) ||
    (node.type === 'CallExpression' &&
      node.callee.type === 'Identifier' &&
      /^__(?:toESM|tifaV)(?:\$\d+)?$/.test(node.callee.name) &&
      node.arguments.length > 0 &&
      loads(node.arguments[0]))
  const calls = []
  const holders = new Map() // a name to whether every value it is given is the runtime
  const give = (name, runtime) => holders.set(name, (holders.get(name) ?? true) && runtime)
  const visit = (node) => {
    if (node.type === 'VariableDeclarator' && node.init) {
      for (const name of patternNames(node.id)) give(name, node.id.type === 'Identifier' && isRuntime(node.init))
    } else if (node.type === 'AssignmentExpression') {
      for (const name of patternNames(node.left)) give(name, node.operator === '=' && node.left.type === 'Identifier' && isRuntime(node.right))
    } else if (node.type === 'UpdateExpression') {
      for (const name of patternNames(node.argument)) give(name, false)
    } else if (node.type === 'CallExpression') {
      calls.push(node)
    }
    for (const value of Object.values(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child)
      }
    }
  }
  visit(program)
  const newlines = (from, to) => '\n'.repeat(code.slice(from, to).split(/\r\n|[\n\r\u2028\u2029]/).length - 1)
  const edits = []
  for (const call of calls) {
    let callee = call.callee
    if (callee.type === 'SequenceExpression') callee = callee.expressions.at(-1)
    if (
      callee.type !== 'MemberExpression' ||
      callee.computed ||
      callee.object.type !== 'Identifier' ||
      callee.property.type !== 'Identifier' ||
      (callee.property.name !== 'jsx' && callee.property.name !== 'jsxs') ||
      holders.get(callee.object.name) !== true
    ) {
      continue
    }
    stats.sites++
    const [type, config, key] = call.arguments
    if (call.arguments.length < 2 || call.arguments.length > 3 || type.type === 'SpreadElement' || config.type !== 'ObjectExpression') continue
    if (config.properties.some((p) => p.type !== 'Property' || p.computed || ['key', 'ref', '__proto__'].includes(propertyName(p)))) continue
    let keyText = 'null'
    if (key) {
      if (key.type === 'Literal' && (typeof key.value === 'string' || typeof key.value === 'number')) {
        keyText = JSON.stringify(String(key.value))
      } else if (key.type === 'Identifier' && !writes(config, key.name)) {
        keyText = `(void 0 !== ${key.name} ? "" + ${key.name} : null)`
      } else {
        continue
      }
      stats.keyed++
    }
    stats.inlined++
    edits.push({ start: call.start, end: type.start, text: `({ $$typeof: ${symbol}, type: ` + newlines(call.start, type.start) })
    edits.push({ start: type.end, end: config.start, text: `, key: ${keyText}, ref: null, props: ` + newlines(type.end, config.start) })
    edits.push({ start: config.end, end: call.end, text: ' })' + newlines(config.end, call.end) })
  }
  if (stats.inlined === 0) return stats
  // After the directives, before the first statement after them.
  let at = program.body[0]?.start ?? 0
  for (const [i, node] of program.body.entries()) {
    if (typeof node.directive !== 'string') break
    at = program.body[i + 1]?.start ?? node.end
  }
  edits.push({ start: at, end: at, text: `var ${symbol} = Symbol.for("react.transitional.element"); ` })
  edits.sort((a, b) => a.start - b.start || a.end - b.end)
  let changed = ''
  let from = 0
  for (const edit of edits) {
    changed += code.slice(from, edit.start) + edit.text
    from = edit.end
  }
  stats.code = changed + code.slice(from)
  return stats
}

// propertyName returns the name of the property node of an object literal, or '' for a name that
// is not an identifier, a string or a number.
function propertyName(property) {
  const key = property.key
  if (key.type === 'Identifier') return key.name
  return key.type === 'Literal' ? String(key.value) : ''
}

// patternNames returns the names the target node of a declaration, an assignment or an update
// writes.
function patternNames(node) {
  switch (node.type) {
    case 'Identifier':
      return [node.name]
    case 'ArrayPattern':
      return node.elements.flatMap((e) => (e ? patternNames(e) : []))
    case 'ObjectPattern':
      return node.properties.flatMap((p) => patternNames(p.type === 'Property' ? p.value : p))
    case 'AssignmentPattern':
      return patternNames(node.left)
    case 'RestElement':
      return patternNames(node.argument)
    default:
      return []
  }
}

// writes reports whether the code of node assigns to name, or counts it up or down.
function writes(node, name) {
  if (node.type === 'AssignmentExpression' && patternNames(node.left).includes(name)) return true
  if (node.type === 'UpdateExpression' && patternNames(node.argument).includes(name)) return true
  for (const value of Object.values(node)) {
    for (const child of Array.isArray(value) ? value : [value]) {
      if (child && typeof child.type === 'string' && writes(child, name)) return true
    }
  }
  return false
}

// hostLoads returns the places where the bundle may still load a module at run time, which
// QuickJS cannot do, one "file:line:column (package): what" line each. It checks a copy of the
// code made for it:
//
//   - QuickJS has no require, so typeof require is "undefined" wherever the code does not declare
//     require. oxc replaces it there and only there, and its minifier then drops the code that
//     runs only with a require, like any if (false) branch.
//   - The minifier renames every name the bundle's function declares, so a require that is left
//     is the host's.
//
// The source maps of the copy and of the chunk name the place in the source.
async function hostLoads(chunk, vite, parse) {
  // Most bundles never name require or import(), and need no copy.
  if (!mayLoadModules(chunk.code)) return []
  const defined = await vite.transformWithOxc(chunk.code, chunk.fileName, {
    lang: 'js',
    define: { 'typeof require': '"undefined"' },
    sourcemap: true,
  })
  const minified = vite.minifySync(chunk.fileName, defined.code, { compress: true, mangle: true, sourcemap: true })
  if (minified.errors?.length > 0) throw new Error(`checking the SSR bundle: ${minified.errors.map((e) => e.message).join('\n')}`)
  const found = []
  const visit = (node, parent, key) => {
    if (node.type === 'Identifier' && node.name === 'require' && isReference(parent, key)) {
      const [name] = parent.type === 'CallExpression' && key === 'callee' ? parent.arguments.map(stringValue) : []
      found.push({
        at: node.start,
        what:
          typeof name !== 'string'
            ? 'require loads a module by a name known only at run time'
            : `require(${JSON.stringify(name)}) loads a Node module`,
      })
    } else if (node.type === 'ImportExpression') {
      found.push({ at: node.start, what: 'import() loads a module at run time' })
    }
    for (const [k, value] of Object.entries(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child, node, k)
      }
    }
  }
  visit(parse(minified.code), null, '')
  if (found.length === 0) return []
  // The copy, then the chunk, then the source.
  const maps = [sourcePositions(minified.map), sourcePositions(defined.map), chunk.map && sourcePositions(chunk.map)]
  const starts = [0]
  for (let i = minified.code.indexOf('\n'); i >= 0; i = minified.code.indexOf('\n', i + 1)) starts.push(i + 1)
  const places = found.map(({ at, what }) => {
    const line = lastAtOrBefore(starts, at, (start) => start)
    let position = maps[0](line, at - starts[line])
    position &&= maps[1](position.line, position.column)
    if (!position) return `${chunk.fileName}: ${what}`
    const source = maps[2] && maps[2](position.line, position.column)
    if (!source) return `${chunk.fileName}:${position.line + 1}:${position.column + 1}: ${what}`
    // The sources are relative to the application root, see outputOptions.
    const name = packageName(source.source)
    return `${source.source}:${source.line + 1}:${source.column + 1}${name ? ` (${name})` : ''}: ${what}`
  })
  return [...new Set(places)]
}

// rewriteStatements changes the code of the SSR bundle after the minifier, which writes
// statements that QuickJS compiles badly:
//
//   - A statement that is a && b, a || b or a ? b : c compiles to a copy of the value and a drop
//     on each path, which if (a) b does not need. Each becomes an if statement. Every branch
//     is in braces, so what a branch starts with cannot turn into a block or a declaration. A
//     statement of comma separated expressions with one of these is split into statements.
//   - !0 and !1 compile to a number and a negation, true and false to one push.
//
// Nothing else changes: the operands stay as they were, in the order they were evaluated, with
// their comments and lines. The bundle is an ES module, which has no completion value, so the
// value of every statement is dropped and none changes. parse gives the syntax tree of a source. mappings is the mappings string of the source map of code. It gives the new
// code, the mappings string that fits it (undefined without mappings) and the number of changes.
export function rewriteStatements(code, parse, mappings) {
  const edits = []
  const visit = (node, parent, key) => {
    if (node.type === 'UnaryExpression') {
      if (node.operator === '!' && /^![01]$/.test(code.slice(node.start, node.end))) {
        // return!0 is return true: a word in front needs a space.
        if (identifierChar(code.charCodeAt(node.start - 1))) edits.push({ start: node.start, end: node.start, text: ' ' })
        edits.push({ start: node.start, end: node.end, text: code[node.start + 1] === '0' ? 'true' : 'false' })
      }
    } else if (node.type === 'ExpressionStatement' && !node.directive) {
      statementEdits(code, node, Array.isArray(parent[key]), edits)
    }
  }
  visitNodes(parse(code), visit)
  if (edits.length === 0) return { code, mappings, count: 0 }
  // At one place, an insertion goes before a replacement, and insertions go in the order they were
  // made.
  const order = new Map(edits.map((edit, i) => [edit, i]))
  edits.sort((a, b) => a.start - b.start || a.end - b.end || order.get(a) - order.get(b))
  let out = ''
  let at = 0
  for (const edit of edits) {
    if (edit.start < at) throw new Error(`rewriting the SSR bundle: two changes overlap at ${edit.start}`)
    out += code.slice(at, edit.start) + edit.text
    at = edit.end
  }
  out += code.slice(at)
  return { code: out, mappings: mappings && shiftMappings(mappings, code, edits), count: edits.length }
}

// What a branch must not start with: after if (a) { it would be a block, a declaration or a
// function declaration, not an expression.
const startsStatement = /\{|function\b|class\b|async\b|let\b/y

// statementEdits adds to edits what changes the expression statement stmt. inList tells whether
// its parent holds a list of statements, so that the statement can become several. Otherwise it
// is the body of an if, a loop or a label, and a block keeps it one statement.
function statementEdits(code, stmt, inList, edits) {
  const expr = stmt.expression
  // A statement in parentheses is none of the forms below, and starts one character before them.
  if (expr.start !== stmt.start) return
  const end = code[stmt.end - 1] === ';' ? stmt.end - 1 : stmt.end
  const own = []
  if (expr.type === 'SequenceExpression') {
    const parts = []
    let from = stmt.start
    let converted = false
    for (const [i, part] of expr.expressions.entries()) {
      const last = i === expr.expressions.length - 1
      const comma = last ? -1 : operatorAfter(code, part.end, ',')
      if (!last && comma < 0) return
      const start = afterSpace(code, from)
      // After a comma, a part that starts with a block or a function would be a statement.
      if (i > 0 && startsAStatement(code, start)) return
      // Parentheses in front of a part are no part of its node. The part is not a branch then.
      const branch = start === part.start ? branchEdits(code, part, last ? end : comma) : null
      if (branch) converted = true
      parts.push({ branch, comma, last })
      from = comma + 1
    }
    if (!converted) return
    for (const { branch, comma, last } of parts) {
      if (branch) own.push(...branch)
      if (!last) own.push({ start: comma, end: comma + 1, text: branch ? '' : ';' })
    }
    if (parts.at(-1).branch && inList && end < stmt.end) own.push({ start: end, end: stmt.end, text: '' })
  } else {
    const branch = branchEdits(code, expr, end)
    if (!branch) return
    own.push(...branch)
    if (inList && end < stmt.end) own.push({ start: end, end: stmt.end, text: '' })
  }
  if (inList) {
    edits.push(...own)
  } else {
    edits.push({ start: stmt.start, end: stmt.start, text: '{' }, ...own, { start: stmt.end, end: stmt.end, text: '}' })
  }
}

// branchEdits returns the changes that turn expression node into if statements, or null when it
// is none of the forms or a branch of it could not follow the opening brace. node starts where a
// statement or a branch starts, and its branches end at edge, where the closing brace goes.
function branchEdits(code, node, edge) {
  if (node.type === 'LogicalExpression' && (node.operator === '&&' || node.operator === '||')) {
    const operator = operatorAfter(code, node.left.end, node.operator)
    if (operator < 0) return null
    const right = afterSpace(code, operator + 2)
    if (startsAStatement(code, right)) return null
    const and = node.operator === '&&'
    const edits = [
      { start: node.start, end: node.start, text: and ? 'if(' : 'if(!(' },
      { start: operator, end: operator + 2, text: and ? '){' : ')){' },
    ]
    // a || b && c is a || (b && c), and its right side is a statement of its own.
    if (right === node.right.start) edits.push(...(branchEdits(code, node.right, edge) ?? []))
    edits.push({ start: edge, end: edge, text: '}' })
    return edits
  }
  if (node.type === 'ConditionalExpression') {
    const question = operatorAfter(code, node.test.end, '?')
    const colon = question < 0 ? -1 : operatorAfter(code, node.consequent.end, ':')
    if (colon < 0) return null
    const consequent = afterSpace(code, question + 1)
    const alternate = afterSpace(code, colon + 1)
    if (startsAStatement(code, consequent) || startsAStatement(code, alternate)) return null
    const edits = [
      { start: node.start, end: node.start, text: 'if(' },
      { start: question, end: question + 1, text: '){' },
    ]
    if (consequent === node.consequent.start) edits.push(...(branchEdits(code, node.consequent, colon) ?? []))
    edits.push({ start: colon, end: colon + 1, text: '}else{' })
    if (alternate === node.alternate.start) edits.push(...(branchEdits(code, node.alternate, edge) ?? []))
    edits.push({ start: edge, end: edge, text: '}' })
    return edits
  }
  return null
}

function startsAStatement(code, at) {
  startsStatement.lastIndex = at
  return startsStatement.test(code)
}

// operatorAfter returns where the operator op is, looking from i past white space, comments and
// closing parentheses (those of an operand, which the node of the operand does not include), or
// -1 when something else comes first.
function operatorAfter(code, i, op) {
  for (;;) {
    i = afterSpace(code, i)
    if (code[i] !== ')') break
    i++
  }
  return code.startsWith(op, i) ? i : -1
}

// shiftMappings returns mappings, the mappings string of the source map of code, for the code
// that edits make of it. The edits keep the lines. A segment at a place where an edit inserts
// moves behind the new text, a segment inside what an edit replaces moves to the start of the new
// text, and each later one moves by the change in length.
function shiftMappings(mappings, code, edits) {
  const lines = []
  const starts = [0]
  for (let i = code.indexOf('\n'); i >= 0; i = code.indexOf('\n', i + 1)) starts.push(i + 1)
  for (const edit of edits) {
    if (code.slice(edit.start, edit.end).includes('\n') || edit.text.includes('\n')) {
      throw new Error('rewriting the SSR bundle: a change spans lines, which the source map cannot follow')
    }
    const line = lastAtOrBefore(starts, edit.start, (start) => start)
    ;(lines[line] ??= []).push({ ...edit, start: edit.start - starts[line], end: edit.end - starts[line] })
  }
  return mappings
    .split(';')
    .map((text, line) => {
      const changes = lines[line]
      if (!changes || !text) return text
      let next = 0 // the first change that ends after the segment
      let moved = 0 // what the changes before it add to the length
      let column = 0 // column of the segment before, in the old code
      let newColumn = 0 // and in the new code
      return text
        .split(',')
        .map((segment) => {
          if (!segment) return segment
          const [delta, length] = firstVLQ(segment)
          column += delta
          while (next < changes.length && changes[next].end <= column) {
            moved += changes[next].text.length - (changes[next].end - changes[next].start)
            next++
          }
          const inside = next < changes.length && changes[next].start <= column
          const placed = (inside ? changes[next].start : column) + moved
          const out = encodeVLQ(placed - newColumn) + segment.slice(length)
          newColumn = placed
          return out
        })
        .join(',')
    })
    .join(';')
}

// firstVLQ decodes the first Base64 VLQ number of a source map segment and returns it with the
// number of characters it takes.
function firstVLQ(segment) {
  let value = 0
  let shift = 0
  let i = 0
  for (;;) {
    const digit = base64.indexOf(segment[i++])
    value += (digit & 31) * 2 ** shift
    if (!(digit & 32)) break
    shift += 5
  }
  return [value % 2 ? -(value - 1) / 2 : value / 2, i]
}

// encodeVLQ is the Base64 VLQ of n.
function encodeVLQ(n) {
  let value = n < 0 ? -2 * n + 1 : 2 * n
  let text = ''
  do {
    let digit = value % 32
    value = Math.floor(value / 32)
    if (value > 0) digit |= 32
    text += base64[digit]
  } while (value > 0)
  return text
}

// The keywords after which a / starts a regular expression, not a division.
const beforeExpression = new Set(['return', 'typeof', 'instanceof', 'in', 'of', 'new', 'delete', 'void', 'throw', 'case', 'do', 'else', 'yield', 'await'])

// mayLoadModules reports whether code names require, or import followed by (, outside comments,
// strings, template text and regular expressions. It reads the tokens and builds no syntax tree,
// so it costs little next to the check. A / starts a regular expression where an expression may
// start: first, after a punctuator other than ) and ], or after a keyword such as return.
export function mayLoadModules(code) {
  const templates = [] // the depth of braces at which each ${ of the open templates started
  let depth = 0
  let expression = true
  let i = 0
  while (i < code.length) {
    const c = code.charCodeAt(i)
    if (c === 32 || c === 9 || c === 10 || c === 13) {
      i++
    } else if (c === 47 && (code.charCodeAt(i + 1) === 47 || code.charCodeAt(i + 1) === 42)) {
      i = afterComment(code, i)
    } else if (c === 47) {
      i = expression ? afterRegExp(code, i) : i + 1
      expression = !expression
    } else if (c === 39 || c === 34) {
      i = afterString(code, i)
      expression = false
    } else if (c === 96 || (c === 125 && templates.at(-1) === depth - 1)) {
      // The text of a template, from its start or from the end of one of its expressions.
      if (c === 125) templates.pop(), depth--
      const [end, open] = afterTemplateText(code, i + 1)
      if (open) templates.push(depth++)
      i = end
      expression = open
    } else if (identifierChar(c) && !(c >= 48 && c <= 57)) {
      let end = i + 1
      while (end < code.length && identifierChar(code.charCodeAt(end))) end++
      const word = end - i === 7 || end - i === 6 ? code.slice(i, end) : ''
      if (word === 'require') return true
      if (word === 'import' && code.charCodeAt(afterSpace(code, end)) === 40) return true
      expression = beforeExpression.has(code.slice(i, end))
      i = end
    } else if (c >= 48 && c <= 57) {
      while (i < code.length && (identifierChar(code.charCodeAt(i)) || code.charCodeAt(i) === 46)) i++
      expression = false
    } else {
      if (c === 123) depth++
      if (c === 125) depth--
      expression = c !== 41 && c !== 93
      i++
    }
  }
  return false
}

// identifierChar reports whether the character code c may be part of a name. Any character
// outside ASCII counts, which is enough to find require and import.
function identifierChar(c) {
  return (c >= 97 && c <= 122) || (c >= 65 && c <= 90) || (c >= 48 && c <= 57) || c === 36 || c === 95 || c === 92 || c > 127
}

// afterComment returns where the comment at i of code ends.
function afterComment(code, i) {
  const end = code.charCodeAt(i + 1) === 47 ? code.indexOf('\n', i) : code.indexOf('*/', i + 2) + 2
  return end <= 1 ? code.length : end
}

// afterSpace returns where the white space and comments from i of code end.
function afterSpace(code, i) {
  for (;;) {
    const c = code.charCodeAt(i)
    if (c === 32 || c === 9 || c === 10 || c === 13) i++
    else if (c === 47 && (code.charCodeAt(i + 1) === 47 || code.charCodeAt(i + 1) === 42)) i = afterComment(code, i)
    else return i
  }
}

// afterString returns where the string at i of code ends. A string ends at the end of its line
// at the latest.
function afterString(code, i) {
  const quote = code.charCodeAt(i)
  for (i++; i < code.length; i++) {
    const c = code.charCodeAt(i)
    if (c === 92) i++
    else if (c === quote) return i + 1
    else if (c === 10) return i
  }
  return i
}

// afterRegExp returns where the regular expression at i of code ends, its flags included.
function afterRegExp(code, i) {
  let inClass = false
  for (i++; i < code.length; i++) {
    const c = code.charCodeAt(i)
    if (c === 92) i++
    else if (c === 10) return i
    else if (c === 91) inClass = true
    else if (c === 93) inClass = false
    else if (c === 47 && !inClass) break
  }
  for (i++; i < code.length && identifierChar(code.charCodeAt(i)); i++);
  return i
}

// afterTemplateText returns where the text of a template from i of code ends, and whether an
// expression follows it: after ${, or after the closing backquote.
function afterTemplateText(code, i) {
  for (; i < code.length; i++) {
    const c = code.charCodeAt(i)
    if (c === 92) i++
    else if (c === 96) return [i + 1, false]
    else if (c === 36 && code.charCodeAt(i + 1) === 123) return [i + 2, true]
  }
  return [i, false]
}

// stringValue returns the value of a string literal, or of a template literal without
// expressions as the minifier writes strings, undefined for any other node.
function stringValue(node) {
  if (node.type === 'Literal' && typeof node.value === 'string') return node.value
  if (node.type === 'TemplateLiteral' && node.expressions.length === 0) return node.quasis[0].value.cooked
  return undefined
}

// sourcePositions decodes map and returns a function that gives the source, line and column (all
// from 0) that a generated line and column come from: those of the last segment at or before the
// column. It gives null when there is none on that line, or when that segment has no source.
function sourcePositions(map) {
  const lines = []
  // Fields other than the generated column carry over from the segments before, on every line.
  let source = 0
  let line = 0
  let column = 0
  for (const text of map.mappings.split(';')) {
    const segments = []
    let generated = 0
    for (const segment of text.split(',')) {
      if (!segment) continue
      const fields = vlq(segment)
      generated += fields[0]
      if (fields.length < 4) {
        segments.push({ generated, position: null })
        continue
      }
      source += fields[1]
      line += fields[2]
      column += fields[3]
      segments.push({ generated, position: { source: map.sources[source], line, column } })
    }
    lines.push(segments)
  }
  return (l, c) => {
    const segments = lines[l] ?? []
    const i = lastAtOrBefore(segments, c, (segment) => segment.generated)
    return i < 0 ? null : segments[i].position
  }
}

// lastAtOrBefore returns the index of the last item of sorted, in the order of key, whose key is
// at most value, or -1.
function lastAtOrBefore(sorted, value, key) {
  let low = 0
  let high = sorted.length
  while (low < high) {
    const middle = (low + high) >>> 1
    if (key(sorted[middle]) <= value) low = middle + 1
    else high = middle
  }
  return low - 1
}

// packageName returns the npm package that file belongs to, by the last node_modules in its
// path, or '' for a file outside node_modules.
function packageName(file) {
  const parts = file.split(/[/\\]/)
  const at = parts.lastIndexOf('node_modules')
  if (at < 0 || at + 1 >= parts.length) return ''
  return parts[at + 1].startsWith('@') ? parts.slice(at + 1, at + 3).join('/') : parts[at + 1]
}

const base64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'

// vlq decodes the Base64 VLQ numbers of one source map segment.
function vlq(segment) {
  const values = []
  let value = 0
  let shift = 0
  for (const char of segment) {
    const digit = base64.indexOf(char)
    value += (digit & 31) * 2 ** shift
    if (digit & 32) {
      shift += 5
      continue
    }
    values.push(value % 2 ? -(value - 1) / 2 : value / 2)
    value = shift = 0
  }
  return values
}

// isReference reports whether an identifier at key of parent refers to a binding, unlike a
// property name or a label.
function isReference(parent, key) {
  switch (parent?.type) {
    case 'MemberExpression':
      return key !== 'property' || parent.computed
    case 'Property':
    case 'MethodDefinition':
    case 'PropertyDefinition':
      return key !== 'key' || parent.computed
    case 'LabeledStatement':
    case 'BreakStatement':
    case 'ContinueStatement':
      return key !== 'label'
    case 'ExportSpecifier':
      return key !== 'exported'
    case 'ImportSpecifier':
      return key !== 'imported'
    default:
      return true
  }
}

// reactServerExports replaces react-dom/server.edge.js, a CommonJS barrel that loads both the
// stream and the string renderer, with an ES module that re-exports the same public API. A named
// renderToString import then bundles only the string renderer. It reads the barrel's program: in
// the production branch of an if on process.env.NODE_ENV, one require of a renderer module per
// variable, and then exports.name = variable.member. It returns null for any other form.
function reactServerExports(program) {
  const modules = new Map()
  const exports = []
  for (const node of program.body) {
    const expr = node.type === 'ExpressionStatement' ? node.expression : null
    if (node.type === 'ExpressionStatement' && node.directive) continue
    if (node.type === 'VariableDeclaration' && node.declarations.every((d) => !d.init)) continue
    if (node.type === 'IfStatement' && productionTest(node.test)) {
      const branch = node.test.operator === '===' ? node.consequent : node.alternate
      if (branch?.type !== 'BlockStatement') return null
      for (const statement of branch.body) {
        const assign = statement.type === 'ExpressionStatement' ? statement.expression : null
        if (assign?.type !== 'AssignmentExpression' || assign.left.type !== 'Identifier' || !requireCall(assign.right)) {
          return null
        }
        modules.set(assign.left.name, assign.right.arguments[0].value)
      }
      continue
    }
    if (
      expr?.type === 'AssignmentExpression' &&
      expr.operator === '=' &&
      member(expr.left) &&
      expr.left.object.name === 'exports' &&
      member(expr.right) &&
      modules.has(expr.right.object.name)
    ) {
      exports.push([expr.left.property.name, expr.right.object.name, expr.right.property.name])
      continue
    }
    return null
  }
  const string = exports.find(([name]) => name === 'renderToString')
  if (!string) return null
  // The tifa plugin adds prerenderToString to the string renderer, see prerenderToString.
  exports.push(['prerenderToString', string[1], 'prerenderToString'])
  const named = []
  const namespace = []
  for (const [name, variable, prop] of exports) {
    named.push(`export { ${prop} as ${name} } from ${JSON.stringify(modules.get(variable))}`)
    namespace.push(`${name}: ${variable}.${prop}`)
  }
  const imports = [...modules].map(([variable, target]) => `import * as ${variable} from ${JSON.stringify(target)}`)
  // CommonJS default imports remain supported. The pure factory lets a named renderToString
  // import discard the otherwise unused default namespace.
  const code =
    named.join('\n') +
    '\n' +
    imports.join('\n') +
    '\nexport default /* @__PURE__ */ (() => ({' +
    namespace.join(',') +
    '}))()'
  return { code, targets: [...modules.values()] }
}

// productionTest reports whether node is process.env.NODE_ENV === 'production', or !==.
function productionTest(node) {
  if (node.type !== 'BinaryExpression' || (node.operator !== '===' && node.operator !== '!==')) return false
  const env = (n) =>
    member(n) && n.property.name === 'NODE_ENV' && member(n.object) && n.object.property.name === 'env' &&
    n.object.object.name === 'process'
  const production = (n) => n.type === 'Literal' && n.value === 'production'
  return (env(node.left) && production(node.right)) || (production(node.left) && env(node.right))
}

// member reports whether node is a.b with names only.
function member(node) {
  return (
    node.type === 'MemberExpression' &&
    !node.computed &&
    node.property.type === 'Identifier' &&
    (node.object.type === 'Identifier' || member(node.object))
  )
}

function requireCall(node) {
  return (
    node.type === 'CallExpression' &&
    node.callee.type === 'Identifier' &&
    node.callee.name === 'require' &&
    node.arguments.length === 1 &&
    node.arguments[0].type === 'Literal' &&
    typeof node.arguments[0].value === 'string'
  )
}

function packageVersion(dir) {
  try {
    return JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8')).version
  } catch {
    return '(unknown version)'
  }
}

// findPackage returns the real directory of the package name that root resolves, the way Node
// finds packages, or '' when no directory has it. Vite reports module paths with symbolic links
// resolved, so the adapters compare against the real directory.
function findPackage(root, name) {
  for (let dir = root; ; dir = path.dirname(dir)) {
    const pkg = path.join(dir, 'node_modules', name)
    if (fs.existsSync(path.join(pkg, 'package.json'))) return fs.realpathSync(pkg)
    if (path.dirname(dir) === dir) return ''
  }
}
