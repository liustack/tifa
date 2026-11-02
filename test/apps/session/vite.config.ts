import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react()],
})
