import './host'
import { renderToString } from 'react-dom/server.edge'
import { useId, Suspense } from 'react'

function Content() {
  return <div id={useId()}>{'<script>订单 & 🙂</script>'}</div>
}

globalThis.result = renderToString(
  <Suspense fallback="loading">
    <Content />
  </Suspense>,
)
