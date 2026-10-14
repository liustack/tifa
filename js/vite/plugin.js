// The Vite plugin of tifa. tifa writes it into .tifa/vite/plugin.js of an application, and the
// application's vite.config.ts uses it through .tifa/vite.js. It tells Vite where tifa's
// generated entries are, resolves the modules tifa generates, and builds the SSR bundle that
// QuickJS runs: one IIFE file with every dependency inside and no Node API.
import fs from 'node:fs'
import path from 'node:path'
import { createHash } from 'node:crypto'
import { builtinModules } from 'node:module'

// The two adapters below depend on the exact source of one npm file each. A dependency upgrade
// fails the build until someone reviews the adapter again and updates the version and hash here.
const reactDOMVersion = '19.3.0'
const reactServerEdgeHash = 'a631527404b0a7bbe10aa79db04852196fd7fd88a056c1bb84cf1a1885e1e934'
const coreJSVersion = '3.50.0'
const internalStateHash = '72bd626c79e840676152fbd2652e546ae5605da8542b2df0e28756d9e99fef20'

// devBase is where the Vite dev server serves modules under tifa dev. tifa dev sends every
// request under it to Vite and every other one to the application.
export const devBase = '/__tifa/vite/'

const clientOnlyStub = '\0tifa-client-only'
const coreJSState = '\0tifa-core-js-property-state'
const routeModule = /^\.\.?\/(?:.*\/)?\+(?:route|layout)(?:\.js|\.ts)?$/
const clientSource = /\.client(?:\.[jt]sx?)?$/
const reactServerEntry = /[/\\]react-dom[/\\]server\.edge\.js$/
const reactProduction = /if \(process\.env\.NODE_ENV === 'production'\) \{([^}]+)\}/
const reactRequire = /(\w+) = require\('([^']+)'\);/g
const reactExport = /exports\.(\w+) = (\w+)\.(\w+);/g
const nodeBuiltins = new Set(builtinModules)

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
        // The executable and the build output change with every rebuild and are no modules.
        server: { watch: { ignored: [path.join(root, 'bin') + '/**', dist + '/**'] } },
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
          ssr: ssrEnvironment(path.join(gen, 'server.ts'), dist),
        },
      }
    },
  }
  return [plugin, generated(), ssrPlugin(() => root)]
}

// generated resolves the imports that lead to what tifa generates: tifa/client, ./+route and
// ./+layout, and @/, which is the application root.
export function generated() {
  let root = ''
  return {
    name: 'tifa:generated',
    config(config) {
      root = fs.realpathSync(path.resolve(config.root ?? '.'))
      return {
        resolve: {
          alias: [
            { find: /^tifa\/client$/, replacement: path.join(root, '.tifa', 'runtime', 'client.ts') },
            { find: /^@\//, replacement: root + '/' },
          ],
        },
      }
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
// higher one may emit syntax QuickJS lacks. Whoever builds it sets build.minify.
export function ssrEnvironment(entry, dir) {
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
      rolldownOptions: {
        output: { format: 'iife', entryFileNames: 'ssr.js', codeSplitting: false },
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
// APIs in page code, trims React's server entry to the renderer actually used, switches core-js to
// its property-backed internal state, and checks that the bundle is one file with no imports.
export function ssrPlugin(getRoot) {
  const reactTargets = new Map() // server.edge.js path to the renderer modules it imports
  const builtins = new Set() // the Node modules dependencies import
  let coreJS // real directory of the core-js package the application resolves, '' for none
  let coreJSChecked = false
  return {
    name: 'tifa:ssr',
    applyToEnvironment: (env) => env.name === 'ssr',
    buildStart() {
      builtins.clear()
      coreJS = findPackage(getRoot(), 'core-js')
      coreJSChecked = false
    },
    resolveId: {
      order: 'pre',
      async handler(source, importer) {
        const fromDependency = importer?.includes(`${path.sep}node_modules${path.sep}`)
        if (source.startsWith('node:') || nodeBuiltins.has(source)) {
          if (!fromDependency) this.error(`Node APIs are not supported in page code: ${source}`)
          // A dependency may import a Node module it never uses on this platform. The check of
          // the output finds the import if it stays.
          builtins.add(source)
          return { id: source, external: true }
        }
        if (clientSource.test(source)) return clientOnlyStub
        if (importer && reactTargets.get(importer)?.includes(source)) {
          // The reviewed renderer modules only initialize their own engine. An unused engine need
          // not run for the other's API.
          return { id: path.join(path.dirname(importer), source), moduleSideEffects: false }
        }
        if (
          coreJS &&
          source === '../internals/weak-map-basic-detection' &&
          importer === path.join(coreJS, 'internals', 'internal-state.js')
        ) {
          return coreJSState
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
      if (id === coreJSState) return 'module.exports = false'
      if (coreJS && !coreJSChecked && id.startsWith(coreJS + path.sep)) {
        coreJSChecked = true
        checkCoreJS(coreJS)
      }
      if (reactServerEntry.test(id)) {
        const { code, targets } = reactServerExports(id)
        reactTargets.set(id, targets)
        return code
      }
      return null
    },
    generateBundle(_, bundle) {
      const chunks = Object.values(bundle).filter((output) => output.type === 'chunk')
      if (chunks.length !== 1) this.error(`the SSR bundle has ${chunks.length} files, want 1`)
      const [chunk] = chunks
      const imports = [...chunk.imports, ...chunk.dynamicImports].filter((name) => name !== chunk.fileName)
      // A CommonJS dependency keeps its require call of an external module in the code.
      for (const name of builtins) {
        if (chunk.code.includes(`require(${JSON.stringify(name)})`)) imports.push(name)
      }
      if (imports.length > 0) {
        this.error(`SSR bundle has unresolved runtime dependencies: ${JSON.stringify(imports)}`)
      }
    },
  }
}

// reactServerExports replaces react-dom/server.edge.js, a CommonJS barrel that loads both the
// stream and the string renderer, with an ES module that re-exports the same public API. A named
// renderToString import then bundles only the string renderer.
function reactServerExports(file) {
  const source = fs.readFileSync(file, 'utf8')
  const { version } = JSON.parse(fs.readFileSync(path.join(path.dirname(file), 'package.json'), 'utf8'))
  if (version !== reactDOMVersion || sha256(source) !== reactServerEdgeHash) {
    throw new Error(`Review the React SSR build adapter for react-dom ${version}`)
  }
  const production = reactProduction.exec(source)
  if (!production) throw new Error('React SSR production entry changed')
  const modules = new Map()
  for (const [, variable, target] of production[1].matchAll(reactRequire)) modules.set(variable, target)
  const exports = [...source.matchAll(reactExport)]
  if (modules.size !== 2 || exports.length !== 5 || !exports.some((m) => m[1] === 'renderToString')) {
    throw new Error('React SSR exports changed, review the build adapter')
  }
  const named = []
  const namespace = []
  for (const [, name, variable, member] of exports) {
    const target = modules.get(variable)
    if (!target) throw new Error(`Unknown React SSR implementation: ${variable}`)
    named.push(`export { ${member} as ${name} } from ${JSON.stringify(target)}`)
    namespace.push(`${name}: ${variable}.${member}`)
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

function checkCoreJS(dir) {
  let version = ''
  let state = ''
  try {
    version = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8')).version
    state = fs.readFileSync(path.join(dir, 'internals', 'internal-state.js'), 'utf8')
  } catch {}
  if (version !== coreJSVersion || sha256(state) !== internalStateHash) {
    throw new Error(
      `Review the QuickJS core-js compatibility adapter after upgrading core-js (found ${version} at ${dir})`,
    )
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

function sha256(text) {
  return createHash('sha256').update(text).digest('hex')
}
