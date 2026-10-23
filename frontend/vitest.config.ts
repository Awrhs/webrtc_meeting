import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
    // e2e/ 目录下是 Playwright 测试，由 playwright runner 运行，vitest 排除
    exclude: ['**/node_modules/**', '**/dist/**', 'e2e/**'],
  },
})
