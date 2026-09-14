import { defineConfig } from 'vite'
import legacy from '@vitejs/plugin-legacy'
import { resolve } from 'path'

// scalar（OpenAPI 文档页）独立构建。
// 与主 App 构建解耦：产物只包含 @scalar/api-reference，稳定且无需每次随主应用重建；
// 输出到独立目录 server/scalar，由后端 /scalar.html 与 /scalar/assets 路由单独托管。
// 兼容策略与主构建保持一致（同样启用 legacy 双份产物）。
export default defineConfig({
  plugins: [
    legacy({
      targets: ['defaults', 'not IE 11'],
      modernPolyfills: true,
    }),
  ],
  base: '/scalar/',
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  build: {
    outDir: '../server/scalar',
    emptyOutDir: true,
    sourcemap: false,
    // 文档页面向现代浏览器，使用 esbuild 压缩，速度快
    minify: 'esbuild',
    rollupOptions: {
      input: {
        scalar: resolve(__dirname, 'scalar.html'),
      },
      output: {
        chunkFileNames: 'assets/js/[name]-[hash].js',
        entryFileNames: 'assets/js/[name]-[hash].js',
        assetFileNames: 'assets/[ext]/[name]-[hash][extname]'
      }
    }
  }
})