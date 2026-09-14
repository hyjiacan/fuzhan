import { ref, computed } from 'vue'
import { analyzeLatestVersions, compareFileNames } from '@/utils'
import store from '@/store'

// 文件页表格尺寸测量、界面排序与最新版本标记
export function useHomeTable({ isSearching, searchCompleted }) {
  const fileList = computed(() => store.state.fileList)

  const isDir = (row) => row.type === 'dir' || row.type === 'directory'

  // 版本标记：仅浏览模式（非搜索）下，分析当前目录，返回"最新版本"文件的 path 集合
  const latestVersionPaths = computed(() => {
    if (isSearching.value || searchCompleted.value) return new Set()
    return analyzeLatestVersions(fileList.value)
  })

  // ============ 界面排序（不涉及后台）============
  // 默认按名称升序；支持按路径(文件名)、修改时间、下载次数排序
  // 注意：el-table-v2 的 sort-state 要求为 { [columnKey]: 'asc'|'desc' } 映射形式，
  // 而非 { key, order }；否则表头不显示排序指示且首次点击 order 为 undefined
  const sortState = ref({ name: 'asc' })

  const onColumnSort = (state) => {
    if (state && state.key) {
      // 首次点击未排序列时，el-table-v2 传来的 order 为 undefined，归一化为 asc
      const order = state.order === 'asc' || state.order === 'desc' ? state.order : 'asc'
      sortState.value = { [state.key]: order }
    }
  }

  const sortedFileList = computed(() => {
    const entry = Object.entries(sortState.value)[0]
    const key = entry ? entry[0] : 'name'
    const order = entry ? entry[1] : 'asc'

    // 默认序（名称升序、目录优先）与 store.setFileList 的排序口径一致，
    // 直接复用 store 已排好的数组，避免对大数组做重复的复制 + 排序，
    // 仅在用户点击表头自定义排序时才复制排序。
    if (key === 'name' && order === 'asc') {
      return fileList.value
    }

    const list = [...fileList.value]
    const dirFirst = (a, b) => {
      const aDir = isDir(a)
      const bDir = isDir(b)
      if (aDir !== bDir) return aDir ? -1 : 1
      return 0
    }

    let cmp
    if (key === 'modifiedTime') {
      cmp = (a, b) => {
        const ta = String(a.modifiedTime || '')
        const tb = String(b.modifiedTime || '')
        if (ta < tb) return -1
        if (ta > tb) return 1
        return 0
      }
    } else if (key === 'downloadCount') {
      cmp = (a, b) => (a.downloadCount || 0) - (b.downloadCount || 0)
    } else {
      // 默认为路径/文件名排序，保持"英文在前、中文按拼音"规则
      cmp = (a, b) => compareFileNames(a.name, b.name)
    }

    list.sort((a, b) => {
      const d = dirFirst(a, b)
      if (d !== 0) return d
      const r = cmp(a, b)
      return order === 'desc' ? -r : r
    })
    return list
  })

  // 对路径的每段分别编码，避免斜杠被编码
  const encodePath = (path) => {
    return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
  }

  return {
    fileList,
    isDir,
    latestVersionPaths,
    sortState,
    onColumnSort,
    sortedFileList,
    encodePath
  }
}