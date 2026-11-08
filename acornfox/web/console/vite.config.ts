/// <reference types="vitest/config" />
// 构建产物写到 ../../internal/console/ui，由 internal/console 用 go:embed 嵌进程序。
// 产物只引用同源文件：不内联脚本（CSP 是 script-src 'self'），资源不转成 data: 地址（字体只允许同源）。
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import buildMeta from './scripts/licenses-plugin.mjs'

export default defineConfig({
  base: '/',
  plugins: [react(), buildMeta()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: {
    outDir: fileURLToPath(new URL('../../internal/console/ui', import.meta.url)),
    emptyOutDir: true,
    assetsInlineLimit: 0,
    sourcemap: false,
    target: 'es2020',
    chunkSizeWarningLimit: 900,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
