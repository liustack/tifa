// The driver: tifa runs it with node --input-type=module -e in the application root to use the
// application's own Vite. It reads one JSON request per line from stdin and writes one JSON
// answer per line to file descriptor 3, so that what Vite and its plugins print on stdout and
// stderr stays apart from the answers. Every answer is {id, error} or {id, ...result}.
//
// Requests:
//
//	{op: "exports", files}       the names each TypeScript or JavaScript file exports
//	{op: "client"}               builds the browser bundle
//	{op: "ssr", assets, minify}  builds the SSR bundle, assets is the value of __TIFA_ASSETS__.
//	                             The SSR bundle is always a production build: in a process
//	                             whose NODE_ENV is not production, such as the one of the dev
//	                             server, it is built on a worker thread with its own NODE_ENV
//	{op: "serve", port}          starts the dev server on 127.0.0.1:port, answers with the tags
//	                             that load Vite's client and the React refresh runtime
//	{op: "script", dir, file, plugin}
//	                             builds file in dir alone like an SSR bundle, with the SSR part
//	                             of the tifa plugin at the path plugin and no Vite config, and
//	                             answers with its code and the modules in it. go generate ./js
//	                             builds the SSR host patches this way
//
// tifa sets NODE_ENV: production for tifa build, development for tifa dev.
//
// The driver ends when stdin closes.
import fs from 'node:fs'
import path from 'node:path'
import readline from 'node:readline'
import { pathToFileURL } from 'node:url'
import { Worker } from 'node:worker_threads'

const answer = (value) => fs.writeSync(3, JSON.stringify(value) + '\n')

let vite
try {
  vite = await import('vite')
} catch (err) {
  const missing = err?.code === 'ERR_MODULE_NOT_FOUND' && String(err.message).includes("'vite'")
  answer({
    id: 0,
    error: missing
      ? 'Vite is not installed in the application, install it with:\n\tpnpm add -D vite @vitejs/plugin-react'
      : `loading Vite: ${message(err)}`,
  })
  process.exit(0)
}
answer({ id: 0, version: vite.version })

const root = process.cwd()
let server = null
let ssrWorker = null // builds the SSR bundle when this process is not a production one

// logger prints Vite's warnings and errors. Its progress and file lists are left out, and so is
// the line that says a build failed, as the error goes back to tifa, which prints it.
const logger = vite.createLogger('warn', { allowClearScreen: false })
const logError = logger.error
logger.error = (msg, options) => {
  if (!/Build failed in /.test(msg)) logError(msg, options)
}

const ops = {
  async exports({ files }) {
    const exports = {}
    for (const file of files) {
      const code = fs.readFileSync(file, 'utf8')
      const lang = path.extname(file).slice(1)
      const result = vite.parseSync(file, code, { lang, sourceType: 'module' })
      if (result.errors.length > 0) {
        throw new Error(result.errors.map((e) => e.codeframe || e.message).join('\n'))
      }
      const names = new Set()
      for (const statement of result.module.staticExports) {
        for (const entry of statement.entries) {
          if (entry.isType) continue
          if (entry.exportName.kind === 'Default') names.add('default')
          else if (entry.exportName.kind === 'Name') names.add(entry.exportName.name)
        }
      }
      exports[file] = [...names].sort()
    }
    return { exports }
  },

  async client() {
    const builder = await createBuilder({})
    await builder.build(builder.environments.client)
    return {}
  },

  async ssr({ assets, minify }) {
    if (process.env.NODE_ENV === 'production') {
      await buildSSR(createBuilder, { assets, minify })
      return {}
    }
    ssrWorker ??= startSSRWorker()
    await ssrWorker.build({ assets, minify })
    return {}
  },

  async serve({ port }) {
    server = await vite.createServer({
      root,
      customLogger: logger,
      server: { host: '127.0.0.1', port, strictPort: true },
    })
    checkPlugin(server.config)
    await server.listen()
    // The plugins add the tags a page needs to the head of an empty document, as they would for
    // an index.html: Vite's client, and the React refresh runtime with @vitejs/plugin-react.
    const html = await server.transformIndexHtml('/', '<html><head></head><body></body></html>')
    const head = html.slice(html.indexOf('<head>') + 6, html.indexOf('</head>'))
    return { head: head.trim() }
  },

  async script({ dir, file, plugin }) {
    const { ssrEnvironment, ssrPlugin } = await import(pathToFileURL(plugin).href)
    const env = ssrEnvironment(path.resolve(dir, file), path.join(dir, 'out'))
    const builder = await vite.createBuilder({
      root: dir,
      configFile: false,
      logLevel: 'warn',
      customLogger: logger,
      plugins: [ssrPlugin(() => fs.realpathSync(dir))],
      environments: {
        ssr: {
          ...env,
          // The host patches change the CommonJS files of abort-controller and event-target-shim,
          // which their package.json names without an extension, so .js comes before .mjs.
          resolve: { ...env.resolve, extensions: ['.js', '.mjs', '.mts', '.ts', '.jsx', '.tsx', '.json'] },
          build: { ...env.build, minify: true, write: false },
        },
      },
    })
    const result = await builder.build(builder.environments.ssr)
    const [chunk] = (Array.isArray(result) ? result[0] : result).output
    return { code: chunk.code, modules: Object.keys(chunk.modules) }
  },
}

// buildSSR builds the SSR bundle with the builder that createBuilder returns. The SSR worker
// thread runs its source text, so it uses only its arguments.
async function buildSSR(createBuilder, { assets, minify }) {
  const builder = await createBuilder({
    environments: {
      ssr: { define: { __TIFA_ASSETS__: JSON.stringify(assets) }, build: { minify } },
    },
  })
  await builder.build(builder.environments.ssr)
}

// ssrThread runs on the SSR worker thread, next to buildSSR. Vite reads NODE_ENV once per
// thread, so the worker loads its own copy of Vite with NODE_ENV set to production and runs
// buildSSR for each message.
async function ssrThread() {
  const { parentPort, workerData } = await import('node:worker_threads')
  const vite = await import(workerData.vite)
  const logger = vite.createLogger('warn', { allowClearScreen: false })
  const logError = logger.error
  logger.error = (msg, options) => {
    if (!/Build failed in /.test(msg)) logError(msg, options)
  }
  const createBuilder = (inline) =>
    vite.createBuilder({ root: workerData.root, logLevel: 'warn', customLogger: logger, ...inline })
  parentPort.on('message', async ({ id, request }) => {
    try {
      await buildSSR(createBuilder, request)
      parentPort.postMessage({ id })
    } catch (err) {
      let text = err instanceof Error ? err.message : String(err)
      if (err?.frame && !text.includes(err.frame)) text += '\n' + err.frame
      parentPort.postMessage({ id, error: text })
    }
  })
}

// startSSRWorker starts the SSR worker thread. Its build returns once the worker has built the
// bundle, and throws the error of a failed build.
function startSSRWorker() {
  const worker = new Worker(`${buildSSR}\n(${ssrThread})()`, {
    eval: true,
    env: { ...process.env, NODE_ENV: 'production' },
    workerData: { vite: import.meta.resolve('vite'), root },
  })
  const pending = new Map()
  let next = 0
  let failed = null
  const fail = (err) => {
    failed = err
    for (const { reject } of pending.values()) reject(err)
    pending.clear()
  }
  worker.on('message', ({ id, error }) => {
    const call = pending.get(id)
    pending.delete(id)
    if (error === undefined) call.resolve()
    else call.reject(new Error(error))
  })
  worker.on('error', fail)
  worker.on('exit', (code) => fail(new Error(`the SSR worker thread exited with code ${code}`)))
  worker.unref()
  return {
    build(request) {
      if (failed) return Promise.reject(failed)
      const id = ++next
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject })
        worker.postMessage({ id, request })
      })
    },
    close: () => worker.terminate(),
  }
}

// createBuilder loads the application's Vite config with inline added to it.
async function createBuilder(inline) {
  const builder = await vite.createBuilder({ root, logLevel: 'warn', customLogger: logger, ...inline })
  checkPlugin(builder.config)
  return builder
}

function checkPlugin(config) {
  if (!config.configFile) {
    throw new Error(
      'the application has no vite.config.ts, create it:\n\n' +
        "\timport { defineConfig } from 'vite'\n\timport react from '@vitejs/plugin-react'\n\timport tifa from './.tifa/vite.js'\n\n" +
        '\texport default defineConfig({ plugins: [tifa(), react()] })',
    )
  }
  if (!config.plugins.some((p) => p.name === 'tifa')) {
    const file = path.relative(root, config.configFile)
    throw new Error(
      `${file} does not use the tifa plugin, add it:\n\n` +
        "\timport tifa from './.tifa/vite.js'\n\texport default defineConfig({ plugins: [tifa(), react()] })",
    )
  }
}

function message(err) {
  if (!(err instanceof Error)) return String(err)
  let text = err.message
  if (err.frame && !text.includes(err.frame)) text += '\n' + err.frame
  return text
}

const lines = readline.createInterface({ input: process.stdin })
let queue = Promise.resolve()
lines.on('line', (line) => {
  const request = JSON.parse(line)
  queue = queue.then(async () => {
    try {
      answer({ id: request.id, ...(await ops[request.op](request)) })
    } catch (err) {
      answer({ id: request.id, error: message(err) })
    }
  })
})
lines.on('close', async () => {
  await queue
  await ssrWorker?.close()
  await server?.close()
  process.exit(0)
})
