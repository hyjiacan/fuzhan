<template>
  <div class="recent-view">
    <div class="recent-cards">
      <!-- 最近搜索 -->
      <n-card title="最近搜索" class="recent-card">
        <div v-if="recentKeywords.length > 0" class="keyword-tags">
          <n-tag v-for="kw in recentKeywords" :key="kw.word" style="cursor: pointer" @click="searchKeyword(kw.word)">
            {{ kw.word }}
          </n-tag>
        </div>
        <n-empty v-else description="暂无搜索记录" />
      </n-card>

      <!-- 最近上传 -->
      <n-card title="最近上传" class="recent-card">
        <n-data-table :columns="columns" :data="uploads" :loading="loading"
          :pagination="false" :row-key="row => row.id" striped />
        <template v-if="uploadTotal > uploads.length" #footer>
          <div class="card-footer">
            <n-pagination size="small" :page="uploadPage" :page-size="uploadPageSize" :item-count="uploadTotal"
              :page-sizes="[5, 10, 20]" show-size-picker @update:page="onUploadPageChange"
              @update:page-size="onUploadPageSizeChange" />
          </div>
        </template>
      </n-card>

      <!-- 最近下载 -->
      <n-card title="最近下载" class="recent-card">
        <n-data-table :columns="columns" :data="downloads" :loading="loading"
          :pagination="false" :row-key="row => row.id" striped />
        <template v-if="downloadTotal > downloads.length" #footer>
          <div class="card-footer">
            <n-pagination size="small" :page="downloadPage" :page-size="downloadPageSize" :item-count="downloadTotal"
              :page-sizes="[5, 10, 20]" show-size-picker @update:page="onDownloadPageChange"
              @update:page-size="onDownloadPageSizeChange" />
          </div>
        </template>
      </n-card>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import {
  NCard, NDataTable, NEmpty, NPagination, NTag, useMessage
} from 'naive-ui'
import { FileApi, MonitorApi } from '@/api'
import { NumberUtils, TimeUtils, PathUtils } from '@/utils'

const router = useRouter()
const message = useMessage()

// State
const loading = ref(false)
const recentKeywords = ref([])

// Upload pagination
const uploads = ref([])
const uploadTotal = ref(0)
const uploadPage = ref(1)
const uploadPageSize = ref(5)

// Download pagination
const downloads = ref([])
const downloadTotal = ref(0)
const downloadPage = ref(1)
const downloadPageSize = ref(5)

// 格式化
const formatFileSize = NumberUtils.formatFileSize

// 路径编码
const encodePath = (path) => {
  return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
}

// 导航到目录
const navigateToDir = (dirPath) => {
  router.push('/files/' + encodePath(dirPath))
}

// 点击关键词跳转到文件页并搜索
const searchKeyword = (keyword) => {
  router.push({ path: '/files', query: { q: keyword } })
}

// 判断是否为目录
const isDir = (row) => row.type === 'dir' || row.type === 'directory'

// 获取文件类型图标类名
const getFileIconClass = (row) => {
  if (isDir(row)) return 'icon-filetype-folder'
  const ext = row.fileName?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

// 表格列
const columns = [
  {
    title: '文件名',
    key: 'fileName',
    ellipsis: { tooltip: true },
    render(row) {
      const fullPath = row.fullPath || row.path || ''
      const segments = fullPath.split('/').filter(Boolean)
      const fileName = segments[segments.length - 1] || row.fileName || '未知文件'
      const pathSegments = segments.slice(0, -1)
      const iconClass = `icon-filetype ${getFileIconClass(row)}`
      const dirPath = row.fullPath || row.path || ''

      return h('div', {
        class: 'file-name-cell',
        title: fileName
      }, [
        h('div', { class: 'file-icon-wrapper' }, [
          h('span', { class: iconClass })
        ]),
        h('span', { class: 'file-path-content' }, [
          ...pathSegments.map((seg, idx) => {
            const segPath = '/' + pathSegments.slice(0, idx + 1).join('/')
            return [
              h('a', {
                class: 'path-segment',
                onClick: (e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  navigateToDir(segPath)
                }
              }, seg),
              '/'
            ]
          }).flat(),
          isDir(row)
            ? h('a', {
                class: 'file-link',
                onClick: (e) => {
                  e.preventDefault()
                  navigateToDir(dirPath)
                }
              }, fileName)
            : h('a', {
                href: `/api/v1/download/${PathUtils.encodeFilePath(fullPath)}`,
                class: 'file-link'
              }, fileName)
        ])
      ])
    }
  },
  {
    title: '大小',
    key: 'fileSize',
    width: 100,
    render: (row) => formatFileSize(row.fileSize || 0)
  },
  {
    title: '时间',
    key: 'createdAt',
    width: 180,
    render: (row) => {
      const text = TimeUtils.formatDateTime(row.createdAt)
      if (TimeUtils.isRecent24h(row.createdAt)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  }
]

// Pagination events
const onUploadPageChange = (page) => {
  uploadPage.value = page
  loadUploads()
}

const onUploadPageSizeChange = (size) => {
  uploadPageSize.value = size
  uploadPage.value = 1
  loadUploads()
}

const onDownloadPageChange = (page) => {
  downloadPage.value = page
  loadDownloads()
}

const onDownloadPageSizeChange = (size) => {
  downloadPageSize.value = size
  downloadPage.value = 1
  loadDownloads()
}

// 加载最近搜索关键词
const loadRecentKeywords = async () => {
  try {
    const data = await MonitorApi.getRecentKeywords()
    if (data.success) {
      recentKeywords.value = data.data || []
    }
  } catch (error) {
    console.error('获取最近搜索词失败:', error)
  }
}

// 对记录去重：优先按 id 去重，id 相同保留第一个；
// 若 id 不同但文件路径+文件名相同，也视为重复
const deduplicateRecords = (records) => {
  const seenById = new Set()
  const seenByFile = new Set()
  return records.filter(rec => {
    // 按 id 去重
    if (rec.id != null) {
      if (seenById.has(rec.id)) return false
      seenById.add(rec.id)
    }
    // 按文件身份去重（路径+文件名）
    const fileKey = [rec.rootName || '', rec.path || '', rec.fileName || ''].join('|')
    if (seenByFile.has(fileKey)) return false
    seenByFile.add(fileKey)
    return true
  })
}

// 加载最近上传
const loadUploads = async () => {
  try {
    const data = await FileApi.recent(uploadPage.value, uploadPageSize.value, 'upload')
    if (data.success && data.data) {
      uploads.value = deduplicateRecords(data.data.records || [])
      uploadTotal.value = data.data.total || 0
    }
  } catch (error) {
    message.error('加载上传记录失败')
    console.error(error)
  }
}

// 加载最近下载
const loadDownloads = async () => {
  loading.value = true
  try {
    const data = await FileApi.recent(downloadPage.value, downloadPageSize.value, 'download')
    if (data.success && data.data) {
      downloads.value = deduplicateRecords(data.data.records || [])
      downloadTotal.value = data.data.total || 0
    }
  } catch (error) {
    message.error('加载下载记录失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

// Lifecycle
onMounted(() => {
  loadRecentKeywords()
  loadUploads()
  loadDownloads()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.recent-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .recent-cards {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }

  .recent-card {
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @card-hover-shadow;
    }
  }

  .keyword-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 20px;
    padding: 8px 0;

    .n-tag {
      min-width: 30px;
      justify-content: center;
      transition: transform @transition-fast, box-shadow @transition-fast;

      &:hover {
        transform: translateY(-2px) scale(1.05);
        box-shadow: 0 2px 8px fade(@primary-color, 30%);
      }

      &:active {
        transform: scale(0.95);
      }
    }
  }

  .card-footer {
    display: flex;
    justify-content: center;
    padding: 8px 0 0 0;
  }
}

@media @tablet {
  .recent-view {
    padding: 12px;
  }
}

@media @mobile {
  .recent-view {
    padding: 8px;
  }
}
</style>
