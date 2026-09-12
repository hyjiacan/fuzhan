<template>
  <div v-if="items.length > 0" class="upload-queue">
    <div class="queue-header">
      <span>上传队列 ({{ items.length }} 个)</span>
      <span style="color: #999;">等待 {{ pendingCount }} 个</span>
    </div>
    <div class="queue-list" style="max-height: 160px; overflow-y: auto;">
      <template v-for="item in items" :key="item.id">
        <div
          :ref="el => { if (el) queueItemRefs[item.id] = el }"
          class="queue-item"
        >
          <div class="queue-item-info">
            <template v-if="editingId === item.id">
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
              @click="handlers.selectFileForItem(item)"
            >
              选择文件
            </el-button>
            <el-button
              v-if="item.status === 'uploading'"
              type="info"
              size="small"
              @click="handlers.pauseUpload(item)"
            >
              暂停
            </el-button>
            <el-button
              v-if="item.status === 'paused'"
              type="success"
              size="small"
              @click="handlers.resumeUpload(item)"
            >
              继续
            </el-button>
            <el-button
              v-if="item.status === 'failed'"
              type="warning"
              size="small"
              @click="handlers.retryUpload(item)"
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
              @click="handlers.removeFromQueue(item.id)"
            >
              <el-icon><component :is="DeleteIcon" /></el-icon>
            </el-button>
          </div>
        </div>
        <!-- 备注区域 -->
        <div v-if="item.showNotes" class="queue-item-extra" :style="{ padding: '0 12px 8px' }">
          <el-input
            v-model="item.notes"
            :maxlength="4096"
            type="textarea"
            placeholder="输入文件备注..."
            :rows="2"
            size="small"
          />
        </div>
        <!-- 依赖区域 -->
        <div v-if="item.showDeps" class="queue-item-extra" :style="{ padding: '0 12px 8px' }">
          <div :style="{ display: 'flex', gap: '6px' }">
            <el-autocomplete
              v-model="item.depFileName"
              :maxlength="255"
              :fetch-suggestions="(q, cb) => handlers.fetchDepSuggestions(item, q, cb)"
              placeholder="搜索并选择依赖文件"
              size="small"
              clearable
              :teleported="true"
              style="flex: 1"
              @select="(opt) => handlers.handleDepSelect(item, opt)"
            />
            <el-select
              v-model="item.depRelation"
              size="small"
              placeholder="依赖关系"
              :teleported="true"
              style="width: 150px"
            >
              <el-option v-for="opt in depRelationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
            </el-select>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, nextTick } from 'vue'
import { NumberUtils } from '@/utils'
import { elTagType, getStatusText, getStatusTagType, DEP_RELATION_OPTIONS } from './constants'
import { DeleteIcon } from './icons'

const props = defineProps({
  items: {
    type: Array,
    required: true
  },
  pendingCount: {
    type: Number,
    default: 0
  },
  // 队列项 DOM ref 映射（id -> element），由父级持有以便滚动定位
  queueItemRefs: {
    type: Object,
    default: () => ({})
  },
  handlers: {
    type: Object,
    required: true
  }
})
const { items, pendingCount, queueItemRefs, handlers } = props

// 内联编辑文件名状态（本组件内聚）
const editingId = ref(null)
const renameValue = ref('')
const renameError = ref('')
const renameInputRef = ref(null)

const canEditFile = (item) => ['pending', 'paused', 'failed', 'needFile'].includes(item.status)

const startRename = (item) => {
  if (!canEditFile(item)) return
  editingId.value = item.id
  renameValue.value = item.name
  renameError.value = ''
  nextTick(() => {
    renameInputRef.value?.focus()
  })
}

const confirmRename = (item) => {
  if (editingId.value !== item.id) return
  const newName = renameValue.value.trim()
  if (!newName) {
    cancelRename()
    return
  }
  if (newName.includes('/') || newName.includes('\\')) {
    renameError.value = '文件名不能包含路径分隔符'
    return
  }
  item.name = newName
  editingId.value = null
  renameError.value = ''
  if (item.uploadId) {
    handlers.saveUploadSessions?.()
  }
}

const cancelRename = () => {
  editingId.value = null
  renameValue.value = ''
  renameError.value = ''
}

const formatFileSize = (bytes) => NumberUtils.formatFileSize(bytes)

const depRelationOptions = DEP_RELATION_OPTIONS
</script>