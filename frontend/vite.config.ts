import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 3000,
    host: '0.0.0.0',
    // 注意：前端代码直接连接 API/SFU，不经过 Vite proxy
    // 配置 VITE_API_HOST 和 VITE_SFU_HOST 来指定后端地址
  },
})
