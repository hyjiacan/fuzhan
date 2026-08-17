// 此文件仅用于配置全局属性，组件由 unplugin-vue-components 自动导入
import { uploadProps } from 'naive-ui'

export default {
  install(app) {
    // 暴露 uploadProps
    app.config.globalProperties.uploadProps = uploadProps
  }
}