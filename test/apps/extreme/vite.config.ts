import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react(), tailwindcss()],
  // Photos stay files under /assets/ however small, so a page never carries them inline.
  build: { assetsInlineLimit: (file) => (file.endsWith('.avif') ? false : undefined) },
})
