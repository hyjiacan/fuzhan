import { ref, computed, h } from 'vue'
import { encodePath } from './fileTableColumns'

/**
 * 搜索相关状态与逻辑。displayList 依据 store 的搜索结果与本目录列表共同决定，
 * fileList/checkedRowKeys/loadCurrentDir 由 useFilesTable 注入。
 */
export const useFilesSearch = ({ store, router, fileList, checkedRowKeys, loadCurrentDir }) => {
  const searchQuery = ref('')
  const searchInputRef = ref(null)
  const searchStartTime = ref(0)

  const searchState = computed(() => store.state.searchState)
  const isSearching = computed(() => searchState.value.isSearching)
  const searchCompleted = computed(() => searchState.value.isCompleted)
  const searchResultCount = computed(() => store.state.fileList.length)
  const searchTime = computed(() => store.state.searchState.searchTime || 0)

  // 当前显示的列表：搜索时使用 store.fileList，浏览时使用本地 fileList
  const displayList = computed(() => {
    if (isSearching.value || searchCompleted.value) {
      return store.state.fileList
    }
    return fileList.value
  })

  // 从搜索关键词中提取用于高亮的关键词（排除扩展名过滤条件）
  // 与后端 SearchService.SearchFiles 的扩展名提取逻辑保持一致
  const getHighlightKeywords = (keywords) => {
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

  // 高亮搜索关键字（使用 VNode），支持多关键词
  // 同一查询在搜索结果里会被逐行反复调用，单个结果行路径分多段也会重复调用，
  // 因此按转义后的关键词缓存编译好的正则，避免每次都重复 new RegExp。
  const highlightKeywordCache = new Map()
  const getHighlightRegex = (escaped) => {
    const key = escaped.join('|')
    let re = highlightKeywordCache.get(key)
    if (!re) {
      re = {
        splitRegex: new RegExp(`(${key})`, 'gi'),
        testRegex: new RegExp(`(${key})`, 'i')
      }
      // 防止查询词持续变化导致缓存无限增长
      if (highlightKeywordCache.size >= 100) highlightKeywordCache.clear()
      highlightKeywordCache.set(key, re)
    }
    return re
  }

  const highlightKeyword = (text) => {
    if (!searchQuery.value.trim() || (!isSearching.value && !searchCompleted.value)) {
      return text
    }
    const allKeywords = searchQuery.value.trim().split(/\s+/)
    const keywords = getHighlightKeywords(allKeywords)
    if (keywords.length === 0) {
      return text
    }
    const escaped = keywords.map(k => k.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    const { splitRegex, testRegex } = getHighlightRegex(escaped)
    const parts = text.split(splitRegex)

    return parts.map((part, idx) => {
      if (part && testRegex.test(part)) {
        return h('mark', { class: 'search-highlight', key: idx }, part)
      }
      return part
    })
  }

  // 搜索文件（复用 store，与 HomeView 一致）
  const searchFiles = () => {
    checkedRowKeys.value = []
    if (!searchQuery.value.trim()) {
      store.clearSearchState()
      loadCurrentDir()
      return
    }
    store.actions.adminSearchFiles(searchQuery.value)
  }

  // 清除搜索（与 HomeView 一致）
  const clearSearch = () => {
    searchQuery.value = ''
    checkedRowKeys.value = []
    store.clearSearchState()
    loadCurrentDir()
  }

  // 点击路径段导航到目录
  const navigateToDir = (dirPath) => {
    searchQuery.value = ''
    checkedRowKeys.value = []
    store.clearSearchState()
    router.push('/admin/files/' + encodePath(dirPath))
  }

  // 路由切换时清空搜索态（由视图在 watch 中调用）
  const resetFromRoute = () => {
    searchQuery.value = ''
    store.clearSearchState()
    checkedRowKeys.value = []
  }

  return {
    searchQuery, searchInputRef, searchStartTime,
    searchState, isSearching, searchCompleted, searchResultCount, searchTime, displayList,
    getHighlightKeywords, highlightKeyword, searchFiles, clearSearch, navigateToDir,
    resetFromRoute
  }
}