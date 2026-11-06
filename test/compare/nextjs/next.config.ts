import path from 'node:path'
import type { NextConfig } from 'next'

// The shared domain code lives next to the app, so the root of the build is the folder above it.
const root = path.resolve(process.cwd(), '..')

const config: NextConfig = {
  turbopack: { root },
  outputFileTracingRoot: root,
  poweredByHeader: false,
  // GET / goes to the first shop, as app/route.go of the shop does.
  async redirects() {
    return [{ source: '/', destination: '/projects/studio', permanent: false }]
  },
}

export default config
