import path from 'node:path'
import type { NextConfig } from 'next'

// The shared domain code lives next to the app, so the root of the build is the folder above it.
const root = path.resolve(process.cwd(), '..')

const config: NextConfig = {
  turbopack: { root },
  outputFileTracingRoot: root,
  poweredByHeader: false,
  // GET / goes to the first shop, as app/route.go of the shop does.
  // The storefront is at /store, and in English at /en-US/store. The routes keep the language as a
  // segment, so the public URLs are rewritten to them, and the write routes under /store/compare-write
  // keep their own.
  async rewrites() {
    return {
      beforeFiles: [
        { source: '/en-US/store', destination: '/store/en-US' },
        { source: '/en-US/store/:path*', destination: '/store/en-US/:path*' },
        { source: '/store', destination: '/store/zh-CN' },
        { source: '/store/:path((?!zh-CN|en-US|compare-write)[^/]+.*)', destination: '/store/zh-CN/:path' },
      ],
      afterFiles: [],
      fallback: [],
    }
  },
  async redirects() {
    return [{ source: '/', destination: '/projects/studio', permanent: false }]
  },
}

export default config
