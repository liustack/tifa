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
//	                             server, it is built on a worker thread with its own NODE_ENV.
//	                             There it answers with styles, the tags that link the
//	                             stylesheets the bundle imports from the dev server
//	{op: "serve", port, pollFiles}
//	                             starts the dev server on 127.0.0.1:port, answers with the tags
//	                             that load Vite's client and the React refresh runtime. With
//	                             pollFiles it then restarts with a watcher that polls, unless the
//	                             watcher uses FSEvents
//	{op: "pollFiles"}            makes the running dev server's watcher poll, unless it uses
//	                             FSEvents, and answers with polling and fsEvents
//	{op: "script", dir, file, plugin}
//	                             builds file in dir alone like an SSR bundle, with the SSR part
//	                             of the tifa plugin at the path plugin and no Vite config, and
//	                             answers with its code and the modules in it. go generate ./js
//	                             builds the SSR host patches this way
//
// tifa sets NODE_ENV: production for tifa build, development for tifa dev.
//
// Between answers, the driver writes notices:
//
//	{notice: "changed", file}    a file outside the root that the dev server's watcher saw
//	                             added, changed or removed. tifa dev watches the root itself
//	{notice: "restarted"}        the dev server restarted, or tried to, and no other restart
//	                             runs. A file outside the root may have changed meanwhile
//
// tifa passes the close timeout in milliseconds as the only argument. The driver closes when
// stdin ends: it lets the request it runs, the SSR worker and its output finish, and after the
// close timeout stops and exits, which also ends a request that still runs. So a plugin that never
// ends does not keep the driver and Vite's dev server alive after tifa is gone.
import fs from 'node:fs'
import path from 'node:path'
import readline from 'node:readline'
import { pathToFileURL } from 'node:url'
import { Worker } from 'node:worker_threads'

const closeTimeout = Number(process.argv[1])
if (!(closeTimeout > 0)) throw new Error(`the close timeout ${process.argv[1]} is not a number of milliseconds`)

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
const realRoot = fs.realpathSync(root)
let server = null
let listening = false // server.listen returned
let ssrWorker = null // builds the SSR bundle when this process is not a production one

// restarts follows the restarts of the dev server, whoever starts them: Vite after a change to
// its config or a .env file, the watcher running out of watches, and tifa. A restart creates a
// new server and then closes the environments of the old one, so loading a module there fails
// meanwhile. The SSR build waits for a restart that runs, and runs again after one that came in
// between and made it fail, see devLoad and ops.ssr. begun counts the restarts that began.
//
// A restart that fails to create the new server keeps the old one. One whose new server cannot
// listen, because another program took the port, leaves no server listening: failed holds its
// error until a restart works, and pollFiles tries again.
//
// The new server's watcher knows nothing of the files outside the root that the old one watched,
// and a file there that changed while the servers changed hands has no event. So once no restart
// runs, whether the restarts worked or not, the driver gives the watcher the files outside the
// root that the old ones watched, and writes the notice restarted: tifa dev then builds the SSR
// bundle again. outside collects those files while restarts run.
const restarts = { running: null, begun: 0, failed: null, outside: new Set() }

// trackRestart runs restart, a restart of the dev server, as one of restarts, and returns what
// restart returns.
function trackRestart(restart) {
  restarts.begun++
  for (const file of watchedOutside()) restarts.outside.add(file)
  const run = restart()
  const tracked = run
    .then(
      () => {
        restarts.failed = null
      },
      (err) => {
        restarts.failed = err
      },
    )
    .finally(() => {
      if (restarts.running !== tracked) return
      restarts.running = null
      const files = [...restarts.outside].filter((file) => fs.existsSync(file))
      restarts.outside.clear()
      if (files.length > 0) server.watcher.add(files)
      fs.writeSync(3, JSON.stringify({ notice: 'restarted' }) + '\n')
    })
  restarts.running = tracked
  return run
}

// restartsDone returns once no restart of the dev server runs.
async function restartsDone() {
  while (restarts.running) await restarts.running
}

// The dev server's watcher polls once tifa asks for it or the watcher runs out of watches. The
// wish stays in devState.pollFiles: from then on the tifa plugin adds usePolling to the config of
// every dev server Vite creates, also when Vite restarts on its own. switching is the restart
// that makes the watcher poll, while it runs. Whether it worked is read from the watcher and from
// restarts.failed: the next call tries again.
const polling = { switching: null, warned: false }

const watcherPolls = () => server?.watcher.options.usePolling === true
const watcherUsesFSEvents = () => server?.watcher.options.useFsEvents === true

// pollFiles makes the dev server's watcher poll, and returns once it does or the restart failed.
// It does not throw: it prints why a restart failed.
function pollFiles() {
  devState.pollFiles = true
  if (!listening) return Promise.resolve() // serve restarts the server once it listens
  polling.switching ??= (async () => {
    try {
      await restartsDone() // a restart that runs may have read the config before the wish
      if (!watcherPolls() || restarts.failed) await server.restart()
      if (!watcherPolls()) {
        logger.warn("Vite's dev server did not restart with a watcher that polls, it tries again at the next error of its watcher")
      }
    } catch (err) {
      logger.error(
        `Vite's dev server did not restart: ${message(err)}\n` +
          'It tries again at the next error of its watcher, or when Vite restarts after a change to its config',
      )
    } finally {
      polling.switching = null
    }
  })()
  return polling.switching
}

// outOfWatches handles a watcher that reports running out of inotify watches (ENOSPC) or file
// descriptors (EMFILE, ENFILE): the watcher goes on without the paths the system refused, so the
// dev server switches to a watcher that polls, which takes neither, and nobody has to raise a
// system limit. This holds for a watcher that uses FSEvents too.
function outOfWatches(err) {
  if (!polling.warned) {
    polling.warned = true
    logger.warn(`${err.code}: the system ran out of file watches, so Vite's dev server restarts and polls the files for changes`)
  }
  pollFiles()
}

// What the tifa plugin and the driver share, see devState in js/vite/plugin.js.
const devState = (globalThis[Symbol.for('tifa.dev')] ??= {})
Object.assign(devState, { pollFiles: false, changed: changedOutside, restart: trackRestart, outOfWatches })

// logger prints Vite's warnings and errors. Its progress and file lists are left out, and so is
// the line that says a build failed, as the error goes back to tifa, which prints it.
const logger = vite.createLogger('warn', { allowClearScreen: false })
const logError = logger.error
logger.error = (msg, options) => {
  // outOfWatches says once that the watcher polls when the system runs out of watches.
  if (/file watcher error: .*\b(?:ENOSPC|EMFILE|ENFILE)\b/.test(msg)) return
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
    for (;;) {
      await restartsDone()
      const begun = restarts.begun
      try {
        const { styles } = await ssrWorker.build({ assets, minify })
        return { styles: styleLinks(styles) }
      } catch (err) {
        // A restart came in between: build again once it is done.
        if (restarts.begun === begun && !restarts.running) throw err
      }
    }
  },

  async serve({ port, pollFiles: poll }) {
    const earlier = new Set([...process.stdin.listeners('end'), ...process.listeners('SIGTERM')])
    server = await vite.createServer({ root, customLogger: logger, server: { host: '127.0.0.1', port, strictPort: true } })
    checkPlugin(server.config)
    // Vite's dev server ends the process when stdin ends, as for a parent that went away, with
    // the listener it also adds for SIGTERM. Here stdin carries the requests, and its end starts
    // the close below, which first lets the SSR worker and the output finish. The stdin
    // listeners of plugins stay. A restart keeps the listener off: Vite adds it again only once
    // every server it created is closed.
    const sigterm = process.listeners('SIGTERM')
    for (const listener of process.stdin.listeners('end')) {
      if (!earlier.has(listener) && sigterm.includes(listener)) process.stdin.off('end', listener)
    }
    await server.listen()
    listening = true
    if ((poll && !watcherUsesFSEvents()) || devState.pollFiles) await pollFiles()
    // The plugins add the tags a page needs to the head of an empty document, as they would for
    // an index.html: Vite's client, and the React refresh runtime with @vitejs/plugin-react.
    const html = await server.transformIndexHtml('/', '<html><head></head><body></body></html>')
    const head = html.slice(html.indexOf('<head>') + 6, html.indexOf('</head>'))
    return { head: head.trim() }
  },

  async pollFiles() {
    if (!watcherUsesFSEvents()) await pollFiles()
    return { polling: watcherPolls(), fsEvents: watcherUsesFSEvents() }
  },

  async script({ dir, file, plugin }) {
    const { ssrEnvironment, ssrPlugin } = await import(pathToFileURL(plugin).href)
    const env = ssrEnvironment(path.resolve(dir, file), path.join(dir, 'out'))
    const builder = await vite.createBuilder({
      root: dir,
      configFile: false,
      logLevel: 'warn',
      customLogger: logger,
      plugins: [ssrPlugin(() => fs.realpathSync(dir), async () => vite)],
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

// buildSSR builds the SSR bundle with the builder that createBuilder returns, with plugins added
// to those of the Vite config. The SSR worker thread runs its source text, so it uses only its
// arguments.
async function buildSSR(createBuilder, { assets, minify }, plugins = []) {
  const builder = await createBuilder({
    plugins,
    environments: {
      ssr: { define: { __TIFA_ASSETS__: JSON.stringify(assets) }, build: { minify } },
    },
  })
  await builder.build(builder.environments.ssr)
}

// ssrThread runs on the SSR worker thread, next to buildSSR. Vite reads NODE_ENV once per
// thread, so the worker loads its own copy of Vite with NODE_ENV set to production and runs
// buildSSR for each message.
//
// The worker builds for tifa dev, where the browser gets its modules from Vite's dev server. An
// asset a module imports by URL, such as an image, then gets its URL from the dev server too: the
// worker asks the main thread to load it in the dev server's client environment, so the server
// render and the browser modules use one URL. Each build also answers with the stylesheets the
// bundle imports, in the order the modules run, for the head of dev pages.
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
  const loads = new Map() // the loads the main thread has not answered, by ref
  let nextLoad = 0
  const devAssets = {
    name: 'tifa:dev-assets',
    enforce: 'pre',
    applyToEnvironment: (env) => env.name === 'ssr',
    load(id) {
      // ?raw and ?inline give the content, which is the same in both.
      if (id.startsWith('\0') || /[?&](?:raw|inline)(?:&|=|$)/.test(id)) return null
      if (!/[?&]url(?:&|=|$)/.test(id) && !this.environment.config.assetsInclude(id.split('?')[0])) return null
      const ref = ++nextLoad
      return new Promise((resolve, reject) => {
        loads.set(ref, { resolve, reject })
        parentPort.postMessage({ load: id, ref })
      })
    },
  }
  // devStyles collects the stylesheets of a build into styles.
  const devStyles = (styles) => ({
    name: 'tifa:dev-styles',
    applyToEnvironment: (env) => env.name === 'ssr',
    generateBundle(_, bundle) {
      const entry = Object.values(bundle).find((output) => output.type === 'chunk' && output.isEntry)
      const seen = new Set()
      const visit = (id) => {
        if (seen.has(id)) return
        seen.add(id)
        if (/\.(?:css|less|sass|scss|styl|stylus|pcss|postcss|sss)$/.test(id)) styles.push(id)
        const info = this.getModuleInfo(id)
        for (const dep of [...(info?.importedIds ?? []), ...(info?.dynamicallyImportedIds ?? [])]) visit(dep)
      }
      visit(entry.facadeModuleId)
    },
  })
  parentPort.on('message', async ({ id, request, ref, code, error, close }) => {
    // With its port closed the thread has nothing left to do and ends on its own, after its
    // output, which an end by terminate() may drop.
    if (close) return parentPort.close()
    if (ref !== undefined) {
      const load = loads.get(ref)
      loads.delete(ref)
      if (error === undefined) load.resolve(code)
      else load.reject(new Error(error))
      return
    }
    try {
      const styles = []
      await buildSSR(createBuilder, request, [devAssets, devStyles(styles)])
      parentPort.postMessage({ id, styles })
    } catch (err) {
      let text = err instanceof Error ? err.message : String(err)
      if (err?.frame && !text.includes(err.frame)) text += '\n' + err.frame
      parentPort.postMessage({ id, error: text })
    }
  })
}

// startSSRWorker starts the SSR worker thread. Its build returns once the worker has built the
// bundle, and throws the error of a failed build. Its close returns once the worker has ended and
// all it printed is in the output of this process.
function startSSRWorker() {
  const worker = new Worker(`${buildSSR}\n(${ssrThread})()`, {
    eval: true,
    env: { ...process.env, NODE_ENV: 'production' },
    workerData: { vite: import.meta.resolve('vite'), root },
    stdout: true,
    stderr: true,
  })
  worker.stdout.pipe(process.stdout, { end: false })
  worker.stderr.pipe(process.stderr, { end: false })
  // The streams end once the worker has ended on its own and all it printed came through.
  const printed = Promise.all([worker.stdout, worker.stderr].map((stream) => new Promise((resolve) => stream.once('end', resolve))))
  const exited = new Promise((resolve) => worker.once('exit', resolve))
  const pending = new Map()
  let next = 0
  let failed = null
  const fail = (err) => {
    failed = err
    for (const { reject } of pending.values()) reject(err)
    pending.clear()
  }
  worker.on('message', ({ id, error, load, ref, ...result }) => {
    if (load !== undefined) {
      devLoad(load).then(
        (code) => worker.postMessage({ ref, code }),
        (err) => worker.postMessage({ ref, error: message(err) }),
      )
      return
    }
    const call = pending.get(id)
    pending.delete(id)
    if (error === undefined) call.resolve(result)
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
    async close() {
      // An unreferenced worker would let this process end before the worker does.
      worker.ref()
      worker.postMessage({ close: true })
      // A plugin may keep the worker alive, then the close timeout ends the process.
      await exited
      await printed
    },
  }
}

// changedOutside writes a notice for an event of the dev server's watcher about a file outside
// the root and outside node_modules: the dev server watches what it loads from there, such as a
// package of the workspace, and the SSR bundle may import it too.
function changedOutside(event, file) {
  if (!['add', 'change', 'unlink'].includes(event) || !outside(file)) return
  fs.writeSync(3, JSON.stringify({ notice: 'changed', file }) + '\n')
}

// outside reports whether file is outside the root and outside node_modules.
function outside(file) {
  for (const r of [root, realRoot]) {
    const rel = path.relative(r, file)
    if (rel !== '..' && !rel.startsWith('..' + path.sep) && !path.isAbsolute(rel)) return false
  }
  return !file.split(/[\\/]/).includes('node_modules')
}

// watchedOutside returns the files outside the root and outside node_modules that the dev
// server's watcher watches.
function watchedOutside() {
  const files = []
  for (const [dir, names] of Object.entries(server.watcher.getWatched())) {
    for (const name of names) {
      const file = path.resolve(root, dir, name)
      if (!outside(file)) continue
      try {
        if (fs.statSync(file).isFile()) files.push(file)
      } catch {} // gone meanwhile
    }
  }
  return files
}

// styleLinks returns the tags that link the stylesheets files from Vite's dev server. Vite's
// client finds them by data-vite-dev-id and leaves updating them to their own hot updates.
function styleLinks(files) {
  const { base, root } = server.config
  let tags = ''
  for (const file of files) {
    const url = file.startsWith(root + path.sep) ? path.relative(root, file).split(path.sep).join('/') : '@fs' + file
    tags += `<link rel="stylesheet" href="${attribute(base + encodeURI(url))}" data-vite-dev-id="${attribute(file)}">`
  }
  return tags
}

function attribute(value) {
  return value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
}

// devLoad loads id in the client environment of Vite's dev server and returns the code. It waits
// for a restart that runs, and tries again on the new server when one came in between.
async function devLoad(id) {
  if (!server) throw new Error(`loading ${id}: Vite's dev server is not running`)
  for (;;) {
    await restartsDone()
    const begun = restarts.begun
    let result
    try {
      result = await server.environments.client.pluginContainer.load(id)
    } catch (err) {
      if (restarts.begun === begun && !restarts.running) throw err
      continue
    }
    if (result == null) throw new Error(`Vite's dev server cannot load ${id}`)
    return typeof result === 'string' ? result : result.code
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
  // Exiting stops the worker. It may drop the end of what the worker printed.
  setTimeout(() => process.exit(0), closeTimeout)
  await queue
  await ssrWorker?.close()
  await server?.close()
  // Node writes to a pipe in the background, and exit drops what it has not written yet.
  await Promise.all([written(process.stdout), written(process.stderr)])
  process.exit(0)
})

// written waits until stream has written out all it was given: the callback of a write runs
// after the writes before it.
function written(stream) {
  return new Promise((resolve) => stream.write('', resolve))
}
