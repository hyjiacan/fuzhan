// 复制文本到剪贴板（统一实现，供各视图复用，避免多份降级逻辑不一致）。
// 优先使用 navigator.clipboard（需安全上下文 https/localhost）；
// 在非安全上下文（http）或 API 不可用时降级为临时 textarea + execCommand。
// 返回 Promise<boolean>，调用方据此给出各自的成功/失败提示。
export async function copyToClipboard(text) {
  if (!text) return false

  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch (e) {
      // 安全上下文判定通过但实际写入失败时，继续尝试降级方案
    }
  }

  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '-1000px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch (e) {
    return false
  }
}

export default { copyToClipboard }
