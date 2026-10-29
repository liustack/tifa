import type { NextConfig } from 'next'

const config: NextConfig = {
  turbopack: { root: process.cwd() },
  outputFileTracingRoot: process.cwd(),
  // GET / goes to the first shop, as app/route.go of the shop does.
  async redirects() {
    return [{ source: '/', destination: '/projects/studio', permanent: false }]
  },
}

export default config
