<template>
  <div class="temp-files" @dragover.prevent @drop.prevent="handleGlobalDrop">
    <!-- Breadcrumb -->
    <div class="breadcrumb-row">
      <div class="breadcrumb-left">
        <n-breadcrumb v-if="breadcrumb.length > 1">
          <n-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <a href="#" class="breadcrumb-link" @click.prevent="navigateToDir(item.path)">{{ item.name }}</a>
          </n-breadcrumb-item>
        </n-breadcrumb>
        <span v-else class="breadcrumb-root">临时文件</span>
        <span class="temp-description">无需登录即可上传分享，文件到期自动删除</span>
      </div>
      <div class="breadcrumb-right">
        <span class="ip-badge">{{ clientIP || '加载中...' }}</span>
        <span class="quota-badge">用量: {{ formatSize(used) }} / {{ quota > 0 ? formatSize(quota) : '无限制' }}</span>
      </div>
    </div>

    <div class="toolbar-row">
        <!-- 左侧：访问码入口 -->
        <div class="access-code-section">
          <n-input-group>
            <n-input
              v-model:value="accessCodeInput"
              placeholder="输入访问码快速访问文件"
              size="small"
              style="width: 200px"
              @keydown.enter="handleAccessCode"
            />
            <n-button type="primary" size="small" @click="handleAccessCode" :loading="accessingCode">
              访问
            </n-button>
          </n-input-group>
        </div>
        <!-- 右侧：搜索和上传 -->
        <div class="toolbar-right">
          <n-input
            ref="searchInputRef"
            v-model:value="searchQuery"
            placeholder="搜索文件..."
            size="small"
            class="search-input"
            clearable
          />
          <n-button @click="loadFiles(currentDir)" :loading="loading">
            <template #icon><n-icon><RefreshIcon /></n-icon></template>
            刷新
          </n-button>
          <n-button type="primary" @click="showUploadDialog = true">
            <template #icon><n-icon><UploadIcon /></n-icon></template>
            上传文件
          </n-button>
        </div>
    </div>

    <!-- 文件列表 -->
    <div class="content-table">
      <n-data-table
        v-if="tableData.length > 0 || loading"
        :columns="columns"
        :data="tableData"
        :loading="loading"
        :pagination="pagination"
        :row-key="row => row._key"
        :bordered="false"
        size="small"
      />
      <div v-else class="empty-state">
        <n-empty :description="searchQuery ? '未找到匹配的文件' : '暂无临时文件'" />
      </div>
    </div>

    <!-- 上传弹窗 -->
    <n-modal :show="showUploadDialog" preset="card" title="上传临时文件" class="upload-dialog"
      @update:show="onUploadDialogShowChange" :mask-closable="false" :closeable="false">
      <upload-manager :upload-api="tempUploadApi" :default-dir="currentDir" @upload-success="onUploadSuccess" ref="uploadManagerRef" @close="handleUploadDialogClose" />
    </n-modal>

    <!-- 预览弹窗 -->
    <n-modal v-model:show="previewDialogVisible" preset="card" title="文件预览" :class="previewMaximized ? 'preview-maximized' : ''"
      :style="previewMaximized ? { width: '100vw', height: '100vh', maxWidth: '100vw', maxHeight: '100vh', top: 0, left: 0, transform: 'none', borderRadius: 0 } : { width: '900px', maxHeight: '80vh' }">
      <file-preview :file="previewFileData" :maximized="previewMaximized" :download-api="() => TempApi.download(previewFileData.code)"
        @close="previewDialogVisible = false" />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <n-button @click="previewMaximized = !previewMaximized">{{ previewMaximized ? '还原' : '最大化' }}</n-button>
          <n-button type="primary" @click="downloadFile(previewFileData.code)">下载</n-button>
          <n-button @click="previewDialogVisible = false">关闭</n-button>
        </div>
      </template>
    </n-modal>

    <!-- 访问码查询结果弹窗 -->
    <n-modal v-model:show="showAccessCodeDialog" preset="card" title="文件信息" style="width: 400px">
      <n-descriptions :column="1" v-if="accessedFile">
        <n-descriptions-item label="文件名">
          <span :class="getFileIconClass(accessedFile)"></span>
          {{ accessedFile.filename }}
        </n-descriptions-item>
        <n-descriptions-item label="文件大小">
          {{ formatSize(accessedFile.fileSize) }}
        </n-descriptions-item>
        <n-descriptions-item label="访问码">
          <n-tag type="info">{{ accessedFile.code }}</n-tag>
        </n-descriptions-item>
        <n-descriptions-item label="过期时间">
          {{ new Date(accessedFile.expiredAt).toLocaleString() }}
        </n-descriptions-item>
      </n-descriptions>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAccessCodeDialog = false; accessedFile = null; accessCodeInput = ''">关闭</n-button>
          <n-button type="primary" @click="downloadFile(accessedFile.code)">下载</n-button>
        </n-space>
      </template>
    </n-modal>

	    <!-- 删除确认弹窗 -->
	    <n-modal v-model:show="showDeleteConfirm" preset="card" title="确认删除" style="width: 400px">
	      <p>确定要删除文件「{{ deleteTarget?.filename }}」吗？此操作不可恢复。</p>
	      <template #footer>
	        <n-space justify="end">
	          <n-button @click="showDeleteConfirm = false; deleteTarget = null">取消</n-button>
	          <n-button type="error" :loading="deleting" @click="confirmDelete">删除</n-button>
	        </n-space>
	      </template>
	    </n-modal>
	  </div>
	</template>
	
	<script setup>
	import { ref, computed, onMounted, onUnmounted, nextTick, h } from 'vue'
	import { NButton, NIcon, NDataTable, NTag, NEmpty, NProgress, NModal, NSpace, NInput, NInputGroup, NDescriptions, NDescriptionsItem, NBreadcrumb, NBreadcrumbItem, useMessage, useDialog } from 'naive-ui'
import { TempApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'

const message = useMessage()
const dialog = useDialog()

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

const pagination = { pageSize: 10 }

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

// 上传弹框关闭保护
const handleUploadDialogClose = () => {
  const mgr = uploadManagerRef.value
  if (mgr?.hasActiveUploads) {
    dialog.warning({
      title: '上传进行中',
      content: '有文件正在上传，关闭弹框将中断所有上传。是否确认关闭？',
      positiveText: '确认关闭',
      negativeText: '继续上传',
      onPositiveClick: () => {
        showUploadDialog.value = false
      }
    })
  } else if (mgr?.hasUrlUploading) {
    message.info('URL 下载在后台继续执行，您可以在通知中查看进度', { duration: 4000 })
    showUploadDialog.value = false
  } else {
    showUploadDialog.value = false
  }
}

const onUploadDialogShowChange = (show) => {
  if (!show) {
    handleUploadDialogClose()
  }
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
    ellipsis: { tooltip: true },
    render(row) {
      if (row._type === 'dir') {
        return h('div', { class: 'file-name-cell', style: 'cursor: pointer;' }, [
          h(NIcon, { size: 18, style: 'color: #f0a020; margin-right: 8px;' }, () => h(FolderIcon)),
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
    render: (row) => row._type === 'dir' ? h('span', { style: 'color: #999;' }, '-') : formatSize(row.fileSize)
  },
  {
    title: '访问码',
    key: 'code',
    width: 100,
    render: (row) => row._type === 'dir' ? null : h(NTag, { size: 'small', type: 'info', style: 'cursor: pointer', onClick: () => copyCode(row.code) }, () => row.code)
  },
  {
    title: '上传时间',
    key: 'createdAt',
    width: 180,
    render: (row) => {
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
    render: (row) => {
      if (row._type === 'dir') return null
      const expire = new Date(row.expiredAt)
      const now = new Date()
      if (expire < now) return h(NTag, { type: 'error', size: 'small' }, () => '已过期')
      return TimeUtils.formatDateTime(row.expiredAt)
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 160,
    render(row) {
      if (row._type === 'dir') return null
      const expire = new Date(row.expiredAt)
      const now = new Date()
      const isExpired = expire < now

      return h('div', { class: 'action-buttons' }, [
        !isExpired && h(NButton, {
          size: 'tiny',
          type: 'primary',
          onClick: () => downloadFile(row.code)
        }, () => '下载'),
        h(NButton, {
          size: 'tiny',
          type: 'error',
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
    message.error('加载文件列表失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

const copyCode = (code) => {
  navigator.clipboard.writeText(code).then(() => {
    message.success('访问码已复制')
  }).catch(() => {
    message.error('复制失败')
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
      message.success('删除成功')
      loadFiles(currentDir.value)
    } else {
      message.error(data.message || '删除失败')
    }
  } catch (error) {
    message.error('删除失败')
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
    message.warning('请输入访问码')
    return
  }
  accessingCode.value = true
  try {
    const data = await TempApi.getInfo(code)
    if (data.success) {
      accessedFile.value = data.data
      showAccessCodeDialog.value = true
    } else {
      message.error(data.message || '访问码无效或文件已过期')
    }
  } catch (error) {
    message.error('获取文件信息失败')
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
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
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
    color: #333;
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

      .n-input-group {
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
    background: #fff;
    border-radius: @content-radius;
    overflow: hidden;
    box-shadow: @shadow-sm;
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }

    :deep(.n-data-table) {
      @media @mobile {
        overflow-x: auto;
        .n-data-table-th,
        .n-data-table-td {
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

.preview-maximized :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
