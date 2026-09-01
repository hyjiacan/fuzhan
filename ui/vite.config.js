import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import legacy from '@vitejs/plugin-legacy'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    // 兼容老旧浏览器（IE 11+、旧版 Chrome/Firefox/Safari）
    legacy({
      targets: ['defaults', 'not IE 11'],
      // 为现代构建生成 polyfill
      modernPolyfills: true,
    }),
    vue(),
    AutoImport({
      imports: ['vue'],
      resolvers: [ElementPlusResolver()],
      dts: 'src/auto-imports.d.ts'
    }),
    Components({
      resolvers: [ElementPlusResolver()],
      dts: 'src/components.d.ts'
    })
  ],
  base: '/',
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  build: {
    outDir: '../server/web',
    emptyOutDir: true,
    // 多页面入口配置
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        scalar: resolve(__dirname, 'scalar.html'),
      },
      output: {
        // 手动分包策略
        manualChunks: {
          // Vue 核心
          'vue-vendor': ['vue', 'vue-router'],
          // Axios
          'axios-vendor': ['axios']
        },
        // 文件名模板
        chunkFileNames: 'assets/js/[name]-[hash].js',
        entryFileNames: 'assets/js/[name]-[hash].js',
        assetFileNames: (assetInfo) => {
          // icons 放在 assets/icons 下
          if (assetInfo.name.includes('icons') || assetInfo.name.includes('icon')) {
            return 'assets/icons/[name]-[hash][extname]'
          }
          return 'assets/[ext]/[name]-[hash][extname]'
        }
      }
    },
    // 压缩配置
    minify: 'terser',
    terserOptions: {
      compress: {
        drop_console: true, // 生产环境移除 console
        drop_debugger: true
      }
    },
    // 启用 source map 便于调试（生产可关闭）
    sourcemap: false
  },
  server: {
    port: 3000,
    historyApiFallback: true,
    proxy: {
      '/api': {
        target: 'http://localhost:8888',
        changeOrigin: true
      },
      '/download': {
        target: 'http://localhost:8888',
        changeOrigin: true
      }
    }
  },
  // 依赖预构建优化
  optimizeDeps: {
    include: ['vue', 'vue-router', 'axios', 'element-plus', '@element-plus/icons-vue']
  }
})
