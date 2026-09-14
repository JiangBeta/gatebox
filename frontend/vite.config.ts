import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { AntDesignVueResolver } from 'unplugin-vue-components/resolvers'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [
    vue(),
    Components({
      resolvers: [
        AntDesignVueResolver({
          importStyle: false, // 按需加载样式
        }),
      ],
    }),
  ],
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
    // 固定端口:host 绑 0.0.0.0 供局域网访问,strictPort 防止被占用后漂移。
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
    proxy: {
      // ws 必须开启:容器日志与 exec 控制台走 WebSocket。
      '/api': {
        target: `http://127.0.0.1:${process.env.GATEBOX_DEV_PORT || '8099'}`,
        ws: true,
      },
    },
  },
})
