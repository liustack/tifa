import test from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { buildSSR } from './ssr-build.mjs'

function entry(contents) {
  return [{ name: 'test-entry', setup(builder) {
    builder.onLoad({ filter: /\/web\/server\.tsx$/ }, () => ({ contents, loader: 'tsx' }))
  } }]
}

test('React entry trimming preserves public synchronous rendering and escaping', async () => {
  const source = `
    import './runtime/host'
    import { renderToString } from 'react-dom/server.edge'
    import { useId, Suspense } from 'react'
    function Content() { return <div id={useId()}>{'<script>订单 & 🙂</script>'}</div> }
    globalThis.html = renderToString(<Suspense fallback="loading"><Content /></Suspense>)
  `
  const outputs = []
  for (const trimReact of [false, true]) {
    const result = await buildSSR({ write: false, trimReact, plugins: entry(source) })
    const context = vm.createContext({})
    vm.runInContext(result.outputFiles[0].text, context)
    outputs.push(context.html)
    const bundledInputs = Object.values(result.metafile.outputs)[0].inputs
    const hasStream = Object.keys(bundledInputs).some((name) => name.endsWith('react-dom-server.edge.production.js'))
    assert.equal(hasStream, !trimReact)
  }
  assert.equal(outputs[0], outputs[1])
  assert.match(outputs[1], /&lt;script&gt;订单 &amp; 🙂&lt;\/script&gt;/)
  assert.match(outputs[1], /<!--\$-->/)
})

test('all public edge exports remain available when explicitly imported', async () => {
  const source = `import './runtime/host'; import * as api from 'react-dom/server.edge'; globalThis.keys = Object.keys(api).sort().join(',')`
  const outputs = []
  for (const trimReact of [false, true]) {
    const result = await buildSSR({ write: false, trimReact, plugins: entry(source) })
    const context = vm.createContext({})
    vm.runInContext(result.outputFiles[0].text, context)
    outputs.push(context.keys.split(',').filter((key) => key !== 'default').join(','))
  }
  assert.equal(outputs[0], outputs[1])
  assert.equal(outputs[1], 'renderToReadableStream,renderToStaticMarkup,renderToString,resume,version')
  const defaultResult = await buildSSR({ write: false, plugins: entry(`import './runtime/host'; import api from 'react-dom/server.edge'; globalThis.keys = Object.keys(api).sort().join(',')`) })
  const context = vm.createContext({})
  vm.runInContext(defaultResult.outputFiles[0].text, context)
  assert.equal(context.keys, outputs[1])
})

test('production SSR contains no profiler and rejects Node runtime imports', async () => {
  const result = await buildSSR({ write: false })
  assert.ok(!result.outputFiles[0].text.includes('__ssrNow'))
  assert.ok(!result.outputFiles[0].text.includes('renderProfile'))
  await assert.rejects(buildSSR({ write: false, plugins: entry(`import { readFileSync } from 'node:fs'; globalThis.value = readFileSync('/tmp/secret')`) }), /Node APIs are not supported/)
})

test('constant elements preserve request data, context, hooks, refs and mutable props', async () => {
  const source = `
    import './runtime/host'
    import * as fixture from '../scripts/fixtures/ssr-constant-elements'
    globalThis.result = {
      staticReused: fixture.staticElement() === fixture.staticElement(),
      mutableReused: fixture.mutableElement() === fixture.mutableElement(),
      refReused: fixture.refElement() === fixture.refElement(),
      html: ['alpha', 'beta', 'alpha'].map(fixture.renderCase),
      effectEvaluations: fixture.effectEvaluations(),
    }
  `
  const results = []
  for (const constantElements of [false, true]) {
    const bundle = await buildSSR({ write: false, constantElements, plugins: entry(source) })
    const context = vm.createContext({})
    vm.runInContext(bundle.outputFiles[0].text, context)
    const result = JSON.parse(vm.runInContext('JSON.stringify(result)', context))
    assert.equal(result.staticReused, constantElements)
    assert.equal(result.mutableReused, false)
    assert.equal(result.refReused, false)
    assert.equal(result.effectEvaluations, 3)
    assert.match(result.html[0], /data-tenant="alpha"/)
    assert.match(result.html[1], /data-tenant="beta"/)
    assert.match(result.html[1], /title="beta"/)
    assert.match(result.html[1], /value="beta"/)
    assert.match(result.html[1], /&lt;script&gt;订单 &amp; 🙂&lt;\/script&gt;/)
    assert.equal(result.html[0], result.html[2])
    results.push(result.html)
  }
  assert.deepEqual(results[0], results[1])
})
