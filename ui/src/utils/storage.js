// 安全的 localStorage 封装：隐私模式、禁用存储或配额已满时访问会抛异常，
// 统一捕获以免影响页面主流程。业务侧读到 null/写入失败时按「无存储」语义处理。
export const safeStorage = {
  get(key) {
    try {
      return localStorage.getItem(key)
    } catch {
      return null
    }
  },
  set(key, value) {
    try {
      localStorage.setItem(key, value)
    } catch {
      /* 忽略存储异常 */
    }
  },
  remove(key) {
    try {
      localStorage.removeItem(key)
    } catch {
      /* 忽略存储异常 */
    }
  }
}