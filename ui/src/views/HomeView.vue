<template>
  <div class="home-view">
    <!-- Breadcrumb + Actions -->
    <div class="content-header">
      <div class="breadcrumb-actions">
        <!-- 搜索状态显示 -->
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
            <a :href="getBreadcrumbHref(item)" class="breadcrumb-link">{{ item.name }}</a>
          </n-breadcrumb-item>
        </n-breadcrumb>
        <span v-else class="breadcrumb-root">文件</span>
        <div class="header-actions">
          <n-input ref="searchInputRef" v-model:value="searchQuery" :maxlength="200" placeholder="搜索文件..." size="small"
            class="search-input" clearable @keydown.enter="searchFiles" />
          <n-button @click="searchFiles" size="small">
            搜索
          </n-button>
          <n-button @click="showUploadDialog" type="primary" size="small">
            上传
          </n-button>
        </div>
      </div>
    </div>

    <!-- File List -->
    <div class="content-table">
      <n-data-table
        :columns="columns"
        :data="fileList"
        size="small"
        :pagination="false"
        :row-key="row => row.path"
        :bordered="false"
        :loading="store.state.loading || isSearching"
        virtual-scroll
        flex-height
      />
    </div>

    <dependency-tree-dialog ref="depTreeDialogRef" />
    <!-- 备注编辑弹窗 -->
    <n-modal v-model:show="notesModalVisible" preset="card" title="编辑备注" style="width: 500px">
      <n-input v-model:value="editNotes" :maxlength="500" type="textarea" :rows="4" placeholder="输入备注内容..." />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <n-button @click="notesModalVisible = false">取消</n-button>
          <n-button type="primary" :loading="savingNotes" @click="saveNotes">保存</n-button>
        </div>
      </template>
    </n-modal>
    <!-- Dialogs -->
    <n-modal :show="uploadDialogVisible" preset="card" title="上传文件" class="upload-dialog"
      @update:show="onUploadDialogShowChange" :mask-closable="false" :closeable="false">
      <upload-manager ref="uploadManagerRef" :upload-api="uploadApi" @upload-start="onUploadStart"
        @upload-success="onUploadSuccess" @upload-error="onUploadError" @upload-change="uploadQueueCount = $event"
        @close="handleUploadDialogClose" />
    </n-modal>

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
import { ref, computed, h, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NButton, NDataTable, NBreadcrumb, NBreadcrumbItem, NModal, NInput, NSpin, useMessage, useDialog } from 'naive-ui'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'
import DependencyTreeDialog from '@/components/file/DependencyTreeDialog.vue'
import { NumberUtils, TimeUtils, PathUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'
import { isPreviewable } from '@/config/preview'
import { FileRecordApi } from '@/api'
import { isAppInitialized, initializationComplete } from '@/main'

// 上传 API
const uploadApi = {
  type: 'chunked',
  createSession: () => '/api/v1/uploads/session',
  getSession: (uploadId) => `/api/v1/uploads/session/${uploadId}`,
  cancel: (uploadId) => `/api/v1/uploads/session/${uploadId}`,
  uploadChunk: () => '/api/v1/uploads/chunk',
  finalize: () => '/api/v1/uploads/finalize'
}

const router = useRouter()
const route = useRoute()
const message = useMessage()
const modalDialog = useDialog()

// 备注编辑
const notesModalVisible = ref(false)
const editNotes = ref('')
const editNotesRow = ref(null)
const savingNotes = ref(false)

const openNotesEditor = (row) => {
  editNotesRow.value = row
  editNotes.value = row.notes || ''
  notesModalVisible.value = true
}

const saveNotes = async () => {
  const row = editNotesRow.value
  if (!row) {
    message.warning('无法获取文件记录')
    return
  }
  savingNotes.value = true
  try {
    // 如果 row 没有 recordId，通过索引表查询
    let recordId = row.recordId
    if (!recordId) {
      const fullPath = row.path || ''
      const rootName = row.rootName || ''
      const fileName = row.name || ''
      // row.path 现在是完整路径 (rootName/relativePath)，需要提取相对路径
      const indexPath = rootName ? fullPath.slice(rootName.length + 1) : fullPath
      const findRes = await FileRecordApi.findRecord(fileName, rootName, indexPath)
      if (!findRes.success || !findRes.data?.record) {
        message.warning('未找到文件索引记录，请稍后重试')
        return
      }
      recordId = findRes.data.record.id
    }
    const res = await FileRecordApi.updateNotes(recordId, editNotes.value)
    if (res.success) {
      message.success('备注已更新')
      store.setFileNotes(row.path, editNotes.value, recordId)
      notesModalVisible.value = false
    } else {
      message.error(res.message || '更新备注失败')
    }
  } catch (e) {
    message.error(formatErrorMessage(e, '更新备注失败'))
  } finally {
    savingNotes.value = false
  }
}
const uploadDialogVisible = ref(false)
const previewDialogVisible = ref(false)
const previewMaximized = ref(false)
const previewFileData = ref({})
const uploadManagerRef = ref(null)
const uploadQueueCount = ref(0)
const depTreeDialogRef = ref(null)
const searchQuery = ref('')
const searchInputRef = ref(null)
const searchResultCount = ref(-1)
const searchTime = ref(0)
const searchStartTime = ref(0)

let searchTimer = null
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

const fileList = computed(() => store.state.fileList)
const breadcrumb = computed(() => store.state.breadcrumb)
const searchState = computed(() => store.state.searchState)

const isSearching = computed(() => searchState.value.isSearching)
const searchCompleted = computed(() => searchState.value.isCompleted)

const isDir = (row) => row.type === 'dir' || row.type === 'directory'

// HTML 转义函数
const escapeHtml = (str) => {
  if (typeof str !== 'string') return ''
  const escapeMap = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }
  return str.replace(/[&<>"']/g, c => escapeMap[c] || c)
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

// 高亮搜索关键字（返回 VNode 数组），支持多关键词
const highlightKeyword = (text) => {
  if (!searchQuery.value.trim() || (!isSearching.value && !searchCompleted.value)) {
    return escapeHtml(String(text || ''))
  }
  const allKeywords = searchQuery.value.trim().split(/\s+/)
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

const getFileIconClass = (row) => {
  if (isDir(row)) return 'icon-filetype-folder'
  const ext = row.name?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

const canPreview = (row) => {
  if (isDir(row)) return false
  return isPreviewable(row.name)
}

// 对路径的每段分别编码，避免斜杠被编码
const encodePath = (path) => {
  return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
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


// 点击目录时加载该目录并清空搜索
const navigateToDir = (dirPath) => {
  searchQuery.value = ''
  store.clearSearchState()
  router.push('/files/' + encodePath(dirPath))
}

const columns = [
  {
    title: '文件名',
    key: 'name',
    ellipsis: { tooltip: true },
    render(row) {
      const iconClass = `icon-filetype ${getFileIconClass(row)}`
      // 导航到子目录：row.path 已是完整路径 (rootName/subPath)
      const currentNavPath = store.state.currentPath
      const dirPath = row.path
      const downloadFullPath = row.path
      const fileHref = `/api/v1/download/${PathUtils.encodeFilePath(downloadFullPath)}`
      const isPreview = canPreview(row)
      const fullPath = row.path || ''
      const segments = fullPath.split('/').filter(Boolean)
      const fileName = segments[segments.length - 1] || ''
      const pathSegments = segments.slice(0, -1)

      // 直接浏览模式（不显示路径）
      const renderNormalMode = () => [
        h('div', { class: 'file-icon-wrapper' }, [
          h('span', { class: iconClass }),
          isPreview && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
        ]),
        isDir(row)
          ? h('a', {
            class: 'file-link',
            onClick: (e) => {
              e.preventDefault()
              router.push('/files/' + encodePath(dirPath))
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
      ]

      // 搜索结果模式（显示完整路径）
      const renderSearchMode = () => [
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
              class: 'file-link',
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

      return h('div', {
        class: 'file-name-cell',
        title: fileName
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
  { title: '备注', key: 'notes', width: 150, ellipsis: { tooltip: true },
    render: (row) => {
      return h('span', {
        class: `notes-cell`,
        onClick: () => openNotesEditor(row)
      }, row.notes || '')
    }
  },
  { title: '依赖', width: 80,
    render: (row) => isDir(row) ? null : h(NButton, {
      size: 'tiny', quaternary: true,
      onClick: () => openDepTree(row)
    }, () => '依赖')
  }
]

const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

const showUploadDialog = () => { uploadDialogVisible.value = true }

// 获取面包屑链接地址（对每段路径分别编码，避免斜杠被编码）
const getBreadcrumbHref = (item) => {
  const path = item.path === '/' ? '' : item.path.replace(/^\//, '')
  if (!path) return '#/files'
  const encodedPath = path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
  return `#/files/${encodedPath}`
}

const previewFile = (file) => {
  previewFileData.value = file
  previewDialogVisible.value = true
}

const downloadFile = (file) => {
  if (file?.path) {
    const fullPath = file.path
    window.open(`/api/v1/download/${PathUtils.encodeFilePath(fullPath)}`, '_blank')
  }
}

// 打开依赖树
const openDepTree = (row) => {
  // 如果有 recordId 直接用，否则尝试从文件名搜索
  if (row.recordId) {
    depTreeDialogRef.value?.open(row.recordId)
  } else {
    // 通过文件名搜索记录
    FileRecordApi.searchFiles(row.name).then(res => {
      if (res.success && res.data.records?.length > 0) {
        depTreeDialogRef.value?.open(res.data.records[0].id)
      } else {
        message.warning('未找到文件索引记录')
      }
    })
  }
}

const onUploadSuccess = () => {
  store.actions.loadFileList(store.state.currentPath || '/')
}

const onUploadError = (error) => {
  console.error('Upload error:', error)
}

// Lifecycle
onMounted(async () => {
  window.addEventListener('keydown', handleKeydown)

  // 等待 main.js 中的初始化检查完成（避免重复调用 setup/status）
  if (!initializationComplete) {
    // 轮询等待最多 5 秒
    for (let i = 0; i < 50; i++) {
      await new Promise(r => setTimeout(r, 100))
      if (initializationComplete) break
    }
  }

  if (!isAppInitialized) {
    router.push('/setup')
    return
  }

  store.actions.initialize()

  // 优先处理 q=xx 搜索参数
  const q = route.query.q
  if (q) {
    searchQuery.value = q
    searchFiles()
    return
  }

  // 从路由参数获取路径
  const pathMatch = route.params.pathMatch
  if (pathMatch) {
    const path = Array.isArray(pathMatch) ? pathMatch.join('/') : pathMatch
    store.actions.loadFileList('/' + decodeURIComponent(path))
  } else {
    store.actions.loadFileList('/')
  }
})

// Ctrl+F 聚焦搜索框
const handleKeydown = (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
    e.preventDefault()
    searchInputRef.value?.focus()
  }
}

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})

// 监听路由参数变化
watch(
  () => route.params.pathMatch,
  (newPathMatch) => {
    if (newPathMatch) {
      const path = Array.isArray(newPathMatch) ? newPathMatch.join('/') : newPathMatch
      store.actions.loadFileList('/' + decodeURIComponent(path))
    } else {
      store.actions.loadFileList('/')
    }
  }
)

const onUploadStart = () => {
  // 上传开始
}

// 上传弹框关闭保护
// 由于使用了 :show 而非 v-model:show，需要手动控制显示状态
const handleUploadDialogClose = () => {
  const mgr = uploadManagerRef.value
  if (mgr?.hasActiveUploads) {
    // 本地文件正在上传，需要确认
    modalDialog.warning({
      title: '上传进行中',
      content: '有文件正在上传，关闭弹框将中断所有上传。是否确认关闭？',
      positiveText: '确认关闭',
      negativeText: '继续上传',
      onPositiveClick: () => {
        uploadDialogVisible.value = false
      },
      onNegativeClick: () => {
        // 不关闭，保持打开
      }
    })
    return
  } else if (mgr?.hasUrlUploading) {
    // URL 上传在后台执行，仅提示
    message.info('URL 下载在后台继续执行，您可以在通知中查看进度', { duration: 4000 })
  }
  uploadDialogVisible.value = false
}

// 拦截模态框关闭事件（点击遮罩或按 ESC）
const onUploadDialogShowChange = (show) => {
  if (!show) {
    handleUploadDialogClose()
  }
}

// 重写 showUploadDialog 以兼容 :show 模式
const showUploadDialogInternal = () => {
  uploadDialogVisible.value = true
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.home-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .content-header {
    flex: 0 0 auto;
    margin-bottom: 16px;

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
          text-decoration: none;
          color: inherit;
          transition: color @transition-fast;

          &:hover {
            color: @primary-color;
          }
        }
      }

      .breadcrumb-root {
        flex: 1;
        font-size: 14px;
        font-weight: 500;
        color: #333;
      }

      .header-actions {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-shrink: 0;

        .search-input {
          width: 200px;
        }

        .n-button {
          transition: transform @transition-smooth, box-shadow @transition-smooth;

          &:hover {
            transform: translateY(-1px);
            box-shadow: @button-hover-shadow;
          }

          &:active {
            transform: scale(0.97);
          }
        }
      }
    }
  }

  .content-table {
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

/* 小屏头部布局调整 */
@media @tablet {
  .home-view {
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

  .breadcrumb, .breadcrumb-root, .search-status {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

@media @mobile {
  .home-view {
    padding: 8px;
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
}

.action-buttons {
  display: flex;
  gap: 4px;
}


.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.preview-maximized :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
<style lang="less">
.notes-cell {
  cursor: pointer;
  color: #666;
  font-size: 12px;
}

.n-data-table-tbody {
  .n-data-table-tr {
    &:hover {
      .notes-cell {
        &:empty:after {
          content: '点击填写备注';
          color: #bbb;
        }
      }
    }
  }
}
</style>
