<template>
  <div class="home-view" @dragenter.prevent="handlePageDragEnter" @dragover.prevent="handlePageDragOver"
    @dragleave.prevent="handlePageDragLeave" @drop.prevent="handlePageDrop">
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
            clearable @select="onSuggestionSelect" @keydown.enter="searchFiles"
            @focus="searchInputFocused = true" @blur="searchInputFocused = false"
            class="search-input" :style="searchInputWidth" size="large">
            <template #default="{ item }">
              <div class="suggest-item">
                <span class="suggest-name">{{ item.value }}</span>
                <span v-if="item.corrected" class="suggest-tag">纠错</span>
              </div>
            </template>
          </el-autocomplete>
          <el-button @click="searchFiles" size="large">
            搜索
          </el-button>
          <el-button @click="showUploadDialog" type="primary">
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
          :data="sortedFileList"
          :width="tableWidth"
          :height="tableHeight"
          :row-height="32"
          row-key="path"
          :sort-state="sortState"
          @column-sort="onColumnSort"
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

    <!-- 上传弹窗（dialog 集成在 UploadManager 组件内） -->
    <upload-manager ref="uploadManagerRef" v-model="uploadDialogVisible" :upload-api="uploadApi" title="上传文件"
      @upload-success="onUploadSuccess" @upload-error="onUploadError" />

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

    <!-- 全局拖放上传提醒 -->
    <transition name="drop-overlay-fade">
      <div v-if="isPageDragging" class="global-drop-overlay">
        <div class="global-drop-overlay-content">
          <el-icon :size="64" color="#fff"><component :is="UploadIcon" /></el-icon>
          <div class="overlay-title">松开以上传文件</div>
          <div class="overlay-sub">{{ dragFileCount }} 个文件即将上传</div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'
import DependencyTreeDialog from '@/components/file/DependencyTreeDialog.vue'
import { PathUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'
import { FileRecordApi } from '@/api'
import { isAppInitialized, initializationComplete } from '@/main'
import { useHomeSearch } from './home/useHomeSearch'
import { useHomeDragDrop } from './home/useHomeDragDrop'
import { useHomeTable } from './home/useHomeTable'
import { createColumns } from './home/columns'

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

// 上传图标
const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])

// ===== 搜索逻辑 =====
const searchInputFocused = ref(false)
const searchInputWidth = computed(() => ({ width: searchInputFocused.value ? '400px' : '200px' }))
const {
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
} = useHomeSearch()

// ===== 页面级拖放上传 =====
const uploadManagerRef = ref(null)
const {
  uploadDialogVisible,
  isPageDragging,
  dragFileCount,
  handlePageDragEnter,
  handlePageDragOver,
  handlePageDragLeave,
  handlePageDrop,
  showUploadDialog
} = useHomeDragDrop(uploadManagerRef)

// ===== 表格（尺寸/排序/版本标记）=====
const {
  isDir,
  latestVersionPaths,
  sortState,
  onColumnSort,
  sortedFileList,
  encodePath
} = useHomeTable({ isSearching, searchCompleted })

const breadcrumb = computed(() => store.state.breadcrumb)

// 对路径的每段分别编码，避免斜杠被编码
// 点击目录时加载该目录并清空搜索
const navigateToDir = (dirPath) => {
  resetSearch()
  router.push('/files/' + encodePath(dirPath))
}

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
// 公开文件的管理（重命名/移动/删除）已迁移至管理员页面

// 预览与下载
const previewDialogVisible = ref(false)
const previewMaximized = ref(false)
const previewFileData = ref({})

const previewFile = (file) => {
  previewFileData.value = file
  previewDialogVisible.value = true
}

const downloadFile = (file) => {
  if (file?.path) {
    const fullPath = file.path
    window.open(`/download/${PathUtils.encodeFilePath(fullPath)}`, '_blank')
    refreshAfterDownload()
  }
}

// 下载后刷新文件列表，让下载次数列更新（搜索模式重新搜索）
let downloadRefreshTimer = null
const refreshAfterDownload = () => {
  clearTimeout(downloadRefreshTimer)
  downloadRefreshTimer = setTimeout(() => {
    if (isSearching.value || searchCompleted.value) {
      if (searchQuery.value.trim()) {
        store.actions.searchFiles(searchQuery.value)
      }
    } else {
      store.actions.loadFileList(store.state.currentPath || '/')
    }
  }, 500)
}

// 依赖树
const depTreeDialogRef = ref(null)
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

const columns = createColumns({
  latestVersionPaths,
  isSearching,
  searchCompleted,
  searchQuery,
  navigateToDir,
  previewFile,
  openNotesEditor,
  openDepTree,
  refreshAfterDownload
})

const onUploadSuccess = () => {
  store.actions.loadFileList(store.state.currentPath || '/')
}

const onUploadError = () => {
  // 上传失败时同样刷新列表（可能产生了部分文件）；逐项错误已由上传组件内部展示
  store.actions.loadFileList(store.state.currentPath || '/')
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

// 获取面包屑链接地址（对每段路径分别编码，避免斜杠被编码）
const getBreadcrumbHref = (item) => {
  const path = item.path === '/' ? '' : item.path.replace(/^\//, '')
  if (!path) return '#/files'
  const encodedPath = path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
  return `#/files/${encodedPath}`
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
  clearTimeout(downloadRefreshTimer)
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
          transition: width @transition-smooth;

          :deep(.el-input) {
            width: 100%;
          }
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

  // 页面级拖放上传遮罩
  .global-drop-overlay {
    position: fixed;
    inset: 0;
    z-index: 3000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(33, 33, 33, 0.72);
    animation: dropOverlayIn 0.18s ease-out;

    .global-drop-overlay-content {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 12px;
      padding: 40px 56px;
      background: rgba(0, 0, 0, 0.55);
      border: 2px dashed rgba(255, 255, 255, 0.6);
      border-radius: 12px;
      color: #fff;

      .overlay-title {
        font-size: 18px;
        font-weight: 600;
      }

      .overlay-sub {
        font-size: 13px;
        color: rgba(255, 255, 255, 0.75);
      }
    }
  }
}

.drop-overlay-fade-enter-active,
.drop-overlay-fade-leave-active {
  transition: opacity 0.15s ease;
}

.drop-overlay-fade-enter-from,
.drop-overlay-fade-leave-to {
  opacity: 0;
}

@keyframes dropOverlayIn {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
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
}

.icon-filetype {
  color: #888;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
</style>
<style lang="less">
// 依据基名判定"最新版本"的文件使用绿色加粗标识。
// 该样式必须放在非 scoped 区块：el-table-v2 的列渲染通过 h() 动态创建节点，
// 不携带组件的 data-v 属性，scoped 选择器无法命中。
.file-link.latest-version {
  color: #2e8b57;
  font-weight: 700;
}

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
