<template>
  <div class="temp-files" @dragover.prevent @drop.prevent="handleGlobalDrop">
    <!-- Breadcrumb -->
    <div class="breadcrumb-row">
      <div class="breadcrumb-left">
        <el-breadcrumb v-if="breadcrumb.length > 1" separator="/">
          <el-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <a href="#" class="breadcrumb-link" @click.prevent="navigateToDir(item.path)">{{ item.name }}</a>
          </el-breadcrumb-item>
        </el-breadcrumb>
        <span v-else class="breadcrumb-root">临时文件</span>
        <span class="temp-description">无需登录即可上传分享，文件到期自动删除</span>
      </div>
      <div class="breadcrumb-right">
        <span>临时文件根据你的IP</span>
        <span class="ip-badge">{{ clientIP || '加载中...' }}</span>
        <span>执行数据隔离</span>
        <span class="quota-badge">用量: {{ formatSize(used) }} / {{ quota > 0 ? formatSize(quota) : '无限制' }}</span>
      </div>
    </div>

    <div class="toolbar-row">
        <!-- 左侧：访问码入口 -->
        <div class="access-code-section">
          <el-input
            v-model="accessCodeInput"
            placeholder="输入访问码快速访问文件"
            size="small"
            style="width: 200px"
            @keydown.enter="handleAccessCode"
          >
            <template #append>
              <el-button type="primary" size="small" @click="handleAccessCode" :loading="accessingCode">
                访问
              </el-button>
            </template>
          </el-input>
        </div>
        <!-- 右侧：搜索和上传 -->
        <div class="toolbar-right">
          <el-input
            ref="searchInputRef"
            v-model="searchQuery"
            placeholder="搜索文件..."
            size="small"
            class="search-input"
            clearable
          />
          <el-button @click="loadFiles(currentDir)" :loading="loading">
            <el-icon><RefreshIcon /></el-icon>
            刷新
          </el-button>
          <el-button type="primary" @click="showUploadDialog = true">
            <el-icon><UploadIcon /></el-icon>
            上传文件
          </el-button>
        </div>
    </div>

    <!-- 文件列表 -->
    <div class="content-table">
      <div ref="tableWrapRef" class="table-v2-wrap" v-loading="loading">
        <el-table-v2
          :columns="columns"
          :data="pagedData"
          :width="tableWidth"
          :height="tableHeight"
          row-key="_key"
        :row-height="32" />
      </div>
      <div v-if="tableData.length > pageSize" class="table-pagination">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :total="tableData.length"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          @size-change="currentPage = 1"
        />
      </div>
    </div>

    <!-- 上传弹窗（dialog 集成在 UploadManager 组件内） -->
    <upload-manager v-model="showUploadDialog" title="上传临时文件" :upload-api="tempUploadApi" :default-dir="currentDir"
      @upload-success="onUploadSuccess" ref="uploadManagerRef"
      :delete-on-download="deleteOnDownload" @update:delete-on-download="deleteOnDownload = $event" />

    <!-- 预览弹窗 -->
    <el-dialog v-model="previewDialogVisible" title="文件预览" :class="previewMaximized ? 'preview-maximized' : ''"
      :style="previewMaximized ? { width: '100vw', maxWidth: '100vw' } : { width: '900px', maxHeight: '80vh' }">
      <file-preview :file="previewFileData" :maximized="previewMaximized" :download-api="() => TempApi.download(previewFileData.code)"
        @close="previewDialogVisible = false" />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="previewMaximized = !previewMaximized">{{ previewMaximized ? '还原' : '最大化' }}</el-button>
          <el-button type="primary" @click="downloadFile(previewFileData.code)">下载</el-button>
          <el-button @click="previewDialogVisible = false">关闭</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 访问码查询结果弹窗 -->
    <el-dialog v-model="showAccessCodeDialog" title="文件信息" width="400px">
      <el-descriptions :column="1" v-if="accessedFile">
        <el-descriptions-item label="文件名">
          <span :class="getFileIconClass(accessedFile)"></span>
          {{ accessedFile.filename }}
        </el-descriptions-item>
        <el-descriptions-item label="文件大小">
          {{ formatSize(accessedFile.fileSize) }}
        </el-descriptions-item>
        <el-descriptions-item label="访问码">
          <el-tag type="info">{{ accessedFile.code }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="过期时间">
          {{ TimeUtils.formatDateTime(accessedFile.expiredAt) }}
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="showAccessCodeDialog = false; accessedFile = null; accessCodeInput = ''">关闭</el-button>
          <el-button type="primary" @click="downloadFile(accessedFile.code)">下载</el-button>
        </div>
      </template>
    </el-dialog>

      <!-- 删除确认弹窗 -->
      <el-dialog v-model="showDeleteConfirm" title="确认删除" width="400px">
        <p>确定要删除文件「{{ deleteTarget?.filename }}」吗？此操作不可恢复。</p>
        <template #footer>
          <div style="display: flex; justify-content: flex-end; gap: 8px;">
            <el-button @click="showDeleteConfirm = false; deleteTarget = null">取消</el-button>
            <el-button type="danger" :loading="deleting" @click="confirmDelete">删除</el-button>
          </div>
        </template>
      </el-dialog>
    </div>
  </template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick, h } from 'vue'
import { ElMessage, ElButton, ElTag, ElIcon } from 'element-plus'
import { TempApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'

// Icons
const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])
const RefreshIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M17.65 6.35C16.2 4.9 14.21 4 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08c-.82 2.33-3.04 4-5.65 4-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z' })
])
const FolderIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z' })
])

// 上传相关
const showUploadDialog = ref(false)
const deleteOnDownload = ref(false)
const showAccessCodeDialog = ref(false)
const previewDialogVisible = ref(false)
const previewMaximized = ref(false)
const previewFileData = ref({})
const uploadManagerRef = ref(null)
const searchQuery = ref('')
const searchInputRef = ref(null)

// State
const loading = ref(false)
const files = ref([])
const subdirs = ref([])
const currentDir = ref('')
const quota = ref(0)
const used = ref(0)
const clientIP = ref('')
const accessCodeInput = ref('')
const accessingCode = ref(false)
const accessedFile = ref(null)

// 客户端分页（原 naive 表格内置分页 pageSize=10）
const pageSize = ref(10)
const currentPage = ref(1)

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

// 面包屑
const breadcrumb = computed(() => {
  const crumbs = [{ name: '临时文件', path: '' }]
  if (currentDir.value) {
    const segments = currentDir.value.split('/').filter(Boolean)
    let accumulated = ''
    segments.forEach(seg => {
      accumulated = accumulated ? accumulated + '/' + seg : seg
      crumbs.push({ name: seg, path: accumulated })
    })
  }
  return crumbs
})

// 合并目录和文件为表格数据
const tableData = computed(() => {
  let dirRows = subdirs.value.map(name => ({
    _key: 'dir:' + name,
    _type: 'dir',
    name: name,
    filename: name,
    fileSize: 0,
    code: '',
    createdAt: '',
    expiredAt: '',
    dirName: name
  }))
  let fileRows = files.value.map(f => ({
    ...f,
    _key: 'file:' + f.code,
    _type: 'file'
  }))
  // 搜索过滤（目录和文件均按名称匹配）
  if (searchQuery.value.trim()) {
    const keywords = searchQuery.value.trim().toLowerCase().split(/\s+/)
    dirRows = dirRows.filter(row => {
      const name = row.filename?.toLowerCase() || ''
      return keywords.every(kw => name.includes(kw))
    })
    fileRows = fileRows.filter(row => {
      const name = row.filename?.toLowerCase() || ''
      const code = row.code?.toLowerCase() || ''
      return keywords.every(kw => name.includes(kw) || code.includes(kw))
    })
  }
  return [...dirRows, ...fileRows]
})

// 当前页数据
const pagedData = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return tableData.value.slice(start, start + pageSize.value)
})

// 格式化
const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

// 计算配额颜色
const quotaColor = computed(() => {
  const percent = (used.value / quota.value) * 100
  if (percent >= 90) return '#d03050'
  if (percent >= 70) return '#f0a020'
  return '#18a058'
})

// 获取文件图标类名
const getFileIconClass = (row) => {
  const ext = row.filename?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype icon-filetype-${ext}`
}

// 是否可预览
const canPreview = (row) => {
  const ext = row.filename?.split('.').pop()?.toLowerCase() || ''
  const previewableExts = ['txt', 'md', 'json', 'js', 'ts', 'css', 'html', 'xml', 'yaml', 'yml', 'log']
  return previewableExts.includes(ext)
}

// 导航到子目录
const navigateToDir = (dir) => {
  currentDir.value = dir
  searchQuery.value = ''
  currentPage.value = 1
  loadFiles(dir)
}

// 上传 API
const tempUploadApi = {
  type: 'chunked',
  createSession: () => '/api/v1/temp/upload/session',
  getSession: (uploadId) => `/api/v1/temp/upload/session/${uploadId}`,
  cancel: (uploadId) => `/api/v1/temp/upload/session/${uploadId}`,
  uploadChunk: () => '/api/v1/temp/upload/chunk',
  finalize: () => '/api/v1/temp/upload/finalize'
}

const onUploadSuccess = () => {
  // 上传成功后不关闭弹框，保持打开以便继续上传
  loadFiles(currentDir.value)
}

const handleGlobalDrop = async (e) => {
  const dropFiles = e.dataTransfer?.files
  if (dropFiles && dropFiles.length > 0) {
    showUploadDialog.value = true
    await nextTick()
    if (uploadManagerRef.value) {
      Array.from(dropFiles).forEach(file => {
        uploadManagerRef.value.addFile(file)
      })
    }
  }
}

// 预览文件
const previewFile = (file) => {
  previewFileData.value = file
  previewDialogVisible.value = true
}

// 表格列配置
const columns = computed(() => [
  {
    title: '文件名',
    key: 'filename',
    minWidth: 220,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => {
      if (row._type === 'dir') {
        return h('div', { class: 'file-name-cell', style: 'cursor: pointer;' }, [
          h(ElIcon, { size: 18, style: 'color: #f0a020; margin-right: 8px;' }, () => h(FolderIcon)),
          h('span', {
            class: 'file-link',
            style: 'color: #2080f0;',
            onClick: () => navigateToDir(currentDir.value ? currentDir.value + '/' + row.dirName : row.dirName)
          }, row.dirName)
        ])
      }
      const iconClass = `icon-filetype ${getFileIconClass(row)}`
      const fileHref = TempApi.download(row.code)
      const isPreview = canPreview(row)
      const expire = new Date(row.expiredAt)
      const now = new Date()
      const isExpired = expire < now

      return h('div', { class: 'file-name-cell' }, [
        h('div', { class: 'file-icon-wrapper' }, [
          h('span', { class: iconClass }),
          isPreview ? h('span', { class: 'preview-icon' }) : null
        ]),
        isExpired
          ? h('span', { class: 'file-link expired' }, row.filename)
          : h('a', {
              href: fileHref,
              class: 'file-link',
              target: '_blank',
              onClick: (e) => {
                if (isPreview) {
                  e.preventDefault()
                  previewFile(row)
                }
              }
            }, row.filename)
      ])
    }
  },
  {
    title: '大小',
    key: 'fileSize',
    width: 100,
    cellRenderer: ({ rowData: row }) => row._type === 'dir' ? h('span', { style: 'color: var(--el-text-color-secondary);' }, '-') : formatSize(row.fileSize)
  },
  {
    title: '访问码',
    key: 'code',
    width: 100,
    cellRenderer: ({ rowData: row }) => row._type === 'dir' ? null : h(ElTag, { size: 'small', type: 'info', style: 'cursor: pointer', onClick: () => copyCode(row.code) }, () => row.code)
  },
  {
    title: '上传时间',
    key: 'createdAt',
    width: 180,
    cellRenderer: ({ rowData: row }) => {
      if (row._type === 'dir') return null
      const text = TimeUtils.formatDateTime(row.createdAt)
      if (TimeUtils.isRecent24h(row.createdAt)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  },
  {
    title: '过期时间',
    key: 'expiredAt',
    width: 180,
    cellRenderer: ({ rowData: row }) => {
      if (row._type === 'dir') return null
      const expire = new Date(row.expiredAt)
      const now = new Date()
      if (expire < now) return h(ElTag, { type: 'danger', size: 'small' }, () => '已过期')
      return TimeUtils.formatDateTime(row.expiredAt)
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 160,
    cellRenderer: ({ rowData: row }) => {
      if (row._type === 'dir') return null
      const expire = new Date(row.expiredAt)
      const now = new Date()
      const isExpired = expire < now

      return h('div', { class: 'action-buttons' }, [
        !isExpired && h(ElButton, {
          size: 'small',
          type: 'primary',
          onClick: () => downloadFile(row.code)
        }, () => '下载'),
        h(ElButton, {
          size: 'small',
          type: 'danger',
          onClick: () => handleDelete(row)
        }, () => '删除')
      ])
    }
  }
])

// Methods
const loadClientIP = async () => {
  try {
    const data = await TempApi.getClientIP()
    if (data.success) {
      clientIP.value = data.data?.ip || ''
    }
  } catch (e) {
    console.error('获取客户端IP失败:', e)
  }
}

const loadFiles = async (dir) => {
  loading.value = true
  try {
    const data = await TempApi.list(dir || '')
    if (data.success) {
      files.value = data.data?.files || []
      subdirs.value = data.data?.subdirs || []
      quota.value = data.data?.quota?.limit || 0
      used.value = data.data?.quota?.used || 0
    }
  } catch (error) {
    ElMessage.error('加载文件列表失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

const copyCode = (code) => {
  navigator.clipboard.writeText(code).then(() => {
    ElMessage.success('访问码已复制')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

const downloadFile = (code) => {
  window.open(TempApi.download(code), '_blank')
}

// 删除确认弹窗
const showDeleteConfirm = ref(false)
const deleteTarget = ref(null)
const deleting = ref(false)

const handleDelete = (file) => {
  deleteTarget.value = file
  showDeleteConfirm.value = true
}

const confirmDelete = async () => {
  const file = deleteTarget.value
  if (!file) return
  deleting.value = true
  try {
    const data = await TempApi.delete(file.code)
    if (data.success) {
      ElMessage.success('删除成功')
      loadFiles(currentDir.value)
    } else {
      ElMessage.error(data.message || '删除失败')
    }
  } catch (error) {
    ElMessage.error('删除失败')
    console.error(error)
  } finally {
    deleting.value = false
    showDeleteConfirm.value = false
    deleteTarget.value = null
  }
}

// 访问码查询
const handleAccessCode = async () => {
  const code = accessCodeInput.value.trim().toUpperCase()
  if (!code) {
    ElMessage.warning('请输入访问码')
    return
  }
  accessingCode.value = true
  try {
    const data = await TempApi.getInfo(code)
    if (data.success) {
      accessedFile.value = data.data
      showAccessCodeDialog.value = true
    } else {
      ElMessage.error(data.message || '访问码无效或文件已过期')
    }
  } catch (error) {
    ElMessage.error('获取文件信息失败')
    console.error(error)
  } finally {
    accessingCode.value = false
  }
}

// 键盘快捷键
const handleKeydown = (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
    e.preventDefault()
    searchInputRef.value?.focus()
  }
}

// Lifecycle
onMounted(() => {
  loadClientIP()
  loadFiles('')
  window.addEventListener('keydown', handleKeydown)
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (tableWrapRef.value) {
    tableResizeObs.observe(tableWrapRef.value)
  }
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  tableResizeObs?.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.temp-files {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .breadcrumb-row {
    font-size: 14px;
    display: flex;
    justify-content: space-between;
    align-items: center;

    .breadcrumb-link {
      text-decoration: none;
      color: inherit;
      transition: color @transition-fast;

      &:hover {
        color: @primary-color;
      }
    }
  }

  .breadcrumb-left {
    display: flex;
    align-items: center;
    gap: 12px;

    .temp-description {
      font-size: @font-size-sm;
      color: @text-color-secondary;
      white-space: nowrap;
    }
  }

  .breadcrumb-root {
    font-size: 18px;
    font-weight: 600;
    color: @text-color;
  }

  .breadcrumb-right {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: @font-size-sm;
    color: @text-color-secondary;

    .ip-badge,
    .quota-badge {
      display: inline-flex;
      align-items: center;
      padding: 0 8px;
      height: 22px;
      background: @bg-color-secondary;
      border: 1px solid @border-color-light;
      border-radius: @border-radius-sm;
      font-size: @font-size-xs;
      color: @text-color-secondary;
    }

    .ip-badge {
      font-family: monospace;
    }
  }

  .toolbar-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;

    .access-code-section {
      flex: 1;

      .el-input-group {
        max-width: 360px;
      }
    }

    .toolbar-right {
      display: flex;
      gap: 12px;
      align-items: center;

      .search-input {
        width: 200px;
      }
    }
  }

  .content-table {
    background: @bg-color;
    border-radius: @content-radius;
    overflow: hidden;
    box-shadow: @shadow-sm;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }

    .table-v2-wrap {
      height: 400px;
    }

    .table-pagination {
      display: flex;
      justify-content: flex-end;
      padding: 8px 16px;
      border-top: 1px solid @border-color-light;
    }

    :deep(.el-table-v2) {
      @media @mobile {
        overflow-x: auto;
        .el-table-v2__row-cell {
          white-space: nowrap;
        }
      }
    }
  }

  .empty-state {
    display: flex;
    justify-content: center;
    align-items: center;
    height: 200px;
  }
}

@media @tablet {
  .temp-files {
    padding: 12px;
  }

  .toolbar-row {
    flex-direction: column;
    align-items: stretch;

    .toolbar-right {
      flex-wrap: wrap;
      gap: 8px;

      .search-input {
        flex: 1;
        min-width: 120px;
      }

      .quota-info {
        width: 100%;
        justify-content: flex-end;
      }
    }
  }
}

@media @mobile {
  .temp-files {
    padding: 8px;
  }

  .toolbar-row {
    .toolbar-right {
      .quota-info-text {
        display: none;
      }
    }
  }
}
</style>
