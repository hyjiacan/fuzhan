<template>
  <div class="home-view page-container page-container--fill" @dragenter.prevent="handlePageDragEnter" @dragover.prevent="handlePageDragOver"
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
            class="search-input" :style="searchInputWidth">
            <template #default="{ item }">
              <div class="suggest-item">
                <span class="suggest-text">
                  <span v-if="item.prefix" class="suggest-prefix">{{ item.prefix }}</span>
                  <span class="suggest-name">{{ item.token }}</span>
                </span>
                <span v-if="item.corrected" class="suggest-tag">纠错</span>
                <span v-else-if="item.related" class="suggest-tag suggest-tag--related">相关</span>
              </div>
            </template>
          </el-autocomplete>
          <el-button @click="searchFiles">
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
          :row-height="TableConst.ROW_HEIGHT"
          row-key="path"
          :sort-state="sortState"
          @column-sort="onColumnSort"
        >
          <template #empty>
            <div class="table-empty">
              <el-empty v-if="isSearching || searchCompleted" description="未找到匹配的文件" :image-size="72">
                <template #default>
                  <el-button type="primary" plain @click="clearSearch">
                    <el-icon><component :is="BackIcon" /></el-icon>
                    清除搜索
                  </el-button>
                </template>
              </el-empty>
              <el-empty v-else description="当前目录暂无文件" :image-size="72">
                <template #default>
                  <el-button type="primary" @click="showUploadDialog">
                    <el-icon><component :is="UploadIcon" /></el-icon>
                    上传第一个文件
                  </el-button>
                </template>
              </el-empty>
            </div>
          </template>
        </el-table-v2>
      </div>
    </div>

    <dependency-tree-dialog ref="depTreeDialogRef" @changed="refreshListDebounced" />

    <!-- 重命名文件弹窗 -->
    <el-dialog v-model="renameModalVisible" title="重命名文件" width="480px">
      <el-form label-width="90px">
        <el-form-item label="当前文件名">
          <el-input :model-value="renameTarget?.name" disabled />
        </el-form-item>
        <el-form-item label="新文件名">
          <el-input v-model="renameNewName" maxlength="255" placeholder="输入新的文件名" @keyup.enter="handleRename" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="renameModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="renaming" @click="handleRename">确定</el-button>
        </el-space>
      </template>
    </el-dialog>

    <!-- 移动文件弹窗 -->
    <el-dialog v-model="moveModalVisible" title="移动文件" width="560px">
      <el-form label-width="90px">
        <el-form-item label="文件名">
          <el-input :model-value="moveTarget?.name" disabled />
        </el-form-item>
        <el-form-item label="当前路径">
          <el-input :model-value="moveTarget?.path" disabled />
        </el-form-item>
        <el-form-item label="目标路径">
          <div class="target-path-input">
            <el-input :model-value="moveTargetRoot" disabled placeholder="根目录" class="root-input" />
            <span class="path-separator">/</span>
            <el-input v-model="moveTargetSubPath" maxlength="1024" placeholder="输入子目录/文件名" class="sub-path-input" />
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

    <!-- 备注编辑弹窗 -->
    <el-dialog v-model="notesModalVisible" title="编辑备注" width="500px" :class="notesMaximized ? 'preview-maximized' : ''">
      <el-input v-model="editNotes" type="textarea" :rows="4" maxlength="4096" placeholder="输入备注内容..." />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="notesMaximized = !notesMaximized">{{ notesMaximized ? '还原' : '最大化' }}</el-button>
          <el-button @click="notesModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="savingNotes" @click="saveNotes">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 上传弹窗（dialog 集成在 UploadManager 组件内） -->
    <upload-manager ref="uploadManagerRef" v-model="uploadDialogVisible" :upload-api="uploadApi" title="上传文件"
      @upload-success="onUploadSuccess" @upload-error="onUploadError" />

    <!-- 文件预览弹窗（dialog 集成在 FilePreview 组件内） -->
    <file-preview v-model="previewDialogVisible" :file="previewFileData" :download="downloadFile" />

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
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'
import DependencyTreeDialog from '@/components/file/DependencyTreeDialog.vue'
import { TableConst, PathUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import { downloadWithTicket } from '@/utils/download'
import store from '@/store'
import { FileRecordApi, FileApi } from '@/api'
import { isAppInitialized, initializationComplete } from '@/main'
import { useHomeSearch } from './home/useHomeSearch'
import { useHomeDragDrop } from './home/useHomeDragDrop'
import { useHomeTable } from './home/useHomeTable'
import { createColumns } from './home/columns'

// 上传 API
const uploadApi = {
  type: 'chunked',
  createSession: () => '/api/v1/web/uploads/sessions',
  getSession: (uploadId) => `/api/v1/web/uploads/sessions/${uploadId}`,
  cancel: (uploadId) => `/api/v1/web/uploads/sessions/${uploadId}`,
  uploadChunk: () => '/api/v1/web/uploads/chunks',
  finalize: () => '/api/v1/web/uploads/finalize'
}

const router = useRouter()
const route = useRoute()

// 上传图标
const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])

// 返回/清除搜索图标
const BackIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M11.67 3.87L9.9 2.1 0 12l9.9 9.9 1.77-1.77L3.54 12z' })
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
const notesMaximized = ref(false)
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
    let recordId = row.record_id
    if (!recordId) {
      const fullPath = row.path || ''
      const rootName = row.root_name || ''
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

// 公开文件的自助管理（重命名/移动/删除）。是否展示入口由后端返回的
// can_rename / can_move / can_delete 决定，后端 handler 会再次校验权限。
const renameModalVisible = ref(false)
const renameTarget = ref(null)
const renameNewName = ref('')
const renaming = ref(false)

const openRenameModal = (row) => {
  renameTarget.value = row
  renameNewName.value = row.name || ''
  renameModalVisible.value = true
}

const handleRename = async () => {
  const row = renameTarget.value
  if (!row) return
  const newName = (renameNewName.value || '').trim()
  if (!newName) {
    ElMessage.warning('请输入新文件名')
    return
  }
  if (newName === row.name) {
    renameModalVisible.value = false
    return
  }
  renaming.value = true
  try {
    const res = await FileApi.rename(row.path, newName)
    if (res.success) {
      ElMessage.success('重命名成功')
      renameModalVisible.value = false
      refreshListDebounced()
    } else {
      ElMessage.error(res.message || '重命名失败')
    }
  } catch (e) {
    ElMessage.error(formatErrorMessage(e, '重命名失败'))
  } finally {
    renaming.value = false
  }
}

const moveModalVisible = ref(false)
const moveTarget = ref(null)
const moveTargetRoot = ref('')
const moveTargetSubPath = ref('')
const moving = ref(false)

const openMoveModal = (row) => {
  moveTarget.value = row
  moveTargetRoot.value = row.root_name || ''
  // 预填相对根目录的路径（去掉 rootName 前缀），避免拼接时重复嵌套
  let rel = row.path || ''
  const prefix = '/' + (row.root_name || '')
  if (rel.startsWith(prefix + '/')) {
    rel = rel.slice(prefix.length + 1)
  }
  moveTargetSubPath.value = rel
  moveModalVisible.value = true
}

const handleMove = async () => {
  const row = moveTarget.value
  if (!row) return
  const subPath = (moveTargetSubPath.value || '').trim()
  if (!subPath) {
    ElMessage.warning('请输入目标路径（子目录或新文件名）')
    return
  }
  if (subPath.includes('..')) {
    ElMessage.error('目标路径无效，不能包含 ..')
    return
  }
  moving.value = true
  try {
    const res = await FileApi.move(row.path, moveTargetRoot.value + '/' + subPath)
    if (res.success) {
      ElMessage.success('移动成功')
      moveModalVisible.value = false
      refreshListDebounced()
    } else {
      ElMessage.error(res.message || '移动失败')
    }
  } catch (e) {
    ElMessage.error(formatErrorMessage(e, '移动失败'))
  } finally {
    moving.value = false
  }
}

const handleDeleteRow = async (row) => {
  const isDirectory = !!row.is_dir
  try {
    await ElMessageBox.confirm(
      isDirectory
        ? `确定要删除目录 "${row.name}" 及其所有内容吗？此操作不可恢复。`
        : `确定要删除文件 "${row.name}" 吗？此操作不可恢复。`,
      '确认删除',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const res = await FileApi.delete(row.path)
    if (res.success) {
      ElMessage.success('删除成功')
      refreshListDebounced()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (e) {
    ElMessage.error(formatErrorMessage(e, '删除失败'))
  }
}

// 预览与下载
const previewDialogVisible = ref(false)
const previewFileData = ref({})

const previewFile = (file) => {
  previewFileData.value = file
  previewDialogVisible.value = true
}

const downloadFile = async (file) => {
  if (file?.path) {
    const fullPath = file.path
    const ok = await downloadWithTicket(`/download/${PathUtils.encodeFilePath(fullPath)}`)
    if (ok) refreshListDebounced()
  }
}

// 防抖刷新文件列表：下载后更新下载次数，依赖增删后更新依赖计数（搜索模式重新搜索）
let refreshTimer = null
const refreshListDebounced = () => {
  clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
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
  if (row.record_id) {
    depTreeDialogRef.value?.open(row.record_id)
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
  openRenameModal,
  openMoveModal,
  handleDeleteRow,
  refreshListDebounced
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
  clearTimeout(refreshTimer)
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
  .content-header {
    .breadcrumb-actions .header-actions .search-input {
      transition: width @transition-smooth;

      :deep(.el-input) {
        width: 100%;
      }
    }
  }

  .content-table {
    .table-v2-wrap {
      height: 100%;
    }

    .table-empty {
      padding: 24px 0;
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

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

// 移动弹窗的目标路径输入（根目录名 + 子路径）
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

.help-text {
  font-size: 13px;
  color: @text-color-secondary;
  line-height: 1.8;
  margin-top: 8px;

  p {
    margin: 4px 0;
  }

  code {
    padding: 0 4px;
    background: @bg-color-secondary;
    border-radius: 3px;
  }
}
</style>
<style lang="less">
@import '@/styles/variables.less';
// 依据基名判定"最新版本"的文件使用绿色加粗标识。
// 该样式必须放在非 scoped 区块：el-table-v2 的列渲染通过 h() 动态创建节点，
// 不携带组件的 data-v 属性，scoped 选择器无法命中。
.file-link.latest-version {
  color: @success-color;
  font-weight: 700;
  // 叠加下划线，避免仅凭绿色加粗传达"最新版本"，方便色弱用户识别
  text-decoration-line: underline;
  text-decoration-color: @success-color;
  text-underline-offset: 2px;
}

// 操作列按钮容器：same 原因（h() 动态节点），放在非 scoped 区块
.row-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: nowrap;
}

// 无 href 的自定义目录链接/路径段：鼠标手型，键盘焦点环交给全局 :focus-visible
.file-link:not([href]),
.path-segment {
  cursor: pointer;
}

.notes-cell {
  cursor: pointer;
  color: @text-color-secondary;
  font-size: 12px;
}

.n-data-table-tbody {
  .n-data-table-tr {
    &:hover {
      .notes-cell {
        &:empty:after {
          content: '点击填写备注';
          color: @text-color-placeholder;
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

  .suggest-text {
    display: flex;
    align-items: baseline;
    min-width: 0;
  }

  .suggest-prefix {
    flex-shrink: 0;
    margin-right: 2px;
    color: @text-color-secondary;
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
    background: @primary-color;
    border-radius: 3px;
    white-space: nowrap;
  }

  /* 相关词候选：弱化处理，与"纠错"徽标区分 */
  .suggest-tag--related {
    color: @text-color-secondary;
    background: transparent;
    border: 1px solid @text-color-placeholder;
    line-height: 16px;
  }
}
</style>
