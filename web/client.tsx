import { hydrateRoot } from 'react-dom/client'
import { RouterClient } from '@tanstack/react-router/ssr/client'
import { createAppRouter } from './runtime/router'
import type { Snapshot } from './runtime/data'
import './styles.css'

const bootElement = document.getElementById('go-quick-config')
if (!bootElement?.textContent)
  throw new Error('Missing Go Quick document configuration')
const boot = JSON.parse(bootElement.textContent) as { buildId: string }
const router = createAppRouter({
  load: async (url, signal) => {
    const query = new URLSearchParams({ url, build: boot.buildId })
    const response = await fetch(`/_framework/page-data?${query}`, {
      signal,
      headers: { Accept: 'application/json' },
    })
    if (response.status === 409) {
      window.location.assign(url)
      // Do not commit old route state while a document with new assets is loading.
      return await new Promise<never>(() => {})
    }
    const value = await response.json()
    if (!response.ok) throw new Error(value.error || '页面数据加载失败')
    if (
      value.kind !== 'data' ||
      value.protocol !== 1 ||
      value.buildId !== boot.buildId ||
      value.url !== url ||
      !value.scopes
    )
      throw new Error('页面数据版本不一致，请刷新后重试')
    return value as Snapshot
  },
})
const root = document.getElementById('app')
if (!root) throw new Error('Missing application root')
hydrateRoot(root, <RouterClient router={router} />, {
  onRecoverableError: (error) => {
    console.error('Hydration failed', error)
  },
})
