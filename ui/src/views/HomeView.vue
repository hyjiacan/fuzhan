<template>
  <div class="home-view">
    <!-- Breadcrumb + Actions -->
    <div class="content-header">
      <div class="breadcrumb-actions">
        <!-- 搜索状态显示 -->
        <div v-if="isSearching || searchCompleted" class="search-status">
          <el-icon v-if="isSearching && searchResultCount < 0" class="is-loading" :size="16"><Loading /></el-icon>
          <template v-else>
            <span v-if="isSearching">搜索中...</span>
            <span v-else>搜索完成</span>
            <span class="search-count">{{ searchResultCount }} 个结果</span>
            <span class="search-time">耗时 {{ searchTime }}ms</span>
          </template>
          <el-button v-if="!isSearching" link size="small" @click="clearSearch" title="清除搜索">
            ×
          </el-button>
        </div>
        <el-breadcrumb v-else-if="breadcrumb.length > 1" class="breadcrumb" separator="/">
          <el-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <a :href="getBreadcrumbHref(item)" class="breadcrumb-link">{{ item.name }}</a>
          </el-breadcrumb-item>
        </el-breadcrumb>
        <span v-else class="breadcrumb-root">文件</span>
        <div class="header-actions">
          <el-autocomplete ref="searchInputRef" v-model="searchQuery" :maxlength="200"
            :fetch-suggestions="querySuggestions" :trigger-on-focus="false" placeholder="搜索文件..."
            size="small" clearable highlight-first-item @select="onSuggestionSelect" @keydown.enter="searchFiles"
            class="search-input">
            <template #default="{ item }">
              <div class="suggest-item">
                <span class="suggest-name">{{ item.value }}</span>
                <span v-if="item.corrected" class="suggest-tag">纠错</span>
              </div>
            </template>
          </el-autocomplete>
          <el-button @click="searchFiles" size="small">
            搜索
          </el-button>
          <el-button @click="showUploadDialog" type="primary" size="small">
            上传
          </el-button>
        </div>
      </div>
    </div>

    <!-- File List -->
    <div class="content-table">
      <div ref="tableWrapRef" class="table-v2-wrap">
        <el-table-v2
          :columns="columns"
          :data="fileList"
          :width="tableWidth"
          :height="tableHeight"
          :row-height="32"
          row-key="path"
        />
      </div>
    </div>

    <dependency-tree-dialog ref="depTreeDialogRef" />
    <!-- 备注编辑弹窗 -->
    <el-dialog v-model="notesModalVisible" title="编辑备注" width="500px">
      <el-input v-model="editNotes" type="textarea" :rows="4" maxlength="500" placeholder="输入备注内容..." />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="notesModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="savingNotes" @click="saveNotes">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 上传弹窗（关闭保护：由 UploadManager 的 close 事件控制） -->
    <el-dialog v-model="uploadDialogVisible" title="上传文件" class="upload-dialog" width="600px"
      :close-on-click-modal="false" :close-on-press-escape="false" :show-close="false">
      <upload-manager ref="uploadManagerRef" :upload-api="uploadApi" @upload-start="onUploadStart"
        @upload-success="onUploadSuccess" @upload-error="onUploadError" @upload-change="uploadQueueCount = $event"
        @close="handleUploadDialogClose" />
    </el-dialog>

    <!-- 文件预览弹窗 -->
    <el-dialog
      v-model="previewDialogVisible"
      title="文件预览"
      width="80%"
      top="5vh"
      :class="['preview-dialog', previewMaximized ? 'preview-maximized' : '']">
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
import { ref, computed, h, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox, ElButton } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'
import DependencyTreeDialog from '@/components/file/DependencyTreeDialog.vue'
import { NumberUtils, TimeUtils, PathUtils, analyzeLatestVersions } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'
import { isPreviewable } from '@/config/preview'
import { FileRecordApi, SearchApi } from '@/api'
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
    ElMessage.warning('无法获取文件记录')
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
        ElMessage.warning('未找到文件索引记录，请稍后重试')
        return
      }
      recordId = findRes.data.record.id
    }
    const res = await FileRecordApi.updateNotes(recordId, editNotes.value)
    if (res.success) {
      ElMessage.success('备注已更新')
      store.setFileNotes(row.path, editNotes.value, recordId)
      notesModalVisible.value = false
    } else {
      ElMessage.error(res.message || '更新备注失败')
    }
  } catch (e) {
    ElMessage.error(formatErrorMessage(e, '更新备注失败'))
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

// 搜索框下拉推荐：自动补全 + 拼写纠错
const querySuggestions = (queryString, cb) => {
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
}

// 选中推荐项：用该文件名发起搜索
const onSuggestionSelect = (item) => {
  searchQuery.value = item.value
  searchFiles()
}

const fileList = computed(() => store.state.fileList)
const breadcrumb = computed(() => store.state.breadcrumb)
const searchState = computed(() => store.state.searchState)

const isSearching = computed(() => searchState.value.isSearching)
const searchCompleted = computed(() => searchState.value.isCompleted)

// 版本标记：仅浏览模式（非搜索）下，分析当前目录，返回"最新版本"文件的 path 集合
const latestVersionPaths = computed(() => {
  if (isSearching.value || searchCompleted.value) return new Set()
  return analyzeLatestVersions(fileList.value)
})

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
    minWidth: 240,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => {
      const iconClass = `icon-filetype ${getFileIconClass(row)}`
      const isLatest = latestVersionPaths.value.has(row.path)
      // 导航到子目录：row.path 已是完整路径 (rootName/subPath)
      const currentNavPath = store.state.currentPath
      const dirPath = row.path
      const downloadFullPath = row.path
      const fileHref = `/download/${PathUtils.encodeFilePath(downloadFullPath)}`
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
            class: isLatest ? 'file-link latest-version' : 'file-link',
            onClick: (e) => {
              if (isPreview) {
                e.preventDefault()
                previewFile(row)
              }
            }
          }, [
            highlightKeyword(fileName),
            isLatest ? h('span', { class: 'latest-version-tag' }, '最新') : null
          ])
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
  {
    title: '大小', key: 'size', width: 150,
    cellRenderer: ({ rowData: row }) => formatSize(row.size)
  },
  {
    title: '修改时间', key: 'modifiedTime', width: 200,
    cellRenderer: ({ rowData: row }) => {
      const text = TimeUtils.formatDateTime(row.modifiedTime)
      if (TimeUtils.isRecent24h(row.modifiedTime)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  },
  {
    title: '备注', key: 'notes', width: 150,
    cellRenderer: ({ rowData: row }) => h(ElButton, {
      size: 'small', link: true,
      class: ['notes-link', { 'is-empty': !row.notes }],
      onClick: () => openNotesEditor(row)
    }, () => row.notes || '添加备注')
  },
  {
    title: '依赖', key: 'deps', width: 80,
    cellRenderer: ({ rowData: row }) => isDir(row) ? null : h(ElButton, {
      size: 'small', link: true,
      onClick: () => openDepTree(row)
    }, () => '依赖')
  }
]

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
    window.open(`/download/${PathUtils.encodeFilePath(fullPath)}`, '_blank')
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
        ElMessage.warning('未找到文件索引记录')
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

  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (tableWrapRef.value) {
    tableResizeObs.observe(tableWrapRef.value)
  }

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
  tableResizeObs?.disconnect()
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
const handleUploadDialogClose = () => {
  const mgr = uploadManagerRef.value
  if (mgr?.hasActiveUploads) {
    // 本地文件正在上传，需要确认
    ElMessageBox.confirm('有文件正在上传，关闭弹框将中断所有上传。是否确认关闭？', '上传进行中', {
      confirmButtonText: '确认关闭',
      cancelButtonText: '继续上传',
      type: 'warning'
    }).then(() => {
      uploadDialogVisible.value = false
    }).catch(() => {
      // 不关闭，保持打开
    })
    return
  } else if (mgr?.hasUrlUploading) {
    // URL 上传在后台执行，仅提示
    ElMessage.info('URL 下载在后台继续执行，您可以在通知中查看进度', { duration: 4000 })
  }
  uploadDialogVisible.value = false
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

        .el-button {
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
    flex: 1 1 auto;
    min-height: 0;
    background: #fff;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    transition: box-shadow @transition-smooth;
    overflow: hidden;

    &:hover {
      box-shadow: @shadow-md;
    }

    .table-v2-wrap {
      height: 100%;
      width: 100%;
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
  min-width: 0;
  overflow: hidden;

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
    flex-shrink: 0;

    .icon-filetype {
      color: #888;
    }
  }

  .file-path-content {
    display: inline-flex;
    align-items: center;
    min-width: 0;
    overflow: hidden;
  }

  .file-link {
    display: block;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-link.latest-version {
    font-weight: 700;
  }

  .latest-version-tag {
    flex-shrink: 0;
    margin-left: 4px;
    padding: 0 6px;
    font-size: 11px;
    line-height: 18px;
    font-weight: 500;
    color: #fff;
    background: #FF6600;
    border-radius: 3px;
    white-space: nowrap;
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

/* 搜索框自动补全/纠错下拉（popper 挂载于 body，需全局样式） */
.el-autocomplete-suggestion {
  .suggest-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
  }

  .suggest-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .suggest-tag {
    flex-shrink: 0;
    margin-left: 8px;
    padding: 0 6px;
    font-size: 11px;
    line-height: 18px;
    font-weight: 500;
    color: #fff;
    background: #FF6600;
    border-radius: 3px;
    white-space: nowrap;
  }
}
</style>
