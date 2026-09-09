<template>
  <div class="recent-view">
    <div class="recent-cards">
      <!-- 最近搜索 -->
      <el-card class="recent-card">
        <template #header>最近搜索</template>
        <div v-if="recentKeywords.length > 0" class="keyword-tags">
          <el-tag v-for="kw in recentKeywords" :key="kw.word" style="cursor: pointer" @click="searchKeyword(kw.word)">
            {{ kw.word }}
          </el-tag>
        </div>
        <el-empty v-else description="暂无搜索记录" />
      </el-card>

      <!-- 最近上传 -->
      <el-card class="recent-card">
        <template #header>最近上传</template>
        <div ref="uploadWrapRef" class="table-v2-wrap" v-loading="loading">
          <el-table-v2 :columns="columns" :data="uploads" :width="uploadWidth" :height="uploadHeight"
            row-key="id" :row-height="32" />
        </div>
        <template v-if="uploadTotal > uploads.length" #footer>
          <div class="card-footer">
            <el-pagination size="small" v-model:current-page="uploadPage" v-model:page-size="uploadPageSize"
              :total="uploadTotal" :page-sizes="[5, 10, 20]" layout="total, sizes, prev, pager, next"
              @current-change="onUploadPageChange" @size-change="onUploadPageSizeChange" />
          </div>
        </template>
      </el-card>

      <!-- 最近下载 -->
      <el-card class="recent-card">
        <template #header>最近下载</template>
        <div ref="downloadWrapRef" class="table-v2-wrap" v-loading="loading">
          <el-table-v2 :columns="columns" :data="downloads" :width="downloadWidth" :height="downloadHeight"
            row-key="id" :row-height="32" />
        </div>
        <template v-if="downloadTotal > downloads.length" #footer>
          <div class="card-footer">
            <el-pagination size="small" v-model:current-page="downloadPage" v-model:page-size="downloadPageSize"
              :total="downloadTotal" :page-sizes="[5, 10, 20]" layout="total, sizes, prev, pager, next"
              @current-change="onDownloadPageChange" @size-change="onDownloadPageSizeChange" />
          </div>
        </template>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, h } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { FileApi, MonitorApi } from '@/api'
import { NumberUtils, TimeUtils, PathUtils } from '@/utils'

const router = useRouter()

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

// el-table-v2 需要数值宽高，实时测量容器
const makeTableResize = () => {
  const wrapRef = ref(null)
  const width = ref(600)
  const height = ref(260)
  let resizeObs = null
  const update = () => {
    const el = wrapRef.value
    if (el) {
      width.value = el.clientWidth || 600
      height.value = el.clientHeight || 260
    }
  }
  const bind = () => {
    update()
    resizeObs = new ResizeObserver(update)
    if (wrapRef.value) resizeObs.observe(wrapRef.value)
  }
  const unbind = () => resizeObs?.disconnect()
  return { wrapRef, width, height, bind, unbind }
}
const uploadSize = makeTableResize()
const downloadSize = makeTableResize()

// 将工厂返回的嵌套对象扁平化，供模板直接引用（el-table-v2 需数值 width/height）
const uploadWrapRef = uploadSize.wrapRef
const uploadWidth = uploadSize.width
const uploadHeight = uploadSize.height
const downloadWrapRef = downloadSize.wrapRef
const downloadWidth = downloadSize.width
const downloadHeight = downloadSize.height

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
    minWidth: 220,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => {
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
                href: `/download/${PathUtils.encodeFilePath(fullPath)}`,
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
    cellRenderer: ({ rowData: row }) => formatFileSize(row.fileSize || 0)
  },
  {
    title: '备注',
    key: 'notes',
    minWidth: 160,
    cellRenderer: ({ rowData: row }) => row.notes
      ? h('span', { title: row.notes, style: 'color:#666; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; display:block;' }, row.notes)
      : h('span', { style: 'color:#bbb;' }, '-')
  },
  {
    title: '时间',
    key: 'createdAt',
    width: 180,
    cellRenderer: ({ rowData: row }) => {
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

// 加载最近上传
const loadUploads = async () => {
  try {
    const data = await FileApi.recent(uploadPage.value, uploadPageSize.value, 'upload')
    if (data.success && data.data) {
      uploads.value = data.data.records || []
      uploadTotal.value = data.data.total || 0
    }
  } catch (error) {
    ElMessage.error('加载上传记录失败')
    console.error(error)
  }
}

// 加载最近下载
const loadDownloads = async () => {
  loading.value = true
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
    loading.value = false
  }
}

// Lifecycle
onMounted(() => {
  loadRecentKeywords()
  loadUploads()
  loadDownloads()
  uploadSize.bind()
  downloadSize.bind()
})

onUnmounted(() => {
  uploadSize.unbind()
  downloadSize.unbind()
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

  .table-v2-wrap {
    height: 260px;
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