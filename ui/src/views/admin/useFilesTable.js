import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { AdminApi } from '@/api'
import { PathUtils, compareFileNames } from '@/utils'
import { isDir } from './fileTableColumns'

/**
 * 文件列表核心状态与导航：当前目录、面包屑、勾选、表格尺寸。
 * search 复用 store，因此本模块不依赖 store，仅持有浏览态。
 */
export const useFilesTable = (router) => {
  const loading = ref(false)
  const fileList = ref([])
  const rootOptions = ref([])
  const selectedRoot = ref('')
  const currentPath = ref('')
  const breadcrumb = ref([])
  const currentFile = ref(null)

  // 是否为根目录（根目录不允许操作）
  const isAtRoot = computed(() => !currentPath.value)

  // ==== 勾选状态（el-table-v2 不内置选择列，手动实现）====
  // 勾选基于"当前显示列表"（搜索时为搜索结果），由视图注入 displayList 源，默认为本目录列表
  let listSource = null
  const setListSource = (fn) => { listSource = fn }
  const currentList = () => (listSource ? listSource() : fileList.value)

  const checkedRowKeys = ref([])
  const isAllSelected = computed(() => {
    const list = currentList()
    return list.length > 0 && list.every(f => checkedRowKeys.value.includes(f.path))
  })
  const isIndeterminate = computed(() => {
    const list = currentList()
    if (list.length === 0) return false
    const count = list.filter(f => checkedRowKeys.value.includes(f.path)).length
    return count > 0 && count < list.length
  })
  const toggleSelectAll = (val) => {
    if (val) {
      checkedRowKeys.value = currentList().map(f => f.path)
    } else {
      checkedRowKeys.value = []
    }
  }
  const handleSingleCheck = (row, checked) => {
    if (checked) {
      if (!checkedRowKeys.value.includes(row.path)) {
        checkedRowKeys.value = checkedRowKeys.value.concat(row.path)
      }
    } else {
      checkedRowKeys.value = checkedRowKeys.value.filter(key => key !== row.path)
    }
  }

  // el-table-v2 需要数值宽高，实时测量容器
  const tableWrapRef = ref(null)
  const tableWidth = ref(600)
  const tableHeight = ref(400)
  let tableResizeObs = null
  const updateTableSize = () => {
    const el = tableWrapRef.value
    if (el) {
      tableWidth.value = el.clientWidth || 600
      tableHeight.value = el.clientHeight || 400
    }
  }

  // 进入目录
  const enterDir = (row) => {
    const newPath = row.path
    router.push(`/admin/files/${PathUtils.encodeFilePath(newPath)}`)
  }

  // 导航到面包屑指定层级
  const navigateToBreadcrumb = (index) => {
    if (index < breadcrumb.value.length - 1) {
      // 点击的不是最后一个（当前目录），导航到对应的父目录
      const target = breadcrumb.value[index]
      router.push(`/admin/files/${PathUtils.encodeFilePath(target.path.replace(/^\//, ''))}`)
    }
  }

  // 更新面包屑（与文件页面保持一致）
  const updateBreadcrumb = () => {
    if (!currentPath.value) {
      breadcrumb.value = [{ name: '文件管理', path: '/' }]
      return
    }
    const parts = currentPath.value.split('/').filter(Boolean)
    breadcrumb.value = [
      { name: '文件管理', path: '/' },
      ...parts.map((name, index) => ({
        name,
        path: '/' + parts.slice(0, index + 1).join('/')
      }))
    ]
  }

  // 从 URL 获取当前路径
  const getCurrentPathFromRoute = (route) => {
    const pathMatch = route.params.pathMatch
    if (!pathMatch) return ''
    // pathMatch 可能是字符串或字符串数组
    return Array.isArray(pathMatch) ? pathMatch.join('/') : pathMatch
  }

  // 加载当前目录
  const loadCurrentDir = async () => {
    loading.value = true
    try {
      const data = await AdminApi.listFiles(currentPath.value)
      if (data.success && data.data) {
        // 排序：目录在前，文件在后，按名称排序
        fileList.value = data.data.sort((a, b) => {
          const aIsDir = isDir(a)
          const bIsDir = isDir(b)
          if (aIsDir && !bIsDir) return -1
          if (!aIsDir && bIsDir) return 1
          return compareFileNames(a.name, b.name)
        })

        // 在根目录时一并更新根目录选项（供移动/重命名使用）
        if (!currentPath.value) {
          const dirs = data.data.filter(f => isDir(f))
          rootOptions.value = dirs.map(d => ({
            label: d.name,
            value: d.name
          }))
          if (rootOptions.value.length > 0) {
            selectedRoot.value = rootOptions.value[0].value
          }
        }
      }
    } catch (e) {
      console.error('加载文件列表失败', e)
      ElMessage.error('加载文件列表失败')
    } finally {
      loading.value = false
    }
  }

  // 路由变化时切换目录（由视图在 watch 中调用）
  const handleRouteChange = async (newPathMatch) => {
    currentPath.value = Array.isArray(newPathMatch) ? newPathMatch.join('/') : (newPathMatch || '')
    updateBreadcrumb()
    await loadCurrentDir()
  }

  const setupTableResize = () => {
    updateTableSize()
    tableResizeObs = new ResizeObserver(updateTableSize)
    if (tableWrapRef.value) {
      tableResizeObs.observe(tableWrapRef.value)
    }
  }

  const teardownTableResize = () => {
    tableResizeObs?.disconnect()
    tableResizeObs = null
  }

  return {
    loading, fileList, rootOptions, selectedRoot,
    currentPath, breadcrumb, currentFile, isAtRoot,
    checkedRowKeys, isAllSelected, isIndeterminate,
    toggleSelectAll, handleSingleCheck, setListSource,
    tableWrapRef, tableWidth, tableHeight, updateTableSize,
    enterDir, navigateToBreadcrumb, updateBreadcrumb,
    getCurrentPathFromRoute, loadCurrentDir, handleRouteChange,
    setupTableResize, teardownTableResize
  }
}