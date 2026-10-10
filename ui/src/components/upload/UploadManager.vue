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
            <el-option v-for="opt in rootDirOptions" :key="opt.value" :label="opt.label" :value="opt.value" :disabled="opt.disabled" />
          </el-select>
          <el-input
            v-model="form.uploadDir"
            :maxlength="1024"
            placeholder="输入相对目录，如: test"
            :style="{ flex: 1 }"
          />
        </div>
        <div style="font-size: 12px; color: var(--el-text-color-placeholder);">
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
                <div style="font-size: 12px; color: var(--el-text-color-placeholder); margin-top: 4px;">
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
          <!-- 单条输入（默认） -->
          <el-form-item label="文件的 URL:">
            <div class="url-add-row">
              <el-input
                v-model="urlSingleUrl"
                :maxlength="2048"
                placeholder="输入文件的 URL"
                clearable
                style="flex: 1"
                @keyup.enter="addSingleUrl"
              />
              <el-button type="primary" plain :loading="urlProbing" @click="addSingleUrl">添加</el-button>
              <el-button link type="primary" @click="toggleUrlBatchMode">
                {{ urlBatchMode ? '收起批量' : '批量添加' }}
              </el-button>
            </div>
          </el-form-item>

          <!-- 批量输入（默认隐藏） -->
          <el-form-item v-if="urlBatchMode" label="批量 URL:">
            <el-input
              v-model="urlBatchText"
              type="textarea"
              :rows="5"
              placeholder="每行一个 URL，支持 http/https/ftp/ftps"
            />
            <div class="url-batch-actions">
              <el-button type="primary" plain :loading="urlProbing" @click="addBatchUrls">解析并添加</el-button>
              <el-button link @click="urlBatchText = ''">清空</el-button>
            </div>
          </el-form-item>

          <!-- 条目列表 -->
          <div v-if="urlItems.length" class="url-item-list">
            <div v-for="item in urlItems" :key="item.id" class="url-item-card">
              <div class="url-item-body">
                <div class="url-item-url" :title="item.url">{{ item.url }}</div>
                <div class="url-item-row">
                  <el-input
                    v-model="item.fileName"
                    size="small"
                    placeholder="保存文件名"
                    :disabled="!['draft', 'failed', 'cancelled'].includes(item.status)"
                    class="url-item-name"
                  />
                  <span class="url-item-size">{{ formatFileSize(item.fileSize) }}</span>
                  <el-tag v-if="item.targetExists" type="warning" size="small">目标已存在</el-tag>
                  <el-tag v-if="item.alreadyUploaded" type="info" size="small">历史上传过</el-tag>
                </div>
                <el-input
                  v-if="item.status === 'draft'"
                  v-model="item.notes"
                  size="small"
                  :maxlength="4096"
                  placeholder="备注（可选）"
                />
                <div v-if="item.status === 'draft'" class="url-item-dep">
                  <el-autocomplete
                    v-model="item.depFileName"
                    size="small"
                    :maxlength="255"
                    :fetch-suggestions="(q, cb) => fetchDepSuggestions(item, q, cb)"
                    placeholder="搜索并选择依赖文件（可选）"
                    clearable
                    :teleported="true"
                    style="flex: 1"
                    @select="(opt) => handleDepSelect(item, opt)"
                  />
                  <el-select v-model="item.depRelation" size="small" placeholder="依赖关系" :teleported="true" style="width: 150px">
                    <el-option v-for="opt in depRelationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
                  </el-select>
                </div>
                <div v-if="item.displayText || item.error" class="url-item-status-line">
                  <span v-if="item.status === 'failed'" class="url-item-error">{{ item.error }}</span>
                  <span v-else class="url-item-speed">{{ item.displayText }}</span>
                </div>
                <el-progress
                  v-if="item.status === 'uploading'"
                  :percentage="item.progress"
                  :show-text="false"
                  :stroke-width="4"
                />
                <!-- 已完成：可选择算法计算校验值（md5/sha1/sha256，可多选），结果仅回显供完整性校验 -->
                <div v-if="item.status === 'completed'" class="url-item-checksum">
                  <div class="url-item-checksum-controls">
                    <span class="url-item-checksum-hint">校验值:</span>
                    <el-checkbox-group v-model="item.checksumAlgorithms" size="small" :disabled="item.checksumLoading">
                      <el-checkbox v-for="algo in CHECKSUM_ALGORITHMS" :key="algo.value" :label="algo.value">{{ algo.label }}</el-checkbox>
                    </el-checkbox-group>
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="item.checksumLoading"
                      :disabled="!item.checksumAlgorithms.length"
                      @click="computeUrlChecksum(item.id)"
                    >
                      计算校验值
                    </el-button>
                    <el-button v-if="item.checksums" size="small" link type="primary" @click="copyAllChecksums(item)">全部复制</el-button>
                  </div>
                  <div v-if="item.checksums" class="url-item-checksum-results">
                    <div v-for="algo in CHECKSUM_ALGORITHMS" :key="algo.value" v-show="item.checksums[algo.value]" class="url-item-checksum-row">
                      <span class="url-item-checksum-label">{{ algo.label }}</span>
                      <span class="url-item-checksum-value">{{ item.checksums[algo.value] }}</span>
                      <el-button size="small" link type="primary" @click="copyChecksum(item, algo.value)">复制</el-button>
                    </div>
                  </div>
                  <div v-if="item.checksumError" class="url-item-error">{{ item.checksumError }}</div>
                </div>
              </div>
              <div class="url-item-actions">
                <el-tag :type="elTagType(urlStatusTag(item.status))" size="small">{{ urlStatusText(item.status) }}</el-tag>
                <el-button v-if="item.status === 'draft'" size="small" type="primary" link @click="uploadUrlItem(item.id)">上传</el-button>
                <el-button v-if="item.status === 'pending' || item.status === 'uploading'" size="small" type="danger" link @click="cancelUrlItem(item.id)">取消</el-button>
                <el-button v-if="item.status === 'failed' || item.status === 'cancelled'" size="small" type="primary" link @click="retryUrlItem(item.id)">重试</el-button>
                <el-button size="small" link @click="removeUrlItem(item.id)">删除</el-button>
              </div>
            </div>
          </div>
          <div v-else class="url-empty-hint">
            输入 URL 后点击「添加」；如需一次添加多个，点击「批量添加」粘贴多行。
          </div>

          <!-- 汇总与整体上传 -->
          <div v-if="urlItems.length" class="url-summary">
            <span>共 {{ urlItems.length }} 项 · 待上传 {{ urlDraftCount }} · 已完成 {{ urlCompletedCount }} · 失败 {{ urlFailedCount }}</span>
            <el-button size="small" type="primary" :disabled="urlDraftCount === 0" :loading="urlEnqueuing" @click="uploadAllUrls">
              开始上传 ({{ urlDraftCount }})
            </el-button>
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
          <div v-if="!clipboardRead" style="color: var(--el-text-color-placeholder); font-size: 12px;">
            仅支持读取<strong>文本</strong>和<strong>图片</strong>格式。若剪贴板包含多种格式，您可以手动选择要读取的类型。
          </div>

          <!-- 已读取，未选择类型：展示可选类型 -->
          <div v-else-if="!clipboardTypeConfirmed">
            <div v-if="clipboardOptions.length > 1" style="margin-bottom: 8px; font-size: 13px; color: var(--el-text-color-regular);">
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
          <template v-else-if="form.uploadMethod === 'local'">
            <el-button
              type="primary"
              @click="startUpload"
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
import { useUrlBatchUpload, CHECKSUM_ALGORITHMS } from './hooks/useUrlBatchUpload'
import { copyToClipboard } from '@/utils/clipboard'

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
  uploadMethod: 'local'
})

// Computed
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
    const fallback = rootDirOptions.value.find(r => !r.disabled) || rootDirOptions.value[0]
    return {
      rootName: fallback.value,
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
const deps = useDependency()
const {
  depRelationOptions,
  fetchDepSuggestions,
  handleDepSelect,
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

// ===== URL 批量上传 =====
// 条目仅在前端维护草稿；用户点击「上传」后才入队到后端，由全局队列按并行上限调度
const {
  items: urlItems,
  batchMode: urlBatchMode,
  batchText: urlBatchText,
  singleUrl: urlSingleUrl,
  probing: urlProbing,
  enqueuing: urlEnqueuing,
  hasActive: urlHasActive,
  draftCount: urlDraftCount,
  completedCount: urlCompletedCount,
  failedCount: urlFailedCount,
  statusText: urlStatusText,
  statusTag: urlStatusTag,
  addSingle: addSingleUrl,
  addBatch: addBatchUrls,
  toggleBatchMode: toggleUrlBatchMode,
  removeItem: removeUrlItem,
  uploadItem: uploadUrlItem,
  uploadAll: uploadAllUrls,
  retryItem: retryUrlItem,
  cancelItem: cancelUrlItem,
  computeChecksum: computeUrlChecksum,
  stopAllPolling: stopAllUrlPolling
} = useUrlBatchUpload({
  form,
  getApiUrl,
  needsRootSelection,
  uploadMessage,
  uploadMessageType,
  emit
})

// 复制文本到剪贴板（统一走 utils/clipboard，非安全上下文自动降级）
const copyText = async (text) => {
  if (!text) return
  const ok = await copyToClipboard(text)
  if (ok) {
    message.success('已复制')
  } else {
    message.error('复制失败')
  }
}

// 复制某个算法的校验值
const copyChecksum = (item, algo) => copyText(item.checksums?.[algo])

// 复制条目下已计算的全部校验值（算法: 值，逐行）
const copyAllChecksums = (item) => {
  if (!item.checksums) return
  const lines = CHECKSUM_ALGORITHMS
    .filter(a => item.checksums[a.value])
    .map(a => `${a.label}: ${item.checksums[a.value]}`)
  copyText(lines.join('\n'))
}

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
// URL 标签页自带「开始上传 (N)」，底部按钮只服务本地上传
const canStartUpload = computed(() => {
  if (form.uploadMethod !== 'local') return false
  return uploadQueue.value.some(item => item.status === 'pending')
})

// 关闭保护：本地上传有活跃任务时需确认；URL 上传任务已入后端队列，关闭后继续执行
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
  } else if (urlHasActive.value) {
    ElMessage.info('URL 上传在后台继续执行，您可以在通知中查看进度', { duration: 4000 })
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
// 返回第一个允许上传的根目录选项
const firstUploadableRoot = (opts) => opts.find(o => !o.disabled) || null

const loadRootDirs = async () => {
  try {
    const data = await SetupApi.getStatus()
    if (data.success && data.data?.rootDirs) {
      rootDirOptions.value = data.data.rootDirs.map(dir => ({
        label: dir.name,
        value: dir.name,
        disabled: dir.uploadEnabled === false
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
          // 若当前目录不允许上传，则回退到第一个允许上传的目录
          const currentOpt = rootDirOptions.value.find(o => o.value === rootName)
          if (currentOpt && currentOpt.disabled) {
            const fallback = firstUploadableRoot(rootDirOptions.value)
            form.rootName = fallback ? fallback.value : ''
            form.uploadDir = '/'
          } else {
            form.rootName = rootName
            form.uploadDir = dir
          }
        } else {
          const fallback = firstUploadableRoot(rootDirOptions.value)
          if (fallback) {
            form.rootName = fallback.value
            form.uploadDir = '/'
          }
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
  if (hasActiveUploads.value || urlHasActive.value) {
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
  stopAllUrlPolling()
})

// Expose startUpload
defineExpose({ startUpload, canStartUpload, addFile: addFileToQueue, hasActiveUploads, hasUrlActive: urlHasActive })
</script>

<style lang="less">
@import '@/styles/variables.less';
.upload-manager {
  height: 65vh;
  overflow: auto;

  .drop-zone {
    margin-top: 12px;
    padding: 24px;
    border: 2px dashed @border-color;
    border-radius: 8px;
    text-align: center;
    transition: all 0.2s;
    cursor: pointer;
    color: @text-color-secondary;

    &:hover {
      border-color: @primary-color;
      color: @primary-color;
    }
  }

  .drop-zone-active {
    border-color: @primary-color;
    background: fade(@primary-color, 5%);
    color: @primary-color;
  }

  .drop-zone-content {
    display: flex;
    align-items: center;
    gap: 20px;
  }

  .upload-queue {
    margin-top: 16px;
    border: 1px solid @border-color-light;
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
    background: @bg-color-secondary;
    border-bottom: 1px solid @border-color-light;
    font-size: 13px;
    color: @text-color;
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
      border-bottom: 1px solid @border-color-light;

      &:last-child {
        border-bottom: none;
      }

      .queue-item-index {
        width: 20px;
        flex-shrink: 0;
        text-align: center;
        font-size: 12px;
        color: @text-color-placeholder;
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
            color: @primary-color;
          }
        }
      }

      .rename-input {
        width: 100%;
      }

      .rename-error {
        font-size: 11px;
        color: @error-color;
        margin-top: 2px;
      }

      .queue-item-meta {
        display: flex;
        align-items: center;
        justify-content: space-between;
      }

      .queue-item-size {
        font-size: 11px;
        color: @text-color-placeholder;
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
        color: @text-color-secondary;
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
    background: fade(@primary-accent, 90%);
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
      border: 1px solid @border-color-light;
    }
  }

  .clipboard-preview-file {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 20px;
    border: 1px dashed @border-color;
    border-radius: 8px;
    text-align: center;
  }

  .clipboard-preview-filename {
    font-weight: 500;
    font-size: 14px;
    color: @text-color;
  }

  .clipboard-preview-info {
    color: @text-color-placeholder;
    font-size: 12px;
  }

  // ===== URL 批量上传 =====
  .url-add-row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
  }

  .url-batch-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
  }

  .url-item-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 8px;
  }

  .url-item-card {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 12px;
    border: 1px solid @border-color-light;
    border-radius: 8px;
    background: @bg-color-secondary;
  }

  .url-item-body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .url-item-url {
    font-size: 12px;
    color: @text-color-secondary;
    word-break: break-all;
    line-height: 1.4;
  }

  .url-item-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .url-item-name {
    flex: 1;
    min-width: 0;
  }

  .url-item-size {
    flex-shrink: 0;
    font-size: 11px;
    color: @text-color-placeholder;
    font-variant-numeric: tabular-nums;
  }

  .url-item-dep {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .url-item-status-line {
    font-size: 11px;
    line-height: 1.4;
    word-break: break-all;
    white-space: normal;
  }

  .url-item-speed {
    color: @text-color-secondary;
  }

  .url-item-error {
    color: @error-color;
  }

  .url-item-checksum {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 6px;
    border-top: 1px dashed @border-color-light;
  }

  .url-item-checksum-controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
  }

  .url-item-checksum-hint {
    font-size: 12px;
    color: @text-color-secondary;
  }

  .url-item-checksum-results {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .url-item-checksum-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }

  .url-item-checksum-label {
    flex-shrink: 0;
    width: 56px;
    color: @text-color-secondary;
  }

  .url-item-checksum-value {
    flex: 1;
    min-width: 0;
    font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
    color: @text-color;
    word-break: break-all;
  }

  .url-item-actions {
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
  }

  .url-empty-hint {
    padding: 16px 0;
    font-size: 12px;
    color: @text-color-placeholder;
    line-height: 1.6;
  }

  .url-summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid @border-color-light;
    font-size: 12px;
    color: @text-color-secondary;
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
  color: @text-color-secondary;

  &:hover {
    color: @primary-color;
  }
}

.upload-dialog-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

// 底部提示/错误区：需能容纳超长文本（如含完整 URL 的网络错误），
// 通过换行 + 限高滚动避免撑破弹窗底部
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
  max-height: 84px;
  overflow-y: auto;

  .el-alert__content {
    min-width: 0;
  }

  .el-alert__title {
    word-break: break-all;
    white-space: normal;
    line-height: 1.5;
  }
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