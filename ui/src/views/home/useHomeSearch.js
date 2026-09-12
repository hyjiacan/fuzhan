import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { SearchApi } from '@/api'
import store from '@/store'

// 文件页搜索状态、搜索触发、自动补全/纠错与相关 watch
export function useHomeSearch() {
  const router = useRouter()
  const searchQuery = ref('')
  const searchInputRef = ref(null)
  const searchResultCount = ref(-1)
  const searchTime = ref(0)
  const searchStartTime = ref(0)

  const fileList = computed(() => store.state.fileList)
  const searchState = computed(() => store.state.searchState)
  const isSearching = computed(() => searchState.value.isSearching)
  const searchCompleted = computed(() => searchState.value.isCompleted)

  const searchFiles = () => {
    searchResultCount.value = -1
    if (!searchQuery.value.trim()) {
      store.clearSearchState()
      store.actions.loadFileList(store.state.currentPath || '/')
      return
    }
    searchStartTime.value = Date.now()
    store.actions.searchFiles(searchQuery.value)
  }

  const clearSearch = () => {
    searchQuery.value = ''
    store.clearSearchState()
    store.actions.loadFileList(store.state.currentPath || '/')
    router.replace({ query: {} })
  }

  // 仅清空搜索状态，不重新加载列表（用于目录导航等场景）
  const resetSearch = () => {
    searchQuery.value = ''
    store.clearSearchState()
  }

  // 搜索框下拉推荐：自动补全 + 拼写纠错（输入防抖 300ms，避免每键并发两次请求）
  let suggestTimer = null
  const querySuggestions = (queryString, cb) => {
    if (suggestTimer) clearTimeout(suggestTimer)
    suggestTimer = setTimeout(() => {
      const q = (queryString || '').trim()
      if (!q) {
        cb([])
        return
      }
      // 并行拉取自动补全与纠错建议
      Promise.all([
        SearchApi.autocomplete(q).catch(() => ({ data: [] })),
        SearchApi.spellcheck(q).catch(() => ({ data: [] }))
      ]).then(([autoRes, spellRes]) => {
        const autoNames = Array.isArray(autoRes?.data) ? autoRes.data : []
        const spellNames = Array.isArray(spellRes?.data) ? spellRes.data : []
        const suggestions = new Map() // value -> { value, corrected }
        // 自动补全结果：直接作为推荐
        for (const name of autoNames) {
          if (!suggestions.has(name)) suggestions.set(name, { value: name, corrected: false })
        }
        // 纠错结果：标注为"纠错"，且避免与原查询相同、避免与自动补全重复
        for (const name of spellNames) {
          if (name === q || suggestions.has(name)) continue
          suggestions.set(name, { value: name, corrected: true })
        }
        cb([...suggestions.values()].slice(0, 12))
      })
    }, 300)
  }

  // 选中推荐项：用该关键词发起搜索（下拉项为关键词 term，而非完整文件名）
  const onSuggestionSelect = (item) => {
    searchQuery.value = item.value
    searchFiles()
  }

  // 监听搜索状态变化
  watch(searchState, (newState) => {
    if (newState.isSearching) {
      searchResultCount.value = -1
    }
    if (newState.isCompleted && searchStartTime.value > 0) {
      searchTime.value = Date.now() - searchStartTime.value
      searchResultCount.value = fileList.value.length
    }
    if (!newState.isSearching && !newState.isCompleted) {
      searchStartTime.value = 0
    }
  }, { immediate: true, deep: true })

  // 监听文件列表变化，更新搜索结果计数
  watch(fileList, (newList) => {
    if (isSearching.value || searchCompleted.value) {
      searchResultCount.value = newList.length
    }
  })

  // 搜索框被清空时（用户点击输入框的 ×），清除搜索状态
  watch(searchQuery, (newVal, oldVal) => {
    if (oldVal && !newVal && (isSearching.value || searchCompleted.value)) {
      clearSearch()
    }
  })

  return {
    searchQuery,
    searchInputRef,
    searchResultCount,
    searchTime,
    isSearching,
    searchCompleted,
    searchFiles,
    clearSearch,
    resetSearch,
    querySuggestions,
    onSuggestionSelect
  }
}