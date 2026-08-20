<template>
  <div class="admin-files">
    <!-- Header -->
    <div class="content-header">
      <div class="breadcrumb-actions">
        <!-- 搜索状态显示（与 HomeView 一致） -->
        <div v-if="isSearching || searchCompleted" class="search-status">
          <n-spin v-if="isSearching && searchResultCount < 0" size="small" />
          <template v-else>
            <span v-if="isSearching">搜索中...</span>
            <span v-else>搜索完成</span>
            <span class="search-count">{{ searchResultCount }} 个结果</span>
            <span class="search-time">耗时 {{ searchTime }}ms</span>
          </template>
          <n-button v-if="!isSearching" size="tiny" quaternary @click="clearSearch" title="清除搜索">
            ×
          </n-button>
        </div>
        <n-breadcrumb v-else-if="breadcrumb.length > 1" class="breadcrumb">
          <n-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <span class="breadcrumb-link" @click="navigateToBreadcrumb(index)">{{ item.name }}</span>
          </n-breadcrumb-item>
        </n-breadcrumb>
        <span v-else class="breadcrumb-root">文件管理</span>
        <div class="header-actions">
          <n-input ref="searchInputRef" v-model:value="searchQuery" :maxlength="200" placeholder="搜索文件..." size="small"
            class="search-input" clearable name="search-query" @keydown.enter="searchFiles" />
          <n-button size="small" @click="loadCurrentDir" :loading="loading">刷新</n-button>
          <n-divider vertical class="action-divider" />
          <n-button size="small" type="primary" @click="handleScan" :loading="scanning">
            触发全量扫描
          </n-button>
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
      <n-data-table :columns="columns" :data="displayList" :loading="loading || isSearching" :pagination="false"
        :row-key="row => row.path" @dblclick-row="handleDblClick" virtual-scroll flex-height />
    </div>

    <!-- 移动/重命名对话框（类似 Linux mv 命令） -->
    <n-modal v-model:show="moveModalVisible" preset="card" title="移动或重命名"
      style="width: var(--app-width, 600px); max-width: 80vw">
      <n-form label-placement="left" label-width="100">
        <n-form-item label="文件名">
          <n-input :value="currentFile?.name" disabled />
        </n-form-item>
        <n-form-item label="当前路径">
          <n-input :value="currentFile?.path" disabled />
        </n-form-item>
        <n-form-item label="目标路径">
          <div class="target-path-input">
            <n-input :value="targetRootName" disabled placeholder="根目录" class="root-input" />
            <span class="path-separator">/</span>
            <n-input v-model:value="targetSubPath" :maxlength="1024" placeholder="输入子目录/文件名" class="sub-path-input" />
          </div>
          <template #feedback>
            <div class="help-text">
              <p>输入目标路径（相对于根目录），如 <code>newname.pdf</code> 或 <code>subdir/newname.pdf</code></p>
            </div>
          </template>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="moveModalVisible = false">取消</n-button>
          <n-button type="primary" :loading="moving" @click="handleMove">确定</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 预览对话框 -->
    <n-modal v-model:show="previewDialogVisible" preset="card" title="文件预览" :class="['preview-dialog', previewMaximized ? 'preview-maximized' : '']"
      :style="previewMaximized ? { width: '100vw', height: '100vh', maxWidth: '100vw', maxHeight: '100vh', top: 0, left: 0, transform: 'none', borderRadius: 0 } : {}">
      <file-preview :file="previewFileData" :maximized="previewMaximized"
        @close="previewDialogVisible = false" />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <n-button @click="previewMaximized = !previewMaximized">{{ previewMaximized ? '还原' : '最大化' }}</n-button>
          <n-button type="primary" @click="downloadFile(previewFileData)">下载</n-button>
          <n-button @click="previewDialogVisible = false">关闭</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, h, watch, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NBreadcrumb, NBreadcrumbItem, NButton, NDataTable, NModal, NForm, NFormItem,
  NInput, NTreeSelect, NIcon, NSpin,
  NTag, NDivider,
  useMessage, useDialog, useLoadingBar
} from 'naive-ui'
import { AdminApi, IndexApi } from '@/api'
import { NumberUtils, TimeUtils, PathUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import FilePreview from '@/components/file/FilePreview.vue'
import { isPreviewable } from '@/config/preview'
import store from '@/store'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const loadingBar = useLoadingBar()
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
      message.success('扫描已启动')
      startPollProgress()
    }
  } catch (err) {
    message.error(formatErrorMessage(err, '启动扫描失败'))
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

// Table columns
const columns = [
  {
    title: '文件名',
    key: 'name',
    ellipsis: { tooltip: true },
    render(row) {
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
  { title: '大小', key: 'size', width: 150, render: (row) => formatSize(row.size) },
  { title: '修改时间', key: 'modifiedTime', width: 200,
    render: (row) => {
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
    render(row) {
      // 根目录不显示操作按钮
      if (isAtRoot.value) {
        return null
      }
      const actions = []
      actions.push(
        h(NButton, { size: 'small', quaternary: true, onClick: () => openMoveModal(row) }, () => '移动/重命名'),
        h(NButton, { size: 'small', quaternary: true, type: 'error', onClick: () => handleDelete(row) }, () => '删除')
      )
      return h('div', { class: 'action-buttons' }, actions)
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
  store.clearSearchState()
  loadCurrentDir()
}

// 点击路径段导航到目录
const navigateToDir = (dirPath) => {
  searchQuery.value = ''
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
        return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
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
    message.error('加载文件列表失败')
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
    message.warning('请输入目标路径（子目录或新文件名）')
    return
  }

  // 验证目标路径不能包含 .. 等越权路径
  const subPath = targetSubPath.value.trim()
  if (subPath.includes('..')) {
    message.error('目标路径无效，不能包含 ..')
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
      message.success('操作成功')
      moveModalVisible.value = false
      targetSubPath.value = ''
      loadCurrentDir()
    } else {
      message.error(data.message || '操作失败')
    }
  } catch (e) {
    message.error('操作失败')
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
const handleDelete = async (file) => {
  dialog.warning({
    title: '确认删除',
    content: `确定要删除文件 "${file.name}" 吗？此操作不可恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      deleting.value = true
      try {
        const deletePath = file.path
        const data = await AdminApi.delete(deletePath)
        if (data.success) {
          message.success('删除成功')
          loadCurrentDir()
        } else {
          message.error(data.message || '删除失败')
        }
      } catch (e) {
        message.error('删除失败')
      } finally {
        deleting.value = false
      }
    }
  })
}

// 监听路由变化
watch(
  () => route.params.pathMatch,
  (newPathMatch) => {
    // pathMatch 可能是字符串或字符串数组
    currentPath.value = Array.isArray(newPathMatch) ? newPathMatch.join('/') : (newPathMatch || '')
    updateBreadcrumb()
    // 路由变化时清空搜索状态
    searchQuery.value = ''
    store.clearSearchState()
    loadCurrentDir()
  },
  { immediate: true }
)

onUnmounted(() => {
  stopPollProgress()
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
    height: 100%;
    background: #fff;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }

    :deep(.n-data-table) {
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

.preview-maximized :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
