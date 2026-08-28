<template>
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
          <el-form-item label="选择文件:">
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
          <div v-if="uploadQueue.length > 0" class="upload-queue">
            <div class="queue-header">
              <span>上传队列 ({{ uploadQueue.length }} 个)</span>
              <span style="color: #999;">等待 {{ pendingCount }} 个</span>
            </div>
            <div ref="queueListRef" class="queue-list" style="max-height: 160px; overflow-y: auto;">
              <template v-for="item in uploadQueue" :key="item.id">
                <div
                  :ref="el => { if (el) queueItemRefs[item.id] = el }"
                  class="queue-item"
                >
                  <div class="queue-item-info">
                    <template v-if="editingItemId === item.id">
                      <el-input
                        ref="renameInputRef"
                        v-model="renameValue"
                        :maxlength="255"
                        size="small"
                        class="rename-input"
                        @keyup.enter="confirmRename(item)"
                        @keyup.escape="cancelRename"
                        @blur="confirmRename(item)"
                      />
                      <div v-if="renameError" class="rename-error">{{ renameError }}</div>
                    </template>
                    <template v-else>
                      <span
                        class="queue-item-name"
                        :class="{ editable: canEditFile(item) }"
                        :title="canEditFile(item) ? (item.name + '（点击修改文件名）') : item.name"
                        @click="startRename(item)"
                      >{{ item.name }}</span>
                    </template>
                    <div class="queue-item-meta">
                      <span class="queue-item-size">{{ formatFileSize(item.size) }}</span>
                      <div class="queue-item-progress">
                        <span v-if="item.error && item.status !== 'needFile'" class="queue-item-error">{{ item.error }}</span>
                        <span v-if="item.displayText" class="queue-item-speed">{{ item.displayText }}</span>
                        <span v-if="item.status === 'needFile'" class="queue-item-error">请重新选择文件以继续上传</span>
                      </div>
                    </div>
                    <el-progress
                      v-if="item.status === 'uploading' || item.status === 'failed' || item.status === 'needFile'"
                      :percentage="item.progress"
                      :show-text="false"
                      :stroke-width="4"
                    />
                  </div>
                  <div class="queue-item-actions">
                    <el-tag :type="elTagType(getStatusTagType(item.status))" size="small">{{ getStatusText(item.status) }}</el-tag>
                    <el-button
                      v-if="item.status === 'pending'"
                      size="small"
                      link
                      @click="item.showNotes = !item.showNotes"
                    >
                      {{ item.showNotes ? '收起' : '备注' }}
                    </el-button>
                    <el-button
                      v-if="item.status === 'pending'"
                      size="small"
                      link
                      @click="item.showDeps = !item.showDeps"
                    >
                      {{ item.showDeps ? '收起' : '依赖' }}
                    </el-button>
                    <el-button
                      v-if="item.status === 'needFile'"
                      type="warning"
                      size="small"
                      @click="selectFileForItem(item)"
                    >
                      选择文件
                    </el-button>
                    <el-button
                      v-if="item.status === 'uploading'"
                      type="info"
                      size="small"
                      @click="pauseUpload(item)"
                    >
                      暂停
                    </el-button>
                    <el-button
                      v-if="item.status === 'paused'"
                      type="success"
                      size="small"
                      @click="resumeUpload(item)"
                    >
                      继续
                    </el-button>
                    <el-button
                      v-if="item.status === 'failed'"
                      type="warning"
                      size="small"
                      @click="retryUpload(item)"
                    >
                      重试
                    </el-button>
                    <el-button
                      v-if="item.status === 'needFile' || item.status === 'pending' || item.status === 'failed' || item.status === 'paused' || item.status === 'uploading'"
                      type="danger"
                      size="small"
                      link
                      circle
                      title="取消上传"
                      @click="removeFromQueue(item.id)"
                    >
                      <el-icon><component :is="DeleteIcon" /></el-icon>
                    </el-button>
                  </div>
                </div>
                <!-- 备注区域 -->
                <div v-if="item.showNotes" class="queue-item-extra" :style="{ padding: '0 12px 8px' }">
                  <el-input
                    v-model="item.notes"
                    :maxlength="500"
                    type="textarea"
                    placeholder="输入文件备注..."
                    :rows="2"
                    size="small"
                  />
                </div>
                <!-- 依赖区域 -->
                <div v-if="item.showDeps" class="queue-item-extra" :style="{ padding: '0 12px 8px' }">
                  <div :style="{ display: 'flex', gap: '6px', flexDirection: 'column' }">
                    <el-autocomplete
                      v-model="item.depFileName"
                      :maxlength="255"
                      :fetch-suggestions="(q, cb) => fetchDepSuggestions(item, q, cb)"
                      placeholder="搜索并选择依赖文件"
                      size="small"
                      clearable
                      @select="(opt) => handleDepSelect(item, opt)"
                    />
                    <el-select
                      v-model="item.depRelation"
                      size="small"
                      placeholder="依赖关系"
                    >
                      <el-option v-for="opt in depRelationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
                    </el-select>
                  </div>
                </div>
              </template>
            </div>
          </div>
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
              :maxlength="500"
              type="textarea"
              :rows="2"
              placeholder="输入文件备注（可选）"
            />
          </el-form-item>
          <!-- URL 依赖 -->
          <el-form-item v-if="urlFileInfo.name" label="依赖:">
            <div style="display: flex; flex-direction: column; gap: 6px; width: 100%">
              <el-autocomplete
                v-model="form.urlDepFileName"
                :maxlength="255"
                :fetch-suggestions="fetchUrlDepSuggestions"
                placeholder="搜索并选择依赖文件"
                clearable
                @select="handleUrlDepSelect"
              />
              <el-select v-model="form.urlDepRelation" placeholder="依赖关系">
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
              <el-input v-model="extraNotes" :maxlength="500" type="textarea" :rows="2" placeholder="输入文件备注（可选）" />
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
          </template>
        </el-tab-pane>

        <el-tab-pane name="text" label="新建文本">
          <el-input v-model="textFilename" placeholder="文件名，如 readme.md" clearable style="margin-bottom: 12px;" />
          <el-input
            v-model="textContent"
            type="textarea"
            :rows="15"
            placeholder="在此输入文件内容..."
            style="font-family: monospace; line-height: 1.6; font-size: 13px;"
          />
          <!-- 文本备注/依赖 -->
          <el-form-item label="备注:" style="margin-top: 12px;">
            <el-input v-model="extraNotes" :maxlength="500" type="textarea" :rows="2" placeholder="输入文件备注（可选）" />
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

    <!-- 底部：左侧为剪贴板操作按钮 + 上传提示，右侧为操作按钮 -->
    <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 16px;">
      <div class="upload-footer-left">
        <template v-if="form.uploadMethod === 'clipboard'">
          <div v-if="!clipboardRead">
            <el-button @click="readClipboard" :loading="clipboardReading" type="primary" plain size="small">
              读取剪贴板
            </el-button>
          </div>
          <div v-else style="display: flex; gap: 8px; align-items: center;">
            <el-tag type="success" size="small">已读取</el-tag>
            <el-button size="small" link @click="clearClipboard">重新读取</el-button>
            <el-button v-if="!clipboardTypeConfirmed" size="small" type="primary" @click="applyClipboardSelection" :disabled="!clipboardSelectedType">
              确认选择
            </el-button>
          </div>
        </template>
        <el-alert v-if="uploadMessage" :type="uploadMessageType" :title="uploadMessage" :closable="false" class="upload-footer-message" />
      </div>

      <!-- 右侧：操作按钮 -->
      <div style="display: flex; gap: 12px;">
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
        <el-button @click="emit('close')">关闭</el-button>
      </div>
    </div>
  </div>
</template>


<script setup>
import { ref, reactive, computed, h, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { NumberUtils, xxh3Hash } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'
import { AuthApi, SetupApi, UploadApi, FileRecordApi, DependencyApi, request } from '@/api'

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
  }
})

// API 配置（从 props 获取）
const api = computed(() => props.uploadApi)

const emit = defineEmits(['upload-success', 'upload-error', 'upload-start', 'upload-change', 'close', 'start-upload'])
const message = ElMessage
const dialog = {
  warning: (opts) => ElMessageBox.confirm(opts.content, opts.title, {
    confirmButtonText: opts.positiveText || '确定',
    cancelButtonText: opts.negativeText || '取消',
    type: 'warning',
    closeOnClickModal: !!opts.onMaskClick
  }).then(() => { opts.onPositiveClick?.() }).catch(() => { opts.onNegativeClick?.() })
}

// naive 标签类型 -> element-plus 标签类型映射
const elTagType = (t) => ({ default: '', info: 'info', warning: 'warning', success: 'success', error: 'danger' })[t]

// Icons
const DeleteIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z' })
])
const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])
const DocumentIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 40, height: 40 }, [
  h('path', { d: 'M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V8l-6-6zM6 20V4h7v5h5v11H6z' })
])

// State
const uploadQueue = ref([])
const uploading = ref(false)
const uploadMessage = ref('')
const uploadMessageType = ref('info')
const urlFileInfo = ref({})
const rootDirOptions = ref([])
const isDragging = ref(false)
const dropZoneRef = ref(null)
const fileInputRef = ref(null)
const draggedFileCount = ref(0)
// 队列项 DOM ref 映射 (id -> element)
const queueItemRefs = {}
const queueListRef = ref(null)
let dragCounter = 0 // Track nested drag events
// 上传会话持久化：每个会话独立 key（按 uploadId），避免多页面同时上传时互相覆盖，
// 同时 localStorage 持久化保证浏览器重启后仍可自动恢复
const SESSION_KEY_PREFIX = 'fuzhan_upload_session_'

// 内联编辑文件名状态
const editingItemId = ref(null)
const renameValue = ref('')
const renameError = ref('')
const renameInputRef = ref(null)

// URL 上传进度状态
const urlUploadState = ref({
  fileName: '',
  fileSize: 0,
  progress: 0,
  status: '',      // '', 'uploading', 'completed', 'failed'
  displayText: '',
  error: '',
  taskId: null
})
let urlPollTimer = null

// 剪贴板预览状态
const clipboardPreview = ref({
  type: '',     // 'image', 'text', 'other'
  data: '',     // data URL for image, or text content
  text: '',
  filename: '',
  size: 0,
  file: null    // 实际 File 对象，用于上传
})
const clipboardRead = ref(false)
const clipboardReading = ref(false)
const clipboardFilename = ref('')
const clipboardOptions = ref([])          // 可选的类型列表 [{ value, label, rawType, blob, text, filename }]
const clipboardSelectedType = ref('')     // 用户选择的类型 value
const clipboardTypeConfirmed = ref(false) // 是否已确认选择类型

// 新建文本状态
const textFilename = ref('newfile.txt')
const textContent = ref('')

const getUrlUploadStatusTag = computed(() => {
  switch (urlUploadState.value.status) {
    case 'uploading': return 'info'
    case 'completed': return 'success'
    case 'failed': return 'error'
    default: return 'default'
  }
})
const getUrlUploadStatusText = computed(() => {
  switch (urlUploadState.value.status) {
    case 'uploading': return '下载中'
    case 'completed': return '已完成'
    case 'failed': return '失败'
    default: return ''
  }
})

const stopUrlPolling = () => {
  if (urlPollTimer) {
    clearInterval(urlPollTimer)
    urlPollTimer = null
  }
}

const MAX_POLL_RETRIES = 200 // 最大轮询次数（~5分钟）

const pollUrlTask = (taskId) => {
  stopUrlPolling()
  let pollCount = 0
  urlPollTimer = setInterval(async () => {
    pollCount++
    if (pollCount > MAX_POLL_RETRIES) {
      stopUrlPolling()
      urlUploadState.value.status = 'failed'
      urlUploadState.value.error = '下载超时'
      uploadMessage.value = '文件下载超时，请重试'
      uploadMessageType.value = 'error'
      return
    }

    try {
      const res = await UploadApi.getURLTask(taskId)
      if (!res.success) {
        stopUrlPolling()
        urlUploadState.value.status = 'failed'
        urlUploadState.value.error = '获取进度失败'
        return
      }
      const task = res.data.task
      const pct = task.fileSize > 0 ? Math.round(task.downloadedBytes / task.fileSize * 100) : 0
      urlUploadState.value.progress = pct

      if (task.status === 'completed') {
        stopUrlPolling()
        urlUploadState.value.status = 'completed'
        urlUploadState.value.displayText = '下载完成'
        uploadMessage.value = '文件上传成功'
        uploadMessageType.value = 'success'
        // URL 上传完成后提交备注和依赖
        submitUrlNotesAndDeps()
        emit('upload-success')
      } else if (task.status === 'failed') {
        stopUrlPolling()
        urlUploadState.value.status = 'failed'
        urlUploadState.value.error = task.errorMessage || '下载失败'
        uploadMessage.value = '文件上传失败'
        uploadMessageType.value = 'error'
      } else {
        urlUploadState.value.displayText = `下载中 ${pct}%`
      }
    } catch (e) {
      // 轮询失败不中断，继续尝试
      console.warn('轮询URL下载进度失败:', e)
    }
  }, 1500)
}

// 上传会话持久化：每个会话独立 key（按 uploadId），只写自己的 key，不清理其它会话，
// 避免多页面同时上传时互相覆盖或误删其它页签的会话
const saveUploadSessions = () => {
  const filtered = uploadQueue.value.filter(item => item.status === 'uploading' || item.status === 'pending' || item.status === 'paused')
  for (const item of filtered) {
    if (!item.uploadId) continue
    const session = {
      id: item.id,
      name: item.name,
      size: item.size,
      uploadId: item.uploadId,
      status: item.status,
      progress: item.progress,
      speed: item.speed,
      elapsed: item.elapsed,
      remaining: item.remaining,
      displayText: item.displayText,
      rootName: item.rootName || form.rootName,
      uploadDir: item.uploadDir || form.uploadDir
    }
    localStorage.setItem(SESSION_KEY_PREFIX + item.uploadId, JSON.stringify(session))
  }
}

const loadUploadSessions = () => {
  const sessions = []
  const keys = Object.keys(localStorage)
  for (const key of keys) {
    if (!key.startsWith(SESSION_KEY_PREFIX)) continue
    const saved = localStorage.getItem(key)
    if (!saved) continue
    try {
      const session = JSON.parse(saved)
      if (session && session.uploadId) sessions.push(session)
    } catch (e) {
      localStorage.removeItem(key)
    }
  }
  return sessions
}

const removeUploadSession = (uploadId) => {
  if (!uploadId) return
  localStorage.removeItem(SESSION_KEY_PREFIX + uploadId)
}

const clearUploadSessions = () => {
  const keys = Object.keys(localStorage)
  for (const key of keys) {
    if (key.startsWith(SESSION_KEY_PREFIX)) {
      localStorage.removeItem(key)
    }
  }
}

// 从路径中提取根目录名和相对路径
const extractRootAndPath = (path) => {
  if (!path || path === '/' || path === '\\') {
    return { rootName: '', dir: '/' }
  }

  // 标准化路径分隔符
  const normalizedPath = path.replace(/\\/g, '/')

  // 去掉前导斜杠，用于匹配根目录
  const pathWithoutSlash = normalizedPath.replace(/^\/+/, '')

  for (const root of rootDirOptions.value) {
    const rootName = root.value
    // 匹配根目录（如 _apps 或 _apps/xxx）
    if (pathWithoutSlash === rootName || pathWithoutSlash.startsWith(rootName + '/')) {
      const relativePath = pathWithoutSlash.slice(rootName.length)
      return {
        rootName: rootName,
        dir: relativePath || '/'
      }
    }
  }

  // 没有匹配到，返回第一个根目录
  if (rootDirOptions.value.length > 0) {
    return {
      rootName: rootDirOptions.value[0].value,
      dir: '/' + pathWithoutSlash
    }
  }

  return { rootName: '', dir: '/' }
}

// Load root directories on mount
const loadRootDirs = async () => {
  try {
    const data = await SetupApi.getStatus()
    if (data.success && data.data?.rootDirs) {
      rootDirOptions.value = data.data.rootDirs.map(dir => ({
        label: dir.name,
        value: dir.name
      }))

      if (!needsRootSelection.value) {
        // 不需要根目录选择时，自动设置为第一个可用根目录
        if (rootDirOptions.value.length > 0) {
          form.rootName = rootDirOptions.value[0].value
        }
        // 使用 defaultDir 设置上传目录（私有/临时文件子目录）
        if (props.defaultDir) {
          form.uploadDir = props.defaultDir
        }
      } else {
        // 根据当前浏览路径设置默认值
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

// 获取 API URL（支持函数或字符串）- 需在 restoreUploadSessions 之前定义
const getApiUrl = (key, ...args) => {
  const value = api.value[key]
  if (typeof value === 'function') {
    return value(...args)
  }
  return value
}

// 恢复未完成的上传会话
const restoreUploadSessions = async () => {
  const savedSessions = loadUploadSessions()

  if (savedSessions.length === 0) {
    return
  }

  // 等待 uploadApi 准备好
  if (!api.value || !api.value.createSession) {
    setTimeout(restoreUploadSessions, 500)
    return
  }

  for (const saved of savedSessions) {
    if (!saved.uploadId) continue

    // 检查服务器端会话是否还存在
    try {
      const getSessionUrl = getApiUrl('getSession', saved.uploadId)
      if (!getSessionUrl) {
        continue
      }
      const sessionStatusData = await request.get(getSessionUrl, { baseURL: '' })

      if (sessionStatusData.success && sessionStatusData.data?.session) {
        const session = sessionStatusData.data.session
        // 会话存在且未完成，添加到队列
        if (session.status !== 'completed' && session.status !== 'cancelled') {
          // 根据保存的状态恢复，paused 状态如果需要文件则改为 needFile
          let restoreStatus = saved.status
          if (restoreStatus === 'paused' && !saved.file) {
            restoreStatus = 'needFile'
          }
          const queueItem = {
            id: saved.id,
            name: saved.name,
            size: saved.size,
            uploadId: saved.uploadId,
            status: restoreStatus,
            progress: session.uploadedIndexes?.length
              ? Math.round((session.uploadedIndexes.length / session.totalChunks) * 100)
              : 0,
            error: '',
            speed: saved.speed || '',
            elapsed: saved.elapsed || '',
            remaining: saved.remaining || '',
            displayText: saved.displayText || '',
            rootName: saved.rootName,
            uploadDir: saved.uploadDir
          }
          uploadQueue.value.push(queueItem)
          const statusText = restoreStatus === 'needFile' ? '，请重新选择文件' : (restoreStatus === 'paused' ? '（已暂停）' : '')
          message.info(`已恢复上传会话: ${saved.name}${statusText}`)
        } else {
          // 会话已完成或已取消，清除本地记录
          removeUploadSession(saved.uploadId)
        }
      }
    } catch (e) {
      // 恢复失败时清除该会话的本地记录，用户需要重新开始
      console.error('恢复上传会话失败:', e)
      removeUploadSession(saved.uploadId)
    }
  }
}

const form = reactive({
  rootName: '',
  uploadDir: '',
  uploadMethod: 'local',
  url: '',
  filename: '',
  // URL 上传的备注和依赖
  urlNotes: '',
  urlDepFileName: '',
  urlDepRecordId: null,
  urlDepRelation: 'requires',
  urlDepDescription: ''
})

// 监听当前路径变化，更新上传目录
watch(() => store.state.currentPath, (newPath) => {
  const { rootName, dir } = extractRootAndPath(newPath)
  if (rootName) {
    form.rootName = rootName
    form.uploadDir = dir
  }
})

// 监听 api 就绪，触发恢复上传会话
watch(api, (newApi) => {
  if (newApi && newApi.createSession) {
    nextTick(() => {
      restoreUploadSessions()
    })
  }
}, { immediate: true })

// Computed
const canUrlUpload = computed(() => !!form.url && !!form.filename)

// 是否需要根目录选择（临时文件/私有存储不需要）
const needsRootSelection = computed(() => {
  const url = getApiUrl('createSession')
  return url && !url.includes('/temp/') && !url.includes('/private/')
})

// 能否开始上传：local 模式看队列，url 模式看 canUrlUpload
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

const pendingCount = computed(() =>
  uploadQueue.value.filter(item => item.status === 'pending').length
)

// 依赖关系选项
const depRelationOptions = [
  { label: '依赖 (requires)', value: 'requires' },
  { label: '被引用 (referenced_by)', value: 'referenced_by' },
  { label: '关联 (related)', value: 'related' }
]

// 每个队列项的自动完成选项缓存
const depAutocompleteCache = {}

// 搜索依赖文件（自动完成）
const handleDepSearch = async (item, value) => {
  if (!value || value.length < 1) {
    depAutocompleteCache[item.id] = []
    return
  }
  try {
    const res = await FileRecordApi.searchFiles(value)
    if (res.success && res.data.records) {
      depAutocompleteCache[item.id] = res.data.records.map(r => ({
        label: `${r.fileName} (${r.fullPath})`,
        value: String(r.id)
      }))
    }
  } catch (e) {
    // 静默失败
  }
}

// 获取自动完成选项
const getDepAutocompleteOptions = (item) => {
  return depAutocompleteCache[item.id] || []
}

// el-autocomplete fetch-suggestions（队列依赖）
const fetchDepSuggestions = async (item, query, cb) => {
  await handleDepSearch(item, query)
  cb((depAutocompleteCache[item.id] || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
}

// 选择依赖文件
const handleDepSelect = (item, opt) => {
  item.depFileRecordId = opt.id
  item.depFileName = opt.value.split(' (')[0]
}

// URL 上传的依赖文件自动完成
const urlDepAutocompleteOptions = ref([])
const handleUrlDepSearch = async (value) => {
  if (!value || value.length < 1) {
    urlDepAutocompleteOptions.value = []
    return
  }
  try {
    const res = await FileRecordApi.searchFiles(value)
    if (res.success && res.data.records) {
      urlDepAutocompleteOptions.value = res.data.records.map(r => ({
        label: `${r.fileName} (${r.fullPath})`,
        value: String(r.id)
      }))
    }
  } catch (e) {}
}
const fetchUrlDepSuggestions = async (query, cb) => {
  await handleUrlDepSearch(query)
  cb((urlDepAutocompleteOptions.value || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
}
const handleUrlDepSelect = (opt) => {
  form.urlDepRecordId = opt.id
  form.urlDepFileName = opt.value.split(' (')[0]
}

// 剪贴板/文本上传的备注和依赖（共用）
const extraNotes = ref('')
const extraDepFileName = ref('')
const extraDepRecordId = ref(null)
const extraDepRelation = ref('requires')
const extraDepDescription = ref('')
const extraDepAutocompleteOptions = ref([])
const handleExtraDepSearch = async (value) => {
  if (!value || value.length < 1) {
    extraDepAutocompleteOptions.value = []
    return
  }
  try {
    const res = await FileRecordApi.searchFiles(value)
    if (res.success && res.data.records) {
      extraDepAutocompleteOptions.value = res.data.records.map(r => ({
        label: `${r.fileName} (${r.fullPath})`,
        value: String(r.id)
      }))
    }
  } catch (e) {}
}
const fetchExtraDepSuggestions = async (query, cb) => {
  await handleExtraDepSearch(query)
  cb((extraDepAutocompleteOptions.value || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
}
const handleExtraDepSelect = (opt) => {
  extraDepRecordId.value = opt.id
  extraDepFileName.value = opt.value.split(' (')[0]
}

// 是否有活跃的本地文件上传
const hasActiveUploads = computed(() =>
  uploadQueue.value.some(item => item.status === 'uploading')
)

// 是否有正在进行的 URL 上传
const hasUrlUploading = computed(() =>
  urlUploadState.value.status === 'uploading'
)

// Queue item ID counter
let queueIdCounter = 0

// 计算 xxh3 哈希（与 Go 后端 zeebo/xxh3 一致）
const calculateXXH3 = async (buffer) => {
  return xxh3Hash(buffer)
}

// Methods
const formatFileSize = (bytes) => NumberUtils.formatFileSize(bytes)

const getStatusTagType = (status) => {
  switch (status) {
    case 'pending': return 'default'
    case 'uploading': return 'info'
    case 'paused': return 'warning'
    case 'needFile': return 'warning'
    case 'completed': return 'success'
    case 'failed': return 'error'
    default: return 'default'
  }
}

const getStatusText = (status) => {
  switch (status) {
    case 'pending': return '等待'
    case 'uploading': return '上传中'
    case 'paused': return '已暂停'
    case 'needFile': return '需选文件'
    case 'completed': return '完成'
    case 'failed': return '失败'
    default: return status
  }
}

const handleCustomUpload = async ({ file, onFinish, onError, onProgress }) => {
  // Add to queue
  const queueItem = {
    id: ++queueIdCounter,
    name: file.name,
    size: file.file?.size || file.size,
    status: 'pending',
    progress: 0,
    error: '',
    file: file.file,
    onFinish,
    onError,
    onProgress
  }
  uploadQueue.value.push(queueItem)

  // Start upload if not uploading
  processQueue()
}

const removeFromQueue = async (id) => {
  const index = uploadQueue.value.findIndex(item => item.id === id)
  if (index === -1) return

  const item = uploadQueue.value[index]

  // 如果正在上传，显示确认对话框
  if (item.status === 'uploading') {
    dialog.warning({
      title: '取消上传',
      content: `确定要取消上传 "${item.name}" 吗？`,
      positiveText: '确定取消',
      negativeText: '继续上传',
      onPositiveClick: async () => {
        // 标记为暂停状态，让上传循环检测到后停止
        item.status = 'paused'
        saveUploadSessions()

        // 等待上传循环停止后，从队列移除
        const checkRemoved = setInterval(() => {
          if (item.status !== 'uploading') {
            clearInterval(checkRemoved)
            // 调用取消接口
            if (item.uploadId) {
              const cancelUrl = getApiUrl('cancel', item.uploadId)
              if (cancelUrl) {
                request.delete(cancelUrl, { baseURL: '' }).catch((e) => {
        console.error('取消上传请求失败:', e)
      })
              }
            }
            const idx = uploadQueue.value.findIndex(i => i.id === id)
            if (idx !== -1) {
              uploadQueue.value.splice(idx, 1)
              saveUploadSessions()
              emit('upload-change', uploadQueue.value.length)
            }
          }
        }, 100)

        message.info(`${item.name} 已取消`)
      }
    })
    return
  }

  // 非上传状态，直接移除
  if (item.uploadId) {
    try {
      const cancelUrl = getApiUrl('cancel', item.uploadId)
      if (cancelUrl) {
        await request.delete(cancelUrl, { baseURL: '' })
      }
    } catch (e) {
      console.error('取消上传会话失败:', e)
    }
  }

  uploadQueue.value.splice(index, 1)
  saveUploadSessions()
  emit('upload-change', uploadQueue.value.length)
}

const retryUpload = (item) => {
  item.status = 'pending'
  item.error = ''
  item.progress = 0
  processQueue()
}

const pauseUpload = (item) => {
  item.status = 'paused'
  saveUploadSessions()
  message.info(`${item.name} 已暂停，可点击继续恢复上传`)
}

const resumeUpload = (item) => {
  if (!item.file) {
    item.status = 'needFile'
    message.warning('请先选择文件')
    return
  }
  item.status = 'pending'
  processQueue()
}

// 内联编辑文件名
const canEditFile = (item) => {
  return ['pending', 'paused', 'failed', 'needFile'].includes(item.status)
}

const startRename = (item) => {
  if (!canEditFile(item)) return
  editingItemId.value = item.id
  renameValue.value = item.name
  renameError.value = ''
  nextTick(() => {
    renameInputRef.value?.focus()
  })
}

const confirmRename = (item) => {
  if (editingItemId.value !== item.id) return

  const newName = renameValue.value.trim()

  // 输入为空时视为取消，使用原名
  if (!newName) {
    cancelRename()
    return
  }
  if (newName.includes('/') || newName.includes('\\')) {
    renameError.value = '文件名不能包含路径分隔符'
    return
  }

  item.name = newName
  editingItemId.value = null
  renameError.value = ''

  if (item.uploadId) {
    saveUploadSessions()
  }
}

const cancelRename = () => {
  editingItemId.value = null
  renameValue.value = ''
  renameError.value = ''
}

const selectFileForItem = (item) => {
  const input = document.createElement('input')
  input.type = 'file'
  input.onchange = (e) => {
    const file = e.target.files[0]
    if (file) {
      item.file = file
      item.name = file.name
      item.size = file.size
      item.status = 'pending'
      processQueue()
    }
  }
  input.click()
}

// 滚动到指定队列项
const scrollToQueueItem = (itemId) => {
  nextTick(() => {
    const el = queueItemRefs[itemId]
    if (el) {
      el.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  })
}

const processQueue = async () => {
  // Find next pending item
  const nextItem = uploadQueue.value.find(item => item.status === 'pending')
  if (!nextItem) {
    if (uploading.value) {
      uploading.value = false
      // 检查是否有正在上传或暂停的项目
      const hasActiveItems = uploadQueue.value.some(item =>
        item.status === 'uploading' || item.status === 'paused'
      )
      if (hasActiveItems) {
        // 有活跃项目，不清除 localStorage
        return
      }
      emit('upload-success')
      clearUploadSessions()
    }
    return
  }

  uploading.value = true
  nextItem.status = 'uploading'
  nextItem.progress = 0
  nextItem.error = ''
  scrollToQueueItem(nextItem.id)

  try {
    const result = await uploadSingleFile(nextItem)
    if (result === 'paused') {
      // 暂停状态，保持 paused
      return
    }
    nextItem.status = 'completed'
    nextItem.progress = 100
    removeUploadSession(nextItem.uploadId)
  } catch (e) {
    nextItem.status = 'failed'
    nextItem.error = formatErrorMessage(e, '上传失败')
    emit('upload-error', e)
    saveUploadSessions()
  }

  // Process next
  processQueue()
}

const uploadSingleFile = async (queueItem) => {
  const { file, name, size, uploadId: savedUploadId, rootName: itemRootName, uploadDir: itemUploadDir } = queueItem
  const createSessionUrl = getApiUrl('createSession')
  const isPrivate = createSessionUrl?.includes('/private/')

  // 如果有保存的 uploadId，说明是恢复的会话，跳过创建会话步骤
  if (savedUploadId) {
    queueItem.uploadId = savedUploadId
  }

  // Step 1: Create upload session (或跳过如果已存在)
  const uploadId = savedUploadId || await (async () => {
    const createSessionBody = isPrivate
      ? { filename: name, fileSize: size, dir: itemUploadDir || '' }
      : needsRootSelection.value
        ? { filename: name, fileSize: size, dir: itemUploadDir || '/', rootName: itemRootName || '_apps', targetType: 'local' }
        : { filename: name, fileSize: size, dir: itemUploadDir || '', deleteOnDownload: props.deleteOnDownload }

    const response = await request.post(createSessionUrl, createSessionBody, {
      baseURL: '',
      skipSuccessToast: true,
      headers: { 'Content-Type': 'application/json' }
    })

    if (!response.success) {
      throw new Error(response.message || '创建上传会话失败')
    }

    const { uploadId: newUploadId, totalChunks, chunkSize, overwriteRequired } = response.data
    if (!chunkSize) {
      throw new Error('服务器未返回分片大小配置')
    }

    if (overwriteRequired) {
      // admin 覆盖确认
      let confirmed = false
      await new Promise((resolve, reject) => {
        dialog.warning({
          title: '文件已存在',
          content: `文件 "${name}" 已存在，是否覆盖？`,
          positiveText: '覆盖',
          negativeText: '取消',
          onPositiveClick: () => {
            if (confirmed) return
            confirmed = true
            resolve()
          },
          onNegativeClick: () => {
            if (confirmed) return
            confirmed = true
            // 取消：删除已创建的会话
            const cancelUrl = getApiUrl('cancel', newUploadId)
            request.delete(cancelUrl, { baseURL: '' }).catch(() => {})
            reject(new Error('用户取消了上传'))
          },
          onMaskClick: () => {
            if (confirmed) return
            confirmed = true
            const cancelUrl = getApiUrl('cancel', newUploadId)
            request.delete(cancelUrl, { baseURL: '' }).catch(() => {})
            reject(new Error('用户取消了上传'))
          }
        })
      })
    }

    queueItem.uploadId = newUploadId
    queueItem.totalChunks = totalChunks
    queueItem.chunkSize = chunkSize
    saveUploadSessions()

    return newUploadId
  })()

  // 如果是恢复的会话，需要从服务端获取 chunkSize
  if (!queueItem.chunkSize) {
    const getSessionUrl = getApiUrl('getSession', uploadId)
    const sessionStatusData = await request.get(getSessionUrl, { baseURL: '' })

    if (!sessionStatusData.success) {
      throw new Error(sessionStatusData.message || '获取上传状态失败')
    }

    const session = sessionStatusData.data.session
    if (!session.chunkSize) {
      throw new Error('服务器未返回分片大小配置')
    }

    queueItem.chunkSize = session.chunkSize
    queueItem.totalChunks = session.totalChunks
  }

  const CHUNK_SIZE = queueItem.chunkSize
  const { totalChunks } = queueItem

  // Progress tracking
  const uploadData = []
  let lastUploadTime = Date.now()
  let lastUploadedBytes = 0
  const uploadStartTime = Date.now()
  const updateProgress = (uploadedBytes) => {
    const currentTime = Date.now()
    uploadData.push({ time: currentTime, size: uploadedBytes })
    // Keep last 5 seconds of data
    const fiveSecondsAgo = currentTime - 5000
    while (uploadData.length > 0 && uploadData[0].time < fiveSecondsAgo) {
      uploadData.shift()
    }

    let uploadSpeed = 0
    if (uploadData.length >= 2) {
      const first = uploadData[0]
      const last = uploadData[uploadData.length - 1]
      uploadSpeed = (last.size - first.size) / ((last.time - first.time) / 1000)
    } else {
      const elapsed = currentTime - lastUploadTime
      if (elapsed > 0) {
        uploadSpeed = (uploadedBytes - lastUploadedBytes) / (elapsed / 1000)
      }
    }

    lastUploadTime = currentTime
    lastUploadedBytes = uploadedBytes

    const percentage = ((uploadedBytes / size) * 100).toFixed(1)
    const uploadedSize = NumberUtils.formatFileSize(uploadedBytes)
    const totalSize = NumberUtils.formatFileSize(size)
    const speedStr = NumberUtils.formatFileSize(uploadSpeed) + '/s'

    // 计算耗时（基于上传开始时间）
    const elapsedSeconds = (currentTime - uploadStartTime) / 1000
    let elapsedStr = ''
    if (elapsedSeconds > 3600) {
      elapsedStr = (elapsedSeconds / 3600).toFixed(1) + ' 时'
    } else if (elapsedSeconds > 60) {
      elapsedStr = (elapsedSeconds / 60).toFixed(1) + ' 分'
    } else {
      elapsedStr = elapsedSeconds.toFixed(1) + ' 秒'
    }

    // 计算剩余时间
    let remainingStr = '-'
    if (uploadSpeed > 0 && uploadedBytes < size) {
      const remainingBytes = size - uploadedBytes
      const remainingTime = remainingBytes / uploadSpeed
      if (remainingTime > 3600) {
        remainingStr = (remainingTime / 3600).toFixed(1) + ' 时'
      } else if (remainingTime > 60) {
        remainingStr = (remainingTime / 60).toFixed(1) + ' 分'
      } else {
        remainingStr = remainingTime.toFixed(1) + ' 秒'
      }
    }

    queueItem.progress = Math.round(uploadedBytes / size * 100)
    queueItem.speed = speedStr
    queueItem.remaining = remainingStr
    queueItem.elapsed = elapsedStr
    queueItem.displayText = `${uploadedSize}/${totalSize} (${percentage}%) ${speedStr} | 已耗时 ${elapsedStr} | 剩余 ${remainingStr}`
  }

  // Step 2: Get uploaded chunks and upload remaining
  // Fetch existing uploaded chunks from server for resume support
  let uploadedIndexes = new Set()
  let uploadedBytes = 0

  const getSessionUrl = getApiUrl('getSession', uploadId)
  const sessionStatusData = await request.get(getSessionUrl, { baseURL: '' })

  if (sessionStatusData.success && sessionStatusData.data?.session) {
    const session = sessionStatusData.data.session
    const serverChunkSize = session.chunkSize
    if (!serverChunkSize) {
      throw new Error('服务器未返回分片大小配置')
    }
    if (serverChunkSize !== CHUNK_SIZE) {
      throw new Error(`分片大小不匹配：服务器配置已更改，请重新开始上传`)
    }
    if (session.uploadedIndexes) {
      session.uploadedIndexes.forEach(i => uploadedIndexes.add(i))
      uploadedBytes = uploadedIndexes.size * CHUNK_SIZE
      if (uploadedBytes > size) uploadedBytes = size
      updateProgress(uploadedBytes)
    }
  } else if (!sessionStatusData.success) {
    // 会话不存在或已过期，清除该会话的本地记录并创建新会话
    removeUploadSession(queueItem.uploadId)
    delete queueItem.uploadId
    throw new Error(sessionStatusData.message || '上传会话已失效，请重新开始上传')
  }

  const uploadChunk = async (index) => {
    const start = index * CHUNK_SIZE
    const end = Math.min(start + CHUNK_SIZE, size)
    const chunk = file.slice(start, end)

    // 计算分片 xxh3 校验值
    const chunkBuffer = await chunk.arrayBuffer()
    const checksum = await calculateXXH3(chunkBuffer)

    const formData = new FormData()
    formData.append('uploadId', String(uploadId))
    formData.append('chunkIndex', String(index))
    formData.append('chunk', chunk)
    formData.append('checksum', checksum)

    const data = await request.post(getApiUrl('uploadChunk'), formData, { baseURL: '', skipSuccessToast: true })
    if (!data.success) {
      throw new Error(data.message || `分片 ${index} 上传失败`)
    }

    uploadedIndexes.add(index)
    uploadedBytes = Math.min((index + 1) * CHUNK_SIZE, size)
    updateProgress(uploadedBytes)
  }

  // 串行上传所有分片
  // 上传所有分片，返回是否暂停
  let paused = false
  for (let i = 0; i < totalChunks; i++) {
    if (uploadedIndexes.has(i)) continue
    if (queueItem.status === 'paused') {
      paused = true
      saveUploadSessions()
      break
    }
    await uploadChunk(i)
  }

  if (paused) {
    return 'paused' // 退出，不继续 finalize
  }

  // Step 3: Finalize
  const finalizeUrl = getApiUrl('finalize')
  const finalizeData = await request.post(finalizeUrl, { uploadId }, {
    baseURL: '',
    skipSuccessToast: true,
    headers: { 'Content-Type': 'application/json' }
  })

  if (!finalizeData.success) {
    throw new Error(finalizeData.message || '完成上传失败')
  }

  // 上传成功后提交备注和依赖
  await submitNotesAndDeps(queueItem, name, itemRootName, itemUploadDir)

  return finalizeData.data
}

// 上传成功后提交备注和依赖
const submitNotesAndDeps = async (queueItem, fileName, rootName, uploadDir) => {
  if (!queueItem.notes && !queueItem.depFileRecordId) return

  try {
    // 等一会儿让索引完成
    await new Promise(r => setTimeout(r, 2000))
    // 构建文件路径
    const dir = uploadDir && uploadDir !== '/' ? uploadDir.replace(/^\/+/, '') : ''
    const filePath = dir ? '/' + dir + '/' + fileName : '/' + fileName

    // 查找记录
    const res = await FileRecordApi.findRecord(fileName, rootName, filePath)
    if (!res.success || !res.data.record) {
      // 再试一次（可能索引稍有延迟）
      await new Promise(r => setTimeout(r, 3000))
      const retry = await FileRecordApi.findRecord(fileName, rootName, filePath)
      if (!retry.success || !retry.data.record) {
        console.warn('未找到文件记录，无法设置备注/依赖:', fileName)
        return
      }
      const record = retry.data.record
      if (queueItem.notes) {
        await FileRecordApi.updateNotes(record.id, queueItem.notes)
      }
      if (queueItem.depFileRecordId) {
        await DependencyApi.createPublic(record.id, queueItem.depFileRecordId, queueItem.depRelation, queueItem.depDescription)
      }
      return
    }
    const record = res.data.record
    if (queueItem.notes) {
      await FileRecordApi.updateNotes(record.id, queueItem.notes)
    }
    if (queueItem.depFileRecordId) {
      await DependencyApi.createPublic(record.id, queueItem.depFileRecordId, queueItem.depRelation, queueItem.depDescription)
    }
  } catch (e) {
    console.warn('提交备注/依赖失败:', e)
  }
}

// URL 上传完成后提交备注和依赖
const submitUrlNotesAndDeps = () => {
  const notes = form.urlNotes
  const depFileRecordId = form.urlDepRecordId
  const depRelation = form.urlDepRelation || 'requires'
  const depDescription = form.urlDepDescription || ''
  if (!notes && !depFileRecordId) return

  // URL 上传后，文件路径包含文件名
  const fileName = form.filename
  if (!fileName) return

  // 查找文件记录并提交
  setTimeout(async () => {
    try {
      const dir = form.uploadDir && form.uploadDir !== '/' ? form.uploadDir.replace(/^\/+/, '') : ''
      const filePath = dir ? '/' + dir + '/' + fileName : '/' + fileName
      const rootName = form.rootName
      if (!rootName) return

      const res = await FileRecordApi.findRecord(fileName, rootName, filePath)
      if (!res.success || !res.data.record) {
        await new Promise(r => setTimeout(r, 3000))
        const retry = await FileRecordApi.findRecord(fileName, rootName, filePath)
        if (!retry.success || !retry.data.record) {
          console.warn('未找到文件记录，无法设置 URL 备注/依赖:', fileName)
          return
        }
        const record = retry.data.record
        if (notes) await FileRecordApi.updateNotes(record.id, notes)
        if (depFileRecordId) await DependencyApi.createPublic(record.id, depFileRecordId, depRelation, depDescription)
        return
      }
      const record = res.data.record
      if (notes) await FileRecordApi.updateNotes(record.id, notes)
      if (depFileRecordId) await DependencyApi.createPublic(record.id, depFileRecordId, depRelation, depDescription)
    } catch (e) {
      console.warn('提交 URL 备注/依赖失败:', e)
    }
  }, 2000)
}

// 新建文本文件保存 → 构建 File 对象 → 走本地上传流程
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
  // 清空编辑器
  textFilename.value = 'newfile.txt'
  textContent.value = ''
  extraNotes.value = ''
  extraDepFileName.value = ''
  extraDepRecordId.value = null
  extraDepRelation.value = 'requires'
  extraDepDescription.value = ''
  // 切换到 local 模式并开始上传
  form.uploadMethod = 'local'
  uploadMessage.value = ''
  startUpload()
}

// 读取剪贴板（发现所有可用类型）
const readClipboard = async () => {
  clipboardReading.value = true
  uploadMessage.value = '正在读取剪贴板...'
  uploadMessageType.value = 'info'
  try {
    const items = await navigator.clipboard.read()
    const options = []
    for (const item of items) {
      for (const type of item.types) {
        if (type.startsWith('image/')) {
          const blob = await item.getType(type)
          const ext = type.split('/')[1]
          const dataUrl = await blobToDataURL(blob)
          const filename = `clipboard-${Date.now()}.${ext}`
          options.push({
            value: `image-${type}`,
            label: `图片 (${ext.toUpperCase()})`,
            rawType: type,
            blob,
            text: '',
            dataUrl,
            previewType: 'image',
            filename,
            file: new File([blob], filename, { type })
          })
        } else if (type === 'text/plain') {
          const blob = await item.getType(type)
          const text = await blob.text()
          if (text.trim()) {
            const filename = `clipboard-${Date.now()}.txt`
            options.push({
              value: 'text-plain',
              label: `纯文本 (TXT)`,
              rawType: type,
              blob,
              text,
              dataUrl: '',
              previewType: 'text',
              filename,
              file: new File([text], filename, { type: 'text/plain' })
            })
          }
        } else if (type === 'text/html') {
          const blob = await item.getType(type)
          const html = await blob.text()
          const plainText = html.replace(/<[^>]*>/g, '').trim()
          if (plainText) {
            const filename = `clipboard-${Date.now()}.txt`
            options.push({
              value: 'text-html',
              label: `格式化文本 (HTML)`,
              rawType: type,
              blob,
              text: plainText,
              dataUrl: '',
              previewType: 'text',
              filename,
              file: new File([plainText], filename, { type: 'text/plain' })
            })
          }
        } else if (type.startsWith('text/') || type.startsWith('application/')) {
          const blob = await item.getType(type)
          const filename = `clipboard-${Date.now()}`
          options.push({
            value: `other-${type}`,
            label: `其它 (${type.split('/').pop()})`,
            rawType: type,
            blob,
            text: '',
            dataUrl: '',
            previewType: 'other',
            filename,
            file: new File([blob], filename, { type })
          })
        }
      }
    }
    if (options.length === 0) {
      message.warning('剪贴板中没有可读取的内容')
      uploadMessage.value = ''
      clipboardReading.value = false
      return
    }
    clipboardOptions.value = options
    // 如果只有一种类型，直接选中
    if (options.length === 1) {
      clipboardSelectedType.value = options[0].value
      applyClipboardSelection()
    } else {
      clipboardSelectedType.value = options[0].value
      clipboardRead.value = true
      clipboardTypeConfirmed.value = false
      uploadMessage.value = ''
      message.info(`检测到 ${options.length} 种格式，请选择要读取的类型`)
    }
  } catch (e) {
    message.warning('无法读取剪贴板，请尝试 Ctrl+V 粘贴')
    uploadMessage.value = ''
  } finally {
    clipboardReading.value = false
  }
}

// 应用用户选择的剪贴板类型
const applyClipboardSelection = () => {
  const opt = clipboardOptions.value.find(o => o.value === clipboardSelectedType.value)
  if (!opt) return
  clipboardPreview.value = {
    type: opt.previewType,
    data: opt.dataUrl,
    text: opt.text,
    filename: opt.filename,
    size: opt.blob.size,
    file: opt.file
  }
  clipboardFilename.value = opt.filename
  clipboardRead.value = true
  clipboardTypeConfirmed.value = true
  uploadMessage.value = ''
  message.success('已选择剪贴板数据类型，确认后点击"开始上传"')
}

// Blob 转 Data URL
const blobToDataURL = (blob) => {
  return new Promise((resolve) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result)
    reader.readAsDataURL(blob)
  })
}

// 清除剪贴板预览
const clearClipboard = () => {
  clipboardPreview.value = { type: '', data: '', text: '', filename: '', size: 0, file: null }
  clipboardRead.value = false
  clipboardFilename.value = ''
  clipboardOptions.value = []
  clipboardSelectedType.value = ''
  clipboardTypeConfirmed.value = false
  uploadMessage.value = ''
}

// 上传剪贴板内容
const uploadClipboard = async () => {
  if (!clipboardRead.value || !clipboardPreview.value.file) {
    message.warning('请先读取剪贴板')
    return
  }
  const file = clipboardPreview.value.file
  // 使用用户自定义的文件名
  const finalName = clipboardFilename.value.trim() || file.name
  addFileToQueue({
    name: finalName,
    size: file.size,
    file: new File([file], finalName, { type: file.type }),
    notes: extraNotes.value,
    depFileName: extraDepFileName.value,
    depFileRecordId: extraDepRecordId.value,
    depRelation: extraDepRelation.value,
    depDescription: extraDepDescription.value
  })
  message.success(`已添加文件: ${finalName}`)
  // 切换到 local 模式并开始上传
  form.uploadMethod = 'local'
  uploadMessage.value = ''
  startUpload()
}

const urlInputTimer = ref(null)

const handleUrlInput = () => {
  if (form.url) {
    clearTimeout(urlInputTimer.value)
    urlInputTimer.value = setTimeout(() => {
      fetchUrlFileInfo()
    }, 500)
  } else {
    urlFileInfo.value = {}
    form.filename = ''
  }
}

const fetchUrlFileInfo = async () => {
  if (!form.url) return

  uploadMessage.value = '正在获取文件信息...'
  uploadMessageType.value = 'info'

  try {
    const response = await UploadApi.getUrlInfo(form.url)
    if (response.success) {
      const filename = response.data.filename || 'downloaded_file'
      form.filename = filename
      urlFileInfo.value = {
        name: filename,
        size: response.data.size ? NumberUtils.formatFileSize(response.data.size) : '未知'
      }
      uploadMessage.value = ''
    } else {
      urlFileInfo.value = {}
      uploadMessage.value = '获取文件信息失败: ' + response.message
      uploadMessageType.value = 'error'
    }
  } catch (e) {
    urlFileInfo.value = {}
    uploadMessage.value = formatErrorMessage(e, '获取文件信息失败')
    uploadMessageType.value = 'error'
  }
}

const handleUrlUpload = async () => {
  if (!form.url || !form.filename) return

  uploading.value = true
  uploadMessage.value = '正在准备下载...'
  uploadMessageType.value = 'info'

  try {
    let data
    if (needsRootSelection.value) {
      data = await UploadApi.uploadFromUrlLocal(form.url, form.filename, form.uploadDir || '/', form.rootName || '_apps')
    } else {
      // 根据 API 路径判断存储类型
      const createUrl = getApiUrl('createSession')
      let storageType = ''
      if (createUrl?.includes('/temp/')) {
        storageType = 'temp'
      } else if (createUrl?.includes('/private/')) {
        storageType = 'private'
      }
      data = await UploadApi.uploadFromUrl(form.url, form.filename, form.uploadDir || '', storageType)
    }

    if (data.success && data.data && data.data.taskId) {
      // 异步下载，开始轮询进度
      store.setActiveUrlTasks(true) // 激活通知轮询
      const taskData = data.data
      urlUploadState.value = {
        fileName: taskData.fileName || form.filename,
        fileSize: taskData.fileSize || 0,
        progress: 0,
        status: 'uploading',
        displayText: '正在下载...',
        error: '',
        taskId: taskData.taskId
      }
      pollUrlTask(taskData.taskId)
    } else {
      uploadMessage.value = data.message || '上传失败'
      uploadMessageType.value = 'error'
    }
  } catch (e) {
    uploadMessage.value = formatErrorMessage(e, '上传失败')
    uploadMessageType.value = 'error'
  } finally {
    uploading.value = false
  }
}

// Drag and drop handlers
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

  // Add files to queue
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
  // Reset input
  e.target.value = ''
}

const addFileToQueue = (fileData) => {
  const queueItem = {
    id: ++queueIdCounter,
    name: fileData.name,
    size: fileData.size,
    status: 'pending',
    progress: 0,
    error: '',
    file: fileData.file,
    rootName: form.rootName,
    uploadDir: form.uploadDir,
    // 备注和依赖
    notes: fileData.notes || '',
    depFileName: fileData.depFileName || '',
    depFileRecordId: fileData.depFileRecordId || null,
    depRelation: fileData.depRelation || 'requires',
    depDescription: fileData.depDescription || '',
    showNotes: false,
    showDeps: false,
    uploadDirPath: form.uploadDir
  }
  uploadQueue.value.push(queueItem)
  emit('upload-change', uploadQueue.value.length)
  // 新添加的文件滚动到可视区
  scrollToQueueItem(queueItem.id)
}

// Start upload
const startUpload = () => {
  if (uploadQueue.value.length === 0) return
  emit('upload-start')
  processQueue()
}

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
</style>