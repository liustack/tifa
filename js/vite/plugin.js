// The Vite plugin of tifa. tifa writes it into .tifa/vite/plugin.js of an application, and the
// application's vite.config.ts uses it through .tifa/vite.js. It tells Vite where tifa's
// generated entries are, resolves the modules tifa generates, and builds the SSR bundle that
// QuickJS runs: one IIFE file with every dependency inside and no Node API.
import fs from 'node:fs'
import path from 'node:path'
import { builtinModules } from 'node:module'

// devBase is where the Vite dev server serves modules under tifa dev. tifa dev sends every
// request under it to Vite and every other one to the application.
export const devBase = '/__tifa/vite/'

const clientOnlyStub = '\0tifa-client-only'
const propertyState = '\0tifa-core-js-property-state'
const routeModule = /^\.\.?\/(?:.*\/)?\+(?:route|layout)(?:\.js|\.ts)?$/
const clientSource = /\.client(?:\.[jt]sx?)?$/
const reactServerEntry = /[/\\]react-dom[/\\]server\.edge\.js$/
const reactStringRenderer = /[/\\]react-dom[/\\]cjs[/\\]react-dom-server-legacy\.browser\.production\.js$/
// The error handler of renderToString, which ignores the errors of the render.
const ignoredErrors = 'function onError() {}'
const nodeBuiltins = new Set(builtinModules)

// pollInterval is how often, in milliseconds, Vite's watcher looks at each file once it polls.
// Polling 10,000 files every second takes about 8% of one CPU core.
const pollInterval = 1000

// devState is what the plugin and tifa's Vite driver (js/vite/driver.mjs) share in the Node
// process of tifa dev. Vite bundles the config, this file with it, so they meet on globalThis.
// The driver sets it up, so outside tifa dev it is empty:
//   - pollFiles: the dev server's watcher is to poll, for a system that ran out of watches.
//   - changed(event, file): gets every event of the dev server's watcher.
//   - restart(run): runs run, a restart of the dev server, so the driver knows when one runs.
//   - outOfWatches(err): the watcher ran out of watches or file descriptors.
function devState() {
  return (globalThis[Symbol.for('tifa.dev')] ??= {})
}

/**
 * tifa returns the plugins of a tifa application.
 *
 * settings.target is build.target of the project configuration, the browsers the browser bundle
 * runs in.
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
  return [plugin, generated(), ssrPlugin(() => root, () => import('vite'))]
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
// The syntax target is ES2022, which QuickJS runs as it is: a lower one only slows rendering, a
// higher one may emit syntax QuickJS lacks. Whoever builds it sets build.minify. sourcemap is
// build.sourcemap, hidden when undefined.
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
        output: { format: 'iife', entryFileNames: 'ssr.js', codeSplitting: false, sourcemapExcludeSources: true },
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
// APIs in page code, trims React's server entry to the renderer actually used, passes the errors
// renderToString ignores to the runtime, switches core-js to its property-backed internal state,
// checks that the bundle is one file, and warns of the places where it may load a module at run
// time. Its source map names sources relative to the application root.
export function ssrPlugin(getRoot, loadVite) {
  const reactTargets = new Map() // server.edge.js path to the renderer modules it imports
  let coreJS // real directory of the core-js package the application resolves, '' for none
  let coreJSState = false // whether the internal state module of coreJS is in the bundle
  let coreJSAdapted = false // whether it got the property-backed state
  let nodeImporters // Node module to the packages that import it
  return {
    name: 'tifa:ssr',
    applyToEnvironment: (env) => env.name === 'ssr',
    buildStart() {
      coreJS = findPackage(getRoot(), 'core-js')
      coreJSState = coreJSAdapted = false
      nodeImporters = new Map()
    },
    outputOptions(options) {
      const root = getRoot()
      return {
        ...options,
        // The places in the source map, relative to the application root.
        sourcemapPathTransform: (source, map) => path.relative(root, path.resolve(path.dirname(map), source)),
        // An import of a Node module that stays in the bundle reads require("node:fs", "the
        // packages that import it"). The check of the output finds it, and QuickJS, which has no
        // require, fails at it with a stack trace the server explains with the packages.
        globals: (id) => `require(${JSON.stringify(id)}, ${JSON.stringify([...(nodeImporters.get(id) ?? [])].join(', '))})`,
      }
    },
    resolveId: {
      order: 'pre',
      async handler(source, importer) {
        const fromDependency = importer?.includes(`${path.sep}node_modules${path.sep}`)
        if (source.startsWith('node:') || nodeBuiltins.has(source)) {
          if (!fromDependency) this.error(`Node APIs are not supported in page code: ${source}`)
          if (!nodeImporters.has(source)) nodeImporters.set(source, new Set())
          nodeImporters.get(source).add(packageName(importer))
          // A dependency may import a Node module it never uses on this platform. The check of
          // the output finds the import if it stays: the bundle loads it with require.
          return { id: source, external: true }
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
    load(id) {
      if (id === clientOnlyStub) {
        return 'export default function ClientOnlyStub(){ throw new Error("Client-only module invoked during SSR") }'
      }
      // Select core-js's existing property-backed internal state. Do not replace the global
      // WeakMap or patch generated output.
      if (id === propertyState) return 'module.exports = false'
      if (coreJS && id === path.join(coreJS, 'internals', 'internal-state.js')) coreJSState = true
      if (reactStringRenderer.test(id)) {
        // renderToString leaves a Suspense boundary whose content threw to the browser, and its
        // error handler ignores the error. Give renderToString and renderToStaticMarkup the
        // onError option of React's other renderers, for the runtime to report the error. Each
        // call reports to its own onError, so a render inside a render keeps its errors.
        const source = fs.readFileSync(id, 'utf8')
        if (source.split(ignoredErrors).length !== 2) {
          this.warn(
            `react-dom ${packageVersion(path.resolve(id, '..', '..'))}: ${id} has no "${ignoredErrors}" the tifa ` +
              'plugin knows, so a server render that fails inside a Suspense boundary cannot say why',
          )
          return null
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
    async generateBundle(_, bundle) {
      const chunks = Object.values(bundle).filter((output) => output.type === 'chunk')
      if (chunks.length !== 1) this.error(`the SSR bundle has ${chunks.length} files, want 1`)
      const [chunk] = chunks
      const loads = await hostLoads(chunk, await loadVite(), (code) => this.parse(code))
      if (loads.length > 0) {
        this.warn(
          'the SSR bundle may load modules at run time, which QuickJS cannot do: a server render that ' +
            'reaches one of these places fails. The list is a hint, it may name code no render reaches ' +
            `and miss some:\n${loads.join('\n')}`,
        )
      }
    },
  }
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
// The bundle loads the Node modules it imports with require("node:fs") and the like (see
// outputOptions of ssrPlugin), so they are found the same way. The source maps of the copy and of the chunk
// name the place in the source.
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
      const [name, importers] = parent.type === 'CallExpression' && key === 'callee' ? parent.arguments.map(stringValue) : []
      found.push({
        at: node.start,
        what:
          typeof name !== 'string'
            ? 'require loads a module by a name known only at run time'
            : `require(${JSON.stringify(name)}) loads a Node module${importers ? ` imported by ${importers}` : ''}`,
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
  if (!exports.some(([name]) => name === 'renderToString')) return null
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
