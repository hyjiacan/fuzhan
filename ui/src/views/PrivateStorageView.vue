<template>
  <div class="private-storage">
    <div class="header-section">
      <!-- Breadcrumb -->
      <div class="breadcrumb-row">
        <n-breadcrumb v-if="breadcrumb.length > 1">
          <n-breadcrumb-item v-for="(item, index) in breadcrumb" :key="index">
            <a href="#" class="breadcrumb-link" @click.prevent="navigateToDir(item.path)">{{ item.name }}</a>
          </n-breadcrumb-item>
        </n-breadcrumb>
        <span v-else class="breadcrumb-root">私有存储</span>
      </div>

      <div class="header-row">
        <div class="header-left">
          <p class="description">上传个人文件，安全存储在你的账号下。</p>
        </div>
        <div class="header-right" v-if="quota > 0">
          <span class="quota-info-text">配额: {{ formatSize(used) }} / {{ formatSize(quota) }}</span>
        </div>
      </div>
      <div class="toolbar-row">
        <div class="toolbar-left">
          <n-input
            ref="searchInputRef"
            v-model:value="searchQuery"
            :maxlength="200"
            placeholder="搜索文件..."
            size="small"
            class="search-input"
            clearable
          />
        </div>
        <div class="toolbar-right">
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
        <n-empty :description="searchQuery ? '未找到匹配的文件' : '暂无私有文件'">
          <template #extra>
            <n-button size="small" type="primary" @click="showUploadDialog = true">上传第一个文件</n-button>
          </template>
        </n-empty>
      </div>
    </div>

    <!-- 上传弹窗 -->
    <n-modal :show="showUploadDialog" preset="card" title="上传文件" class="upload-dialog"
      @update:show="onUploadDialogShowChange" :mask-closable="false" :closeable="false">
      <upload-manager :upload-api="privateUploadApi" :default-dir="currentDir" @upload-success="onUploadSuccess" @upload-error="onUploadError" ref="uploadManagerRef" @close="handleUploadDialogClose" />
    </n-modal>

    <!-- 预览弹窗 -->
    <n-modal v-model:show="previewDialogVisible" preset="card" title="文件预览" :class="previewMaximized ? 'preview-maximized' : ''"
      :style="previewMaximized ? { width: '100vw', height: '100vh', maxWidth: '100vw', maxHeight: '100vh', top: 0, left: 0, transform: 'none', borderRadius: 0 } : { width: '900px', maxHeight: '80vh' }">
      <file-preview :file="previewFileData" :maximized="previewMaximized" :download-api="() => PrivateApi.download(previewFileData.code)"
        @close="previewDialogVisible = false" />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <n-button @click="previewMaximized = !previewMaximized">{{ previewMaximized ? '还原' : '最大化' }}</n-button>
          <n-button type="primary" @click="downloadFile(previewFileData.code)">下载</n-button>
          <n-button @click="previewDialogVisible = false">关闭</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, h } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NIcon, NDataTable, NTag, NEmpty, NProgress, NModal, NSpace, NInput, NBreadcrumb, NBreadcrumbItem, useMessage, useDialog } from 'naive-ui'
import { PrivateApi, AuthApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import UploadManager from '@/components/upload/UploadManager.vue'
import FilePreview from '@/components/file/FilePreview.vue'

const router = useRouter()
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

// State
const loading = ref(false)
const files = ref([])
const subdirs = ref([])
const currentDir = ref('')
const quota = ref(0)
const used = ref(0)
const showUploadDialog = ref(false)
const previewDialogVisible = ref(false)
const previewMaximized = ref(false)
const previewFileData = ref({})
const uploadManagerRef = ref(null)
const uploadQueueCount = ref(0)
const searchQuery = ref('')
const searchInputRef = ref(null)

// 面包屑
const breadcrumb = computed(() => {
  const crumbs = [{ name: '私有存储', path: '' }]
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
    uploadTime: '',
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

// 私有存储上传 API 配置
const privateUploadApi = {
  type: 'chunked',
  createSession: () => '/api/v1/private/uploads/session',
  getSession: (uploadId) => `/api/v1/private/uploads/session/${uploadId}`,
  cancel: (uploadId) => `/api/v1/private/uploads/session/${uploadId}`,
  uploadChunk: () => '/api/v1/private/uploads/chunk',
  finalize: () => '/api/v1/private/uploads/finalize'
}

const pagination = { pageSize: 10 }

// 格式化
const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

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

// 返回上级目录
const goUp = () => {
  if (!currentDir.value) return
  const segments = currentDir.value.split('/').filter(Boolean)
  segments.pop()
  const parent = segments.join('/')
  navigateToDir(parent)
}

// Methods
const onUploadSuccess = () => {
  message.success('上传成功')
  showUploadDialog.value = false
  loadFiles(currentDir.value)
}

const onUploadError = (error) => {
  message.error(formatErrorMessage(error, '上传失败'))
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
      const fileHref = PrivateApi.download(row.code)
      const isPreview = canPreview(row)

      return h('div', { class: 'file-name-cell' }, [
        h('div', { class: 'file-icon-wrapper' }, [
          h('span', { class: iconClass }),
          isPreview ? h('span', { class: 'preview-icon' }) : null
        ]),
        isPreview
          ? h('span', {
              class: 'file-link',
              style: 'cursor: pointer; color: #2080f0;',
              onClick: () => previewFile(row)
            }, row.filename)
          : h('a', {
              href: fileHref,
              class: 'file-link',
              target: '_blank'
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
    key: 'uploadTime',
    width: 180,
    render: (row) => {
      if (row._type === 'dir') return null
      const text = TimeUtils.formatDateTime(row.uploadTime)
      if (TimeUtils.isRecent24h(row.uploadTime)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 120,
    render(row) {
      if (row._type === 'dir') return null
      return h('div', { class: 'action-buttons' }, [
        h(NButton, {
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

const loadFiles = async (dir) => {
  loading.value = true
  try {
    const data = await PrivateApi.list(dir || '')
    if (data.success) {
      files.value = data.data?.files || []
      subdirs.value = data.data?.subdirs || []
      quota.value = data.data?.quota?.user || 0
      used.value = data.data?.used || 0
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
  window.open(PrivateApi.download(code), '_blank')
}

const handleDelete = (file) => {
  dialog.warning({
    title: '确认删除',
    content: `确定要删除文件「${file.filename}」吗？此操作不可恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const data = await PrivateApi.delete(file.code)
        if (data.success) {
          message.success('删除成功')
          loadFiles(currentDir.value)
        } else {
          message.error(data.message || '删除失败')
        }
      } catch (error) {
        message.error('删除失败')
        console.error(error)
      }
    }
  })
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
  loadFiles('')
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.private-storage {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    .breadcrumb-row {
      margin-bottom: 8px;
      font-size: 14px;

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
      font-size: 14px;
      font-weight: 500;
      color: #333;
      margin-bottom: 8px;
      display: block;
    }

    .header-row {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
    }

    .header-left {
      h2 {
        margin: 0 0 8px 0;
        font-size: @font-size-xxl;
        font-weight: 600;
      }

      .description {
        margin: 0;
        color: @text-color-secondary;
        font-size: @font-size-base;
      }
    }

    .header-right {
      display: flex;
      align-items: center;
      gap: 16px;
      padding: 8px 16px;
      background: @bg-color-secondary;
      border-radius: @border-radius-lg;
      font-size: @font-size-sm;
      color: @text-color-secondary;
      flex-shrink: 0;
    }

    .toolbar-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 16px;
      gap: 16px;

      .toolbar-left {
        flex: 1;
      }

      .toolbar-right {
        display: flex;
        gap: 12px;
        align-items: center;
      }

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
  .private-storage {
    padding: 12px;
  }

  .header-row {
    flex-direction: column;
    gap: 8px;

    .header-right {
      align-self: flex-start;
    }
  }

  .toolbar-row {
    .search-input {
      width: 150px;
    }
  }
}

@media @mobile {
  .private-storage {
    padding: 8px;
  }

  .toolbar-row {
    flex-direction: column;
    align-items: stretch;

    .toolbar-left {
      .search-input {
        width: 100%;
      }
    }

    .toolbar-right {
      justify-content: flex-end;
    }
  }
}

.preview-maximized :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
