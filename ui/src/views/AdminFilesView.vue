<template>
  <div class="admin-files">
    <!-- Header -->
    <div class="content-header">
      <div class="breadcrumb-actions">
        <!-- 搜索状态显示（与 HomeView 一致） -->
        <div v-if="isSearching || searchCompleted" class="search-status">
          <el-icon v-if="isSearching && searchResultCount < 0" class="is-loading" size="14"><Loading /></el-icon>
          <template v-else>
            <span v-if="isSearching">搜索中...</span>
            <span v-else>搜索完成</span>
            <span class="search-count">{{ searchResultCount }} 个结果</span>
            <span class="search-time">耗时 {{ searchTime }}ms</span>
          </template>
          <el-button v-if="!isSearching" size="small" link @click="clearSearch" title="清除搜索">
            ×
          </el-button>
        </div>
        <el-breadcrumb v-else-if="breadcrumb.length > 1" class="breadcrumb" separator="/">
          <el-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <span class="breadcrumb-link" @click="navigateToBreadcrumb(index)">{{ item.name }}</span>
          </el-breadcrumb-item>
        </el-breadcrumb>
        <span v-else class="breadcrumb-root">文件管理</span>
        <div class="header-actions">
          <el-input ref="searchInputRef" v-model="searchQuery" :maxlength="200" placeholder="搜索文件..." size="small"
            class="search-input" clearable name="search-query" @keydown.enter="searchFiles" />
          <el-button size="small" @click="loadCurrentDir" :loading="loading">刷新</el-button>
          <el-button v-if="checkedRowKeys.length > 0" size="small" type="danger" @click="handleBatchDelete">
            删除选中 ({{ checkedRowKeys.length }})
          </el-button>
          <el-divider direction="vertical" class="action-divider" />
          <el-button size="small" type="primary" @click="handleScan" :loading="scanning">
            触发全量扫描
          </el-button>
          <span v-if="scanProgress.status === 'running'" class="scan-progress-text">
            扫描中: {{ scanProgress.scannedFiles }} / {{ scanProgress.totalFiles }}
          </span>
          <span v-else-if="scanProgress.status === 'completed'" class="scan-progress-text completed">
            扫描完成
          </span>
          <span v-else-if="scanProgress.status === 'failed'" class="scan-progress-text failed">
            扫描失败: {{ scanProgress.errorMessage }}
          </span>
        </div>
      </div>
      </div>

      <!-- 文件浏览 -->
    <div class="content-table">
      <div ref="tableWrapRef" class="table-v2-wrap" v-loading="loading || isSearching">
        <el-table-v2
          :columns="columns"
          :data="displayList"
          :width="tableWidth"
          :height="tableHeight"
          :estimated-row-height="34"
          row-key="path"
          @row-dblclick="handleDblClick"
        />
      </div>
    </div>

    <!-- 移动/重命名对话框（类似 Linux mv 命令） -->
    <el-dialog v-model="moveModalVisible" title="移动或重命名" width="620px">
      <el-form label-width="100px">
        <el-form-item label="文件名">
          <el-input :model-value="currentFile?.name" disabled />
        </el-form-item>
        <el-form-item label="当前路径">
          <el-input :model-value="currentFile?.path" disabled />
        </el-form-item>
        <el-form-item label="目标路径">
          <div class="target-path-input">
            <el-input :model-value="targetRootName" disabled placeholder="根目录" class="root-input" />
            <span class="path-separator">/</span>
            <el-input v-model="targetSubPath" :maxlength="1024" placeholder="输入子目录/文件名" class="sub-path-input" />
          </div>
          <div class="help-text">
            <p>输入目标路径（相对于根目录），如 <code>newname.pdf</code> 或 <code>subdir/newname.pdf</code></p>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="moveModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="moving" @click="handleMove">确定</el-button>
        </el-space>
      </template>
    </el-dialog>

    <!-- 预览对话框 -->
    <el-dialog v-model="previewDialogVisible" title="文件预览" width="700px"
      :class="['preview-dialog', previewMaximized ? 'preview-maximized' : '']"
      :style="previewMaximized ? { width: '100vw', maxWidth: '100vw' } : {}">
      <file-preview :file="previewFileData" :maximized="previewMaximized"
        @close="previewDialogVisible = false" />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="previewMaximized = !previewMaximized">{{ previewMaximized ? '还原' : '最大化' }}</el-button>
          <el-button type="primary" @click="downloadFile(previewFileData)">下载</el-button>
          <el-button @click="previewDialogVisible = false">关闭</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, h, watch, onUnmounted, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, ElButton, ElCheckbox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { AdminApi, IndexApi } from '@/api'
import { NumberUtils, TimeUtils, PathUtils, compareFileNames } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import FilePreview from '@/components/file/FilePreview.vue'
import { isPreviewable } from '@/config/preview'
import store from '@/store'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const moving = ref(false)

// 搜索状态（复用 store，与 HomeView 一致）
const searchQuery = ref('')
const searchInputRef = ref(null)
const searchStartTime = ref(0)

// 使用 store 的搜索状态
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

// 数据
const rootOptions = ref([])
const selectedRoot = ref('')
const currentPath = ref('')
const breadcrumb = ref([])
const fileList = ref([])
const currentFile = ref(null)

// 是否为根目录（根目录不允许操作）
const isAtRoot = computed(() => !currentPath.value)

// 对话框状态
const moveModalVisible = ref(false)
const targetRootName = ref('')
const targetSubPath = ref('')

// ============ 索引扫描 ============
const scanning = ref(false)
const scanProgress = ref({ status: 'idle', scannedFiles: 0, totalFiles: 0, currentFile: '', errorMessage: '' })

async function loadScanProgress() {
  try {
    const res = await IndexApi.getScanProgress()
    if (res.success) {
      scanProgress.value = res.data
    }
  } catch (err) {
    // 静默失败
  }
}

async function handleScan() {
  scanning.value = true
  try {
    const res = await IndexApi.triggerScan()
    if (res.success) {
      ElMessage.success('扫描已启动')
      startPollProgress()
    }
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '启动扫描失败'))
  } finally {
    scanning.value = false
  }
}

let progressTimer = null

function startPollProgress() {
  loadScanProgress()
  progressTimer = setInterval(() => {
    loadScanProgress()
  }, 3000)
}

function stopPollProgress() {
  if (progressTimer) {
    clearInterval(progressTimer)
    progressTimer = null
  }
}

// ============ 索引扫描 ============
const isDir = (row) => row.type === 'dir' || row.type === 'directory'

// 获取文件类型图标类名
const getFileIconClass = (row) => {
  if (isDir(row)) return 'icon-filetype-folder'
  const ext = row.name?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

// 格式化文件大小
const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

// 判断文件是否可预览
const canPreview = (row) => {
  if (isDir(row)) return false
  return isPreviewable(row.name)
}

// 对路径的每段分别编码，避免斜杠被编码
const encodePath = (path) => {
  return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
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

// ==== 勾选状态（el-table-v2 不内置选择列，手动实现）====
const isAllSelected = computed(() => {
  return displayList.value.length > 0 && displayList.value.every(f => checkedRowKeys.value.includes(f.path))
})
const isIndeterminate = computed(() => {
  if (displayList.value.length === 0) return false
  const count = displayList.value.filter(f => checkedRowKeys.value.includes(f.path)).length
  return count > 0 && count < displayList.value.length
})
const toggleSelectAll = (val) => {
  if (val) {
    checkedRowKeys.value = displayList.value.map(f => f.path)
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

// Table columns
const columns = [
  {
    key: 'selection',
    width: 40,
    headerCellRenderer: () => h(ElCheckbox, {
      modelValue: isAllSelected.value,
      indeterminate: isIndeterminate.value,
      disabled: isAtRoot.value,
      onChange: (val) => toggleSelectAll(val)
    }),
    cellRenderer: ({ rowData: row }) => h(ElCheckbox, {
      modelValue: checkedRowKeys.value.includes(row.path),
      disabled: isAtRoot.value,
      onChange: (val) => handleSingleCheck(row, val)
    })
  },
  {
    title: '文件名',
    key: 'name',
    minWidth: 300,
    cellRenderer: ({ rowData: row }) => {
      const iconClass = `icon-filetype ${getFileIconClass(row)}`
      // 导航到子目录：row.path 已是完整路径 (rootName/subPath)
      const currentNavPath = currentPath.value
      const dirPath = row.path
      const downloadFullPath = row.path
      const fileHref = `/api/v1/admin/download/${PathUtils.encodeFilePath(downloadFullPath)}`
      const isPreview = canPreview(row)

      // 直接浏览模式（与 HomeView 一致）
      const renderNormalMode = () => [
        h('div', { class: 'file-icon-wrapper' }, [
          h('span', { class: iconClass }),
          isPreview && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
        ]),
        isDir(row)
          ? h('a', {
            class: 'file-link dir-link',
            onClick: (e) => {
              e.preventDefault()
              router.push('/admin/files/' + encodePath(dirPath))
            }
          }, highlightKeyword(row.name))
          : h('a', {
            href: fileHref,
            class: 'file-link',
            onClick: (e) => {
              if (isPreview) {
                e.preventDefault()
                previewFile(row)
              }
            }
          }, highlightKeyword(row.name))
      ]

      // 搜索结果模式（显示完整路径，与 HomeView 一致）
      const renderSearchMode = () => {
        const fullPath = row.path || ''
        const segments = fullPath.split('/').filter(Boolean)
        const fileName = segments[segments.length - 1] || ''
        const pathSegments = segments.slice(0, -1)

        return [
          h('div', { class: 'file-icon-wrapper' }, [
            h('span', { class: iconClass }),
            isPreview && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
          ]),
          h('span', { class: 'file-path-content' }, [
            ...pathSegments.map((seg, idx) => {
              const segPath = '/' + pathSegments.slice(0, idx + 1).join('/')
              return [
                h('a', {
                  class: 'path-segment',
                  onClick: (e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    navigateToDir(segPath)
                  }
                }, highlightKeyword(seg)),
                '/'
              ]
            }).flat(),
            isDir(row)
              ? h('a', {
                class: 'file-link dir-link',
                onClick: (e) => {
                  e.preventDefault()
                  navigateToDir(dirPath)
                }
              }, highlightKeyword(fileName))
              : h('a', {
                href: fileHref,
                class: 'file-link',
                onClick: (e) => {
                  if (isPreview) {
                    e.preventDefault()
                    previewFile(row)
                  }
                }
              }, highlightKeyword(fileName))
          ])
        ]
      }

      return h('div', {
        class: 'file-name-cell',
        title: row.name
      }, isSearching.value || searchCompleted.value ? renderSearchMode() : renderNormalMode())
    }
  },
  { title: '大小', key: 'size', width: 150, cellRenderer: ({ rowData: row }) => formatSize(row.size) },
  { title: '修改时间', key: 'modifiedTime', width: 200,
    cellRenderer: ({ rowData: row }) => {
      const text = TimeUtils.formatDateTime(row.modifiedTime)
      if (TimeUtils.isRecent24h(row.modifiedTime)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    cellRenderer: ({ rowData: row }) => {
      // 根目录不显示操作按钮
      if (isAtRoot.value) {
        return null
      }
      return h('div', { class: 'action-buttons' }, [
        h(ElButton, { size: 'small', link: true, onClick: () => openMoveModal(row) }, () => '移动/重命名'),
        h(ElButton, { size: 'small', link: true, type: 'danger', onClick: () => handleDelete(row) }, () => '删除')
      ])
    }
  }
]

// 双击行
const handleDblClick = (row) => {
  if (isDir(row)) {
    enterDir(row)
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
  const splitRegex = new RegExp(`(${escaped.join('|')})`, 'gi')
  const testRegex = new RegExp(`(${escaped.join('|')})`, 'i')
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

// 从 URL 获取当前路径
const getCurrentPathFromRoute = () => {
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
        const aIsDir = a.type === 'dir' || a.type === 'directory'
        const bIsDir = b.type === 'dir' || b.type === 'directory'
        if (aIsDir && !bIsDir) return -1
        if (!aIsDir && bIsDir) return 1
        return compareFileNames(a.name, b.name)
      })

      // 在根目录时一并更新根目录选项（供移动/重命名使用）
      if (!currentPath.value) {
        const dirs = data.data.filter(f => f.type === 'dir' || f.type === 'directory')
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

// 移动/重命名（类似 Linux mv 命令）
const openMoveModal = (file) => {
  currentFile.value = file
  targetRootName.value = file.rootName || ''
  targetSubPath.value = file.path || ''
  moveModalVisible.value = true
}

const handleMove = async () => {
  if (!targetSubPath.value || !targetSubPath.value.trim()) {
    ElMessage.warning('请输入目标路径（子目录或新文件名）')
    return
  }

  // 验证目标路径不能包含 .. 等越权路径
  const subPath = targetSubPath.value.trim()
  if (subPath.includes('..')) {
    ElMessage.error('目标路径无效，不能包含 ..')
    return
  }

  // 目标路径 = 根目录名 + 子路径
  const target = targetRootName.value + '/' + subPath
  // 源路径 = 根目录名 + 原相对路径
  const oldFullPath = currentFile.value.path

  moving.value = true
  try {
    const data = await AdminApi.move(oldFullPath, target)
    if (data.success) {
      ElMessage.success('操作成功')
      moveModalVisible.value = false
      targetSubPath.value = ''
      loadCurrentDir()
    } else {
      ElMessage.error(data.message || '操作失败')
    }
  } catch (e) {
    ElMessage.error('操作失败')
  } finally {
    moving.value = false
  }
}

// 预览对话框状态
const previewDialogVisible = ref(false)
const previewMaximized = ref(false)
const previewFileData = ref({})

// 预览文件
const previewFile = (file) => {
  previewFileData.value = file
  previewDialogVisible.value = true
}

// 下载文件
const downloadFile = (file) => {
  if (file?.path) {
    const fullPath = file.path
    window.open(`/api/v1/admin/download/${PathUtils.encodeFilePath(fullPath)}`, '_blank')
  }
}

// 删除
const deleting = ref(false)
const checkedRowKeys = ref([])
const handleDelete = async (file) => {
  const isDirectory = isDir(file)
  try {
    await ElMessageBox.confirm(
      isDirectory
        ? `确定要删除目录 "${file.name}" 及其所有内容吗？此操作不可恢复。`
        : `确定要删除文件 "${file.name}" 吗？此操作不可恢复。`,
      '确认删除',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消'
      }
    )
  } catch {
    return
  }
  deleting.value = true
  try {
    const deletePath = file.path
    const data = await AdminApi.delete(deletePath)
    if (data.success) {
      ElMessage.success('删除成功')
      loadCurrentDir()
    } else {
      ElMessage.error(data.message || '删除失败')
    }
  } catch (e) {
    ElMessage.error('删除失败')
  } finally {
    deleting.value = false
  }
}

// 批量删除选中的文件
const handleBatchDelete = async () => {
  const selected = displayList.value.filter(f => checkedRowKeys.value.includes(f.path))
  if (selected.length === 0) return
  const dirCount = selected.filter(f => isDir(f)).length
  const fileCount = selected.length - dirCount
  let contentText
  if (dirCount > 0 && fileCount > 0) {
    contentText = `确定要删除选中的 ${fileCount} 个文件和 ${dirCount} 个目录（含目录下所有内容）吗？此操作不可恢复。`
  } else if (dirCount > 0) {
    contentText = `确定要删除选中的 ${dirCount} 个目录（含目录下所有内容）吗？此操作不可恢复。`
  } else {
    contentText = `确定要删除选中的 ${fileCount} 个文件吗？此操作不可恢复。`
  }
  try {
    await ElMessageBox.confirm(contentText, '确认删除', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  deleting.value = true
  try {
    let successCount = 0
    for (const file of selected) {
      const data = await AdminApi.delete(file.path)
      if (data.success) {
        successCount++
      }
    }
    ElMessage.success(`已删除 ${successCount} 个文件`)
    checkedRowKeys.value = []
    loadCurrentDir()
  } catch (e) {
    ElMessage.error('删除失败')
  } finally {
    deleting.value = false
  }
}

// 监听路由变化
watch(
  () => route.params.pathMatch,
  (newPathMatch) => {
    // pathMatch 可能是字符串或字符串数组
    currentPath.value = Array.isArray(newPathMatch) ? newPathMatch.join('/') : (newPathMatch || '')
    updateBreadcrumb()
    // 路由变化时清空搜索状态和选中项
    searchQuery.value = ''
    store.clearSearchState()
    checkedRowKeys.value = []
    loadCurrentDir()
  },
  { immediate: true }
)

onMounted(() => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (tableWrapRef.value) {
    tableResizeObs.observe(tableWrapRef.value)
  }
})

onUnmounted(() => {
  stopPollProgress()
  tableResizeObs?.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-files {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .content-header {
    margin-bottom: 12px;
    flex-shrink: 0;

    .breadcrumb-actions {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;

      .search-status {
        flex: 1;
        font-size: 14px;
        color: #666;
        display: flex;
        align-items: center;
        gap: 12px;

        .search-count {
          color: @primary-color;
          font-weight: 500;
        }

        .search-time {
          color: #999;
        }
      }

      .breadcrumb {
        flex: 1;

        .breadcrumb-link {
          cursor: pointer;
          color: #444;
          transition: color @transition-fast;

          &:hover {
            color: @primary-color;
          }
        }
      }

      .breadcrumb-root {
        font-size: @font-size-base;
        font-weight: 500;
        color: @text-color;
      }

      .header-actions {
        display: flex;
        gap: 8px;
        flex-shrink: 0;

        .search-input {
          width: 200px;
        }
      }
    }
  }

  .scan-progress-text {
    font-size: @font-size-sm;
    color: @text-color-secondary;

    &.completed {
      color: @success-color;
    }

    &.failed {
      color: @error-color;
    }
  }

  .action-divider {
    height: 24px;
  }

  .content-table {
    flex: 1;
    min-height: 0;
    background: #fff;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }

    .table-v2-wrap {
      height: 100%;
    }
  }

  
}

.file-name-cell {
  display: flex;
  align-items: center;
  gap: 8px;

  &:hover {
    .icon-filetype {
      color: #e98b4c;
    }

    .path-segment,
    .file-link {
      color: #FF6600;
    }
  }

  .file-icon-wrapper {
    display: flex;
    align-items: center;
    gap: 4px;

    .icon-filetype {
      color: #888;
    }
  }
}

.icon-filetype {
  color: #888;
  font-size: 20px;
}

.file-link {
  color: #444;
  cursor: pointer;
}

.dir-link {
  cursor: pointer;
  color: #444;

  &:hover {
    color: @primary-color;
  }
}

.help-text {
  font-size: 13px;
  color: @text-color-secondary;
  line-height: 1.8;
  padding: 8px 12px;
  background: @bg-color-secondary;
  border-radius: 4px;
  border-left: 3px solid @primary-color;
  margin-top: 8px;

  p {
    margin: 4px 0;
  }

  strong {
    color: @text-color;
  }

  code {
    background: @bg-color-tertiary;
    padding: 1px 6px;
    border-radius: 3px;
    font-family: monospace;
    color: @primary-color;
  }
}

.target-path-input {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;

  .root-input {
    width: 120px;
    flex-shrink: 0;
  }

  .path-separator {
    color: @text-color-secondary;
    font-size: 16px;
    flex-shrink: 0;
  }

  .sub-path-input {
    flex: 1;
  }
}

@media @tablet {
  .admin-files {
    padding: 12px;
  }

  .breadcrumb-actions {
    flex-wrap: wrap;

    .header-actions {
      width: 100%;
      .search-input {
        flex: 1;
        min-width: 0;
      }
    }
  }
}

@media @mobile {
  .admin-files {
    padding: 8px;
  }
}

.preview-maximized :deep(.el-dialog) {
  height: 100vh;
  max-height: 100vh;
  max-width: 100vw;
  margin: 0;
  top: 0;
}
.preview-maximized :deep(.el-dialog__body) {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
</style>