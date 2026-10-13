import { build } from 'esbuild'
import { readFile } from 'node:fs/promises'
import { builtinModules, createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import path from 'node:path'
import { transformAsync } from '@babel/core'
import constantElementsPlugin from '@babel/plugin-transform-react-constant-elements'

const require = createRequire(import.meta.url)

// Only this reviewed public CommonJS barrel is converted to ESM. Application
// code keeps using the public React API. Dependency upgrades require review.
async function reactServerExports() {
  const entry = require.resolve('react-dom/server.edge')
  const source = await readFile(entry, 'utf8')
  const { version } = require('react-dom/package.json')
  const hash = createHash('sha256').update(source).digest('hex')
  if (
    version !== '19.3.0' ||
    hash !== 'a631527404b0a7bbe10aa79db04852196fd7fd88a056c1bb84cf1a1885e1e934'
  )
    throw new Error(`Review the React SSR build adapter for react-dom ${version}`)
  const production = source.match(
    /if \(process\.env\.NODE_ENV === 'production'\) \{([^}]+)\}/,
  )?.[1]
  if (!production) throw new Error('React SSR production entry changed')
  const modules = new Map(
    [...production.matchAll(/(\w+) = require\('([^']+)'\);/g)].map(
      (m) => [m[1], m[2]],
    ),
  )
  const exports = [...source.matchAll(/exports\.(\w+) = (\w+)\.(\w+);/g)]
  if (
    modules.size !== 2 ||
    exports.length !== 5 ||
    !exports.some((m) => m[1] === 'renderToString')
  )
    throw new Error('React SSR exports changed, review the build adapter')
  const named = exports
    .map(([, name, variable, member]) => {
      const target = modules.get(variable)
      if (!target) throw new Error(`Unknown React SSR implementation: ${variable}`)
      return `export { ${member} as ${name} } from ${JSON.stringify(target)}`
    })
    .join('\n')
  // CommonJS default imports remain supported. The pure factory lets a named
  // renderToString import discard the otherwise-unused default namespace.
  const imports = [...modules]
    .map(([variable, target]) => `import * as ${variable} from ${JSON.stringify(target)}`)
    .join('\n')
  const namespace = exports
    .map(([, name, variable, member]) => `${name}: ${variable}.${member}`)
    .join(',')
  const contents = `${named}\n${imports}\nexport default /* @__PURE__ */ (() => ({${namespace}}))()`
  return { entry, contents, modules: new Set(modules.values()) }
}

export async function buildSSR({
  root = process.cwd(),
  entry = 'web/server.tsx',
  outfile = 'internal/ssr/assets/server.js',
  minify = true,
  trimReact = true,
  propertyState = true,
  constantElements = true,
  profile = false,
  write = true,
  plugins = [],
} = {}) {
  const react = trimReact ? await reactServerExports() : undefined
  const internalState = require.resolve('core-js/internals/internal-state.js')
  if (propertyState) {
    const hash = createHash('sha256').update(await readFile(internalState)).digest('hex')
    if (
      require('core-js/package.json').version !== '3.50.0' ||
      hash !== '72bd626c79e840676152fbd2652e546ae5605da8542b2df0e28756d9e99fef20'
    )
      throw new Error('Review the QuickJS core-js compatibility adapter after upgrading core-js')
  }
  const result = await build({
    absWorkingDir: root,
    bundle: true,
    target: 'es2022',
    tsconfig: path.join(root, 'tsconfig.json'),
    metafile: true,
    logLevel: 'warning',
    entryPoints: [entry],
    outfile,
    platform: 'neutral',
    conditions: ['worker', 'import'],
    mainFields: ['module', 'main'],
    format: 'iife',
    minify,
    write,
    external: [...builtinModules, ...builtinModules.map((name) => `node:${name}`)],
    define: {
      'process.env.NODE_ENV': '"production"',
      global: 'globalThis',
      __SSR_PROFILE__: String(profile),
    },
    plugins: [
      ...plugins,
      {
        name: 'quickjs-ssr',
        setup(builder) {
          builder.onResolve({ filter: /.*/ }, (args) => {
            if (
              (args.path.startsWith('node:') || builtinModules.includes(args.path)) &&
              !args.importer.includes('node_modules')
            )
              return {
                errors: [{ text: `Node APIs are not supported in page code: ${args.path}` }],
              }
            if (react && args.importer === react.entry && react.modules.has(args.path)) {
              // The reviewed renderer modules only initialize their own engine.
              // An unused engine need not run for the other engine's API.
              return {
                path: path.resolve(path.dirname(react.entry), args.path),
                sideEffects: false,
              }
            }
            if (
              propertyState &&
              args.importer === internalState &&
              args.path === '../internals/weak-map-basic-detection'
            )
              return { path: 'property-state', namespace: 'quickjs-core-js' }
          })
          if (react)
            builder.onLoad({ filter: /\/react-dom\/server\.edge\.js$/ }, (args) => {
              if (args.path !== react.entry) return
              return { contents: react.contents, loader: 'js', resolveDir: path.dirname(react.entry) }
            })
          // Select core-js's existing property-backed internal state. Do not
          // replace global WeakMap or patch generated/minified output.
          builder.onLoad({ filter: /.*/, namespace: 'quickjs-core-js' }, () => ({
            contents: 'module.exports = false',
            loader: 'js',
          }))
          builder.onResolve({ filter: /\.client(?:\.[jt]sx?)?$/ }, (args) => ({
            path: args.path,
            namespace: 'client-only',
          }))
          builder.onLoad({ filter: /.*/, namespace: 'client-only' }, () => ({
            contents: 'export default function ClientOnlyStub(){ throw new Error("Client-only module invoked during SSR") }',
            loader: 'js',
          }))
          if (constantElements)
            builder.onLoad({ filter: /\.[jt]sx$/, namespace: 'file' }, async (args) => {
              if (args.path.split(path.sep).includes('node_modules')) return
              const source = await readFile(args.path, 'utf8')
              // Babel owns the scope/purity analysis. Keep its conservative
              // defaults and leave TS/JSX lowering and bundling to esbuild.
              const result = await transformAsync(source, {
                filename: args.path,
                babelrc: false,
                configFile: false,
                parserOpts: {
                  plugins: args.path.endsWith('.tsx') ? ['jsx', 'typescript'] : ['jsx'],
                },
                plugins: [constantElementsPlugin],
              })
              if (result?.code == null) throw new Error(`Babel did not emit SSR source: ${args.path}`)
              return {
                contents: result.code,
                loader: args.path.endsWith('.tsx') ? 'tsx' : 'jsx',
                resolveDir: path.dirname(args.path),
              }
            })
        },
      },
    ],
  })
  for (const output of Object.values(result.metafile.outputs)) {
    if (output.imports.length)
      throw new Error(`SSR bundle has unresolved runtime dependencies: ${JSON.stringify(output.imports)}`)
  }
  if (
    Object.keys(result.metafile.inputs).some(
      (file) => /\.client\.[jt]sx?$/.test(file) && !file.startsWith('client-only:'),
    )
  )
    throw new Error('Client-only source leaked into SSR')
  return result
}
