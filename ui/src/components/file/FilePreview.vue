<template>
  <div :style="{ minHeight: '400px', maxHeight: maximized ? 'none' : '600px', overflow: 'hidden', display: 'flex', flexDirection: 'column', flex: maximized ? 1 : 'none' }">
    <!-- 分段导航 -->
    <div v-if="chunked && totalChunks > 1" class="chunk-nav">
      <el-button-group size="small">
        <el-button :disabled="currentChunk <= 0" @click="loadChunk(currentChunk - 1)">
          <el-icon><component :is="ChevronLeftIcon" /></el-icon> 上一页
        </el-button>
        <el-button disabled>
          第 {{ currentChunk + 1 }} / {{ totalChunks }} 段
        </el-button>
        <el-button :disabled="currentChunk >= totalChunks - 1" @click="loadChunk(currentChunk + 1)">
          下一页 <el-icon><component :is="ChevronRightIcon" /></el-icon>
        </el-button>
      </el-button-group>
      <span class="chunk-info">
        位置: {{ formatOffset(startOffset) }} - {{ formatOffset(endOffset) }} / {{ formatSize(totalSize) }}
      </span>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading" style="display: flex; justify-content: center; align-items: center; height: 300px;">
      <el-icon class="is-loading" :size="24"><Loading /></el-icon>
      <span v-if="chunked" style="margin-left: 12px;">加载分段 {{ currentChunk + 1 }}...</span>
    </div>

    <!-- Markdown 文件预览（渲染结果） -->
    <div v-else-if="isMarkdown && textContent" class="markdown-preview" :style="markdownContainerStyle">
      <div v-html="renderedMarkdown"></div>
    </div>

    <!-- 文本文件预览 -->
    <div v-else-if="fileType === 'text'" :style="textContainerStyle">
      <el-input
        type="textarea"
        v-model="textContent"
        readonly
        :autosize="maximized ? undefined : { minRows: 20, maxRows: 30 }"
        :style="inputStyleComputed"
      />
    </div>

    <!-- 图片文件预览 -->
    <div v-else-if="fileType === 'image'" style="display: flex; justify-content: center; align-items: center; height: 100%;">
      <img
        :src="imageUrl"
        :alt="fileName"
        :style="{ maxWidth: '100%', maxHeight: maximized ? 'calc(100vh - 200px)' : '500px', objectFit: 'contain' }"
      />
    </div>

    <!-- PDF文件预览 -->
    <div v-else-if="fileType === 'pdf'" style="height: 100%;">
      <iframe
        :src="pdfUrl"
        style="width: 100%; height: 500px;"
        frameborder="0"
      />
    </div>

    <!-- 不支持预览的文件 -->
    <div v-else style="display: flex; flex-direction: column; justify-content: center; align-items: center; height: 300px;">
      <el-icon :size="64" style="color: var(--el-text-color-placeholder)">
        <component :is="FileIcon" />
      </el-icon>
      <p style="font-size: 16px; color: var(--el-text-color-regular); margin: 20px 0;">{{ errorMessage || '该文件类型不支持预览' }}</p>
      <el-button type="primary" @click="downloadFile">下载文件</el-button>
    </div>

  </div>
</template>

<script setup>
import { ref, computed, watch, h } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import { FileApi } from '@/api'
import { PathUtils } from '@/utils'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

const props = defineProps({
  file: { type: Object, default: () => ({}) },
  maximized: { type: Boolean, default: false }
})

const emit = defineEmits(['close'])

const loading = ref(false)
const textContent = ref('')
const imageUrl = ref('')
const pdfUrl = ref('')
const fileType = ref('unsupported')
const errorMessage = ref('')

// 分段加载状态
const chunked = ref(false)
const currentChunk = ref(0)
const totalChunks = ref(0)
const startOffset = ref(0)
const endOffset = ref(0)
const totalSize = ref(0)

// Icons
const FileIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z' })
])
const ChevronLeftIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M15.41 7.41L14 6l-6 6 6 6 1.41-1.41L10.83 12z' })
])
const ChevronRightIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M10 6L8.59 7.41 13.17 12l-4.58 4.59L10 18l6-6z' })
])

const fileName = computed(() => props.file.name || '')

const textContainerStyle = computed(() => {
  if (props.maximized) {
    return { flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column', minHeight: 0 }
  }
  return { flex: 1, overflow: 'hidden' }
})

const markdownContainerStyle = computed(() => {
  if (props.maximized) {
    return { flex: 1, overflow: 'auto' }
  }
  return { maxHeight: '70vh', overflow: 'auto' }
})

const inputStyleComputed = computed(() => {
  const base = { fontFamily: 'Courier New, monospace' }
  if (props.maximized) {
    return { ...base, flex: 1 }
  }
  return { ...base, height: '100%' }
})

// Markdown 渲染
const isMarkdown = computed(() => {
  const ext = props.file?.name?.toLowerCase() || ''
  return ext.endsWith('.md') || ext.endsWith('.markdown')
})

const renderedMarkdown = computed(() => {
  if (!textContent.value) return ''
  return DOMPurify.sanitize(marked.parse(textContent.value))
})

// 判断文件类型（MIME 类型判断）
const detectFileType = (mimeType) => {
  if (!mimeType) return 'unsupported'
  if (mimeType.startsWith('text/')) return 'text'
  if (mimeType.startsWith('image/')) return 'image'
  if (mimeType === 'application/json' || mimeType === 'application/javascript' || mimeType === 'application/xml') return 'text'
  if (mimeType === 'application/pdf') return 'pdf'
  return 'unsupported'
}

// 格式化文件大小
const formatSize = (bytes) => {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  while (bytes >= 1024 && i < units.length - 1) {
    bytes /= 1024
    i++
  }
  return `${bytes.toFixed(1)} ${units[i]}`
}

// 格式化偏移量
const formatOffset = (offset) => {
  return formatSize(offset)
}

// 加载预览内容
const loadPreviewContent = async () => {
  if (!props.file?.path || typeof props.file.path !== 'string') {
    fileType.value = 'unsupported'
    errorMessage.value = '文件路径无效'
    return
  }

  loading.value = true
  errorMessage.value = ''
  textContent.value = ''
  chunked.value = false

  try {
    const fullPath = props.file.path
    const result = await FileApi.preview(fullPath)

    if (!result.success) {
      errorMessage.value = result.message || '加载预览失败'
      fileType.value = 'unsupported'
      return
    }

    const data = result.data || {}
    fileType.value = detectFileType(data.mimeType)

    if (data.previewable) {
      if (data.content) {
        // 小文件，直接显示内容
        textContent.value = data.content
        chunked.value = false
      } else if (data.chunked) {
        // 大文件，需要分段加载
        chunked.value = true
        totalChunks.value = data.totalChunks || 1
        currentChunk.value = 0
        totalSize.value = data.size || 0
        // 自动加载第一段
        await loadChunk(0)
      } else if (data.fileUrl) {
        // 图片/PDF
        imageUrl.value = data.fileUrl
        pdfUrl.value = data.fileUrl
      }
    } else {
      errorMessage.value = '该文件类型不支持预览'
    }
  } catch (e) {
    console.error('预览加载失败:', e)
    errorMessage.value = '加载预览失败'
    fileType.value = 'unsupported'
  } finally {
    loading.value = false
  }
}

// 加载指定分段
const loadChunk = async (chunkIndex) => {
  if (chunkIndex < 0 || chunkIndex >= totalChunks.value) return

  loading.value = true
  currentChunk.value = chunkIndex

  try {
    const fullPath = props.file.path
    const result = await FileApi.previewChunk(fullPath, chunkIndex)

    if (result.success && result.data) {
      const data = result.data
      if (data.content !== undefined && data.content !== null) {
        textContent.value = data.content || ''
      } else {
        errorMessage.value = '分段内容为空，请重试'
      }
      startOffset.value = data.startOffset || 0
      endOffset.value = data.endOffset || 0
    } else {
      errorMessage.value = result.message || '加载分段失败'
    }
  } catch (e) {
    console.error('分段加载失败:', e)
    errorMessage.value = '网络错误，请检查连接后重试'
  } finally {
    loading.value = false
  }
}

const downloadFile = () => {
  if (props.file.path) {
    const fullPath = props.file.path
    window.open(`/download/${PathUtils.encodeFilePath(fullPath)}`, '_blank')
  }
}

watch(() => props.file, () => {
  loadPreviewContent()
}, { immediate: true, deep: true })
</script>

<style lang="less">
.file-preview {
  .chunk-nav {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: var(--el-fill-color-light);
    border-bottom: 1px solid var(--el-border-color-light);
    margin-bottom: 8px;

    .chunk-info {
      font-size: 13px;
      color: var(--el-text-color-regular);
    }
  }
}
.preview-maximized .el-textarea {
  flex: 1;
  min-height: 0;
  height: 100%;

  .el-textarea__inner {
    height: 100% !important;
    min-height: 0;
  }
}

</style>