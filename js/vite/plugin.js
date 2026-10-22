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
// ask the plugins further down. Each entry has the name of the function, that first line, the
// call of the plugins further down, and the first line that asks the plugins first.
const serovalArrays = [
  [
    'parseObject',
    '\tif (Array.isArray(current)) return parseArray(ctx, depth, id, current);\n',
    '\tconst parsed = parsePlugin(ctx, depth, id, current);\n',
    '\tif (Array.isArray(current)) return parsePlugin(ctx, depth, id, current) || parseArray(ctx, depth, id, current);\n',
  ],
  [
    'parseObjectAsync',
    '\tif (Array.isArray(current)) return parseArray$1(ctx, depth, id, current);\n',
    '\tconst parsed = await parsePlugin$1(ctx, depth, id, current);\n',
    '\tif (Array.isArray(current)) return await parsePlugin$1(ctx, depth, id, current) || parseArray$1(ctx, depth, id, current);\n',
  ],
]
// The error handler of renderToString, which ignores the errors of the render.
const ignoredErrors = 'function onError() {}'
// React's HTML escape in its string renderer, with the regular expression that finds the first
// character to escape.
const escapeSource = 'matchHtmlRegExp = /["\'&<>]/;\nfunction escapeTextForBrowser(text) {'
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
  return [plugin, generated(), lazyRoutes(), ssrPlugin(() => root, () => import('vite'))]
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
// APIs in page code, trims React's server entry to the renderer actually used, caches the HTML
// escapes of that renderer and makes them with replaceAll, joins the chunks it writes, puts Set
// checks in front of its long switches on strings, passes the errors renderToString ignores to the
// runtime, keeps the renderer from building component stacks and from putting the names of
// components into their key paths, gives the importers of React namespaces of plain values,
// switches core-js to its property-backed internal state, checks that the bundle is one file, and
// warns of the places where it may load a module at run time. Its source map names sources
// relative to the application root. It comes with serovalPlugin, which changes seroval.
export function ssrPlugin(getRoot, loadVite) {
  const reactTargets = new Map() // server.edge.js path to the renderer modules it imports
  let coreJS // real directory of the core-js package the application resolves, '' for none
  let coreJSState = false // whether the internal state module of coreJS is in the bundle
  let coreJSAdapted = false // whether it got the property-backed state
  let nodeImporters // Node module to the packages that import it
  let namesDropped = false // whether React's key paths have no names of components
  let serverActions // the modules outside React DOM with $$FORM_ACTION in their code
  const plugin = {
    name: 'tifa:ssr',
    applyToEnvironment: (env) => env.name === 'ssr',
    buildStart() {
      coreJS = findPackage(getRoot(), 'core-js')
      coreJSState = coreJSAdapted = false
      nodeImporters = new Map()
      namesDropped = false
      serverActions = new Set()
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
    transform: {
      filter: { code: /\$\$FORM_ACTION/ },
      handler(_, id) {
        if (packageName(id) !== 'react-dom') serverActions.add(path.relative(getRoot(), id))
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
      if (reactStringRenderer.test(id)) {
        const react = `react-dom ${packageVersion(path.resolve(id, '..', '..'))}: ${id}`
        let source = fs.readFileSync(id, 'utf8')
        const cached = cacheEscapes(source)
        if (cached) {
          source = cached
        } else {
          this.warn(
            `${react} has no matchHtmlRegExp and escapeTextForBrowser in the form the tifa plugin knows, so ` +
              'the tifa plugin does not cache the escapes of the HTML React writes. Pages render the same, but ' +
              'long texts and attribute values with & < > " or \' in them take longer to render',
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
        // A syntax tree goes only with the source it was parsed from: each change below parses
        // the source the changes before it left.
        const parse = (code) => this.parse(code)
        const read = keyPathNamesRead(parse(source))
        const unnamed = !read && dropKeyPathNames(source)
        if (unnamed) {
          source = unnamed
          namesDropped = true
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
      // The code keeps the lines and places it had, so the source map fits it as it is.
      return plain.code === code ? null : { code: plain.code, map: null }
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
  return [plugin, serovalPlugin()]
}

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
  for (const [fn, check, plugins, first] of serovalArrays) {
    if (source.split(check).length !== 2 || source.split(plugins).length !== 2) missing.push(fn)
    else source = source.replace(check, () => first)
  }
  return { source, missing }
}

// cacheEscapes returns the source of React's string renderer with a cache in front of
// escapeTextForBrowser, or null when the source does not have it in the form it knows. React
// escapes every text and attribute value it writes. A string with nothing to escape costs one
// search of the regular expression, but one with & < > " or ' goes through a loop over each
// character, which takes long in QuickJS. Such strings are mostly the same from render to render,
// like the class names of Tailwind CSS ([&_svg]:size-4, has-[>svg]:px-3), so the cache keeps their
// escapes, keyed by the string. It holds escapeCacheSize code units, and when the next string does
// not fit it starts over empty: texts of the data, which differ in each request, fill it, and the
// strings every render writes come back at the next render. A string larger than the whole cache
// is not kept. The cache lives in the module, so each VM has its own. It searches with exec, which
// takes less time in QuickJS than test: test looks up exec and calls it.
function cacheEscapes(source) {
  if (source.split(escapeSource).length !== 2) return null
  const cached = `matchHtmlRegExp = /["'&<>]/;
var tifaEscapes = new Map(),
  tifaEscapesSize = 0;
function escapeTextForBrowser(text) {
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
// packages, and finds the helpers and the calls by the form rolldown writes them in: when the form
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
  for (const m of code.matchAll(/^\/\/#region ([^\n]+)\n[\t ]*var (require_[\w$]+) = \/\* @__PURE__ \*\/ __commonJSMin(?:\$\d+)?\(/gm)) {
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
