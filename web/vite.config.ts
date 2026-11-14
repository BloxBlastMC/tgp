/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      // During `pnpm dev`, forward API and git traffic to a local tgp-server.
      '/api': 'http://localhost:3000',
      '/healthz': 'http://localhost:3000',
    },
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})
