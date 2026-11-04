// The Vite plugin of tifa. tifa writes it into .tifa/vite/plugin.js of an application, and the
// application's vite.config.ts uses it through .tifa/vite.js. It tells Vite where tifa's
// generated entries are, resolves the modules tifa generates, and builds the SSR bundle that
// QuickJS runs: one ES module file with every dependency inside, no imports and no Node API.
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { builtinModules, createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

// devBase is where the Vite dev server serves modules under tifa dev. tifa dev sends every
// request under it to Vite and every other one to the application.
export const devBase = '/__tifa/vite/'

const clientOnlyStub = '\0tifa-client-only'
const propertyState = '\0tifa-core-js-property-state'
const routeModule = /^\.\.?\/(?:.*\/)?route\.gen(?:\.js|\.ts)?$/
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
// clsx joins the class names of an element, for the className of nearly every component of MUI and
// of many other libraries. It reads its arguments from the arguments object and calls a function for
// each one, even for a string, which nearly every argument is. The tifa plugin changes dist/clsx.mjs,
// see clsxFast. clsxSource is that file in clsx 2.0.1 to 2.1.1, which have the same text.
const clsxModule = /[/\\]clsx[/\\]dist[/\\]clsx\.mjs$/
const clsxSource =
  'function r(e){var t,f,n="";if("string"==typeof e||"number"==typeof e)n+=e;else if("object"==typeof e)if(Array.isArray(e)){var o=e.length;for(t=0;t<o;t++)e[t]&&(f=r(e[t]))&&(n&&(n+=" "),n+=f)}else for(f in e)e[f]&&(n&&(n+=" "),n+=f);return n}export function clsx(){for(var e,t,f=0,n="",o=arguments.length;f<o;f++)(e=arguments[f])&&(t=r(e))&&(n&&(n+=" "),n+=t);return n}export default clsx;'
// memoTheme of @mui/system keeps the styles a function makes from the theme as long as the theme is
// the same object. On the server ThemeProvider spreads the theme into a new object on every render,
// so each memoized style is made again on each render. The tifa plugin changes memoTheme.mjs and
// memoTheme.js (esm/memoTheme.js and modern/memoTheme.js in v6 and v7), see memoThemeByContent.
// memoThemeSource is unstable_memoTheme of @mui/system 6.1 to 9.5, with the call of
// preprocessStyles written as the ES module (preprocessStyles) or the CommonJS file
// ((0, _preprocessStyles.default)) has it.
export const memoThemeModule = /[/\\]@mui[/\\]system[/\\](?:esm[/\\]|modern[/\\])?memoTheme\.m?js$/
const memoThemeSource = (preprocess) => `function unstable_memoTheme(styleFn) {
  let lastValue;
  let lastTheme;
  return function styleMemoized(props) {
    let value = lastValue;
    if (value === undefined || props.theme !== lastTheme) {
      arg.theme = props.theme;
      value = ${preprocess}(styleFn(arg));
      lastValue = value;
      lastTheme = props.theme;
    }
    return value;
  };
}`
const memoThemeArg = `const arg = {
  theme: undefined
};`
// memoThemeSize is how many themes memoThemeByContent keeps.
const memoThemeSize = 8
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
// The start of pushAttribute after inlineAttributeCalls, and the check of the name in its default
// branch, which cacheAttributeNames changes.
const attributeStart = `function pushAttribute(target, name, value) {
  switch (name) {`
const attributeDefault = `    default:
      if (
        !(2 < name.length) ||
        ("o" !== name[0] && "O" !== name[0]) ||
        ("n" !== name[1] && "N" !== name[1])
      )
        if (((name = aliases.get(name) || name), hasOwnProperty.call(validatedAttributeNameCache, name) || isAttributeNameSafe(name))) {`
// attributeNamesKept is how many names the module keeps. React's own cache of valid names has no limit, but
// the names here include those of event handlers, which React does not keep, and an app may spread an
// object of its users' keys into a tag.
const attributeNamesKept = 4096
// What the module knows of the names of attributes at the start: the chunk before the value for the
// names whose case writes a string as it is with its own name. It adds the valid names without a case, as
// the chunk with the name in the form React writes it (the alias of htmlFor is for), and the names that
// start with on, as an empty chunk. className and style, which pushAttribute answers on its own, are not in it.
const attributeChunks = `
var tifaAttributeChunks = new Map([["tabIndex", ' tabindex="'], ["dir", ' dir="'], ["role", ' role="'], ["viewBox", ' viewBox="'], ["width", ' width="'], ["height", ' height="']]);
`
// renderNodeDestructive of React's string renderer after dropComponentStacks, white space collapsed:
// it forwards a node to retryNode, which reads it from the task.
const nodeForwarder =
  'function renderNodeDestructive(request, task, node, childIndex) { null !== task.replay && "number" === typeof task.replay.slots ? ' +
  'resumeNode(request, task, task.replay.slots, node, childIndex) : ((task.node = node), (task.childIndex = childIndex), retryNode(request, task)); }'
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
// The pieces of React's production JSX runtime that decide what the element jsx returns is, as React
// 19.3.0 and 19.3.1 have them, by the name of the piece and the fingerprint of its structure (see
// jsxRuntimePieces): the symbol that marks an element, the function that makes one and the two
// exports that are that function. See inlineElements, which writes the elements these make. React's
// own text is in react/cjs/react-jsx-runtime.production.js.
const reviewedJsxPieces = new Map([
  ['REACT_ELEMENT_TYPE', '0ab98ba119d2'],
  ['jsxProd', '1ada34b9dc5b'],
  ['exports.jsx', 'c77445f51117'],
  ['exports.jsxs', 'db32306fd41f'],
])
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

// notRecognized starts every warning that says a rewrite did not recognize the code it was to change,
// so that a check can look for this text alone: internal/bundle and test/e2e fail on it, and so does
// the scheduled CI job that builds with the latest React. Do not reword it.
const notRecognized = 'tifa rewrite not recognized: '

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
        // The program reads .env when it starts. A page gets what it needs from its Load.
        envDir: false,
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
  return [
    plugin,
    generated(),
    lazyRoutes(),
    markdownPlugin(() => root, settings.markdown),
    ssrPlugin(() => root, () => import('vite'), { i18nextCache: settings.i18nextCache }),
  ]
}

// markdownPackages are the packages the Markdown of content collections turns into HTML with, and
// the major version tifa is tested with of each. The app installs them: they are loaded from its
// root, and only when it has collections.
export const markdownPackages = [
  ['unified', 11],
  ['remark-parse', 11],
  ['remark-gfm', 4],
  ['remark-rehype', 11],
  ['rehype-raw', 7],
  ['rehype-stringify', 10],
]

// contentEntries is the file of .tifa where the build lists the entries to convert, contentHeadings
// the one the plugin writes the headings of each into, by the file of its body (EntriesFile and
// HeadingsFile of internal/content).
const contentEntries = 'content/entries.json'
const contentHeadings = 'content/headings.json'

/**
 * markdownPlugin turns the Markdown of the content collections into HTML. tifa build lists the
 * entries in .tifa/content/entries.json, and the plugin writes the body of each into .tifa/dist
 * and its headings into .tifa/content/headings.json, which tifa build puts into content.json.
 * Without the list, as for an app without collections, it does nothing and loads no package.
 *
 * In tifa build it converts at the end of the browser build, which outputs the files the bodies
 * refer to as assets. Under tifa dev the driver calls api.convert, and the bodies refer to the
 * files through Vite's dev server.
 *
 * markdown is the markdown option of tifa(): remarkPlugins and rehypePlugins, see markdownProcessor.
 */
export function markdownPlugin(getRoot, markdown) {
  let command = ''
  return {
    name: 'tifa:markdown',
    configResolved(config) {
      command = config.command
    },
    async generateBundle() {
      if (command !== 'build' || this.environment?.name !== 'client') return
      const root = getRoot()
      const entries = readContentEntries(root)
      if (!entries) return
      const urls = new Map() // real path of a file to its URL
      const result = await convertContent(root, entries, {
        markdown,
        command: 'tifa build',
        asset: (file) => {
          let url = urls.get(file)
          if (url === undefined) {
            const ref = this.emitFile({ type: 'asset', name: assetName(path.basename(file)), source: fs.readFileSync(file) })
            url = '/' + encodePath(this.getFileName(ref))
            urls.set(file, url)
          }
          return url
        },
      })
      for (const w of result.warnings) this.warn(w)
      writeContent(root, result)
    },
    api: {
      // convert turns the entries of .tifa/content/entries.json into HTML and returns the warnings
      // of the plugins. For tifa dev with pages the bodies load their files from Vite's dev server.
      // alone is for an app without pages, where no bundle is built and no dev server runs: the
      // files go into .tifa/dist/assets as Vite names its assets, <name>-<hash>.<ext>, and
      // command is the tifa command that converts, for the messages.
      async convert({ alone = false, command: by = 'tifa dev' } = {}) {
        const root = getRoot()
        const entries = readContentEntries(root)
        if (!entries) return []
        const real = fs.realpathSync(root)
        let asset = (file) => devBase + encodePath(path.relative(real, file).split(path.sep).join('/'))
        const assets = path.join(root, '.tifa', 'dist', 'assets')
        if (alone) {
          fs.rmSync(assets, { recursive: true, force: true })
          asset = (file) => '/assets/' + encodePath(writeAsset(assets, file))
        }
        const result = await convertContent(root, entries, { markdown, command: by, asset })
        writeContent(root, result)
        return result.warnings
      },
    },
  }
}

// readContentEntries returns the entries tifa lists in .tifa/content/entries.json, null without
// the file.
function readContentEntries(root) {
  let text
  try {
    text = fs.readFileSync(path.join(root, '.tifa', contentEntries), 'utf8')
  } catch (err) {
    if (err.code === 'ENOENT') return null
    throw err
  }
  return JSON.parse(text).entries
}

// writeContent writes the HTML of each entry into .tifa/dist, in place of the bodies there, and
// the headings into .tifa/content/headings.json.
function writeContent(root, result) {
  const dist = path.join(root, '.tifa', 'dist')
  fs.rmSync(path.join(dist, 'content'), { recursive: true, force: true })
  for (const [out, html] of Object.entries(result.html)) {
    const file = path.join(dist, out)
    fs.mkdirSync(path.dirname(file), { recursive: true })
    fs.writeFileSync(file, html)
  }
  fs.writeFileSync(path.join(root, '.tifa', contentHeadings), JSON.stringify(result.headings))
}

// writeAsset copies the file at the real path file into the folder assets as
// <name>-<hash>.<ext>, the hash being 8 characters of its SHA-256, and returns that name. The
// same file is written once.
function writeAsset(assets, file) {
  const source = fs.readFileSync(file)
  const base = assetName(path.basename(file))
  const ext = path.extname(base)
  const hash = createHash('sha256').update(source).digest('base64url').slice(0, 8)
  const name = `${base.slice(0, base.length - ext.length)}-${hash}${ext}`
  fs.mkdirSync(assets, { recursive: true })
  fs.writeFileSync(path.join(assets, name), source)
  return name
}

// encodePath writes a path with / between its parts as the path of a URL.
function encodePath(p) {
  return p.split('/').map(encodeURIComponent).join('/')
}

// assetName is name with the characters go:embed refuses in a file name replaced by _, so that the
// program can embed the asset.
export function assetName(name) {
  return name.replace(/[^\p{L}0-9._-]/gu, '_')
}

/**
 * loadMarkdown loads the packages of markdownPackages from the app at root. It fails, with the
 * command that installs them, when one is missing or has another major version. command is the
 * tifa command that converts, for the message.
 */
export async function loadMarkdown(root, command) {
  const require = createRequire(path.join(root, 'package.json'))
  const missing = []
  const wrong = []
  const dirs = []
  for (const [name, major] of markdownPackages) {
    const dir = (require.resolve.paths(name) ?? []).map((d) => path.join(d, name)).find((d) => fs.existsSync(path.join(d, 'package.json')))
    if (!dir) {
      missing.push(`${name}@${major}`)
      continue
    }
    const { version } = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8'))
    if (Number(String(version).split('.')[0]) !== major) wrong.push(`${name} ${version} is not a version tifa is tested with: pnpm add -D ${name}@${major}`)
    dirs.push(dir)
  }
  const problems = []
  if (missing.length > 0) {
    problems.push(`the app has content collections, so ${command} turns Markdown into HTML with unified: pnpm add -D ${missing.join(' ')}`)
  }
  if (problems.length + wrong.length > 0) throw new Error([...problems, ...wrong].join('\n'))
  const load = async (name) => {
    const module = await import(pathToFileURL(require.resolve(name)).href)
    return module.default ?? module
  }
  const [{ unified }, remarkParse, remarkGfm, remarkRehype, rehypeRaw, rehypeStringify] = await Promise.all([
    import(pathToFileURL(require.resolve('unified')).href),
    load('remark-parse'),
    load('remark-gfm'),
    load('remark-rehype'),
    load('rehype-raw'),
    load('rehype-stringify'),
  ])
  return { unified, remarkParse, remarkGfm, remarkRehype, rehypeRaw, rehypeStringify }
}

// contentFiles holds what the steps of tifa learn about the file they convert, by the data of its
// vfile.
const contentFiles = new WeakMap()

/**
 * markdownProcessor returns the unified processor that turns the Markdown of an entry into HTML,
 * with the packages p of loadMarkdown. The steps come in this order, and only the plugins of the
 * app can be left out or changed:
 *
 *  1. remark-parse and remark-gfm
 *  2. markdown.remarkPlugins
 *  3. remark-rehype with allowDangerousHtml and rehype-raw: HTML in the Markdown stays as written
 *  4. tifa: HTML comments are removed
 *  5. tifa: h1 to h6 without an id get the id GitHub gives them
 *  6. markdown.rehypePlugins
 *  7. tifa: relative URLs become the URLs of their files, see rewriteURLs
 *  8. tifa: the headings are collected
 *  9. rehype-stringify
 *
 * A plugin is written as unified takes it: a function, or [function, ...options]. An error a
 * plugin of the app throws names it.
 */
export function markdownProcessor(p, markdown = {}) {
  const plugins = (kind) => {
    const list = markdown?.[kind] ?? []
    if (!Array.isArray(list)) throw new Error(`tifa({ markdown }): ${kind} must be a list of plugins, such as [remarkPangu, [rehypeShiki, { theme: 'github-light' }]]`)
    return list.map((item) => namedPlugin(kind === 'remarkPlugins' ? 'remark' : 'rehype', item))
  }
  return p
    .unified()
    .use(p.remarkParse)
    .use(p.remarkGfm)
    .use(plugins('remarkPlugins'))
    .use(p.remarkRehype, { allowDangerousHtml: true })
    .use(p.rehypeRaw)
    .use(dropComments)
    .use(headingIDs)
    .use(plugins('rehypePlugins'))
    .use(rewriteURLs)
    .use(collectHeadings)
    .use(p.rehypeStringify)
}

// namedPlugin wraps a plugin of the app so that an error of its transform names it, such as
// "rehype plugin rehypeShiki".
function namedPlugin(kind, item) {
  const [attacher, ...options] = Array.isArray(item) ? item : [item]
  if (typeof attacher !== 'function') return item // a preset
  const name = `${kind} plugin ${attacher.name || 'without a name'}`
  const tag = (err) => {
    if (err && typeof err === 'object' && !err.tifaPlugin) err.tifaPlugin = name
    return err
  }
  function plugin(...args) {
    const transform = attacher.apply(this, args)
    if (typeof transform !== 'function') return transform
    // unified passes a callback to a transform with three parameters, and this one has three.
    return function (tree, file, next) {
      let result
      try {
        if (transform.length >= 3) return transform.call(this, tree, file, (err, ...rest) => next(tag(err), ...rest))
        result = transform.call(this, tree, file)
      } catch (err) {
        return next(tag(err))
      }
      if (result instanceof Error) return next(tag(result))
      if (result && typeof result.then === 'function') return result.then((tree) => next(undefined, tree), (err) => next(tag(err)))
      next(undefined, result)
    }
  }
  Object.defineProperty(plugin, 'name', { value: attacher.name })
  return [plugin, ...options]
}

// eachElement calls f for each element below node, parents before their children.
function eachElement(node, f) {
  for (const child of node.children ?? []) {
    if (child.type === 'element') f(child)
    eachElement(child, f)
  }
}

// dropComments removes the HTML comments of the body, such as notes for the author.
function dropComments() {
  const drop = (node) => {
    if (!node.children) return
    node.children = node.children.filter((child) => child.type !== 'comment')
    node.children.forEach(drop)
  }
  return drop
}

const headingTag = /^h[1-6]$/

// textOf is the text of node: its text nodes, in order, as hast-util-to-string reads it.
function textOf(node) {
  if (node.type === 'text') return node.value
  return (node.children ?? []).map(textOf).join('')
}

// slugRemoved is what github-slugger 2.0.0 removes from the text of a heading, copied from its
// regex.js: everything that is no letter, mark, decimal digit or connector, space or -, as a fixed
// list of the characters of Unicode 13. A fixed list, not Unicode properties of the regexp of
// Node, so that the ids are those of rehype-slug whatever Node runs.
//
// github-slugger: Copyright (c) 2015, Dan Flettre <fletd01@yahoo.com>. Permission to use, copy,
// modify, and/or distribute this software for any purpose with or without fee is hereby granted,
// provided that the above copyright notice and this permission notice appear in all copies (ISC).
// eslint-disable-next-line no-control-regex, no-misleading-character-class, no-useless-escape
const slugRemoved = /[\0-\x1F!-,\.\/:-@\[-\^`\{-\xA9\xAB-\xB4\xB6-\xB9\xBB-\xBF\xD7\xF7\u02C2-\u02C5\u02D2-\u02DF\u02E5-\u02EB\u02ED\u02EF-\u02FF\u0375\u0378\u0379\u037E\u0380-\u0385\u0387\u038B\u038D\u03A2\u03F6\u0482\u0530\u0557\u0558\u055A-\u055F\u0589-\u0590\u05BE\u05C0\u05C3\u05C6\u05C8-\u05CF\u05EB-\u05EE\u05F3-\u060F\u061B-\u061F\u066A-\u066D\u06D4\u06DD\u06DE\u06E9\u06FD\u06FE\u0700-\u070F\u074B\u074C\u07B2-\u07BF\u07F6-\u07F9\u07FB\u07FC\u07FE\u07FF\u082E-\u083F\u085C-\u085F\u086B-\u089F\u08B5\u08C8-\u08D2\u08E2\u0964\u0965\u0970\u0984\u098D\u098E\u0991\u0992\u09A9\u09B1\u09B3-\u09B5\u09BA\u09BB\u09C5\u09C6\u09C9\u09CA\u09CF-\u09D6\u09D8-\u09DB\u09DE\u09E4\u09E5\u09F2-\u09FB\u09FD\u09FF\u0A00\u0A04\u0A0B-\u0A0E\u0A11\u0A12\u0A29\u0A31\u0A34\u0A37\u0A3A\u0A3B\u0A3D\u0A43-\u0A46\u0A49\u0A4A\u0A4E-\u0A50\u0A52-\u0A58\u0A5D\u0A5F-\u0A65\u0A76-\u0A80\u0A84\u0A8E\u0A92\u0AA9\u0AB1\u0AB4\u0ABA\u0ABB\u0AC6\u0ACA\u0ACE\u0ACF\u0AD1-\u0ADF\u0AE4\u0AE5\u0AF0-\u0AF8\u0B00\u0B04\u0B0D\u0B0E\u0B11\u0B12\u0B29\u0B31\u0B34\u0B3A\u0B3B\u0B45\u0B46\u0B49\u0B4A\u0B4E-\u0B54\u0B58-\u0B5B\u0B5E\u0B64\u0B65\u0B70\u0B72-\u0B81\u0B84\u0B8B-\u0B8D\u0B91\u0B96-\u0B98\u0B9B\u0B9D\u0BA0-\u0BA2\u0BA5-\u0BA7\u0BAB-\u0BAD\u0BBA-\u0BBD\u0BC3-\u0BC5\u0BC9\u0BCE\u0BCF\u0BD1-\u0BD6\u0BD8-\u0BE5\u0BF0-\u0BFF\u0C0D\u0C11\u0C29\u0C3A-\u0C3C\u0C45\u0C49\u0C4E-\u0C54\u0C57\u0C5B-\u0C5F\u0C64\u0C65\u0C70-\u0C7F\u0C84\u0C8D\u0C91\u0CA9\u0CB4\u0CBA\u0CBB\u0CC5\u0CC9\u0CCE-\u0CD4\u0CD7-\u0CDD\u0CDF\u0CE4\u0CE5\u0CF0\u0CF3-\u0CFF\u0D0D\u0D11\u0D45\u0D49\u0D4F-\u0D53\u0D58-\u0D5E\u0D64\u0D65\u0D70-\u0D79\u0D80\u0D84\u0D97-\u0D99\u0DB2\u0DBC\u0DBE\u0DBF\u0DC7-\u0DC9\u0DCB-\u0DCE\u0DD5\u0DD7\u0DE0-\u0DE5\u0DF0\u0DF1\u0DF4-\u0E00\u0E3B-\u0E3F\u0E4F\u0E5A-\u0E80\u0E83\u0E85\u0E8B\u0EA4\u0EA6\u0EBE\u0EBF\u0EC5\u0EC7\u0ECE\u0ECF\u0EDA\u0EDB\u0EE0-\u0EFF\u0F01-\u0F17\u0F1A-\u0F1F\u0F2A-\u0F34\u0F36\u0F38\u0F3A-\u0F3D\u0F48\u0F6D-\u0F70\u0F85\u0F98\u0FBD-\u0FC5\u0FC7-\u0FFF\u104A-\u104F\u109E\u109F\u10C6\u10C8-\u10CC\u10CE\u10CF\u10FB\u1249\u124E\u124F\u1257\u1259\u125E\u125F\u1289\u128E\u128F\u12B1\u12B6\u12B7\u12BF\u12C1\u12C6\u12C7\u12D7\u1311\u1316\u1317\u135B\u135C\u1360-\u137F\u1390-\u139F\u13F6\u13F7\u13FE-\u1400\u166D\u166E\u1680\u169B-\u169F\u16EB-\u16ED\u16F9-\u16FF\u170D\u1715-\u171F\u1735-\u173F\u1754-\u175F\u176D\u1771\u1774-\u177F\u17D4-\u17D6\u17D8-\u17DB\u17DE\u17DF\u17EA-\u180A\u180E\u180F\u181A-\u181F\u1879-\u187F\u18AB-\u18AF\u18F6-\u18FF\u191F\u192C-\u192F\u193C-\u1945\u196E\u196F\u1975-\u197F\u19AC-\u19AF\u19CA-\u19CF\u19DA-\u19FF\u1A1C-\u1A1F\u1A5F\u1A7D\u1A7E\u1A8A-\u1A8F\u1A9A-\u1AA6\u1AA8-\u1AAF\u1AC1-\u1AFF\u1B4C-\u1B4F\u1B5A-\u1B6A\u1B74-\u1B7F\u1BF4-\u1BFF\u1C38-\u1C3F\u1C4A-\u1C4C\u1C7E\u1C7F\u1C89-\u1C8F\u1CBB\u1CBC\u1CC0-\u1CCF\u1CD3\u1CFB-\u1CFF\u1DFA\u1F16\u1F17\u1F1E\u1F1F\u1F46\u1F47\u1F4E\u1F4F\u1F58\u1F5A\u1F5C\u1F5E\u1F7E\u1F7F\u1FB5\u1FBD\u1FBF-\u1FC1\u1FC5\u1FCD-\u1FCF\u1FD4\u1FD5\u1FDC-\u1FDF\u1FED-\u1FF1\u1FF5\u1FFD-\u203E\u2041-\u2053\u2055-\u2070\u2072-\u207E\u2080-\u208F\u209D-\u20CF\u20F1-\u2101\u2103-\u2106\u2108\u2109\u2114\u2116-\u2118\u211E-\u2123\u2125\u2127\u2129\u212E\u213A\u213B\u2140-\u2144\u214A-\u214D\u214F-\u215F\u2189-\u24B5\u24EA-\u2BFF\u2C2F\u2C5F\u2CE5-\u2CEA\u2CF4-\u2CFF\u2D26\u2D28-\u2D2C\u2D2E\u2D2F\u2D68-\u2D6E\u2D70-\u2D7E\u2D97-\u2D9F\u2DA7\u2DAF\u2DB7\u2DBF\u2DC7\u2DCF\u2DD7\u2DDF\u2E00-\u2E2E\u2E30-\u3004\u3008-\u3020\u3030\u3036\u3037\u303D-\u3040\u3097\u3098\u309B\u309C\u30A0\u30FB\u3100-\u3104\u3130\u318F-\u319F\u31C0-\u31EF\u3200-\u33FF\u4DC0-\u4DFF\u9FFD-\u9FFF\uA48D-\uA4CF\uA4FE\uA4FF\uA60D-\uA60F\uA62C-\uA63F\uA673\uA67E\uA6F2-\uA716\uA720\uA721\uA789\uA78A\uA7C0\uA7C1\uA7CB-\uA7F4\uA828-\uA82B\uA82D-\uA83F\uA874-\uA87F\uA8C6-\uA8CF\uA8DA-\uA8DF\uA8F8-\uA8FA\uA8FC\uA92E\uA92F\uA954-\uA95F\uA97D-\uA97F\uA9C1-\uA9CE\uA9DA-\uA9DF\uA9FF\uAA37-\uAA3F\uAA4E\uAA4F\uAA5A-\uAA5F\uAA77-\uAA79\uAAC3-\uAADA\uAADE\uAADF\uAAF0\uAAF1\uAAF7-\uAB00\uAB07\uAB08\uAB0F\uAB10\uAB17-\uAB1F\uAB27\uAB2F\uAB5B\uAB6A-\uAB6F\uABEB\uABEE\uABEF\uABFA-\uABFF\uD7A4-\uD7AF\uD7C7-\uD7CA\uD7FC-\uD7FF\uE000-\uF8FF\uFA6E\uFA6F\uFADA-\uFAFF\uFB07-\uFB12\uFB18-\uFB1C\uFB29\uFB37\uFB3D\uFB3F\uFB42\uFB45\uFBB2-\uFBD2\uFD3E-\uFD4F\uFD90\uFD91\uFDC8-\uFDEF\uFDFC-\uFDFF\uFE10-\uFE1F\uFE30-\uFE32\uFE35-\uFE4C\uFE50-\uFE6F\uFE75\uFEFD-\uFF0F\uFF1A-\uFF20\uFF3B-\uFF3E\uFF40\uFF5B-\uFF65\uFFBF-\uFFC1\uFFC8\uFFC9\uFFD0\uFFD1\uFFD8\uFFD9\uFFDD-\uFFFF]|\uD800[\uDC0C\uDC27\uDC3B\uDC3E\uDC4E\uDC4F\uDC5E-\uDC7F\uDCFB-\uDD3F\uDD75-\uDDFC\uDDFE-\uDE7F\uDE9D-\uDE9F\uDED1-\uDEDF\uDEE1-\uDEFF\uDF20-\uDF2C\uDF4B-\uDF4F\uDF7B-\uDF7F\uDF9E\uDF9F\uDFC4-\uDFC7\uDFD0\uDFD6-\uDFFF]|\uD801[\uDC9E\uDC9F\uDCAA-\uDCAF\uDCD4-\uDCD7\uDCFC-\uDCFF\uDD28-\uDD2F\uDD64-\uDDFF\uDF37-\uDF3F\uDF56-\uDF5F\uDF68-\uDFFF]|\uD802[\uDC06\uDC07\uDC09\uDC36\uDC39-\uDC3B\uDC3D\uDC3E\uDC56-\uDC5F\uDC77-\uDC7F\uDC9F-\uDCDF\uDCF3\uDCF6-\uDCFF\uDD16-\uDD1F\uDD3A-\uDD7F\uDDB8-\uDDBD\uDDC0-\uDDFF\uDE04\uDE07-\uDE0B\uDE14\uDE18\uDE36\uDE37\uDE3B-\uDE3E\uDE40-\uDE5F\uDE7D-\uDE7F\uDE9D-\uDEBF\uDEC8\uDEE7-\uDEFF\uDF36-\uDF3F\uDF56-\uDF5F\uDF73-\uDF7F\uDF92-\uDFFF]|\uD803[\uDC49-\uDC7F\uDCB3-\uDCBF\uDCF3-\uDCFF\uDD28-\uDD2F\uDD3A-\uDE7F\uDEAA\uDEAD-\uDEAF\uDEB2-\uDEFF\uDF1D-\uDF26\uDF28-\uDF2F\uDF51-\uDFAF\uDFC5-\uDFDF\uDFF7-\uDFFF]|\uD804[\uDC47-\uDC65\uDC70-\uDC7E\uDCBB-\uDCCF\uDCE9-\uDCEF\uDCFA-\uDCFF\uDD35\uDD40-\uDD43\uDD48-\uDD4F\uDD74\uDD75\uDD77-\uDD7F\uDDC5-\uDDC8\uDDCD\uDDDB\uDDDD-\uDDFF\uDE12\uDE38-\uDE3D\uDE3F-\uDE7F\uDE87\uDE89\uDE8E\uDE9E\uDEA9-\uDEAF\uDEEB-\uDEEF\uDEFA-\uDEFF\uDF04\uDF0D\uDF0E\uDF11\uDF12\uDF29\uDF31\uDF34\uDF3A\uDF45\uDF46\uDF49\uDF4A\uDF4E\uDF4F\uDF51-\uDF56\uDF58-\uDF5C\uDF64\uDF65\uDF6D-\uDF6F\uDF75-\uDFFF]|\uD805[\uDC4B-\uDC4F\uDC5A-\uDC5D\uDC62-\uDC7F\uDCC6\uDCC8-\uDCCF\uDCDA-\uDD7F\uDDB6\uDDB7\uDDC1-\uDDD7\uDDDE-\uDDFF\uDE41-\uDE43\uDE45-\uDE4F\uDE5A-\uDE7F\uDEB9-\uDEBF\uDECA-\uDEFF\uDF1B\uDF1C\uDF2C-\uDF2F\uDF3A-\uDFFF]|\uD806[\uDC3B-\uDC9F\uDCEA-\uDCFE\uDD07\uDD08\uDD0A\uDD0B\uDD14\uDD17\uDD36\uDD39\uDD3A\uDD44-\uDD4F\uDD5A-\uDD9F\uDDA8\uDDA9\uDDD8\uDDD9\uDDE2\uDDE5-\uDDFF\uDE3F-\uDE46\uDE48-\uDE4F\uDE9A-\uDE9C\uDE9E-\uDEBF\uDEF9-\uDFFF]|\uD807[\uDC09\uDC37\uDC41-\uDC4F\uDC5A-\uDC71\uDC90\uDC91\uDCA8\uDCB7-\uDCFF\uDD07\uDD0A\uDD37-\uDD39\uDD3B\uDD3E\uDD48-\uDD4F\uDD5A-\uDD5F\uDD66\uDD69\uDD8F\uDD92\uDD99-\uDD9F\uDDAA-\uDEDF\uDEF7-\uDFAF\uDFB1-\uDFFF]|\uD808[\uDF9A-\uDFFF]|\uD809[\uDC6F-\uDC7F\uDD44-\uDFFF]|[\uD80A\uD80B\uD80E-\uD810\uD812-\uD819\uD824-\uD82B\uD82D\uD82E\uD830-\uD833\uD837\uD839\uD83D\uD83F\uD87B-\uD87D\uD87F\uD885-\uDB3F\uDB41-\uDBFF][\uDC00-\uDFFF]|\uD80D[\uDC2F-\uDFFF]|\uD811[\uDE47-\uDFFF]|\uD81A[\uDE39-\uDE3F\uDE5F\uDE6A-\uDECF\uDEEE\uDEEF\uDEF5-\uDEFF\uDF37-\uDF3F\uDF44-\uDF4F\uDF5A-\uDF62\uDF78-\uDF7C\uDF90-\uDFFF]|\uD81B[\uDC00-\uDE3F\uDE80-\uDEFF\uDF4B-\uDF4E\uDF88-\uDF8E\uDFA0-\uDFDF\uDFE2\uDFE5-\uDFEF\uDFF2-\uDFFF]|\uD821[\uDFF8-\uDFFF]|\uD823[\uDCD6-\uDCFF\uDD09-\uDFFF]|\uD82C[\uDD1F-\uDD4F\uDD53-\uDD63\uDD68-\uDD6F\uDEFC-\uDFFF]|\uD82F[\uDC6B-\uDC6F\uDC7D-\uDC7F\uDC89-\uDC8F\uDC9A-\uDC9C\uDC9F-\uDFFF]|\uD834[\uDC00-\uDD64\uDD6A-\uDD6C\uDD73-\uDD7A\uDD83\uDD84\uDD8C-\uDDA9\uDDAE-\uDE41\uDE45-\uDFFF]|\uD835[\uDC55\uDC9D\uDCA0\uDCA1\uDCA3\uDCA4\uDCA7\uDCA8\uDCAD\uDCBA\uDCBC\uDCC4\uDD06\uDD0B\uDD0C\uDD15\uDD1D\uDD3A\uDD3F\uDD45\uDD47-\uDD49\uDD51\uDEA6\uDEA7\uDEC1\uDEDB\uDEFB\uDF15\uDF35\uDF4F\uDF6F\uDF89\uDFA9\uDFC3\uDFCC\uDFCD]|\uD836[\uDC00-\uDDFF\uDE37-\uDE3A\uDE6D-\uDE74\uDE76-\uDE83\uDE85-\uDE9A\uDEA0\uDEB0-\uDFFF]|\uD838[\uDC07\uDC19\uDC1A\uDC22\uDC25\uDC2B-\uDCFF\uDD2D-\uDD2F\uDD3E\uDD3F\uDD4A-\uDD4D\uDD4F-\uDEBF\uDEFA-\uDFFF]|\uD83A[\uDCC5-\uDCCF\uDCD7-\uDCFF\uDD4C-\uDD4F\uDD5A-\uDFFF]|\uD83B[\uDC00-\uDDFF\uDE04\uDE20\uDE23\uDE25\uDE26\uDE28\uDE33\uDE38\uDE3A\uDE3C-\uDE41\uDE43-\uDE46\uDE48\uDE4A\uDE4C\uDE50\uDE53\uDE55\uDE56\uDE58\uDE5A\uDE5C\uDE5E\uDE60\uDE63\uDE65\uDE66\uDE6B\uDE73\uDE78\uDE7D\uDE7F\uDE8A\uDE9C-\uDEA0\uDEA4\uDEAA\uDEBC-\uDFFF]|\uD83C[\uDC00-\uDD2F\uDD4A-\uDD4F\uDD6A-\uDD6F\uDD8A-\uDFFF]|\uD83E[\uDC00-\uDFEF\uDFFA-\uDFFF]|\uD869[\uDEDE-\uDEFF]|\uD86D[\uDF35-\uDF3F]|\uD86E[\uDC1E\uDC1F]|\uD873[\uDEA2-\uDEAF]|\uD87A[\uDFE1-\uDFFF]|\uD87E[\uDE1E-\uDFFF]|\uD884[\uDF4B-\uDFFF]|\uDB40[\uDC00-\uDCFF\uDDF0-\uDFFF]/g

// headingIDs gives each h1 to h6 without an id the id GitHub gives it, as rehype-slug does: the
// text in lower case without punctuation, spaces as -, and -1, -2 and so on for a repeat.
function headingIDs() {
  return (tree) => {
    const seen = new Map()
    eachElement(tree, (node) => {
      if (!headingTag.test(node.tagName) || node.properties?.id) return
      const base = textOf(node).toLowerCase().replace(slugRemoved, '').replace(/ /g, '-')
      let id = base
      while (seen.has(id)) {
        const n = seen.get(base) + 1
        seen.set(base, n)
        id = `${base}-${n}`
      }
      seen.set(id, 0)
      node.properties = { ...node.properties, id }
    })
  }
}

// urlProperties lists the properties of the elements whose relative URLs rewriteURLs rewrites,
// with what a message calls each.
const urlProperties = {
  img: [['src', 'image']],
  a: [['href', 'link']],
  video: [
    ['src', 'video'],
    ['poster', 'poster'],
  ],
  audio: [['src', 'audio']],
  source: [['src', 'source']],
}

// notRelative matches a URL that starts with /, # or a scheme, which rewriteURLs leaves alone.
const notRelative = /^(?:[/#]|[a-zA-Z][a-zA-Z0-9+.-]*:)/

// rewriteURLs replaces the relative URLs of the body with the URLs of their files: a file next to
// the Markdown, or else next to the version of the entry in the default language. The file must be
// inside the folder of the collection. A link to a Markdown file is an error: its URL is the one
// of the page that shows it, which only the app knows.
function rewriteURLs() {
  return (tree, file) => {
    const state = contentFiles.get(file.data)
    if (!state) return // a processor outside convertContent
    eachElement(tree, (node) => {
      for (const [prop, kind] of urlProperties[node.tagName] ?? []) {
        const value = node.properties?.[prop]
        if (typeof value !== 'string' || value === '' || notRelative.test(value)) continue
        const url = state.resolve(value, kind, node.position?.start)
        if (url !== undefined) node.properties[prop] = url
      }
    })
    if (state.problems.length > 0) {
      const err = new Error(state.problems.join('\n'))
      err.tifaLines = state.problems
      throw err
    }
  }
}

// collectHeadings records the depth, the id and the text of each h1 to h6, in order.
function collectHeadings() {
  return (tree, file) => {
    const state = contentFiles.get(file.data)
    if (!state) return
    eachElement(tree, (node) => {
      if (headingTag.test(node.tagName)) {
        state.headings.push({ depth: Number(node.tagName[1]), id: String(node.properties?.id ?? ''), text: textOf(node) })
      }
    })
  }
}

// exists returns the fs.Stats of the file at p with its path, undefined when there is none.
function exists(p) {
  try {
    return Object.assign(fs.statSync(p), { path: p })
  } catch (err) {
    if (err.code === 'ENOENT' || err.code === 'ENOTDIR') return undefined
    throw err
  }
}

// within reports whether p is dir or a path below it.
function within(dir, p) {
  const rel = path.relative(dir, p)
  return rel === '' || (!rel.startsWith('..') && !path.isAbsolute(rel))
}

/**
 * convertContent turns entries, as tifa lists them in .tifa/content/entries.json, into HTML. It
 * returns the HTML and the headings of each by its out, and the warnings of the plugins. asset
 * returns the URL of a file a relative URL leads to, given its real path. Every problem of every
 * entry fails it at once, one line each, as file:line:col: reason.
 */
export async function convertContent(root, entries, { markdown, command, asset, packages }) {
  const processor = markdownProcessor(packages ?? (await loadMarkdown(root, command)), markdown)
  const realRoot = fs.realpathSync(root)
  const result = { html: {}, headings: {}, warnings: [] }
  const problems = []
  for (const entry of entries) {
    const collection = path.join(root, 'content', entry.collection)
    const state = {
      headings: [],
      problems: [],
      resolve(url, kind, at) {
        const place = at ? `${entry.source}:${at.line}:${at.column}` : entry.source
        const fail = (msg) => {
          state.problems.push(`${place}: ${msg}`)
        }
        const cut = url.search(/[?#]/)
        const rest = cut < 0 ? '' : url.slice(cut)
        let rel
        try {
          rel = decodeURIComponent(cut < 0 ? url : url.slice(0, cut))
        } catch {
          return fail(`${kind} ${url} is not a valid URL`)
        }
        if (/\.md$/i.test(rel)) {
          let stem = path.posix.basename(rel, path.posix.extname(rel))
          if (stem === 'index') stem = path.posix.basename(path.posix.dirname(path.posix.normalize(rel))) || stem
          const page = ['', entry.lang, entry.collection, stem].filter((s, i) => i === 0 || s).join('/')
          return fail(`${url} is a Markdown file and has no URL of its own: link to the page that shows it, such as ${page}`)
        }
        const shown = (p) => path.relative(root, p).split(path.sep).join('/')
        // The place next to the entry first. The URL is written for it, so leading out of the
        // collection from there is an error.
        const near = path.join(root, path.dirname(entry.source), rel)
        if (!within(collection, near)) return fail(`${kind} ${url} leads to ${shown(near)}, which is outside content/${entry.collection}: a collection holds only its own files`)
        const looked = [near]
        let found = exists(near)
        // Then the place next to the version in the default language, which may sit at another
        // depth: it counts only when the file next to the entry is missing.
        let away = ''
        if (found === undefined && entry.fallback) {
          const far = path.join(root, entry.fallback, rel)
          if (within(collection, far)) {
            looked.push(far)
            found = exists(far)
          } else {
            away = ` (next to the default language it leads to ${shown(far)}, which is outside content/${entry.collection})`
          }
        }
        if (found === undefined) return fail(`${kind} ${url} not found: looked at ${looked.map(shown).join(' and ')}${away}`)
        if (found.isDirectory()) return fail(`${kind} ${url} is a folder, which has no URL of its own`)
        const real = fs.realpathSync(found.path)
        if (!within(fs.realpathSync(collection), real)) {
          return fail(`${kind} ${url} links to ${real}, which is outside content/${entry.collection}: a collection holds only its own files`)
        }
        return asset(real) + rest
      },
    }
    // The vfile unified makes keeps this data object, by which the steps of tifa find state.
    const data = { tifa: { collection: entry.collection, lang: entry.lang, id: entry.id, frontmatter: entry.frontmatter } }
    contentFiles.set(data, state)
    let out
    try {
      out = await processor.process({ value: entry.text, path: path.join(realRoot, entry.source), data })
    } catch (err) {
      problems.push(...contentError(entry, err))
      continue
    }
    result.html[entry.out] = String(out)
    result.headings[entry.out] = state.headings
    for (const m of out.messages) result.warnings.push(`${messagePlace(entry, m)}: ${m.reason ?? m.message}`)
  }
  if (problems.length > 0) throw new Error(problems.join('\n'))
  return result
}

// messagePlace is file:line:col of a message of unified, file alone when it has no place.
function messagePlace(entry, m) {
  const at = m.place?.start ?? m.place ?? (m.line ? { line: m.line, column: m.column } : undefined)
  return at?.line ? `${entry.source}:${at.line}:${at.column ?? 1}` : entry.source
}

// contentError returns the lines that say why the conversion of entry failed with err.
function contentError(entry, err) {
  if (Array.isArray(err?.tifaLines)) return err.tifaLines
  const reason = err?.reason ?? err?.message ?? String(err)
  return [`${messagePlace(entry, err ?? {})}: ${err?.tifaPlugin ? err.tifaPlugin + ': ' : ''}${reason}`]
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
 * Layout stays in the entry script: the root route needs its documentHead, and the entries its
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
              notRecognized +
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
// lazyRouteComponent imported from router. routes.tsx reads default, head and Error of
// a route module (TestHeadModulesReadOnlyWhatTheirStandInExports in internal/codegen holds it to
// that). A stand-in for an export the module lacks is never read, and the bundler leaves it out.
export function lazyRouteModule(router, module) {
  return (
    `import { lazyRouteComponent } from ${JSON.stringify(router)}\n` +
    `const load = () => import(${JSON.stringify(module)})\n` +
    'export default lazyRouteComponent(load)\n' +
    'const error = /* @__PURE__ */ lazyRouteComponent(load, "Error")\n' +
    'export { error as Error }\n' +
    'export const head = (context) => load().then((module) => module.head(context))\n'
  )
}

// generated resolves ./route.gen, the imports that lead to what tifa generates. Other
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

// resolveRouteModule resolves ./route.gen the way the generated tsconfig lets TypeScript see it:
// rootDirs merges the application root and .tifa, so a relative import whose last element is
// route.gen, with or without .js or .ts, and which points into app/<dir> is
// .tifa/app/<dir>/route.gen.ts, whatever the path in between.
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
      `${source}: ${path.dirname(rel).split(path.sep).join('/')} has no generated route.gen module: ` +
        'it is made for a directory with page.tsx, layout.tsx, layout_.tsx or a Layout, or a method handler in route.go',
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
              notRecognized +
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
            notRecognized +
            `@radix-ui/react-slot ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has no mergeProps the tifa ` +
              'plugin knows, so Slot tests a regexp for each prop it merges and builds class names with filter and ' +
              'join. Pages render the same, but take longer to render',
          )
        }
        return merged ?? null
      }
      if (clsxModule.test(id)) {
        const faster = clsxFast(fs.readFileSync(id, 'utf8'))
        if (!faster) {
          this.warn(
            notRecognized +
            `clsx ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has a form the tifa plugin does not know, so ` +
              'clsx reads each argument with a call of a function even for a string. Pages render the same, but ' +
              'take longer to render. clsx 2.0.1 to 2.1.1 is changed',
          )
        }
        return faster ?? null
      }
      if (memoThemeModule.test(id)) {
        const source = fs.readFileSync(id, 'utf8')
        const byContent = memoThemeByContent(source)
        if (!byContent && source.includes('unstable_memoTheme')) {
          const pkg = path.resolve(id, '..', ...(/[/\\](?:esm|modern)[/\\]memoTheme\.js$/.test(id) ? ['..'] : []))
          this.warn(
            notRecognized +
            `@mui/system ${packageVersion(pkg)}: ${id} has no memoTheme the tifa plugin knows, so memoTheme makes ` +
              'the styles of MUI again for each new theme object, which ThemeProvider makes on every render. Pages ' +
              'render the same, but take longer to render',
          )
        }
        return byContent ?? null
      }
      if (reactI18nextModule.test(id)) {
        const source = fs.readFileSync(id, 'utf8')
        const copied = reactI18nextCopy(source)
        if (!copied && source.includes('createI18nWrapper')) {
          this.warn(
            notRecognized +
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
            notRecognized +
            `${i18next} has no fixedT the tifa plugin knows, so the functions getFixedT returns copy all ` +
              'options on every call. Pages render the same, but translations take longer',
          )
        }
        if (unknown.includes('looksLikeObjectPath')) {
          this.warn(
            notRecognized +
            `${i18next} has no looksLikeObjectPath the tifa plugin knows, so it builds its regexp on every ` +
              'call. Pages render the same, but translations take longer',
          )
        }
        const faster = unknown.length === 2 ? null : source
        if (!settings.i18nextCache) return faster
        const cached = i18nextCache(faster ?? original)
        if (cached === undefined) {
          this.warn(
            notRecognized +
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
            notRecognized +
            `@emotion/hash ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has no murmur2 the tifa plugin knows, ` +
              'so emotion hashes its styles in JavaScript. Pages render the same, but styles take longer to serialize',
          )
          return null
        }
        return source.replace(murmur2Source, () => hashInGo(murmur2Source))
      }
      if (reactJsxRuntime.test(id)) {
        const text = fs.readFileSync(id, 'utf8')
        const unread = unreadJsxPieces(this.parse(text), text)
        jsxRuntimes.set(id, unread.length === 0)
        if (unread.length > 0) {
          this.warn(
            notRecognized +
            `react ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has a jsx the tifa plugin does not know (${unread.join(', ')}), so ` +
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
            notRecognized +
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
        // The parts of the file are read in the text as React wrote it, before the changes below.
        const { unread, problems: unreadable } = unreadReactDOMUnits(this.parse(source), source)
        const asked = askEngineToEscape(source)
        if (asked) {
          source = asked
        } else {
          this.warn(
            notRecognized +
            `${react} has no matchHtmlRegExp and escapeTextForBrowser in the form the tifa plugin knows, so ` +
              'the tifa plugin does not ask the engine to escape the HTML React writes. Pages render the same, ' +
              'but texts and attribute values take longer to render',
          )
        }
        const joined = joinSegmentChunks(source)
        if (joined) {
          source = joined
        } else {
          this.warn(
            notRecognized +
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
            notRecognized +
            `${react} has no pushStringAttribute, pushAttribute and isAttributeNameSafe in the form the tifa ` +
              'plugin knows, so the tifa plugin does not write the common attributes without calling the ' +
              'functions that write one string attribute and check an attribute name. Pages render the same, ' +
              'but take longer to render',
          )
        }
        const attributeNames = inlined && cacheAttributeNames(source)
        if (attributeNames) {
          source = attributeNames
        } else if (inlined) {
          this.warn(
            notRecognized +
            `${react} has no pushAttribute in the form the tifa plugin knows, so the tifa plugin does not keep the ` +
              'names of the attributes it has seen. Pages render the same, but take longer to render',
          )
        }
        // A syntax tree goes only with the source it was parsed from: each change below parses
        // the source the changes before it left.
        const parse = (code) => this.parse(code)
        // One decision for the names of components and for the key paths: both are left out only when
        // every part of the file is one that was read to the end (reviewedReactDOM).
        if (unread.length > 0 || unreadable.length > 0) {
          this.warn(
            notRecognized +
              `${react} has ${unread.length > 0 ? `${unread.length} parts the tifa plugin has not read: ${namesOfUnits(unread)}` : unreadable.join(', ')}, ` +
              'so the tifa plugin does not leave the names of components or the key paths out of the elements. Pages render the ' +
              'same, but take longer to render. docs/build.md says how to read a new release',
          )
        } else {
          const read = keyPathNamesRead(parse(source))
          const unnamed = !read && dropKeyPathNames(source)
          if (unnamed) {
            source = unnamed
            namesDropped = true
            const used = keyPathsUsed(parse(source))
            const unkeyed = !used && dropKeyPaths(source)
            if (unkeyed) {
              source = unkeyed
              keyPathsDropped = true
            } else {
              this.warn(
                notRecognized +
                  `${react} ${used || 'has no key paths in the form the tifa plugin knows'}, so the tifa plugin does not ` +
                  'leave out the key paths of the elements. Pages render the same, but take longer to render',
              )
            }
          } else {
            this.warn(
              notRecognized +
                `${react} ${read || `has no "${keyPathName.trim().split('\n')[0]}" the tifa plugin knows`}, ` +
                'so the tifa plugin does not leave the names of components out of their key paths. Pages render the ' +
                'same, but take longer to render',
            )
          }
        }
        const guarded = guardSwitches(source, parse)
        if (guarded.switches.length > 0) {
          source = guarded.code
        } else {
          this.warn(
            notRecognized +
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
            notRecognized +
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
            notRecognized +
            `${react} ${stacksRead || 'has no pushComponentStack in the form the tifa plugin knows'}, so the tifa ` +
              'plugin does not keep React from building component stacks. Pages render the ' +
              'same, but take longer to render',
          )
        }
        const forwarded = unstacked && inlineNodeCalls(source, parse)
        if (forwarded) {
          source = forwarded
        } else if (unstacked) {
          this.warn(
            notRecognized +
            `${react} has no renderNodeDestructive in the form the tifa plugin knows, or no call of it to write out, ` +
              'so the tifa plugin does not hand nodes to retryNode without calling it. Pages render the same, but take ' +
              'longer to render',
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
            notRecognized +
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
          notRecognized +
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
      if (plain.warning) this.warn(notRecognized + plain.warning)
      // Only with a JSX runtime of which every file is known: with another, the elements might differ.
      const elements =
        jsxRuntimes.size > 0 && [...jsxRuntimes.values()].every(Boolean)
          ? inlineElements(plain.code, (source) => this.parse(source))
          : { code: plain.code }
      // The calls of small functions, last, so the functions are the ones the rewrites above left.
      let small = { code: elements.code, warning: '' }
      try {
        small = inlineSmallFunctions(elements.code, (source) => this.parse(source))
      } catch (err) {
        small.warning = `the tifa plugin could not write out the calls of small functions: ${err.stack ?? err}`
      }
      if (small.warning) this.warn(notRecognized + small.warning)
      // The code keeps the lines it had, so the source map fits it as it is.
      return small.code === code ? null : { code: small.code, map: null }
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
          notRecognized +
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
            notRecognized +
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
            notRecognized +
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
          notRecognized +
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

// askEngineToEscape returns the source of React's string renderer with escapeTextForBrowser asking
// the engine first, or null when the source does not have it in the form it knows. React escapes
// every text and attribute value it writes. __tifa_escape, which internal/ssr defines in each VM, is
// that function written in C in the engine and gives the same text for every argument (the tests
// compare it with React's function on every UTF-16 code unit, among other things).
// escapeTextForBrowser becomes the engine's function itself when the VM has it, so a call of it is
// one call, and React's own function otherwise, which is how the bundle runs where the VM has none.
// That is a variable and not a function declaration, so the plugin changes the source only when its
// name does not come before the declaration, which would call it before it holds the function.
function askEngineToEscape(source) {
  if (source.split(escapeSource).length !== 2) return null
  // The name's first place in the source is the declaration's.
  if (source.indexOf('escapeTextForBrowser') !== source.indexOf(escapeSource) + escapeSource.indexOf('escapeTextForBrowser')) return null
  const asked = `matchHtmlRegExp = /["'&<>]/;
var escapeTextForBrowser = "function" === typeof __tifa_escape ? __tifa_escape : tifaEscapeTextForBrowser;
function tifaEscapeTextForBrowser(text) {`
  return source.replace(escapeSource, () => sameLines(escapeSource, asked))
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

// clsxFast returns the source of clsx with the function that joins the class names doing for a call
// of up to six arguments what the old one does in a loop, without the loop or the arguments object
// and without a call for a string, or undefined when the source is not the one it knows. For each
// argument, in order, a falsy one adds nothing, a string is its own text, and any other value goes
// to r, the function the old loop calls for it, so an object, an array, a number or a function is
// read, called and written as before. The texts join with a space between the non-empty ones, as
// clsx writes them. A call with more than six arguments goes to the old function with its
// arguments, before anything else happens. Six is what class-variance-authority passes (cva calls
// clsx with five), and the calls in the MUI test app have up to five. Only the length of the function differs, 6 for 0.
function clsxFast(source) {
  if (source !== clsxSource) return undefined
  const join = (arg, first) =>
    `if(${arg}){t="string"==typeof ${arg}?${arg}:r(${arg});if(t)s=${first ? 't' : 's?s+" "+t:t'}}`
  const fast =
    'export function clsx(a,b,c,d,e,g){if(arguments.length>6)return tifaClsx.apply(null,arguments);var s="",t;' +
    `${join('a', true)}${join('b')}${join('c')}${join('d')}${join('e')}${join('g')}return s}`
  return source.replace('export function clsx(){for(var ', () => `${fast}function tifaClsx(){for(var `)
}

// memoThemeByContent returns the source of memoTheme of @mui/system with styleMemoized comparing,
// in place of the theme, the first theme it saw with the same content, or undefined when the source
// does not have unstable_memoTheme in a form it knows. Only a plain object, whose prototype is
// Object.prototype, as every theme of MUI is, is compared by content: a prototype of its own may
// have getters that read something of each theme, such as a WeakMap keyed by it, so any other
// theme stands for itself, as before. Two plain themes have the same content when they have the
// same own keys in the same order, each a data property with the same enumerability and the same
// value (Object.is). ThemeProvider of MUI makes such a theme on each
// render on the server, with the same objects in it, so the styles memoTheme made on the first
// render are taken again. That needs what memoTheme in the browser and MUI already ask: that a theme
// and the objects in it do not change after a render used them. A theme with an accessor is its own
// theme, as before. tifaSameTheme keeps the last memoThemeSize themes it saw, the one it saw last at
// the front, with the theme each stands for and its content when it was first seen. A theme it no
// longer keeps that comes again stands for the first kept theme of its content, or for itself, so
// the styles are made again at worst. The rest of memoTheme stays, and each change stays on its line.
export function memoThemeByContent(source) {
  if (source.split(memoThemeArg).length !== 2) return undefined
  const known = ['preprocessStyles', '(0, _preprocessStyles.default)']
    .map(memoThemeSource)
    .find((text) => source.split(text).length === 2)
  if (!known) return undefined
  const changed = known
    .replace('    let value = lastValue;\n', () => '    let value = lastValue, tifaTheme = tifaSameTheme(props.theme);\n')
    .replace('props.theme !== lastTheme', () => 'tifaTheme !== lastTheme')
    .replace('      lastTheme = props.theme;\n', () => '      lastTheme = tifaTheme;\n')
  return source.replace(known, () => changed) + memoThemeHelpers
}
const memoThemeHelpers = `
var tifaThemes = [], tifaThemeLast, tifaThemeLastSame;
var tifaThemeKeys = Reflect.ownKeys, tifaThemeProto = Reflect.getPrototypeOf, tifaThemeOwn = Reflect.getOwnPropertyDescriptor, tifaThemeIs = Object.is, tifaThemeObject = Object.prototype;
function tifaThemeContent(theme) {
  if (tifaThemeProto(theme) !== tifaThemeObject) return null;
  const keys = tifaThemeKeys(theme), values = [], hidden = [];
  for (let i = 0; i < keys.length; i++) {
    const d = tifaThemeOwn(theme, keys[i]);
    if (!('value' in d)) return null;
    values[i] = d.value;
    hidden[i] = !d.enumerable;
  }
  return { keys, values, hidden };
}
function tifaThemeSameContent(a, b) {
  const n = a.keys.length;
  if (b.keys.length !== n) return false;
  for (let i = 0; i < n; i++) if (a.keys[i] !== b.keys[i] || a.hidden[i] !== b.hidden[i] || !tifaThemeIs(a.values[i], b.values[i])) return false;
  return true;
}
function tifaSameTheme(theme) {
  if (theme === tifaThemeLast) return tifaThemeLastSame;
  if (typeof theme !== 'object' || theme === null) return theme;
  const themes = tifaThemes;
  let entry;
  for (let i = 0; i < themes.length; i++) {
    if (themes[i].theme === theme) {
      entry = themes[i];
      if (i > 0) { themes.splice(i, 1); themes.unshift(entry); }
      break;
    }
  }
  if (entry === undefined) {
    entry = { theme, same: theme, content: tifaThemeContent(theme) };
    if (entry.content !== null) {
      for (let i = 0; i < themes.length; i++) {
        const seen = themes[i];
        if (seen.content !== null && tifaThemeSameContent(seen.content, entry.content)) {
          entry.same = seen.same;
          entry.content = seen.content;
          break;
        }
      }
    }
    themes.unshift(entry);
    if (themes.length > ${memoThemeSize}) themes.pop();
  }
  tifaThemeLast = theme;
  tifaThemeLastSame = entry.same;
  return entry.same;
}
`

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

// cacheAttributeNames returns the source of React's string renderer, after inlineAttributeCalls, with
// pushAttribute writing an attribute whose name it has seen with one lookup of the name, or null when
// the source does not have pushAttribute in the form it knows.
function cacheAttributeNames(source) {
  if (source.split(attributeStart).length !== 2 || source.split(attributeDefault).length !== 2) return null
  const written = '("string" === typeof value || "function" !== typeof value && "symbol" !== typeof value && "boolean" !== typeof value)'
  const start =
    'function pushAttribute(target, name, value) { var tifaChunk; ' +
    `if ("className" === name) { ${written} && target.push(' class="', escapeTextForBrowser(value), '"'); return; } ` +
    'if ("style" === name) { pushStyleAttribute(target, value); return; } ' +
    'if (void 0 !== (tifaChunk = tifaAttributeChunks.get(name))) { if ("" === tifaChunk) return; ' +
    'if ("string" === typeof value || "number" === typeof value) ' +
    "{ target.push(tifaChunk, escapeTextForBrowser(value), '\"'); return; } }\n  switch (name) {"
  // A name that starts with on writes nothing, of any value, and is kept with an empty chunk.
  const checked = `    default:
      if (
        !(2 < name.length) ||
        ("o" !== name[0] && "O" !== name[0]) ||
        ("n" !== name[1] && "N" !== name[1]) ||
        (tifaAttributeChunks.size < ${attributeNamesKept} && tifaAttributeChunks.set(name, ""), !1)
      )
        if (((tifaChunk = name), (name = aliases.get(name) || name), hasOwnProperty.call(validatedAttributeNameCache, name) || isAttributeNameSafe(name))) { tifaAttributeChunks.size < ${attributeNamesKept} && tifaAttributeChunks.set(tifaChunk, ' ' + name + '="');`
  return (
    source
      .replace(attributeStart, () => start)
      .replace(attributeDefault, () => sameLines(attributeDefault, checked)) + attributeChunks
  )
}

// inlineNodeCalls returns the source of React's string renderer, after dropComponentStacks, with the
// calls of renderNodeDestructive written out as its body, or null when the source has the function
// in another form than nodeForwarder or no call to write out. Every node that React renders goes
// through renderNodeDestructive, a function that only hands the node on to retryNode, so each node
// costs a call more than it needs. A call changes when its first two arguments are names, its other two
// are a name, a number or a property of a name, which have no effect on the task, and no one reads
// its value: it is a statement, or one of the branches of a conditional or the parts of a sequence
// that make one, or the value of a return, which becomes a return of nothing, as the function
// returns. No function around it declares resumeNode or retryNode. renderNodeDestructive stays for
// the calls that do not change.
function inlineNodeCalls(source, parse) {
  const definition = /function renderNodeDestructive\(request, task, node, childIndex\) \{[^]*?\n\}\n/.exec(source)
  const collapse = (text) => text.replace(/\s+/g, ' ').replace(/\( /g, '(').replace(/ \)/g, ')')
  if (!definition || collapse(definition[0]).trim() !== nodeForwarder) return null
  const simple = (arg) =>
    arg.type === 'Identifier' ||
    (arg.type === 'Literal' && typeof arg.value === 'number') ||
    (arg.type === 'UnaryExpression' && arg.operator === '-' && arg.argument.type === 'Literal' && typeof arg.argument.value === 'number') ||
    (arg.type === 'MemberExpression' && !arg.computed && arg.object.type === 'Identifier' && arg.property.type === 'Identifier')
  const changes = []
  const visit = (node, ancestors) => {
    if (
      node.type === 'CallExpression' &&
      node.callee.type === 'Identifier' &&
      node.callee.name === 'renderNodeDestructive' &&
      node.arguments.length === 4 &&
      node.arguments[0].type === 'Identifier' &&
      node.arguments[1].type === 'Identifier' &&
      node.arguments.slice(2).every(simple)
    ) {
      // Whether no one reads the value of the call: its statement, a branch of a conditional or a
      // part of a sequence that is one, and the part of a sequence that is not the last.
      let child = node
      let mode = null
      for (let i = ancestors.length - 1; i >= 0 && !mode; i--) {
        const parent = ancestors[i]
        if (parent.type === 'ExpressionStatement') mode = 'discarded'
        else if (parent.type === 'ReturnStatement') {
          if (child === node) mode = 'returned'
          break
        } else if (parent.type === 'ConditionalExpression' && child !== parent.test) child = parent
        else if (parent.type === 'SequenceExpression') {
          if (child !== parent.expressions.at(-1)) mode = 'discarded'
          else child = parent
        } else break
      }
      const shadowed = ancestors.some((a) => {
        if (!functionTypes.has(a.type)) return false
        const names = declaredNames(a)
        return names.has('resumeNode') || names.has('retryNode')
      })
      if (mode && !shadowed) {
        const [request, task, id, index] = node.arguments.map((arg) => source.slice(arg.start, arg.end))
        const body =
          `null !== ${task}.replay && "number" === typeof ${task}.replay.slots ? ` +
          `resumeNode(${request}, ${task}, ${task}.replay.slots, ${id}, ${index}) : ` +
          `((${task}.node = ${id}), (${task}.childIndex = ${index}), retryNode(${request}, ${task}))`
        const parent = ancestors.at(-1)
        changes.push(
          mode === 'returned'
            ? { start: parent.start, end: parent.end, text: `{ ${body}; return; }` }
            : { start: node.start, end: node.end, text: `(${body})` },
        )
      }
    }
    for (const value of Object.values(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child, [...ancestors, node])
      }
    }
  }
  visit(parse(source), [])
  if (changes.length === 0) return null
  changes.sort((a, b) => b.start - a.start)
  let code = source
  for (const { start, end, text } of changes) {
    code = code.slice(0, start) + sameLines(code.slice(start, end), text) + code.slice(end)
  }
  return code
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

// fingerprintOf returns the first 12 digits of the SHA-256 of the structure of node, a part of the
// program that scopes (analyzeScopes) read. The structure is the syntax tree without its places, its
// comments, the quotes of its strings and the brackets round its expressions, so it is the same for
// two texts that mean the same, and different for two that parse differently: what a line break
// does to `return`, or what a letter next to a keyword does to it, shows in the tree. A name that a
// declaration inside node gives (a parameter, a variable, a function inside) is written as the
// number of its first place in node, so the numbers React's compiler puts on such names do not
// matter, and two names stay two, except the names that are seen from outside (those of functions
// and classes, the name of a variable that an anonymous function is set to, a name that both a
// catch parameter and a var of its body declare), which stay. Every other name is written as it is, with a mark for the
// kind: a property name (.a$1), a name that a declaration outside node gives (a$1@o), and a name no
// declaration gives (a$1@g), so a global that an outer declaration comes to hide differs.
function fingerprintOf(node, scopes, declared) {
  const local = (scope) => scope.kind !== 'module' && scope.node && scope.node.start >= node.start && scope.node.end <= node.end
  const numbers = new Map()
  const out = []
  // The local names that stay as they are, since renaming them would change what code can see or do:
  //   - the name of a function or class (its `name` property), also the name that a variable gives
  //     to the anonymous function or class it is set to,
  //   - a name that a catch clause has as its parameter and a var of its body declares, since the
  //     var's initializer writes the parameter and not the variable of the function.
  const verbatim = new Set()
  const anonymous = (e) => (e.type === 'FunctionExpression' || e.type === 'ClassExpression') && !e.id || e.type === 'ArrowFunctionExpression'
  const bindingOf = (id) => declared.get(id) ?? scopes.refs.get(id)?.binding ?? null
  const find = (n) => {
    if (n.type === 'VariableDeclarator' && n.id.type === 'Identifier' && n.init && anonymous(n.init)) verbatim.add(bindingOf(n.id))
    if ((n.type === 'AssignmentExpression' || n.type === 'AssignmentPattern') && (n.left.type === 'Identifier') && anonymous(n.right)) {
      verbatim.add(bindingOf(n.left))
    }
    for (const child of childNodes(n)) find(child)
  }
  find(node)
  for (const binding of scopes.all) {
    if (binding.kind === 'function' || binding.kind === 'class' || binding.kind === 'funcname') verbatim.add(binding)
    if (binding.kind !== 'catch' || !local(binding.scope)) continue
    const body = binding.defs[0].catch.body
    for (let scope = binding.scope.parent; scope; scope = scope.parent) {
      const other = scope.vars.get(binding.name)
      if (!other) continue
      if (other.kind === 'var' && other.ids.some((id) => id.start >= body.start && id.end <= body.end)) {
        verbatim.add(binding)
        verbatim.add(other)
      }
      break
    }
  }
  const identifier = (id, parent, key) => {
    if (!isReference(parent, key)) return `.${id.name}`
    const binding = bindingOf(id)
    if (binding === null) return `${id.name}@g`
    if (!local(binding.scope)) return `${id.name}@o`
    if (verbatim.has(binding)) return `${id.name}@v`
    if (!numbers.has(binding)) numbers.set(binding, numbers.size)
    return `#${numbers.get(binding)}`
  }
  const visit = (value, parent, key) => {
    if (value === null || value === undefined) {
      out.push('~')
    } else if (typeof value !== 'object') {
      out.push(`${typeof value}:${typeof value === 'string' ? JSON.stringify(value) : String(value)}`)
    } else if (Array.isArray(value)) {
      out.push('[')
      for (const element of value) {
        visit(element, parent, key)
        out.push(',')
      }
      out.push(']')
    } else if (value.type === 'Identifier') {
      out.push(identifier(value, parent, key))
    } else {
      out.push('(', value.type ?? '')
      for (const k of Object.keys(value).sort()) {
        // The raw text of a literal is its spelling, which its value says. That of a template is not.
        if (k === 'type' || k === 'start' || k === 'end' || (k === 'raw' && value.type === 'Literal')) continue
        out.push(' ', k, '=')
        visit(value[k], value, k)
      }
      out.push(')')
    }
  }
  visit(node, null, null)
  return createHash('sha256').update(out.join('')).digest('hex').slice(0, 12)
}

// reactDomUnits reads React's string renderer, program parsed from source, as { units, problems }.
// units are the parts of the file, each with a name for messages, the fingerprint of its structure
// (fingerprintOf) and where it is in the source: every function declaration, every declarator of a
// variable and every other statement at the top of the file, and one more that stands for their
// order and kinds, since the top of the file runs in that order. problems say why the file cannot be
// read (a with statement, a direct eval).
export function reactDomUnits(program, source) {
  const scopes = analyzeScopes(program, source)
  if (scopes.unsupported) return { units: [], problems: [`the file has ${scopes.unsupported}`] }
  const declared = new Map()
  for (const binding of scopes.all) for (const id of binding.ids) declared.set(id, binding)
  const units = []
  const order = []
  const add = (name, node) => {
    const fingerprint = fingerprintOf(node, scopes, declared)
    units.push({ name, fingerprint, start: node.start, end: node.end })
    return fingerprint
  }
  for (const statement of program.body) {
    if (statement.type === 'FunctionDeclaration') {
      add(statement.id.name, statement)
      order.push(`function ${statement.id.name}`)
    } else if (statement.type === 'VariableDeclaration') {
      order.push(statement.kind)
      for (const d of statement.declarations) order.push(add(`${statement.kind} ${d.id.type === 'Identifier' ? d.id.name : '(pattern)'}`, d))
    } else {
      order.push(add(statement.type, statement))
    }
  }
  units.push({ name: 'the order of the top level', fingerprint: createHash('sha256').update(order.join('\n')).digest('hex').slice(0, 12), start: 0, end: 0 })
  return { units, problems: [] }
}

// The pairs of reviewedReactDOM, at the end of this file, as a map of each name to the set of its
// fingerprints: a name has more than one when versions differ in it. A unit is reviewed when its
// name and its fingerprint are a pair of the table, so a body that is the reviewed body of another
// name is not.
let reviewedUnits = null
export function reviewedReactDOMUnits() {
  if (!reviewedUnits) {
    reviewedUnits = new Map()
    for (const line of reviewedReactDOM.split('\n')) {
      const [fingerprint, ...name] = line.trim().split(' ')
      if (!fingerprint) continue
      if (!reviewedUnits.has(name.join(' '))) reviewedUnits.set(name.join(' '), new Set())
      reviewedUnits.get(name.join(' ')).add(fingerprint)
    }
  }
  return reviewedUnits
}

// unreadReactDOMUnits returns the units of React's string renderer, program parsed from source, whose
// name and fingerprint are not a pair of reviewedReactDOM, and the problems of reactDomUnits. When the list is empty, every part
// of the file is one that was read to the end for what it does with a key path.
export function unreadReactDOMUnits(program, source) {
  const { units, problems } = reactDomUnits(program, source)
  const reviewed = reviewedReactDOMUnits()
  return { unread: units.filter((u) => !reviewed.get(u.name)?.has(u.fingerprint)), problems }
}

// namesOfUnits writes the names of units for a warning: the first twenty, then how many more.
function namesOfUnits(units) {
  const names = units.map((u) => u.name ?? u)
  return names.length <= 20 ? names.join(', ') : `${names.slice(0, 20).join(', ')} and ${names.length - 20} more`
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

// jsxRuntimePieces reads the production file of React's JSX runtime, program parsed from source, and
// returns { pieces, problems }: the pieces inlineElements depends on, each with the fingerprint of
// its structure, and why the file is not one whose pieces can be trusted. The pieces are the
// declaration of REACT_ELEMENT_TYPE, the function jsxProd and the statements
// `exports.jsx = jsxProd` and `exports.jsxs = jsxProd` (reviewedJsxPieces has them). Nothing else may touch them, or run before them, so the file may
// only hold, at its top, these statements:
//   - a directive such as "use strict",
//   - a function declaration (it only runs when something calls it, and nothing at the top does),
//   - a variable that is given nothing, a literal or Symbol.for("a string"),
//   - `exports.name = value`, where value is a name, a function or a literal.
// Anything else at the top is run in a place where it could change the pieces (a call, a condition,
// a loop, an assignment to another object). Every use of exports, module or arguments, and every
// top-level `this`, which is exports in a CommonJS module, must be one of the export statements.
// REACT_ELEMENT_TYPE and jsxProd are set once and never again, by a pattern or an update or in any
// other place.
export function jsxRuntimePieces(program, source) {
  const scopes = analyzeScopes(program, source)
  if (scopes.unsupported) return { pieces: new Map(), problems: [`the file has ${scopes.unsupported}`] }
  const declared = new Map()
  for (const binding of scopes.all) for (const id of binding.ids) declared.set(id, binding)
  const problems = []
  const pieces = new Map()
  const piece = (name, node) => {
    while (pieces.has(name)) name += '#'
    pieces.set(name, fingerprintOf(node, scopes, declared))
  }
  const free = (id) => !declared.has(id) && !scopes.refs.get(id)?.binding
  const allowed = new Set() // the identifiers `exports` of the export statements
  const symbolFor = (e) =>
    e.type === 'CallExpression' &&
    e.callee.type === 'MemberExpression' &&
    !e.callee.computed &&
    e.callee.object.type === 'Identifier' &&
    e.callee.object.name === 'Symbol' &&
    e.callee.property.name === 'for' &&
    e.arguments.every((a) => a.type === 'Literal' && typeof a.value === 'string')
  for (const statement of program.body) {
    const e = statement.expression
    if (statement.type === 'ExpressionStatement' && typeof statement.directive === 'string') {
      continue
    } else if (statement.type === 'FunctionDeclaration') {
      if (statement.id.name === 'jsxProd') piece('jsxProd', statement)
    } else if (statement.type === 'VariableDeclaration') {
      for (const d of statement.declarations) {
        const plain = !d.init || (d.init.type === 'Literal' && !d.init.regex) || symbolFor(d.init)
        if (d.id.type !== 'Identifier' || !plain) problems.push('a variable at the top of the file with a value that runs code')
        if (d.id.type === 'Identifier' && d.id.name === 'REACT_ELEMENT_TYPE') piece('REACT_ELEMENT_TYPE', d)
      }
    } else if (
      statement.type === 'ExpressionStatement' &&
      e.type === 'AssignmentExpression' &&
      e.operator === '=' &&
      e.left.type === 'MemberExpression' &&
      !e.left.computed &&
      e.left.object.type === 'Identifier' &&
      e.left.object.name === 'exports' &&
      free(e.left.object) &&
      /^(Identifier|FunctionExpression|ArrowFunctionExpression|Literal)$/.test(e.right.type)
    ) {
      allowed.add(e.left.object)
      if (e.left.property.name === 'jsx' || e.left.property.name === 'jsxs') piece(`exports.${e.left.property.name}`, statement)
    } else {
      problems.push(`a statement at the top of the file that is ${statement.type}`)
    }
  }
  for (const [id, ref] of scopes.refs) {
    if (ref.binding) continue
    if ((id.name === 'exports' && !allowed.has(id)) || id.name === 'module' || id.name === 'arguments') problems.push(`a use of ${id.name}`)
  }
  const thisAtTop = (node, inFunction) => {
    if (node.type === 'ThisExpression' && !inFunction) problems.push('a use of this at the top of the file')
    const inside = inFunction || (functionTypes.has(node.type) && node.type !== 'ArrowFunctionExpression')
    for (const child of childNodes(node)) thisAtTop(child, inside)
  }
  thisAtTop(program, false)
  for (const name of ['jsxProd', 'REACT_ELEMENT_TYPE']) {
    const binding = scopes.root.vars.get(name)
    if (!binding || binding.defs.length !== 1 || binding.assigns !== 0 || binding.loop) problems.push(`${name} is not declared once and never set`)
  }
  return { pieces, problems: [...new Set(problems)] }
}

// unreadJsxPieces returns the names of the pieces of the JSX runtime that are not as
// reviewedJsxPieces has them, and the problems of jsxRuntimePieces: empty when inlineElements may
// write the elements of the runtime.
function unreadJsxPieces(program, source) {
  const { pieces, problems } = jsxRuntimePieces(program, source)
  const names = new Set([...reviewedJsxPieces.keys(), ...pieces.keys()])
  return [...[...names].filter((name) => pieces.get(name) !== reviewedJsxPieces.get(name)), ...problems]
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
// the pieces of that file are ones the plugin knows, see unreadJsxPieces. The symbol is taken once, in a variable declared
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

// ---- Writing out the calls of small functions

// A function whose body has at most this many syntax nodes counts as small.
const smallFunctionNodes = 32

// The most times inlineSmallFunctions goes over the code. A call in the body of a small function, or
// in an argument of a call, is written out in a later round.
const smallFunctionRounds = 2

// scopeOf makes a scope. kind is 'module', 'function' (also the scope of a static block or a field),
// 'block', 'funcname' (the name of a function expression, seen only inside it) or 'class'.
function scopeOf(parent, kind, node = null) {
  return { parent, kind, node, vars: new Map(), all: parent ? parent.all : [] }
}

// declare adds a declaration of name to scope and returns the binding it belongs to. A name
// declared twice in a scope is one binding with two definitions. def says what declared it:
// { function }, { declarator }, { param }, { pattern }, { loop }, { class } or { implicit }.
function declare(scope, name, kind, def, id = null) {
  let binding = scope.vars.get(name)
  if (!binding) {
    binding = { name, scope, kind, defs: [], assigns: 0, loop: false, refs: [], ids: [] }
    scope.vars.set(name, binding)
    scope.all.push(binding)
  }
  if (id) binding.ids.push(id)
  binding.defs.push(def)
  if (def.loop) binding.loop = true
  return binding
}

// stableBinding reports whether the value of binding is the same wherever it is read once it has
// one: a single declaration, set once with its value, a parameter or a function nobody assigns to.
function stableBinding(binding) {
  return binding !== null && binding.defs.length === 1 && binding.assigns === 0 && !binding.loop && binding.kind !== 'import'
}

// boundIdentifiers returns the identifier nodes that the pattern node declares or assigns.
function boundIdentifiers(pattern, out = []) {
  switch (pattern?.type) {
    case 'Identifier':
      out.push(pattern)
      break
    case 'ArrayPattern':
      for (const e of pattern.elements) boundIdentifiers(e, out)
      break
    case 'ObjectPattern':
      for (const p of pattern.properties) boundIdentifiers(p.type === 'Property' ? p.value : p, out)
      break
    case 'AssignmentPattern':
      boundIdentifiers(pattern.left, out)
      break
    case 'RestElement':
      boundIdentifiers(pattern.argument, out)
      break
  }
  return out
}

// endsStatement reports whether the statement node, which the source code holds, ends in a way that
// the next line cannot run into: a semicolon, or the closing brace of a block, a function, a class,
// a try or a switch. The end of a node is exact, so a semicolon in a comment is not one.
function endsStatement(node, code) {
  const last = code[node.end - 1]
  if (last === ';') return true
  const inner = node.type.startsWith('Export') && node.declaration ? node.declaration : node
  return last === '}' && ['BlockStatement', 'FunctionDeclaration', 'ClassDeclaration', 'TryStatement', 'SwitchStatement'].includes(inner.type)
}

// analyzeScopes reads the syntax tree program of an ES module, whose source is code, and gives its
// scopes: for each reference to a name (refs, by the identifier node) the binding it reaches, for
// each binding what declares it and how many times code assigns to it, the names that no declaration
// gives (unresolved), the calls whose callee is a name (calls), where code surely runs after a
// declaration (safeRange), and where a statement starts that the line before may run into
// (risky). A function with a parameter that is a pattern or has a default value has a scope for its
// parameters and one inside it for its body. It stops at what it does not know (a with statement, a
// direct eval) with unsupported set, since names are not what they look like there.
function analyzeScopes(program, code) {
  const root = scopeOf(null, 'module', program)
  const refs = new Map()
  const unresolved = new Set()
  const calls = []
  const callParents = new Map()
  const declared = new Map() // a function declaration to its binding
  const safeRange = new Map() // a declarator or class to where code surely runs after it has run
  const risky = new Set() // where an expression statement starts that the line before may run into
  const result = { root, all: root.all, refs, unresolved, calls, callParents, declared, safeRange, risky, functions: new Map(), unsupported: null }
  let hoisted = null // the function declarations around the code being read, innermost first
  let loops = 0 // the loops around the code being read, inside its function

  const lookup = lookupBinding
  const addRef = (id, scope, assign, parent, key) => {
    const binding = lookup(scope, id.name)
    const ref = { id, scope, binding, assign, parent, key, hoisted }
    refs.set(id, ref)
    if (binding) {
      binding.refs.push(ref)
      if (assign) binding.assigns++
    } else {
      unresolved.add(id.name)
    }
    return ref
  }

  // The declarations of a list of statements that belong to its scope: let, const, class and
  // function declarations, and what an import brings.
  // end is where the list ends, or -1 when the code after a statement of it may not run.
  const declareLexical = (list, scope, end) => {
    for (const [i, outer] of list.entries()) {
      let s = outer
      if (s.type === 'ExportNamedDeclaration' || s.type === 'ExportDefaultDeclaration') s = s.declaration
      if (outer.type === 'ExpressionStatement' && i > 0 && !endsStatement(list[i - 1], code)) risky.add(outer.start)
      if (!s) continue
      if (end >= 0 && s.type === 'VariableDeclaration') for (const d of s.declarations) safeRange.set(d, [outer.end, end])
      if (end >= 0 && s.type === 'ClassDeclaration') safeRange.set(s, [outer.end, end])
      if (s.type === 'FunctionDeclaration' && s.id) declare(scope, s.id.name, 'function', { function: s }, s.id)
      else if (s.type === 'ClassDeclaration' && s.id) declare(scope, s.id.name, 'class', { class: s }, s.id)
      else if (s.type === 'VariableDeclaration' && s.kind !== 'var') declareVariables(s, scope, s.kind)
      else if (s.type === 'ImportDeclaration') {
        for (const spec of s.specifiers) declare(scope, spec.local.name, 'import', { import: spec }, spec.local)
      }
    }
  }
  // A var that something sets again, or whose initializer sets another variable of its name, is
  // not a variable of one value: all the variables of the name around are marked.
  const unsteady = (scope, name) => {
    for (let s = scope; s; s = s.parent) {
      const b = s.vars.get(name)
      if (b) b.loop = true
    }
  }
  const declareVariables = (declaration, scope, kind, loop = false) => {
    for (const d of declaration.declarations) {
      for (const id of boundIdentifiers(d.id)) {
        const def = d.id.type === 'Identifier' ? { declarator: d, loop } : { pattern: d, loop }
        declare(scope, id.name, kind, def, id)
      }
    }
  }
  // The var declarations anywhere in the statements of a function or module, not in the functions
  // and classes inside them.
  const hoistVars = (node, scope) => {
    if (!node) return
    switch (node.type) {
      case 'VariableDeclaration':
        if (node.kind === 'var') declareVariables(node, scope, 'var')
        break
      case 'BlockStatement':
      case 'StaticBlock':
        for (const s of node.body) hoistVars(s, scope)
        break
      case 'IfStatement':
        hoistVars(node.consequent, scope)
        hoistVars(node.alternate, scope)
        break
      case 'ForStatement':
        hoistVars(node.init, scope)
        hoistVars(node.body, scope)
        break
      case 'ForInStatement':
      case 'ForOfStatement':
        if (node.left.type === 'VariableDeclaration' && node.left.kind === 'var') declareVariables(node.left, scope, 'var', true)
        hoistVars(node.body, scope)
        break
      case 'WhileStatement':
      case 'DoWhileStatement':
      case 'LabeledStatement':
        hoistVars(node.body, scope)
        break
      case 'TryStatement':
        hoistVars(node.block, scope)
        hoistVars(node.handler?.body, scope)
        hoistVars(node.finalizer, scope)
        break
      case 'SwitchStatement':
        for (const c of node.cases) for (const s of c.consequent) hoistVars(s, scope)
        break
      case 'ExportNamedDeclaration':
        hoistVars(node.declaration, scope)
        break
      case 'WithStatement':
        result.unsupported ??= 'a with statement'
        break
    }
  }

  // The expressions inside a declaration's pattern: default values and computed keys.
  const visitPatternExpressions = (pattern, scope) => {
    switch (pattern?.type) {
      case 'ArrayPattern':
        for (const e of pattern.elements) visitPatternExpressions(e, scope)
        break
      case 'ObjectPattern':
        for (const p of pattern.properties) {
          if (p.type === 'RestElement') visitPatternExpressions(p.argument, scope)
          else {
            if (p.computed) visit(p.key, scope, p, 'key')
            visitPatternExpressions(p.value, scope)
          }
        }
        break
      case 'AssignmentPattern':
        visitPatternExpressions(pattern.left, scope)
        visit(pattern.right, scope, pattern, 'right')
        break
      case 'RestElement':
        visitPatternExpressions(pattern.argument, scope)
        break
    }
  }
  // A pattern that code assigns to: its names are written, the rest of it is read.
  const visitTarget = (pattern, scope, parent, key) => {
    switch (pattern?.type) {
      case 'Identifier':
        addRef(pattern, scope, true, parent, key)
        break
      case 'ArrayPattern':
        for (const e of pattern.elements) visitTarget(e, scope, pattern, 'elements')
        break
      case 'ObjectPattern':
        for (const p of pattern.properties) {
          if (p.type === 'RestElement') visitTarget(p.argument, scope, p, 'argument')
          else {
            if (p.computed) visit(p.key, scope, p, 'key')
            visitTarget(p.value, scope, p, 'value')
          }
        }
        break
      case 'AssignmentPattern':
        visitTarget(pattern.left, scope, pattern, 'left')
        visit(pattern.right, scope, pattern, 'right')
        break
      case 'RestElement':
        visitTarget(pattern.argument, scope, pattern, 'argument')
        break
      default:
        visit(pattern, scope, parent, key)
    }
  }

  const visitFunction = (node, scope, parent, key) => {
    let outer = scope
    if (node.type === 'FunctionExpression' && node.id) {
      outer = scopeOf(scope, 'funcname', node)
      declare(outer, node.id.name, 'funcname', { function: node }, node.id)
    }
    // With a parameter that is a pattern or has a default value, the parameters have a scope of
    // their own, and the body a scope inside it that the default values do not see.
    const plain = node.params.every((p) => p.type === 'Identifier')
    const ps = scopeOf(outer, plain ? 'function' : 'params', node)
    const fs = plain ? ps : scopeOf(ps, 'function', node)
    ps.arrow = fs.arrow = node.type === 'ArrowFunctionExpression'
    if (!ps.arrow) declare(ps, 'arguments', 'arguments', { implicit: true })
    for (const p of node.params) for (const id of boundIdentifiers(p)) declare(ps, id.name, 'param', { param: node }, id)
    if (node.body.type === 'BlockStatement') {
      declareLexical(node.body.body, fs, node.body.end)
      hoistVars(node.body, fs)
    }
    result.functions.set(node, ps)
    if (node.type === 'FunctionDeclaration' && node.id) declared.set(node, scope.vars.get(node.id.name))
    const outerHoisted = hoisted
    const outerLoops = loops
    if (node.type === 'FunctionDeclaration') hoisted = { node, up: hoisted }
    loops = 0
    for (const p of node.params) visitPatternExpressions(p, ps)
    if (node.body.type === 'BlockStatement') for (const s of node.body.body) visit(s, fs, node.body, 'body')
    else visit(node.body, fs, node, 'body')
    hoisted = outerHoisted
    loops = outerLoops
  }

  const visitClass = (node, scope) => {
    const cs = scopeOf(scope, 'class', node)
    if (node.id) declare(cs, node.id.name, 'class', { class: node }, node.id)
    if (node.superClass) visit(node.superClass, cs, node, 'superClass')
    for (const m of node.body.body) {
      if (m.computed) visit(m.key, cs, m, 'key')
      if (m.type === 'StaticBlock') {
        const fs = scopeOf(cs, 'function', m)
        declareLexical(m.body, fs, m.end)
        hoistVars(m, fs)
        for (const s of m.body) visit(s, fs, m, 'body')
      } else if (m.type === 'MethodDefinition') {
        visit(m.value, cs, m, 'value')
      } else if (m.value) {
        const fs = scopeOf(cs, 'function', m)
        visit(m.value, fs, m, 'value')
      }
    }
  }

  const visitChildren = (node, scope) => {
    for (const key in node) {
      if (key === 'type' || key === 'start' || key === 'end') continue
      const value = node[key]
      if (Array.isArray(value)) {
        for (const child of value) if (child && typeof child.type === 'string') visit(child, scope, node, key)
      } else if (value && typeof value.type === 'string') {
        visit(value, scope, node, key)
      }
    }
  }

  const visit = (node, scope, parent, key) => {
    switch (node.type) {
      case 'Identifier':
        if (isReference(parent, key)) addRef(node, scope, false, parent, key)
        return
      case 'FunctionDeclaration':
      case 'FunctionExpression':
      case 'ArrowFunctionExpression':
        visitFunction(node, scope, parent, key)
        return
      case 'ClassDeclaration':
      case 'ClassExpression':
        visitClass(node, scope)
        return
      case 'BlockStatement': {
        const bs = scopeOf(scope, 'block', node)
        declareLexical(node.body, bs, node.end)
        for (const s of node.body) visit(s, bs, node, 'body')
        return
      }
      case 'ForStatement': {
        const bs = scopeOf(scope, 'block', node)
        if (node.init?.type === 'VariableDeclaration') declareLexical([node.init], bs, -1)
        loops++
        visitChildren(node, bs)
        loops--
        return
      }
      case 'ForInStatement':
      case 'ForOfStatement': {
        const bs = scopeOf(scope, 'block', node)
        if (node.left.type === 'VariableDeclaration') {
          if (node.left.kind !== 'var') declareVariables(node.left, bs, node.left.kind, true)
          for (const d of node.left.declarations) visitPatternExpressions(d.id, bs)
        } else {
          visitTarget(node.left, bs, node, 'left')
        }
        visit(node.right, bs, node, 'right')
        loops++
        visit(node.body, bs, node, 'body')
        loops--
        return
      }
      case 'SwitchStatement': {
        visit(node.discriminant, scope, node, 'discriminant')
        const bs = scopeOf(scope, 'block', node)
        declareLexical(
          node.cases.flatMap((c) => c.consequent),
          bs,
          -1,
        )
        for (const c of node.cases) {
          if (c.test) visit(c.test, bs, c, 'test')
          for (const s of c.consequent) visit(s, bs, c, 'consequent')
        }
        return
      }
      case 'CatchClause': {
        const cs = scopeOf(scope, 'block', node)
        if (node.param) {
          for (const id of boundIdentifiers(node.param)) declare(cs, id.name, 'catch', { catch: node }, id)
          visitPatternExpressions(node.param, cs)
        }
        visit(node.body, cs, node, 'body')
        return
      }
      case 'WhileStatement':
      case 'DoWhileStatement':
        loops++
        visitChildren(node, scope)
        loops--
        return
      case 'VariableDeclaration':
        for (const d of node.declarations) {
          visitPatternExpressions(d.id, scope)
          if (d.init) visit(d.init, scope, d, 'init')
          if (node.kind === 'var') {
            for (const id of boundIdentifiers(d.id)) {
              const b = lookup(scope, id.name)
              // In a loop the variable is set again. In a catch clause with a parameter of the same name
              // the initializer sets the parameter and the variable stays undefined.
              if (loops > 0 || (d.init && !b?.defs.some((def) => def.declarator === d || def.pattern === d))) unsteady(scope, id.name)
            }
          }
        }
        return
      case 'AssignmentExpression':
        visitTarget(node.left, scope, node, 'left')
        visit(node.right, scope, node, 'right')
        return
      case 'UpdateExpression':
        visitTarget(node.argument, scope, node, 'argument')
        return
      case 'Property':
        if (node.shorthand) visit(node.value, scope, node, 'value')
        else visitChildren(node, scope)
        return
      case 'MetaProperty':
      case 'ImportDeclaration':
        return
      case 'ExportNamedDeclaration':
        if (node.declaration) visit(node.declaration, scope, node, 'declaration')
        else if (!node.source) for (const spec of node.specifiers) visit(spec.local, scope, spec, 'local')
        return
      case 'WithStatement':
        result.unsupported ??= 'a with statement'
        return
      case 'CallExpression':
        if (node.callee.type === 'Identifier') {
          calls.push(node)
          callParents.set(node, parent)
          if (node.callee.name === 'eval' && !lookup(scope, 'eval')) result.unsupported ??= 'a direct eval'
        }
        visitChildren(node, scope)
        return
      default:
        visitChildren(node, scope)
    }
  }

  declareLexical(program.body, root, program.end)
  for (const s of program.body) hoistVars(s, root)
  for (const s of program.body) visit(s, root, program, 'body')
  return result
}

// unresolvedNames returns the sorted names that code reads or sets and no declaration of it gives.
// A bundle that runs with the same globals must keep the same list when a rewrite is right.
export function unresolvedNames(code, parse) {
  return [...analyzeScopes(parse(code), code).unresolved].sort()
}

class CannotCopy extends Error {}

// bodyAsExpression gives the value that a call of function node fn returns as one expression, with
// the kind of body: 'expr' (an arrow with an expression), 'stmt' (statements that are all
// expressions, so the call gives nothing), 'empty' or 'return'. It gives null for another form.
// A block with one return, a block of expression statements and a chain of ifs that returns in
// every branch are the forms. The chain becomes a conditional expression.
function bodyAsExpression(fn) {
  const nothing = { type: 'UnaryExpression', operator: 'void', argument: { type: 'Literal', value: 0 } }
  if (fn.body.type !== 'BlockStatement') return { expr: fn.body, kind: 'expr' }
  const list = fn.body.body.filter((s) => !(s.type === 'ExpressionStatement' && typeof s.directive === 'string'))
  if (list.length === 0) return { expr: nothing, kind: 'empty' }
  if (list.every((s) => s.type === 'ExpressionStatement')) {
    const exprs = list.map((s) => s.expression)
    return { expr: exprs.length === 1 ? exprs[0] : { type: 'SequenceExpression', expressions: exprs }, kind: 'stmt' }
  }
  // The value of a list of statements that returns on every path, or null. At the top level the
  // end of the list returns nothing, inside an if it would go on with what follows the if.
  const returned = (stmts, top) => {
    for (const [i, s] of stmts.entries()) {
      if (s.type === 'ReturnStatement') return s.argument ?? nothing
      if (s.type === 'BlockStatement') return returned(s.body, false)
      if (s.type !== 'IfStatement') return null
      const consequent = returned([s.consequent], false)
      const alternate = s.alternate ? returned([s.alternate], false) : returned(stmts.slice(i + 1), top)
      return consequent && alternate ? { type: 'ConditionalExpression', test: s.test, consequent, alternate } : null
    }
    return top ? nothing : null
  }
  const expr = returned(list, true)
  return expr ? { expr, kind: 'return' } : null
}

// childNodes returns the syntax nodes directly under node.
function childNodes(node) {
  const out = []
  for (const key in node) {
    if (key === 'type' || key === 'start' || key === 'end') continue
    const value = node[key]
    if (Array.isArray(value)) {
      for (const child of value) if (child && typeof child.type === 'string') out.push(child)
    } else if (value && typeof value.type === 'string') {
      out.push(value)
    }
  }
  return out
}

// countNodes returns how many syntax nodes node has, itself among them.
function countNodes(node) {
  let n = 1
  for (const child of childNodes(node)) n += countNodes(child)
  return n
}

// hasOptional reports whether node has an optional call or member access, `?.`, in it.
function hasOptional(node) {
  return node.optional === true || childNodes(node).some(hasOptional)
}

// smallFunctionOf reads the function node fn that binding names and gives what its calls need: the
// expression of its body (expr, with its kind), what the expression does in the order it does it
// (events), where it uses each parameter (uses) and the names it reads that are no parameter
// (free). It gives only why when the calls of fn cannot be written out. standIn says whether a
// binding belongs to a stand-in of a Node module.
function smallFunctionOf(fn, binding, scopes, standIn) {
  if (!stableBinding(binding)) return { why: 'the name is set again' }
  if (fn.async || fn.generator) return { why: 'async or a generator' }
  if (fn.params.some((p) => p.type !== 'Identifier')) return { why: 'a parameter that is no plain name' }
  if (new Set(fn.params.map((p) => p.name)).size !== fn.params.length) return { why: 'two parameters of one name' }
  for (const { parent, key } of binding.refs) {
    if (parent?.type === 'NewExpression' && key === 'callee') return { why: 'called with new' }
    if (parent?.type === 'MemberExpression' && key === 'object' && !parent.computed && parent.property.name === 'prototype') return { why: 'has a prototype' }
  }
  const body = bodyAsExpression(fn)
  if (!body) return { why: 'a body of another form' }
  const { expr, kind } = body
  if (countNodes(expr) > smallFunctionNodes) return { why: 'a big body' }
  const fscope = scopes.functions.get(fn)
  const events = []
  const uses = fn.params.map(() => [])
  const free = []
  // The index of the parameter that the name node is, or -1 for a name of the code around.
  const parameterOf = (id) => {
    const ref = scopes.refs.get(id)
    if (!ref) throw new CannotCopy('a name without a reference')
    const b = ref.binding
    if (b === binding) throw new CannotCopy('a function that calls itself')
    if (b && b.scope === fscope && b.kind === 'param') return fn.params.findIndex((p) => p.name === id.name)
    if (b && (b.scope === fscope || b.scope.node === fn)) throw new CannotCopy('a name of the function itself')
    // The error of a stand-in of a Node module names the place that called it (see nodeModuleCode),
    // and a function written out has no place of its own.
    if (b && standIn(b)) throw new CannotCopy('a stand-in of a Node module')
    free.push(ref)
    return -1
  }
  const privateName = (member) => {
    if (member.property.type === 'PrivateIdentifier') throw new CannotCopy('a private name')
  }
  // cond: whether the expression may not be worked out at all.
  const go = (e, cond) => {
    switch (e.type) {
      case 'Literal':
        if (e.regex) events.push({ t: 'new' })
        return
      case 'TemplateLiteral':
        // Each value becomes a string when the next one is worked out.
        for (const x of e.expressions) {
          go(x, cond)
          events.push({ t: 'coerce' })
        }
        return
      case 'Identifier': {
        const i = parameterOf(e)
        if (i >= 0) {
          uses[i].push({ at: events.length, cond })
          events.push({ t: 'use' })
        } else {
          events.push({ t: 'read' })
        }
        return
      }
      case 'MemberExpression':
        privateName(e)
        go(e.object, cond)
        if (e.computed) go(e.property, cond)
        events.push({ t: 'get' })
        return
      case 'ChainExpression':
        return go(e.expression, cond || hasOptional(e.expression))
      case 'CallExpression':
      case 'NewExpression':
        if (e.callee.type === 'MemberExpression') {
          privateName(e.callee)
          go(e.callee.object, cond)
          if (e.callee.computed) go(e.callee.property, cond)
          events.push({ t: 'get' })
        } else {
          go(e.callee, cond)
        }
        for (const a of e.arguments) {
          if (a.type === 'SpreadElement') throw new CannotCopy('a spread')
          go(a, cond)
        }
        events.push({ t: e.type === 'NewExpression' ? 'new' : 'call' })
        return
      case 'BinaryExpression':
        go(e.left, cond)
        go(e.right, cond)
        events.push({ t: e.operator === '===' || e.operator === '!==' ? 'compare' : e.operator === 'in' || e.operator === 'instanceof' ? 'call' : 'coerce' })
        return
      case 'UnaryExpression':
        if (e.operator === 'delete') {
          if (e.argument.type !== 'MemberExpression') throw new CannotCopy('a delete of a name')
          privateName(e.argument)
          go(e.argument.object, cond)
          if (e.argument.computed) go(e.argument.property, cond)
          events.push({ t: 'assign' })
          return
        }
        go(e.argument, cond)
        if (e.operator === '-' || e.operator === '+' || e.operator === '~') events.push({ t: 'coerce' })
        return
      case 'LogicalExpression':
        go(e.left, cond)
        go(e.right, true)
        return
      case 'ConditionalExpression':
        go(e.test, cond)
        go(e.consequent, true)
        go(e.alternate, true)
        return
      case 'SequenceExpression':
        for (const x of e.expressions) go(x, cond)
        return
      case 'ArrayExpression':
        for (const x of e.elements) {
          if (!x) continue
          if (x.type === 'SpreadElement') throw new CannotCopy('a spread')
          go(x, cond)
        }
        events.push({ t: 'new' })
        return
      case 'ObjectExpression':
        for (const p of e.properties) {
          if (p.type !== 'Property' || p.kind !== 'init' || p.method) throw new CannotCopy('a member of an object that is no plain value')
          if (p.computed) {
            go(p.key, cond)
            events.push({ t: 'coerce' }) // the name becomes a property key before the value is worked out
          }
          // { __proto__ } is an own property, which { __proto__: value } is not
          if (p.shorthand && p.key.name === '__proto__') throw new CannotCopy('a shorthand __proto__')
          go(p.value, cond)
        }
        events.push({ t: 'new' })
        return
      case 'AssignmentExpression':
      case 'UpdateExpression': {
        const target = e.type === 'UpdateExpression' ? e.argument : e.left
        const compound = e.type === 'UpdateExpression' || e.operator !== '='
        const logical = e.type === 'AssignmentExpression' && ['||=', '&&=', '??='].includes(e.operator)
        if (target.type === 'Identifier') {
          if (parameterOf(target) >= 0) throw new CannotCopy('an assignment to a parameter')
        } else if (target.type === 'MemberExpression') {
          privateName(target)
          go(target.object, cond)
          if (target.computed) go(target.property, cond)
        } else {
          throw new CannotCopy('an assignment to a pattern')
        }
        if (compound) events.push({ t: 'get' })
        if (e.type === 'AssignmentExpression') go(e.right, cond || logical)
        if (compound && !logical) events.push({ t: 'coerce' })
        events.push({ t: 'assign' })
        return
      }
      default:
        throw new CannotCopy(e.type)
    }
  }
  try {
    go(expr, false)
  } catch (err) {
    if (!(err instanceof CannotCopy)) throw err
    return { why: err.message }
  }
  return { fn, expr, kind, events, uses, free }
}

// isPureArgument says whether the argument node of a call can be written in the body of the
// function as often as the body uses it, or not at all, with nothing else changing: a value, or a
// name that does not change and that the read finds set. A name that no declaration gives would
// have thrown at the call, and only undefined does not. Nor may the argument throw when it is
// worked out.
function isPureArgument(arg, scopes) {
  switch (arg.type) {
    case 'Literal':
      return !arg.regex
    case 'TemplateLiteral':
      return arg.expressions.length === 0
    case 'Identifier': {
      const ref = scopes.refs.get(arg)
      if (ref === undefined) return false
      return ref.binding ? stableBinding(ref.binding) && initialized(ref, scopes) : arg.name === 'undefined'
    }
    case 'UnaryExpression': {
      // An operator that throws for the value is no pure one: +1n does.
      const value = arg.argument
      if (value.type !== 'Literal' || value.regex) return false
      return ['void', '!', 'typeof'].includes(arg.operator) || ((arg.operator === '-' || arg.operator === '+') && typeof value.value === 'number')
    }
    default:
      return false
  }
}

// initialized reports whether code at the reference ref surely finds its binding set, so that a read
// does not throw, finds the value it keeps and finds it again later, and a variable that holds a
// function holds it. A var, a let, a const and a class are set once the code after their statement
// runs. That code is the rest of the list of statements that holds the declaration. A function
// declared in the list can run from anywhere in it, so a read inside one that starts after the
// declaration counts only if every use of that function is in the rest of the list too, directly or
// inside another function of that kind. A parameter with a default value or a pattern is set
// after the ones before it, so it counts only in the body, and so does a binding of a catch
// pattern. Other bindings are known to be set: functions, plain parameters, catch variables.
function initialized(ref, scopes) {
  const b = ref.binding
  const def = b.defs[0]
  if (b.kind === 'param') {
    if (b.scope.kind !== 'params') return true
    for (let s = ref.scope; s; s = s.parent) if (s.kind === 'function' && s.node === b.scope.node) return true
    return false
  }
  if (b.kind === 'catch') return def.catch.param.type === 'Identifier' || ref.id.start >= def.catch.body.start
  if (!['var', 'let', 'const', 'class'].includes(b.kind)) return true
  const range = scopes.safeRange.get(def.declarator ?? def.pattern ?? def.class)
  return range !== undefined && runsAfter(ref, range, scopes, new Set())
}

// runsAfter reports whether the reference ref in the range [from, to] of the source runs only after
// the code before from has run. visiting holds the functions being asked about, which only call
// each other and so add no way in from before.
function runsAfter(ref, [from, to], scopes, visiting) {
  if (ref.id.start < from || ref.id.end > to) return false
  for (let h = ref.hoisted; h; h = h.up) {
    if (h.node.start < from || visiting.has(h.node)) continue
    const binding = scopes.declared.get(h.node)
    if (!binding) return false
    visiting.add(h.node)
    // A function of the module may also be called from outside after the module has run, which is later than the range.
    const ok = binding.refs.every((r) => runsAfter(r, [from, to], scopes, visiting))
    visiting.delete(h.node)
    if (!ok) return false
  }
  return true
}

// canWriteOut says whether the call node, whose callee is the name reference ref, can be replaced
// by the body of the small function info. The value, the effects and their order must stay, the
// names of the body must mean the same at the call, and each argument must be worked out as often
// as before and in the same order.
function canWriteOut(call, ref, info, scopes) {
  const args = call.arguments
  if (call.optional || args.some((a) => a.type === 'SpreadElement')) return false
  const { fn, events, uses, free } = info
  // A name that no declaration gives throws a ReferenceError when the call works it out, unless the body does not use it.
  if (args.some((a) => a.type === 'Identifier' && a.name !== 'undefined' && !scopes.refs.get(a)?.binding)) return false
  const pure = args.map((a) => isPureArgument(a, scopes))
  // An argument beyond the parameters is worked out for its effects, which the body would lose.
  if (args.slice(fn.params.length).some((_, i) => !pure[fn.params.length + i])) return false
  // A function that a variable holds does not exist before the declaration of the variable runs.
  if (!initialized(ref, scopes)) return false
  // A default value of a parameter may read a parameter that is not set yet.
  for (let s = ref.scope; s.kind !== 'function' && s.kind !== 'module'; s = s.parent) {
    if (s.kind === 'params') return false
  }
  for (const f of free) {
    if (lookupBinding(ref.scope, f.id.name) !== f.binding) return false
  }
  let previous = -1
  for (let p = 0; p < fn.params.length; p++) {
    if (p >= args.length || pure[p]) continue
    // An argument with effects is used once, in the place among the others where it was worked
    // out, and not in a branch that the body may not take.
    const [use] = uses[p]
    if (uses[p].length !== 1 || use.cond || use.at < previous) return false
    previous = use.at
  }
  // Before the last of them the body may only read. A call, a conversion or an assignment would
  // happen before the argument is worked out, where the call works it out first. A read of a name
  // or a property goes before it too, which the plugin accepts.
  for (let i = 0; i < previous; i++) {
    if (!readEvents.has(events[i].t)) return false
  }
  return true
}

const readEvents = new Set(['use', 'read', 'get', 'compare'])

// lookupBinding returns the binding that name means in scope, or null for a name that no
// declaration gives.
function lookupBinding(scope, name) {
  for (let s = scope; s; s = s.parent) {
    const binding = s.vars.get(name)
    if (binding) return binding
  }
  return null
}

// bodyText returns the text that stands for the call node in the code: the body of the small
// function info with each parameter replaced by its argument, in parentheses. code is the source
// that holds the function and the call.
function bodyText(code, call, info, scopes) {
  const { fn, expr, kind } = info
  const raw = (a) => code.slice(a.start, a.end)
  const atom = (a) => a.type === 'Identifier' || (a.type === 'Literal' && !a.regex)
  const argText = fn.params.map((_, i) => {
    const a = call.arguments[i]
    if (!a) return '(void 0)'
    // A function or class with no name takes the name of the property or variable it is given to, unless a comma sits in front.
    const unnamed = (a.type === 'FunctionExpression' || a.type === 'ClassExpression') && !a.id
    return atom(a) ? raw(a) : unnamed || a.type === 'ArrowFunctionExpression' ? `(0, ${raw(a)})` : `(${raw(a)})`
  })
  // The source of a part of the body with the parameters replaced. A part that stands alone
  // (level 'top') needs no parentheses, a part of a list or a branch ('item') only if it is a
  // sequence, and a part of anything else ('operand') unless it is a name or a value.
  const render = (node, level) => {
    if (node.start === undefined) {
      switch (node.type) {
        case 'ConditionalExpression':
          return `${render(node.test, 'operand')} ? ${render(node.consequent, 'item')} : ${render(node.alternate, 'item')}`
        case 'SequenceExpression':
          return node.expressions.map((x) => render(x, 'item')).join(', ')
        default:
          return 'void 0'
      }
    }
    const edits = new Map()
    const walk = (n, parent) => {
      if (n.type === 'Identifier') {
        const b = scopes.refs.get(n)?.binding
        if (!b || b.kind !== 'param' || b.scope.node !== fn) return
        const i = fn.params.findIndex((p) => p.name === n.name)
        const arg = call.arguments[i]
        let text = argText[i]
        if (parent?.type === 'CallExpression' && parent.callee === n && arg?.type !== 'Identifier') {
          // A parameter that is called has an empty this: the member of an argument would give it its object.
          text = `(0, ${arg && arg.type !== 'SequenceExpression' ? raw(arg) : text})`
        } else if (parent?.type === 'MemberExpression' && parent.object === n && !parent.computed && arg?.type === 'Literal' && typeof arg.value !== 'string') {
          text = `(${text})` // 1.x is no member of a number
        }
        if (parent?.type === 'Property' && parent.shorthand) text = `${n.name}: ${text}`
        edits.set(n.start, { start: n.start, end: n.end, text })
        return
      }
      for (const child of childNodes(n)) walk(child, n)
    }
    walk(node, null)
    let text = code.slice(node.start, node.end)
    for (const e of [...edits.values()].sort((a, b) => b.start - a.start)) {
      text = text.slice(0, e.start - node.start) + e.text + text.slice(e.end - node.start)
    }
    const bare = level === 'top' || atom(node) || (level === 'item' && node.type !== 'SequenceExpression')
    return bare ? text : `(${text})`
  }
  const inner = render(expr, 'top')
  const parent = scopes.callParents.get(call)
  // A call of a function of statements gives nothing, which a statement or a part of a sequence that
  // is not its last does not use.
  const used = parent?.type !== 'ExpressionStatement' && !(parent?.type === 'SequenceExpression' && parent.expressions.at(-1) !== call)
  const value = kind === 'stmt' && used ? `(${inner}, void 0)` : `(${inner})`
  // A value that is called keeps an empty this, also when it is a member. delete and typeof take a
  // name in parentheses as a name, which delete would not delete as a value and typeof would not throw for.
  const keepsThis =
    (parent?.type === 'CallExpression' && parent.callee === call) ||
    (parent?.type === 'TaggedTemplateExpression' && parent.tag === call) ||
    (parent?.type === 'UnaryExpression' && (parent.operator === 'delete' || parent.operator === 'typeof'))
  return keepsThis ? `(0, ${value})` : value
}

// keepLines returns text, which replaces from, with as many lines as from, or null when it cannot
// have them. A text with fewer lines gets empty lines after it, which no token takes for the end of
// a statement: a line in front of it could end a return before the value. A text with more has its
// lines joined, which a comment of a line, a template or a line continuation would not survive.
function keepLines(from, text) {
  const want = from.split('\n').length - 1
  const have = text.split('\n').length - 1
  if (have <= want) return text + '\n'.repeat(want - have)
  if (/\/\/|`|\\|[\r\u2028\u2029]/.test(text)) return null
  return text.replace(/[ \t]*\n[ \t]*/g, ' ') + '\n'.repeat(want)
}

// standInTest returns a function that says whether a binding is declared in a region of code that
// holds a stand-in of a Node module, which nodeModuleCode makes: its region starts with the ID of
// the module, \0tifa-node:, and ends where the next region starts.
function standInTest(code) {
  const regions = []
  if (code.includes('tifa-node')) {
    const open = /^\/\/(?:#region)? \\0tifa-node:/gm
    const close = /^\/\/(?:#endregion|#region)/gm
    for (let m; (m = open.exec(code)); ) {
      close.lastIndex = m.index + 1
      regions.push([m.index, close.exec(code)?.index ?? code.length])
    }
  }
  return (binding) => {
    if (regions.length === 0) return false
    const def = binding.defs[0]
    const node = def.declarator ?? def.pattern ?? def.function ?? def.class ?? def.param ?? def.import ?? def.catch ?? binding.scope.node
    return regions.some(([from, to]) => node.start >= from && node.start < to)
  }
}

// writeOutRound writes out the calls of small functions in code as far as one round does, and gives
// the new code with the number of calls it wrote out. scopes is analyzeScopes of the code.
function writeOutRound(code, scopes) {
  const standIn = standInTest(code)
  const infos = new Map() // function node to what smallFunctionOf found
  const chosen = []
  for (const call of scopes.calls) {
    const ref = scopes.refs.get(call.callee)
    const binding = ref?.binding
    if (!binding || !['function', 'var', 'let', 'const'].includes(binding.kind) || binding.defs.length !== 1) continue
    const def = binding.defs[0]
    const fn = def.function ?? (functionTypes.has(def.declarator?.init?.type) ? def.declarator.init : null)
    if (!fn) continue
    let info = infos.get(fn)
    if (!info) infos.set(fn, (info = smallFunctionOf(fn, binding, scopes, standIn)))
    if (info.expr && canWriteOut(call, ref, info, scopes)) chosen.push({ call, info })
  }
  // A call with another call in it waits for the next round, when the call in it is written out.
  chosen.sort((a, b) => a.call.start - b.call.start || b.call.end - a.call.end)
  const edits = []
  for (const [i, { call, info }] of chosen.entries()) {
    let holds = false
    for (let j = i + 1; j < chosen.length && chosen[j].call.start < call.end && !holds; j++) holds = chosen[j].call.end <= call.end
    if (holds) continue
    const text = keepLines(code.slice(call.start, call.end), bodyText(code, call, info, scopes))
    // A text in parentheses at the start of a line the line before may run into would be a call of it.
    if (text === null || scopes.risky.has(call.start)) continue
    edits.push({ start: call.start, end: call.end, text })
  }
  let out = ''
  let at = 0
  for (const edit of edits) {
    out += code.slice(at, edit.start) + edit.text
    at = edit.end
  }
  return { code: out + code.slice(at), count: edits.length }
}

// inlineSmallFunctions returns code, the SSR bundle before it is minified, with the calls of
// small functions written out as their body. A function with a body of an expression, a few
// assignments or a chain of ifs that return, of up to smallFunctionNodes syntax nodes, that no
// code sets again, costs a call in QuickJS: a frame, the arguments, a return. The rules are about the
// shape of the function and the call, not about any library. The code keeps its lines. It checks
// its work: if the names that no declaration gives are not the same after, it gives the code as it
// was, with a warning. parse gives the syntax tree of a source. The result has the code, the
// number of calls written out and the warning, which is empty when there is none.
export function inlineSmallFunctions(source, parse) {
  let accepted = { code: source, count: 0, warning: '' }
  let code = source
  let count = 0
  let names = null
  for (let round = 0; ; round++) {
    const scopes = analyzeScopes(parse(code), code)
    const unresolved = [...scopes.unresolved].sort()
    if (names === null) {
      if (scopes.unsupported) return { ...accepted, warning: `the tifa plugin does not write out small functions: the bundle has ${scopes.unsupported}` }
      names = unresolved.join(',')
    } else if (unresolved.join(',') !== names) {
      return { ...accepted, warning: `writing out small functions changed the names that no declaration gives (${names} to ${unresolved.join(',')}), so the tifa plugin left the calls as they were` }
    }
    accepted = { code, count, warning: '' }
    if (round === smallFunctionRounds) return accepted
    const next = writeOutRound(code, scopes)
    if (next.count === 0) return accepted
    code = next.code
    count += next.count
  }
}

// reviewedReactDOM is the parts of React's string renderer, react-dom-server-legacy.browser.production.js,
// that were read to the end for what they do with a key path, by the fingerprint of their structure
// (fingerprintOf) and a name for messages. It is the union of the parts of every version that was
// read: React DOM 19.3.0 and 19.3.1. The plugin leaves the names of components and the key paths out
// only when every part of the file is in it, so a release that changes a function, adds one or
// changes the order of the top of the file keeps both. A comment, white space, brackets, quotes and
// the names a part declares inside it do not change a fingerprint. The name of a part itself and the
// names of the other parts it uses are in it as they are. See "Reviewing a new React" in docs/build.md.
const reviewedReactDOM = `
5f9a49f5957c ExpressionStatement
cabd180d5036 var React
a18ecbf18d71 var ReactDOM
ed29811fe2c2 formatProdErrorMessage
0ab98ba119d2 var REACT_ELEMENT_TYPE
90b665218edd var REACT_PORTAL_TYPE
e45a423912da var REACT_FRAGMENT_TYPE
fef1d983ba11 var REACT_STRICT_MODE_TYPE
4a491d60e4f4 var REACT_PROFILER_TYPE
f08beafd3a7a var REACT_CONSUMER_TYPE
7b40e02350b9 var REACT_CONTEXT_TYPE
62daef5eb741 var REACT_FORWARD_REF_TYPE
21dfb8621cc0 var REACT_SUSPENSE_TYPE
60f13b181aa6 var REACT_SUSPENSE_LIST_TYPE
7b26339ff7e3 var REACT_MEMO_TYPE
29a40641c853 var REACT_LAZY_TYPE
f1e47de9af92 var REACT_SCOPE_TYPE
a2dfcf9833d2 var REACT_ACTIVITY_TYPE
bd40d6e17632 var REACT_LEGACY_HIDDEN_TYPE
db935584e0c5 var REACT_MEMO_CACHE_SENTINEL
d1bb8ee20e94 var REACT_VIEW_TRANSITION_TYPE
cd53040814aa var REACT_RECOVERABLE_TYPE
16affa8d660f var MAYBE_ITERATOR_SYMBOL
d44321ea3f31 getIteratorFn
875241de6e79 var REACT_OPTIMISTIC_KEY
8513e482a12f var isArrayImpl
b05a53df50c8 murmurhash3_32_gc
62a8ba22fbba var assign
3666fc74894c var hasOwnProperty
f78b6d778ddc var VALID_ATTRIBUTE_NAME_REGEX
bba81056b403 var illegalAttributeNameCache
828e144d697b var validatedAttributeNameCache
a23c76a779b7 isAttributeNameSafe
9f9bdc238fa0 var unitlessNumbers
01491c691ce0 var aliases
2b531192e2f5 var matchHtmlRegExp
76ca748aa2e2 escapeTextForBrowser
4d854d34a5f9 var uppercasePattern
1d2f94195a31 var msPattern
85d8128656c9 var isJavaScriptProtocol
13e955663f01 sanitizeURL
df7e6a08558b var ReactSharedInternals
4b7e720c43a3 var ReactDOMSharedInternals
ffab9b05e975 var sharedNotPendingObject
57b3b1f81307 var previousDispatcher
9a2790ec85b2 ExpressionStatement
69a6983860d5 var PRELOAD_NO_CREDS
4e2e84366ee8 var currentlyFlushingRenderState
cf94bc69b674 var scriptRegex
7535aa887463 scriptReplacer
c6deadc88428 createResumableState
048afce938a8 createFormatContext
5b1d053bfa22 getChildFormatContext
05976c892453 getSuspenseViewTransition
29a449389537 getSuspenseFallbackFormatContext
02a742819e41 getSuspenseContentFormatContext
c66a5c495e64 makeId
0eed888afcde pushViewTransitionAttributes
98df3d1aa9a5 var styleNameCache
1953870e1a22 pushStyleAttribute
716d94842101 pushBooleanAttribute
0aa84577f447 pushStringAttribute
53c63ec8142a var actionJavaScriptURL
a5857d2b13db pushAdditionalFormField
d7bb7afe5ac1 validateAdditionalFormField
b11b5bc12d67 getCustomFormFields
c230d701ac3f pushFormActionAttribute
73db916dd96d pushAttribute
d039c52b1d34 pushInnerHTML
be12141bb26e flattenOptionChildren
9399d66e1594 injectFormReplayingRuntime
51cb24778c09 pushLinkImpl
85a352410248 var styleRegex
0cb2c3e520d4 styleReplacer
54fb3bd7e1fd pushSelfClosing
91971158b031 pushTitleImpl
bbf5fbef5032 pushScriptImpl
4b087fb1f926 pushStartSingletonElement
77ae7c46e034 pushStartGenericElement
bd975359093f var VALID_TAG_REGEX
cc0e7336f060 var validatedTagCache
64876d5bfa9c startChunkForTag
0961ceedef65 pushStartInstance
11da83b8ed99 var endTagCache
04e460fb5af6 endChunkForTag
0e9c2cc97758 hoistPreambleState
a102185f46f0 writeBootstrap
5029993bf806 writeStartPendingSuspenseBoundary
f77c7bcf40db writeStartSegment
d5fd46739706 writeEndSegment
3d67f9ed4eca var regexForJSStringsInInstructionScripts
6239c5554cd6 escapeJSStringsForInstructionScripts
2fc443390748 var regexForJSStringsInScripts
22ed1ac6399e escapeJSObjectForInstructionScripts
5a411f7cbb83 var currentlyRenderingBoundaryHasStylesToHoist
f4f68912cbb7 var destinationHasCapacity
65f4e10a3f89 flushStyleTagsLateForBoundary
7315dce680cd hasStylesToHoist
77736b971690 writeHoistablesForBoundary
30e336a33fb3 flushResource
a91bdf7e80f1 var stylesheetFlushingQueue
32d230b21a45 flushStyleInPreamble
d6dd7f3243ee flushStylesInPreamble
e457ef3abbcf preloadLateStyle
d91063e54c42 preloadLateStyles
f6b8400e18bc pushCompletedShellIdAttribute
6b025e9cb756 writeStyleResourceDependenciesInJS
77f1a4e2207b writeStyleResourceAttributeInJS
56f79ce33c69 createHoistableState
ca5d0ed4c7b9 prefetchDNS
a7a49fec9d25 preconnect
025cdba91c1f preload
5d045d5388e1 preloadModule
27ffc79b2d1f preinitStyle
fdcb718f52c9 preinitScript
1fffd1583320 preinitModuleScript
bf8027b45f83 adoptPreloadCredentials
ca687caccca7 getPreloadAsHeader
8c6b13715c43 var regexForHrefInLinkHeaderURLContext
f56026007529 escapeHrefForLinkHeaderURLContextReplacer
3fff18222ea4 var regexForLinkHeaderQuotedParamValueContext
9d27fa656acc escapeStringForLinkHeaderQuotedParamValueContextReplacer
f76032dd99f3 hoistStyleQueueDependency
b1c0b97350cd hoistStylesheetDependency
a1db8159fa44 hoistHoistables
c6b1244c33c2 createRenderState
b46a30d3b308 pushTextInstance
f6d407e171d4 pushSegmentFinale
f7c320ad751c var bind
5c8b02adf753 var REACT_CLIENT_REFERENCE
fc2b2c384364 getComponentNameFromType
639e8371248a var emptyContextObject
e7c7e5d7ae8c var currentActiveSnapshot
1f21a2564c4c popToNearestCommonAncestor
4d4cb979b4d1 popAllPrevious
8ee19eb91490 pushAllNext
8c5b06ccf6c7 popPreviousToCommonLevel
5b843b2334a5 popNextToCommonLevel
030ed7616d5e switchContext
c298c4368349 var classComponentUpdater
cb26567bf4f9 var emptyTreeContext
1511e5585420 getTreeId
b7717f4897a4 pushTreeContext
e3e3151bc1fe var clz32
76bf4249a645 var log
7525b1def290 var LN2
b4e3b1acceb9 clz32Fallback
e594ad272ac6 noop
4fc6eb3b99c4 var SuspenseException
60b5ebab7794 trackUsedThenable
3c74fda29156 var suspendedThenable
35a90bbcaee0 getSuspendedThenable
0c2d9109fc65 is
3d9f1119f46d var objectIs
94d5a2debe99 var currentlyRenderingComponent
cdc491846a9b var currentlyRenderingTask
0b16c55291c0 var currentlyRenderingRequest
1a874f3f11ef var currentlyRenderingKeyPath
765962988708 var firstWorkInProgressHook
2e34e89e25fe var workInProgressHook
055086c2e742 var isReRender
ffac8bbcfb59 var didScheduleRenderPhaseUpdate
b578fb131ca9 var localIdCounter
aaf29222b0e3 var actionStateCounter
d548086dfb2c var actionStateMatchingIndex
7f221e05cd42 var thenableIndexCounter
e1705fe8a076 var thenableState
be0ecfae74b7 createRecoverableError
02a78bb857db isRecoverableError
9aa4e94c62bc cloneRecoverableErrorAsFatal
caea8d0bef08 var renderPhaseUpdates
34fd04d5ff95 var numberOfReRenders
1d51708a0a81 resolveCurrentlyRenderingComponent
28ced1da848e createHook
cf9790394521 createWorkInProgressHook
21356f64b603 getThenableStateAfterSuspending
16b43e2f55f7 resetHooksState
42d7531d7863 basicStateReducer
27f2e9e97d7e useReducer
49255f793ddb useMemo
24002e844813 dispatchAction
e09e336c8446 throwOnUseEffectEventCall
104eb7dae8e0 unsupportedStartTransition
13dd7ad7f237 unsupportedSetOptimisticState
cb01673d66fc useActionState
8b195aae2ee8 unwrapThenable
0eb38df0e806 unsupportedRefresh
a3220db31c23 var HooksDispatcher
2a6e5ba2788f var currentResumableState
1bda6a71b435 var DefaultAsyncDispatcher
6f4f06adfffc var prefix
1ad1d8e2e53c var suffix
8f3e6f745575 describeBuiltInComponentFrame
4f1b7d9fd18d var reentry
960281e90919 describeNativeComponentFrame
ef1bf89a6018 describeComponentStackByType
9c4f3608f990 isEligibleForOutlining
ef32962e0907 defaultErrorHandler
f16067ba2d40 RequestInstance
499cf2d65262 createRequest
5b035ae95a69 var currentRequest
4262b40bb79c pingTask
9b91ee15e551 createSuspenseBoundary
71f1d39352b0 createRenderTask
4c6d02d4e24b createReplayTask
743eb612c276 createPendingSegment
18773d950b9c pushComponentStack
9b04f51e58e6 replaceSuspenseComponentStackWithSuspenseFallbackStack
79871a366950 getThrownInfo
f68cef14888a logRecoverableError
a79cce3f167d fatalError
98226d26bdf7 finishSuspenseListRow
9f519f05b968 unblockSuspenseListRow
38d824c226a8 tryToResolveTogetherRow
4fbb26271ee5 createSuspenseListRow
3d0b8c86f921 renderSuspenseListRows
d84aadb4f05f renderWithHooks
64966c3a9c0c finishFunctionComponent
227176a91e46 renderElement
965db6b33da8 resumeNode
8fd195fe5a74 renderNodeDestructive
fd2d62c08b50 retryNode
499fca6b8ea5 renderChildrenArray
47b9af818eb5 trackPostponedBoundary
4f57b5b50fc6 trackPostpone
821f85ed9ed3 untrackBoundary
ecd85272e009 spawnNewSuspendedReplayTask
5dc680de516b spawnNewSuspendedRenderTask
45c7241d6f80 renderNode
37bd924b1b83 abortTaskSoft
c60f6345f3b8 abortRemainingReplayNodes
1c776670265f abortTask
9e539b788b67 finishAbortedTask
9794b5238474 safelyEmitEarlyPreloads
66eeeeda01b2 completeShell
b512a0b5c6ba completeAll
52ac25d76e26 queueCompletedSegment
c17bc32ba3af finishedTask
eed59fde7cba performWork
affcbc8c50bd preparePreambleFromSubtree
444b5e0603da preparePreambleFromSegment
9f91bc35d59c preparePreamble
8be43808a737 flushSubtree
93b074ebed6f var flushedByteSize
4a70cacc95ef flushSegment
3a884232f150 flushSegmentContainer
3b0529cd0c4c flushCompletedBoundary
aef7f7fb0fae flushPartiallyCompletedSegment
39bb0e12d64f var flushingPartialBoundaries
685387c5ffe2 flushCompletedQueues
4ec5305cc6e8 enqueueFlush
2daf393b89f8 startFlowing
42a52636d27e finishAbort
f409d5a289de endRenderLifetime
4422b7a67a7d abort
29532bc8c8ae addToReplayParent
51e2b44dcb2d onError
010262a3504d renderToStringImpl
ebc7beacf0f1 ExpressionStatement
ab01264de148 ExpressionStatement
f79f069a566c ExpressionStatement
5830fe4f41c6 the order of the top level
d7d505ba2961 ExpressionStatement
54301f8e7ab9 the order of the top level
`
