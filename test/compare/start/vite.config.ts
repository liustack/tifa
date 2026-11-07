import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import { nitro } from 'nitro/vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, 'shop'),
      'tifa/client': path.resolve(import.meta.dirname, 'shop/runtime/client.tsx'),
      'tifa/i18n': path.resolve(import.meta.dirname, 'shop/runtime/i18n.tsx'),
    },
  },
  plugins: [tanstackStart(), nitro(), react(), tailwindcss()],
})
