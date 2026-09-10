import { h } from 'vue'

// HTML 转义函数
export const escapeHtml = (str) => {
  if (typeof str !== 'string') return ''
  const escapeMap = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }
  return str.replace(/[&<>"']/g, c => escapeMap[c] || c)
}

// 从搜索关键词中提取用于高亮的关键词（排除扩展名过滤条件）
// 与后端 SearchService.SearchFiles 的扩展名提取逻辑保持一致
export const getHighlightKeywords = (keywords) => {
  if (keywords.length === 0) return keywords
  const lastIdx = keywords.length - 1
  const lastKw = keywords[lastIdx]

  if (lastKw.startsWith('.')) {
    // 纯扩展名过滤，如 ".pdf" - 不参与高亮
    return keywords.slice(0, lastIdx)
  }
  if (lastKw.includes('.')) {
    // 关键词包含扩展名，如 "file.txt" - 只取关键词部分
    const parts = lastKw.split('.')
    if (parts[0] === '') {
      return keywords.slice(0, lastIdx)
    }
    keywords[lastIdx] = parts[0]
    return keywords
  }
  return keywords
}

// 高亮搜索关键字（返回 VNode 数组），支持多关键词
export function highlightKeyword(text, query, isActive) {
  if (!query.trim() || !isActive) {
    return escapeHtml(String(text || ''))
  }
  const allKeywords = query.trim().split(/\s+/)
  const keywords = getHighlightKeywords(allKeywords)
  if (keywords.length === 0) {
    return escapeHtml(String(text || ''))
  }
  const escapedText = escapeHtml(text)
  const escaped = keywords.map(k => escapeHtml(k).replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  const splitRegex = new RegExp(`(${escaped.join('|')})`, 'gi')
  const testRegex = new RegExp(`(${escaped.join('|')})`, 'i')
  const parts = escapedText.split(splitRegex)

  return parts.map((part, idx) => {
    if (part && testRegex.test(part)) {
      return h('mark', { class: 'search-highlight', key: idx }, part)
    }
    return part
  })
}