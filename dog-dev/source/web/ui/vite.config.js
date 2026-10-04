import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// 产物直接输出到 web/static（go:embed 的目录），后端静态路由零改动。
// index.html 引用的资源为绝对路径 /assets/...，命中 embed 文件服务器；
// 其余路径由后端回落到 index.html（SPA 兜底）。
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  base: '/',
  build: {
    outDir: '../static',
    emptyOutDir: true,
    target: 'es2020',
  },
})
