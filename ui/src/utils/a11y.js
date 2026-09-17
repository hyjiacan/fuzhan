// 为无 href 的自定义可点击链接提供键盘触发（Enter / Space），与 onClick 行为一致
export const keyNav = (fn, ...args) => (e) => {
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    e.stopPropagation()
    fn(...args)
  }
}