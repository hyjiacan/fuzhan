<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    :class="['upload-dialog', maximized ? 'upload-dialog--maximized' : '']"
    width="800px"
    top="10vh"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    :show-close="false"
    :before-close="requestClose"
    append-to-body
  >
    <template #header="{ titleId, titleClass }">
      <div class="upload-dialog-header">
        <span :id="titleId" :class="titleClass" class="upload-dialog-title">{{ title }}</span>
        <el-button link circle :title="maximized ? '还原' : '放大'" class="upload-maximize-btn" @click="toggleMaximize">
          <el-icon :size="14"><component :is="maximized ? RestoreIcon : MaximizeIcon" /></el-icon>
        </el-button>
      </div>
    </template>

    <div class="upload-manager">
      <el-form :model="form" label-width="120px">
      <!-- 共享目录和上传目录合并为一行 -->
      <el-form-item label="上传目录:" v-if="needsRootSelection">
        <div style="display: flex; gap: 8px; width: 100%;">
          <el-select
            v-model="form.rootName"
            placeholder="选择共享目录"
            :style="{ width: '180px' }"
          >
            <el-option v-for="opt in rootDirOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
          <el-input
            v-model="form.uploadDir"
            :maxlength="1024"
            placeholder="输入相对目录，如: test"
            :style="{ flex: 1 }"
          />
        </div>
        <div style="font-size: 12px; color: #999;">
          可选，留空则上传到根目录
        </div>
      </el-form-item>

      <!-- 上传方式：左侧 tabs 切换 -->
      <el-tabs v-model="form.uploadMethod" tab-position="left" class="upload-method-tabs">
        <el-tab-pane name="local" label="上传本地文件">
          <el-form-item label-width="0">
            <!-- Drop zone with button -->
            <div
              ref="dropZoneRef"
              :class="['drop-zone', { 'drop-zone-active': isDragging }]"
              @dragenter.prevent="handleDragEnter"
              @dragover.prevent="handleDragOver"
              @dragleave.prevent="handleDragLeave"
              @drop.prevent="handleDrop"
              @click="triggerFileInput"
            >
              <input
                ref="fileInputRef"
                type="file"
                multiple
                :max="100"
                style="display: none;"
                @change="handleFileSelect"
              />
              <div class="drop-zone-content">
                <el-icon :size="36"><component :is="UploadIcon" /></el-icon>
                <div style="margin-top: 8px; font-size: 14px;">
                  {{ isDragging ? '松开以上传' : '拖拽文件到此处，或点击选择' }}
                </div>
                <div style="font-size: 12px; color: #999; margin-top: 4px;">
                  支持多文件、拖放，大文件自动分片上传
                </div>
              </div>
            </div>

            <!-- Global drag overlay -->
            <transition name="fade">
              <div v-if="isDragging" class="drop-overlay">
                <div class="drop-overlay-content">
                  <el-icon :size="64" :color="'#fff'"><component :is="UploadIcon" /></el-icon>
                  <div style="margin-top: 16px; font-size: 18px; color: #fff;">松开以上传文件</div>
                  <div style="font-size: 13px; color: rgba(255,255,255,0.7); margin-top: 8px;">
                    {{ draggedFileCount }} 个文件即将上传
                  </div>
                </div>
              </div>
            </transition>
          </el-form-item>

          <!-- Upload queue -->
          <UploadQueue
            :items="uploadQueue"
            :pending-count="pendingCount"
            :queue-item-refs="queueItemRefs"
            :handlers="uploadHandlers"
          />
        </el-tab-pane>

        <el-tab-pane name="url" label="从 URL 上传">
          <el-form-item label="文件的 URL:">
            <el-input
              v-model="form.url"
              :maxlength="2048"
              placeholder="输入文件的 URL"
              @input="handleUrlInput"
            />
          </el-form-item>
          <el-form-item v-if="urlFileInfo.name" label="保存文件名:">
            <el-input
              v-model="form.filename"
              :maxlength="255"
              placeholder="输入保存的文件名"
            />
            <span style="color: #909399; font-size: 12px;">原始文件名: {{ urlFileInfo.name }}，大小: {{ urlFileInfo.size }}</span>
          </el-form-item>
          <!-- URL 备注 -->
          <el-form-item v-if="urlFileInfo.name" label="备注:">
            <el-input
              v-model="form.urlNotes"
              :maxlength="4096"
              type="textarea"
              :rows="2"
              placeholder="输入文件备注（可选）"
            />
          </el-form-item>
          <!-- URL 依赖 -->
          <el-form-item v-if="urlFileInfo.name" label="依赖:">
            <div style="display: flex; gap: 6px; width: 100%">
              <el-autocomplete
                v-model="form.urlDepFileName"
                :maxlength="255"
                :fetch-suggestions="fetchUrlDepSuggestions"
                placeholder="搜索并选择依赖文件"
                clearable
                :teleported="true"
                style="flex: 1"
                @select="handleUrlDepSelect"
              />
              <el-select v-model="form.urlDepRelation" placeholder="依赖关系" :teleported="true" style="width: 150px">
                <el-option v-for="opt in depRelationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
              </el-select>
            </div>
          </el-form-item>
          <!-- URL 上传进度 -->
          <div v-if="urlUploadState.status" class="url-upload-progress">
            <div class="queue-item">
              <div class="queue-item-info">
                <span class="queue-item-name" :title="urlUploadState.fileName">{{ urlUploadState.fileName }}</span>
                <div class="queue-item-meta">
                  <span class="queue-item-size">{{ formatFileSize(urlUploadState.fileSize) }}</span>
                  <div class="queue-item-progress">
                    <span v-if="urlUploadState.status === 'failed'" class="queue-item-error">{{ urlUploadState.error }}</span>
                    <span v-if="urlUploadState.displayText" class="queue-item-speed">{{ urlUploadState.displayText }}</span>
                  </div>
                </div>
                <el-progress
                  v-if="urlUploadState.status === 'uploading' || urlUploadState.status === 'failed'"
                  :percentage="urlUploadState.progress"
                  :show-text="false"
                  :stroke-width="4"
                />
              </div>
              <div class="queue-item-actions">
                <el-tag :type="elTagType(getUrlUploadStatusTag)" size="small">{{ getUrlUploadStatusText }}</el-tag>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane name="clipboard" label="从剪贴板粘贴">
          <!-- 剪贴板读取操作 -->
          <div v-if="!clipboardRead" style="margin-bottom: 8px;">
            <el-button @click="readClipboard" :loading="clipboardReading" type="primary" plain size="small">
              读取剪贴板
            </el-button>
          </div>
          <div v-else style="display: flex; gap: 8px; align-items: center; margin-bottom: 8px;">
            <el-tag type="success" size="small">已读取</el-tag>
            <el-button size="small" link @click="clearClipboard">重新读取</el-button>
            <el-button v-if="!clipboardTypeConfirmed" size="small" type="primary" @click="applyClipboardSelection" :disabled="!clipboardSelectedType">
              确认选择
            </el-button>
          </div>

          <!-- 未读取 -->
          <div v-if="!clipboardRead" style="color: #999; font-size: 12px;">
            仅支持读取<strong>文本</strong>和<strong>图片</strong>格式。若剪贴板包含多种格式，您可以手动选择要读取的类型。
          </div>

          <!-- 已读取，未选择类型：展示可选类型 -->
          <div v-else-if="!clipboardTypeConfirmed">
            <div v-if="clipboardOptions.length > 1" style="margin-bottom: 8px; font-size: 13px; color: #666;">
              检测到剪贴板包含多种格式，请选择要读取的数据类型:
            </div>
            <el-radio-group v-model="clipboardSelectedType">
              <div style="display: flex; flex-direction: column; gap: 8px;">
                <el-radio v-for="opt in clipboardOptions" :key="opt.value" :label="opt.value">
                  {{ opt.label }}
                </el-radio>
              </div>
            </el-radio-group>
          </div>

          <!-- 已选择类型：展示预览 -->
          <template v-else>
            <el-input v-model="clipboardFilename" placeholder="保存的文件名" clearable style="margin-bottom: 4px;" />
            <!-- 图片预览 -->
            <div v-if="clipboardPreview.type === 'image'" class="clipboard-preview">
              <img :src="clipboardPreview.data" class="clipboard-preview-img" />
            </div>
            <!-- 文本预览 -->
            <div v-else-if="clipboardPreview.type === 'text'" style="width: 100%;">
              <el-input
                type="textarea"
                :model-value="clipboardPreview.text"
                :rows="8"
                readonly
                style="font-family: monospace; line-height: 1.6; font-size: 13px;"
              />
            </div>
            <!-- 其它文件预览 -->
            <div v-else-if="clipboardPreview.type === 'other'" class="clipboard-preview-file">
              <el-icon :size="40"><component :is="DocumentIcon" /></el-icon>
              <div class="clipboard-preview-filename">{{ clipboardFilename }}</div>
              <div class="clipboard-preview-info">大小: {{ formatFileSize(clipboardPreview.size) }}</div>
            </div>
            <!-- 剪贴板备注/依赖 -->
            <el-form-item label="备注:" style="margin-top: 12px;">
              <el-input v-model="extraNotes" :maxlength="4096" type="textarea" :rows="2" placeholder="输入文件备注（可选）" />
            </el-form-item>
            <el-form-item label="依赖:">
              <div style="display: flex; gap: 6px; width: 100%">
                <el-autocomplete
                  v-model="extraDepFileName"
                  :maxlength="255"
                  :fetch-suggestions="fetchExtraDepSuggestions"
                  placeholder="搜索并选择依赖文件"
                  clearable
                  :teleported="true"
                  style="flex: 1"
                  @select="handleExtraDepSelect"
                />
                <el-select v-model="extraDepRelation" placeholder="依赖关系" :teleported="true" style="width: 150px">
                  <el-option v-for="opt in depRelationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
                </el-select>
              </div>
            </el-form-item>
          </template>
        </el-tab-pane>

        <el-tab-pane name="text" label="新建文本">
          <el-input v-model="textFilename" placeholder="文件名，如 readme.md" clearable style="margin-bottom: 12px;" />
          <el-input
            v-model="textContent"
            type="textarea"
            :rows="10"
            placeholder="在此输入文件内容..."
            style="font-family: monospace; line-height: 1.6; font-size: 13px;"
          />
          <!-- 文本备注/依赖 -->
          <el-form-item label="备注:" style="margin-top: 12px;">
            <el-input v-model="extraNotes" :maxlength="4096" type="textarea" :rows="2" placeholder="输入文件备注（可选）" />
          </el-form-item>
          <el-form-item label="依赖:">
            <div style="display: flex; flex-direction: column; gap: 6px; width: 100%">
              <el-autocomplete
                v-model="extraDepFileName"
                :maxlength="255"
                :fetch-suggestions="fetchExtraDepSuggestions"
                placeholder="搜索并选择依赖文件"
                clearable
                @select="handleExtraDepSelect"
              />
              <el-select v-model="extraDepRelation" placeholder="依赖关系">
                <el-option v-for="opt in depRelationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
              </el-select>
            </div>
          </el-form-item>
        </el-tab-pane>
      </el-tabs>
    </el-form>
    </div>

    <template #footer>
      <div class="upload-dialog-footer">
        <div class="upload-footer-left">
          <el-checkbox v-if="isTempUpload" v-model="deleteOnDownloadModel">下载后自动删除</el-checkbox>
          <el-alert v-if="uploadMessage" :type="uploadMessageType" :title="uploadMessage" :closable="false" class="upload-footer-message" />
        </div>

        <!-- 右侧：操作按钮 -->
        <div class="upload-footer-actions">
          <template v-if="form.uploadMethod === 'clipboard'">
            <el-button
              type="primary"
              @click="uploadClipboard"
              :disabled="!clipboardRead || !clipboardTypeConfirmed || !clipboardFilename.trim()"
              :loading="uploading"
            >
              开始上传
            </el-button>
          </template>
          <template v-else-if="form.uploadMethod === 'text'">
            <el-button
              type="primary"
              @click="saveTextFile"
              :disabled="!textFilename.trim()"
            >
              保存并上传
            </el-button>
          </template>
          <template v-else>
            <el-button
              type="primary"
              @click="handleFooterClick"
              :disabled="!canStartUpload"
              :loading="uploading"
            >
              开始上传
            </el-button>
          </template>
          <el-button @click="requestClose">关闭</el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>


<script setup>
import { ref, reactive, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import store from '@/store'
import { SetupApi } from '@/api'
import { elTagType } from './constants'
import { UploadIcon, DocumentIcon, MaximizeIcon, RestoreIcon } from './icons'
import UploadQueue from './UploadQueue.vue'
import { useDependency } from './hooks/useDependency'
import { useUploadQueue } from './hooks/useUploadQueue'
import { useClipboard } from './hooks/useClipboard'
import { useUrlUpload } from './hooks/useUrlUpload'

const props = defineProps({
  uploadApi: {
    type: Object,
    required: true
    // { type: 'chunked', createSession, getSession, uploadChunk, finalize, uploadUrl }
    // type 必须是 'chunked'
  },
  defaultDir: {
    type: String,
    default: ''
  },
  deleteOnDownload: {
    type: Boolean,
    default: false
  },
  modelValue: {
    type: Boolean,
    default: false
  },
  title: {
    type: String,
    default: '上传文件'
  }
})

const emit = defineEmits(['upload-success', 'upload-error', 'upload-start', 'upload-change', 'close', 'start-upload', 'update:modelValue', 'update:deleteOnDownload'])
const message = ElMessage
const dialog = {
  warning: (opts) => ElMessageBox.confirm(opts.content, opts.title, {
    confirmButtonText: opts.positiveText || '确定',
    cancelButtonText: opts.negativeText || '取消',
    type: 'warning',
    closeOnClickModal: !!opts.onMaskClick
  }).then(() => { opts.onPositiveClick?.() }).catch(() => { opts.onNegativeClick?.() })
}

// API 配置（从 props 获取）
const api = computed(() => props.uploadApi)
const getApiUrl = (key, ...args) => {
  const value = api.value[key]
  if (typeof value === 'function') {
    return value(...args)
  }
  return value
}

// 底部提示信息状态
const uploadMessage = ref('')
const uploadMessageType = ref('info')

// 主要表单状态
const form = reactive({
  rootName: '',
  uploadDir: '',
  uploadMethod: 'local',
  url: '',
  filename: '',
  urlNotes: '',
  urlDepFileName: '',
  urlDepRecordId: null,
  urlDepRelation: 'requires',
  urlDepDescription: ''
})

// Computed
const canUrlUpload = computed(() => !!form.url && !!form.filename)

// 是否需要根目录选择（临时文件/私有存储不需要）
const needsRootSelection = computed(() => {
  const url = getApiUrl('createSession')
  return url && !url.includes('/temp/') && !url.includes('/private/')
})

// 是否为临时文件上传（决定是否显示"下载后自动删除"勾选）
const isTempUpload = computed(() => {
  const url = getApiUrl('createSession')
  return url && url.includes('/temp/')
})

// "下载后自动删除"勾选（支持父级 v-model:delete-on-download）
const deleteOnDownloadModel = computed({
  get: () => props.deleteOnDownload,
  set: (val) => emit('update:deleteOnDownload', val)
})

// 弹窗放大/还原（撑满窗口）
const maximized = ref(false)
const toggleMaximize = () => {
  maximized.value = !maximized.value
}

// ===== 共享目录加载与路径匹配 =====
const rootDirOptions = ref([])
const extractRootAndPath = (path) => {
  if (!path || path === '/' || path === '\\') {
    return { rootName: '', dir: '/' }
  }
  const normalizedPath = path.replace(/\\/g, '/')
  const pathWithoutSlash = normalizedPath.replace(/^\/+/, '')

  for (const root of rootDirOptions.value) {
    const rootName = root.value
    if (pathWithoutSlash === rootName || pathWithoutSlash.startsWith(rootName + '/')) {
      const relativePath = pathWithoutSlash.slice(rootName.length)
      return {
        rootName: rootName,
        dir: relativePath || '/'
      }
    }
  }

  if (rootDirOptions.value.length > 0) {
    return {
      rootName: rootDirOptions.value[0].value,
      dir: '/' + pathWithoutSlash
    }
  }

  return { rootName: '', dir: '/' }
}

// 监听当前路径变化，更新上传目录
watch(() => store.state.currentPath, (newPath) => {
  const { rootName, dir } = extractRootAndPath(newPath)
  if (rootName) {
    form.rootName = rootName
    form.uploadDir = dir
  }
})

// ===== 依赖搜索 =====
const deps = useDependency({ form })
const {
  depRelationOptions,
  fetchDepSuggestions,
  handleDepSelect,
  fetchUrlDepSuggestions,
  handleUrlDepSelect,
  extraNotes,
  extraDepFileName,
  extraDepRecordId,
  extraDepRelation,
  extraDepDescription,
  fetchExtraDepSuggestions,
  handleExtraDepSelect
} = deps

// ===== 上传队列（分片上传 + 会话持久化）=====
const queue = useUploadQueue({
  form,
  getApiUrl,
  needsRootSelection,
  props,
  message,
  dialog,
  emit
})
const {
  uploadQueue,
  uploading,
  queueItemRefs,
  pendingCount,
  hasActiveUploads,
  formatFileSize,
  addFileToQueue,
  removeFromQueue,
  retryUpload,
  pauseUpload,
  resumeUpload,
  selectFileForItem,
  startUpload,
  restoreUploadSessions,
  saveUploadSessions
} = queue

// 队列列表操作回调，下发给 UploadQueue 子组件
const uploadHandlers = {
  selectFileForItem,
  pauseUpload,
  resumeUpload,
  retryUpload,
  removeFromQueue,
  fetchDepSuggestions,
  handleDepSelect,
  saveUploadSessions
}

// ===== 剪贴板上传 =====
const clipboard = useClipboard({
  form,
  message,
  addFileToQueue,
  startUpload,
  uploadMessage,
  uploadMessageType,
  extras: { extraNotes, extraDepFileName, extraDepRecordId, extraDepRelation, extraDepDescription }
})
const {
  clipboardPreview,
  clipboardRead,
  clipboardReading,
  clipboardFilename,
  clipboardOptions,
  clipboardSelectedType,
  clipboardTypeConfirmed,
  readClipboard,
  applyClipboardSelection,
  clearClipboard,
  uploadClipboard
} = clipboard

// ===== URL 上传 =====
const url = useUrlUpload({
  form,
  getApiUrl,
  needsRootSelection,
  uploading,
  uploadMessage,
  uploadMessageType,
  emit
})
const {
  urlFileInfo,
  urlUploadState,
  getUrlUploadStatusTag,
  getUrlUploadStatusText,
  handleUrlInput,
  handleUrlUpload,
  stopUrlPolling,
  urlInputTimer
} = url

// ===== 新建文本 =====
const textFilename = ref('newfile.txt')
const textContent = ref('')
const saveTextFile = async () => {
  if (!textFilename.value.trim()) {
    message.warning('请输入文件名')
    return
  }
  const filename = textFilename.value.trim()
  const blob = new Blob([textContent.value], { type: 'text/plain;charset=utf-8' })
  const file = new File([blob], filename, { type: 'text/plain;charset=utf-8' })
  addFileToQueue({
    name: filename,
    size: file.size,
    file: file,
    notes: extraNotes.value,
    depFileName: extraDepFileName.value,
    depFileRecordId: extraDepRecordId.value,
    depRelation: extraDepRelation.value,
    depDescription: extraDepDescription.value
  })
  message.success(`已添加文本文件: ${filename}`)
  textFilename.value = 'newfile.txt'
  textContent.value = ''
  extraNotes.value = ''
  extraDepFileName.value = ''
  extraDepRecordId.value = null
  extraDepRelation.value = 'requires'
  extraDepDescription.value = ''
  form.uploadMethod = 'local'
  uploadMessage.value = ''
  startUpload()
}

// ===== 能否开始 / 底部按钮 =====
const canStartUpload = computed(() => {
  if (form.uploadMethod === 'url') {
    return canUrlUpload.value
  }
  if (form.uploadMethod === 'clipboard' || form.uploadMethod === 'text') {
    return false
  }
  return uploadQueue.value.some(item => item.status === 'pending')
})

const handleFooterClick = () => {
  if (form.uploadMethod === 'url') {
    handleUrlUpload()
  } else {
    startUpload()
  }
}

const hasUrlUploading = computed(() =>
  urlUploadState.value.status === 'uploading'
)

// 关闭保护：有活跃上传时需确认；URL 下载在后台继续执行
const requestClose = () => {
  if (hasActiveUploads.value) {
    ElMessageBox.confirm('有文件正在上传，关闭弹框将中断所有上传。是否确认关闭？', '上传进行中', {
      confirmButtonText: '确认关闭',
      cancelButtonText: '继续上传',
      type: 'warning'
    }).then(() => {
      emit('update:modelValue', false)
      emit('close')
    }).catch(() => {
      // 不关闭，保持打开
    })
  } else if (hasUrlUploading.value) {
    ElMessage.info('URL 下载在后台继续执行，您可以在通知中查看进度', { duration: 4000 })
    emit('update:modelValue', false)
    emit('close')
  } else {
    emit('update:modelValue', false)
    emit('close')
  }
}

// ===== 拖放 / 文件选择 =====
const dropZoneRef = ref(null)
const fileInputRef = ref(null)
const isDragging = ref(false)
const draggedFileCount = ref(0)
let dragCounter = 0 // Track nested drag events

const handleDragEnter = (e) => {
  dragCounter++
  if (e.dataTransfer?.types?.includes('Files')) {
    isDragging.value = true
    draggedFileCount.value = e.dataTransfer?.files?.length || 0
  }
}

const handleDragOver = (e) => {
  if (e.dataTransfer?.types?.includes('Files')) {
    isDragging.value = true
    draggedFileCount.value = e.dataTransfer?.files?.length || 0
  }
}

const handleDragLeave = (e) => {
  dragCounter--
  if (dragCounter === 0) {
    isDragging.value = false
  }
}

const handleDrop = (e) => {
  dragCounter = 0
  isDragging.value = false

  const files = e.dataTransfer?.files
  if (!files || files.length === 0) return

  Array.from(files).forEach(file => {
    addFileToQueue({
      name: file.name,
      size: file.size,
      file: file
    })
  })

  message.success(`已添加 ${files.length} 个文件到上传队列`)
}

// Trigger file input
const triggerFileInput = () => {
  fileInputRef.value?.click()
}

// Handle file select from input
const handleFileSelect = (e) => {
  const files = e.target.files
  if (!files || files.length === 0) return

  Array.from(files).forEach(file => {
    addFileToQueue({
      name: file.name,
      size: file.size,
      file: file
    })
  })

  message.success(`已添加 ${files.length} 个文件到上传队列`)
  e.target.value = ''
}

// ===== 共享目录加载 =====
const loadRootDirs = async () => {
  try {
    const data = await SetupApi.getStatus()
    if (data.success && data.data?.rootDirs) {
      rootDirOptions.value = data.data.rootDirs.map(dir => ({
        label: dir.name,
        value: dir.name
      }))

      if (!needsRootSelection.value) {
        if (rootDirOptions.value.length > 0) {
          form.rootName = rootDirOptions.value[0].value
        }
        if (props.defaultDir) {
          form.uploadDir = props.defaultDir
        }
      } else {
        const currentPath = store.state.currentPath
        const { rootName, dir } = extractRootAndPath(currentPath)
        if (rootName) {
          form.rootName = rootName
          form.uploadDir = dir
        } else if (rootDirOptions.value.length > 0) {
          form.rootName = rootDirOptions.value[0].value
          form.uploadDir = '/'
        }
      }
    }
  } catch (e) {
    console.error('获取共享目录失败:', e)
  }
}

loadRootDirs()

// 监听 api 就绪，触发恢复上传会话
watch(api, (newApi) => {
  if (newApi && newApi.createSession) {
    nextTick(() => {
      restoreUploadSessions()
    })
  }
}, { immediate: true })

// 页面关闭/刷新时提示
const handleBeforeUnload = (e) => {
  if (hasActiveUploads.value || hasUrlUploading.value) {
    e.preventDefault()
    e.returnValue = '有文件正在上传中，确定要离开吗？'
    return e.returnValue
  }
}

onMounted(() => {
  window.addEventListener('beforeunload', handleBeforeUnload)
})

// 组件卸载时清理定时器
onUnmounted(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload)
  clearTimeout(urlInputTimer.value)
  stopUrlPolling()
})

// Expose startUpload
defineExpose({ startUpload, canUrlUpload, handleUrlUpload, canStartUpload, addFile: addFileToQueue, hasActiveUploads, hasUrlUploading })
</script>

<style lang="less">
.upload-manager {
  height: 65vh;
  overflow: auto;

  .drop-zone {
    margin-top: 12px;
    padding: 24px;
    border: 2px dashed #d9d9d9;
    border-radius: 8px;
    text-align: center;
    transition: all 0.2s;
    cursor: pointer;
    color: #666;

    &:hover {
      border-color: #FF6600;
      color: #FF6600;
    }
  }

  .drop-zone-active {
    border-color: #FF6600;
    background: rgba(255, 102, 0, 0.05);
    color: #FF6600;
  }

  .drop-zone-content {
    display: flex;
    align-items: center;
    gap: 20px;
  }

  .upload-queue {
    margin-top: 16px;
    border: 1px solid #eee;
    border-radius: 8px;
    overflow: hidden;
    width: 100%;
    box-sizing: border-box;
    max-height: 200px;
  }

  .queue-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 12px;
    background: #fafafa;
    border-bottom: 1px solid #eee;
    font-size: 13px;
    color: #333;
  }

  .queue-list {
    max-height: 280px;
    overflow-y: auto;
  }

  .queue-item {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 10px 12px;
      border-bottom: 1px solid #f5f5f5;

      &:last-child {
        border-bottom: none;
      }

      .queue-item-index {
        width: 20px;
        flex-shrink: 0;
        text-align: center;
        font-size: 12px;
        color: #bbb;
        font-variant-numeric: tabular-nums;
      }

      .queue-item-info {
      flex: 1;
      min-width: 0;

      .queue-item-name {
        display: block;
        font-weight: 500;
        font-size: 13px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        transition: color 0.2s;

        &.editable {
          cursor: pointer;

          &:hover {
            color: #FF6600;
          }
        }
      }

      .rename-input {
        width: 100%;
      }

      .rename-error {
        font-size: 11px;
        color: #d03050;
        margin-top: 2px;
      }

      .queue-item-meta {
        display: flex;
        align-items: center;
        justify-content: space-between;
      }

      .queue-item-size {
        font-size: 11px;
        color: #999;
        flex: 2;
      }

      .queue-item-progress {
        flex: 8;
        display: flex;
        gap: 20px;
        justify-content: flex-end;
      }

      .queue-item-speed {
        font-size: 11px;
        color: #666;
        margin-top: 2px;
      }

      .queue-item-error {
        font-size: 11px;
        color: #d03050;
        margin-top: 2px;
      }
    }

    .queue-item-actions {
      display: flex;
      align-items: center;
      gap: 6px;
    }
  }

  .drop-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(255, 102, 0, 0.9);
    display: flex;
    justify-content: center;
    align-items: center;
    z-index: 9999;
    pointer-events: none;

    .drop-overlay-content {
      text-align: center;
    }
  }

  .fade-enter-active,
  .fade-leave-active {
    transition: opacity 0.2s ease;
  }

  .fade-enter-from,
  .fade-leave-to {
    opacity: 0;
  }

  .clipboard-preview {
    display: flex;
    flex-direction: column;
    gap: 8px;

    .clipboard-preview-img {
      max-width: 100%;
      max-height: 300px;
      border-radius: 4px;
      object-fit: contain;
      border: 1px solid #eee;
    }
  }

  .clipboard-preview-file {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 20px;
    border: 1px dashed #d9d9d9;
    border-radius: 8px;
    text-align: center;
  }

  .clipboard-preview-filename {
    font-weight: 500;
    font-size: 14px;
    color: #333;
  }

  .clipboard-preview-info {
    color: #999;
    font-size: 12px;
  }

  .upload-method-tabs {
    margin-top: 4px;

    :deep(.el-tabs__nav) {
      width: 120px;
    }

    :deep(.el-tabs__header) {
      height: 100%;
    }

    :deep(.el-tabs__content) {
      padding-left: 16px;
    }
  }

  .upload-footer-left {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
    padding-right: 12px;
  }

  .upload-footer-message {
    flex: 1;
    min-width: 0;
    padding: 6px 12px;
    font-size: 12px;
  }
}

// ============ 上传弹窗（dialog 集成于组件内，append-to-body 全局作用） ============
.upload-dialog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding-right: 4px;
}

.upload-dialog-title {
  font-size: 16px;
  font-weight: 600;
}

.upload-maximize-btn {
  color: #666;

  &:hover {
    color: #FF6600;
  }
}

.upload-dialog-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.upload-footer-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.upload-dialog--maximized {
  width: 100vw !important;
  max-width: 100vw !important;
  height: 100vh;
  max-height: 100vh;
  margin: 0 !important;
  display: flex;
  flex-direction: column;

  .el-dialog__body {
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .upload-manager {
    height: 100%;
  }
}
</style>