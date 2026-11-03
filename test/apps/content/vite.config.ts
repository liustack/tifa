import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'
import { rehypeLazyImages } from './markdown'

export default defineConfig({
  plugins: [tifa({ markdown: { rehypePlugins: [rehypeLazyImages] } }), react()],
})
