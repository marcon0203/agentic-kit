import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    // 供应商图标（@lobehub/icons-static-svg，900 多个）不要内联成 data URI。
    //
    // Vite 默认把 4KB 以下的资源内联，这些 SVG 恰好都在这条线以下；一旦内
    // 联，按名字取图标就只剩两条路：eager 把上百万字节的 data URI 压进主
    // 包，或者 lazy 生成 900 个只装着一条 data URI 的 JS chunk、运行时逐个
    // 动态 import——后者正是之前"图标加载不出来"的原因，那些 chunk 一旦取
    // 不到就静默失败。
    //
    // 关掉内联之后它们是普通静态资源，主包里只留下短 URL 字符串。
    assetsInlineLimit: (filePath) => (filePath.includes('icons-static-svg') ? false : undefined),
  },
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
