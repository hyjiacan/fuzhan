import { createApiReference } from '@scalar/api-reference'
// 显式导入 Scalar 样式（standalone 模式不会自动注入 CSS）
import '@scalar/api-reference/style.css'
import '@scalar/api-reference/vue-styles.css'

// 离线环境配置：禁用所有需要互联网的功能
createApiReference('#scalar-root', {
  url: '/api/open/v1/openapi.json',
  darkMode: true,
  // 禁用默认字体（不加载 Google Fonts）
  withDefaultFonts: false,
  // 禁用请求代理（不连接 api.scalar.com/proxy.scalar.com）
  proxyUrl: '',
  // 隐藏搜索栏（避免 registry 搜索请求）
  hideSearch: true,
})
