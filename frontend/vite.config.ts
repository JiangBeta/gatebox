import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    // 构建产物直接输出到后端 embed 目录,实现单二进制交付。
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // ws 必须开启:容器日志与 exec 控制台走 WebSocket,
      // 不开的话开发模式下升级请求会被代理成普通 HTTP 而失败。
      // 端口可用 GATEBOX_DEV_PORT 覆盖(本机 8080 被 traefik、8090 被其他服务占用)。
      '/api': {
        target: `http://127.0.0.1:${process.env.GATEBOX_DEV_PORT || '8099'}`,
        ws: true,
      },
    },
  },
})
