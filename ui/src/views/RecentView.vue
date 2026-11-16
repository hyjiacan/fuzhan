<template>
  <div class="recent-view page-container page-container--gap">
    <div class="recent-cards">
      <!-- 最近搜索 -->
      <el-card class="recent-card hover-card">
        <template #header>最近搜索</template>
        <div v-if="recentKeywords.length > 0" class="keyword-tags">
          <el-tag v-for="kw in recentKeywords" :key="kw.word" style="cursor: pointer" @click="searchKeyword(kw.word)">
            {{ kw.word }}
          </el-tag>
        </div>
        <el-empty v-else description="暂无搜索记录">
          <el-button type="primary" plain @click="router.push('/files')">
            <el-icon><component :is="SearchIcon" /></el-icon>
            去搜索文件
          </el-button>
        </el-empty>
      </el-card>

      <!-- 最近上传 -->
      <el-card class="recent-card hover-card">
        <template #header>最近上传</template>
        <div class="recent-table-wrap" v-loading="uploadsLoading">
          <el-table :data="uploads" row-key="id" stripe height="100%">
            <el-table-column label="文件名" min-width="220">
              <template #default="{ row }">
                <div class="file-name-cell" :title="fileNameOf(row)">
                  <div class="file-icon-wrapper">
                    <span :class="['icon-filetype', getFileIconClass(row)]"></span>
                  </div>
                  <span class="file-path-content">
                    <template v-for="(seg, idx) in parentSegments(row)" :key="idx">
                      <a class="path-segment" tabindex="0"
                        @keydown="keyNav(navigateToDir, segmentPath(parentSegments(row), idx))"
                        @click.prevent.stop="navigateToDir(segmentPath(parentSegments(row), idx))">{{ seg }}</a>
                      <span>/</span>
                    </template>
                    <a v-if="isDir(row)" class="file-link" tabindex="0"
                      @keydown="keyNav(navigateToDir, fullPathOf(row))"
                      @click.prevent="navigateToDir(fullPathOf(row))">{{ fileNameOf(row) }}</a>
                    <a v-else class="file-link" :href="downloadHref(row)"
                      @click.prevent="handleDownload(row)">{{ fileNameOf(row) }}</a>
                  </span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="大小" width="140">
              <template #default="{ row }">{{ formatFileSize(row.file_size || 0) }}</template>
            </el-table-column>
            <el-table-column label="备注" width="150">
              <template #default="{ row }">
                <span v-if="row.notes" class="note-text" :title="row.notes">{{ row.notes }}</span>
                <span v-else class="note-empty">-</span>
              </template>
            </el-table-column>
            <el-table-column label="上传时间" width="180">
              <template #default="{ row }">
                <span :style="TimeUtils.isRecent24h(row.created_at) ? 'color: #18a058' : ''">{{ TimeUtils.formatDateTime(row.created_at) }}</span>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无上传记录" :image-size="72">
                <el-button type="primary" plain @click="router.push('/files')">
                  <el-icon><component :is="SearchIcon" /></el-icon>
                  去文件页上传
                </el-button>
              </el-empty>
            </template>
          </el-table>
        </div>
        <template v-if="uploadTotal > uploads.length" #footer>
          <div class="card-footer">
            <el-pagination size="small" v-model:current-page="uploadPage" v-model:page-size="uploadPageSize"
              :total="uploadTotal" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
              @current-change="onUploadPageChange" @size-change="onUploadPageSizeChange" />
          </div>
        </template>
      </el-card>

      <!-- 最近下载 -->
      <el-card class="recent-card hover-card">
        <template #header>最近下载</template>
        <div class="recent-table-wrap" v-loading="downloadsLoading">
          <el-table :data="downloads" row-key="id" stripe height="100%">
            <el-table-column label="文件名" min-width="220">
              <template #default="{ row }">
                <div class="file-name-cell" :title="fileNameOf(row)">
                  <div class="file-icon-wrapper">
                    <span :class="['icon-filetype', getFileIconClass(row)]"></span>
                  </div>
                  <span class="file-path-content">
                    <template v-for="(seg, idx) in parentSegments(row)" :key="idx">
                      <a class="path-segment" tabindex="0"
                        @keydown="keyNav(navigateToDir, segmentPath(parentSegments(row), idx))"
                        @click.prevent.stop="navigateToDir(segmentPath(parentSegments(row), idx))">{{ seg }}</a>
                      <span>/</span>
                    </template>
                    <a v-if="isDir(row)" class="file-link" tabindex="0"
                      @keydown="keyNav(navigateToDir, fullPathOf(row))"
                      @click.prevent="navigateToDir(fullPathOf(row))">{{ fileNameOf(row) }}</a>
                    <a v-else class="file-link" :href="downloadHref(row)"
                      @click.prevent="handleDownload(row)">{{ fileNameOf(row) }}</a>
                  </span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="大小" width="140">
              <template #default="{ row }">{{ formatFileSize(row.file_size || 0) }}</template>
            </el-table-column>
            <el-table-column label="备注" width="150">
              <template #default="{ row }">
                <span v-if="row.notes" class="note-text" :title="row.notes">{{ row.notes }}</span>
                <span v-else class="note-empty">-</span>
              </template>
            </el-table-column>
            <el-table-column label="下载时间" width="180">
              <template #default="{ row }">{{ TimeUtils.formatDateTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="上传时间" width="180">
              <template #default="{ row }">
                <span v-if="row.upload_time && !String(row.upload_time).startsWith('0001')"
                  :style="TimeUtils.isRecent24h(row.upload_time) ? 'color: #18a058' : ''">{{ TimeUtils.formatDateTime(row.upload_time) }}</span>
                <span v-else class="note-empty">-</span>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无下载记录" :image-size="72">
                <el-button type="primary" plain @click="router.push('/files')">
                  <el-icon><component :is="SearchIcon" /></el-icon>
                  去文件页看看
                </el-button>
              </el-empty>
            </template>
          </el-table>
        </div>
        <template v-if="downloadTotal > downloads.length" #footer>
          <div class="card-footer">
            <el-pagination size="small" v-model:current-page="downloadPage" v-model:page-size="downloadPageSize"
              :total="downloadTotal" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
              @current-change="onDownloadPageChange" @size-change="onDownloadPageSizeChange" />
          </div>
        </template>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { FileApi, MonitorApi } from '@/api'
import { NumberUtils, TimeUtils, PathUtils } from '@/utils'
import { downloadWithTicket } from '@/utils/download'
import { keyNav } from '@/utils/a11y'

const router = useRouter()

// 搜索图标
const SearchIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M15.5 14h-.79l-.28-.27a6.5 6.5 0 1 0-.7.7l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0A4.5 4.5 0 1 1 14 9.5 4.5 4.5 0 0 1 9.5 14z' })
])

// State
const uploadsLoading = ref(false)
const downloadsLoading = ref(false)
const recentKeywords = ref([])

// Upload pagination
const uploads = ref([])
const uploadTotal = ref(0)
const uploadPage = ref(1)
const uploadPageSize = ref(10)

// Download pagination
const downloads = ref([])
const downloadTotal = ref(0)
const downloadPage = ref(1)
const downloadPageSize = ref(10)

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
  const ext = row.file_name?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

// 完整路径与路径分段
const fullPathOf = (row) => row.full_path || row.path || ''
const pathSegmentsOf = (row) => fullPathOf(row).split('/').filter(Boolean)
const parentSegments = (row) => pathSegmentsOf(row).slice(0, -1)
const fileNameOf = (row) => {
  const segments = pathSegmentsOf(row)
  return segments[segments.length - 1] || row.file_name || '未知文件'
}
const segmentPath = (segments, idx) => '/' + segments.slice(0, idx + 1).join('/')

// 下载
const downloadHref = (row) => `/download/${PathUtils.encodeFilePath(fullPathOf(row))}`
const handleDownload = (row) => downloadWithTicket(downloadHref(row))

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

// 加载最近上传
const loadUploads = async () => {
  uploadsLoading.value = true
  try {
    const data = await FileApi.recent(uploadPage.value, uploadPageSize.value, 'upload')
    if (data.success && data.data) {
      uploads.value = data.data.records || []
      uploadTotal.value = data.data.total || 0
    }
  } catch (error) {
    ElMessage.error('加载上传记录失败')
    console.error(error)
  } finally {
    uploadsLoading.value = false
  }
}

// 加载最近下载
const loadDownloads = async () => {
  downloadsLoading.value = true
  try {
    const data = await FileApi.recent(downloadPage.value, downloadPageSize.value, 'download')
    if (data.success && data.data) {
      downloads.value = data.data.records || []
      downloadTotal.value = data.data.total || 0
    }
  } catch (error) {
    ElMessage.error('加载下载记录失败')
    console.error(error)
  } finally {
    downloadsLoading.value = false
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
  .recent-cards {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }

  .recent-table-wrap {
    height: 380px;
  }

  .note-text {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--el-text-color-regular);
    text-align: left;
  }

  .note-empty {
    color: var(--el-text-color-placeholder);
  }

  .keyword-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 20px;
    padding: 8px 0;

    .el-tag {
      min-width: 30px;
      justify-content: center;
      transition: transform @transition-fast, box-shadow @transition-fast;

      &:hover {
        transform: translateY(-2px) scale(1.05);
        box-shadow: 0 2px 8px fade(@primary-accent, 30%);
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
</style>
