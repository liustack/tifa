import path from 'node:path'
import type { NextConfig } from 'next'

// The shared domain code lives next to the app and imports the catalogue file of the shop, so the root
// of the build is the root of the repository.
const root = path.resolve(process.cwd(), '../../..')

const config: NextConfig = {
  turbopack: { root },
  outputFileTracingRoot: root,
  poweredByHeader: false,
  // GET / goes to the first shop, as app/route.go of the shop does.
  // The storefront is at /store, and in Chinese at /zh-CN/store. The routes keep the language as a
  // segment, so the public URLs are rewritten to them, and the write routes under /store/compare-write
  // keep their own.
  async rewrites() {
    return {
      beforeFiles: [
        { source: '/zh-CN/store', destination: '/store/zh-CN' },
        { source: '/zh-CN/store/:path*', destination: '/store/zh-CN/:path*' },
        { source: '/store', destination: '/store/en-US' },
        { source: '/store/:path((?!zh-CN|en-US|compare-write)[^/]+.*)', destination: '/store/en-US/:path' },
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
