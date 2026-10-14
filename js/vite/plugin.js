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
      // The check of the output reads the map to name the source of a require it finds. hidden
      // leaves the comment that points to it out of ssr.js.
      sourcemap: 'hidden',
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
  let coreJSState = false // whether the internal state module of coreJS is in the bundle
  let coreJSAdapted = false // whether it got the property-backed state
  return {
    name: 'tifa:ssr',
    applyToEnvironment: (env) => env.name === 'ssr',
    buildStart() {
      builtins.clear()
      coreJS = findPackage(getRoot(), 'core-js')
      coreJSState = coreJSAdapted = false
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
    generateBundle(options, bundle) {
      const chunks = Object.values(bundle).filter((output) => output.type === 'chunk')
      if (chunks.length !== 1) this.error(`the SSR bundle has ${chunks.length} files, want 1`)
      const [chunk] = chunks
      // The map serves the check below only, and is not written.
      if (chunk.sourcemapFileName) delete bundle[chunk.sourcemapFileName]
      const imports = [...chunk.imports, ...chunk.dynamicImports].filter((name) => name !== chunk.fileName)
      // A CommonJS dependency keeps its require call of an external module in the code.
      for (const name of builtins) {
        if (chunk.code.includes(`require(${JSON.stringify(name)})`)) imports.push(name)
      }
      if (imports.length > 0) {
        this.error(`SSR bundle has unresolved runtime dependencies: ${JSON.stringify(imports)}`)
      }
      // A require that stays in the bundle, with a name known only at run time, finds no require
      // in QuickJS. The check reads the final code, so a require in code the bundler drops, such
      // as an if (false) branch, does not count, and the source map names its place.
      const [at] = hostRequires(this.parse(chunk.code))
      if (at !== undefined) {
        this.error(
          `${sourcePlace(chunk, at, options.dir)}: require loads a module by a name known only at run time, ` +
            'and QuickJS has no require: import the module instead',
        )
      }
    },
  }
}

// hostRequires returns the offsets of the references to require in the ESTree program that no
// scope of the program declares, the ones that need the require of the host. typeof require and
// assignments to require need none. It follows the scopes of JavaScript: var, function
// parameters and names to the enclosing function, let, const, class and function declarations
// to their block, catch parameters to the catch clause.
function hostRequires(program) {
  const found = []
  const visit = (node, bound, parent, key) => {
    bound ||= declaresRequire(node)
    if (
      node.type === 'Identifier' &&
      node.name === 'require' &&
      !bound &&
      isReference(parent, key) &&
      !(parent.type === 'UnaryExpression' && parent.operator === 'typeof') &&
      !(parent.type === 'AssignmentExpression' && key === 'left')
    ) {
      found.push(node.start)
    }
    for (const [k, value] of Object.entries(node)) {
      for (const child of Array.isArray(value) ? value : [value]) {
        if (child && typeof child.type === 'string') visit(child, bound, node, k)
      }
    }
  }
  visit(program, false, null, '')
  return found
}

const functionTypes = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression'])

// declaresRequire reports whether the scope that node opens declares require. A binding is
// visited inside the scope that declares it, so the binding itself counts as bound.
function declaresRequire(node) {
  const names = []
  switch (node.type) {
    case 'Program':
      hoisted(node.body, names)
      lexical(node.body, names)
      break
    case 'FunctionDeclaration':
    case 'FunctionExpression':
    case 'ArrowFunctionExpression':
      if (node.type === 'FunctionExpression') bindings(node.id, names)
      node.params.forEach((param) => bindings(param, names))
      if (node.body.type === 'BlockStatement') hoisted(node.body.body, names)
      break
    case 'StaticBlock':
      hoisted(node.body, names)
      lexical(node.body, names)
      break
    case 'BlockStatement':
      lexical(node.body, names)
      break
    case 'SwitchStatement':
      // The cases share one block.
      node.cases.forEach((c) => lexical(c.consequent, names))
      break
    case 'ForStatement':
    case 'ForInStatement':
    case 'ForOfStatement': {
      const head = node.type === 'ForStatement' ? node.init : node.left
      if (head?.type === 'VariableDeclaration' && head.kind !== 'var') head.declarations.forEach((d) => bindings(d.id, names))
      break
    }
    case 'CatchClause':
      bindings(node.param, names)
      break
    case 'ClassExpression':
      bindings(node.id, names)
      break
  }
  return names.includes('require')
}

// hoisted adds the names that var declarations in statements give to the enclosing function,
// at any depth outside nested functions and static blocks.
function hoisted(statements, names) {
  const walk = (node) => {
    if (!node || typeof node.type !== 'string' || functionTypes.has(node.type) || node.type === 'StaticBlock') return
    if (node.type === 'VariableDeclaration' && node.kind === 'var') node.declarations.forEach((d) => bindings(d.id, names))
    for (const value of Object.values(node)) {
      for (const child of Array.isArray(value) ? value : [value]) walk(child)
    }
  }
  statements.forEach(walk)
}

// lexical adds the names that the statements of a block declare in it: let, const, classes,
// functions and imports.
function lexical(statements, names) {
  for (let node of statements) {
    if ((node.type === 'ExportNamedDeclaration' || node.type === 'ExportDefaultDeclaration') && node.declaration) {
      node = node.declaration
    }
    if (node.type === 'VariableDeclaration' && node.kind !== 'var') node.declarations.forEach((d) => bindings(d.id, names))
    else if (node.type === 'FunctionDeclaration' || node.type === 'ClassDeclaration') bindings(node.id, names)
    else if (node.type === 'ImportDeclaration') node.specifiers.forEach((s) => bindings(s.local, names))
  }
}

// bindings adds the names a binding pattern declares.
function bindings(node, names) {
  if (!node) return
  if (node.type === 'Identifier') names.push(node.name)
  else if (node.type === 'ObjectPattern') node.properties.forEach((p) => bindings(p.type === 'RestElement' ? p.argument : p.value, names))
  else if (node.type === 'ArrayPattern') node.elements.forEach((e) => bindings(e, names))
  else if (node.type === 'AssignmentPattern') bindings(node.left, names)
  else if (node.type === 'RestElement') bindings(node.argument, names)
}

// sourcePlace names the source file, line and column that offset of the chunk's code comes
// from, by the chunk's source map, or the place in the bundle when the map names none.
function sourcePlace(chunk, offset, dir) {
  const before = chunk.code.slice(0, offset).split('\n')
  const line = before.length - 1
  const column = before.at(-1).length
  const original = chunk.map && originalPosition(chunk.map, line, column)
  if (!original) return `${chunk.fileName}:${line + 1}:${column + 1}`
  const source = path.resolve(dir ?? '.', chunk.map.sourceRoot ?? '', original.source)
  return `${source}:${original.line + 1}:${original.column + 1}`
}

// originalPosition returns the source, line and column (both from 0) of the last mapping at or
// before the generated line and column, null when there is none on that line.
function originalPosition(map, line, column) {
  const lines = map.mappings.split(';')
  if (line >= lines.length) return null
  // Fields other than the generated column carry over from the segments before, on every line.
  const state = [0, 0, 0, 0]
  let best = null
  for (let i = 0; i <= line; i++) {
    let generated = 0
    for (const segment of lines[i].split(',')) {
      if (!segment) continue
      const fields = vlq(segment)
      generated += fields[0]
      if (fields.length < 4) continue
      for (let f = 1; f < 4; f++) state[f] += fields[f]
      if (i === line && generated <= column) best = { source: map.sources[state[1]], line: state[2], column: state[3] }
    }
  }
  return best
}

const base64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'

// vlq decodes the Base64 VLQ numbers of one source map segment.
function vlq(segment) {
  const values = []
  let value = 0
  let shift = 0
  for (const char of segment) {
    const digit = base64.indexOf(char)
    value += (digit & 31) << shift
    if (digit & 32) {
      shift += 5
      continue
    }
    values.push(value & 1 ? -(value >>> 1) : value >>> 1)
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
