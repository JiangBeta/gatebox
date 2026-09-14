import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    // 构建产物直接输出到后端 embed 目录，实现单二进制交付。
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    // 绑 0.0.0.0 供局域网访问；strictPort 防止端口漂移。
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
    proxy: {
      // ws 必须开启：日志/exec 控制台走 WebSocket。
      '/api': {
        target: `http://127.0.0.1:${process.env.GATEBOX_DEV_PORT || '8099'}`,
        ws: true,
      },
    },
  },
})
