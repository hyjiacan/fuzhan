<template>
  <div class="hot-view">
    <div class="hot-cards">
      <!-- 热门搜索关键词 -->
      <el-card class="hot-card">
        <template #header>热门搜索关键词</template>
        <div v-if="keywords.length > 0" class="keyword-badges">
          <el-badge v-for="kw in keywords" :key="kw.word" :value="kw.count" :max="999" type="warning" class="keyword-badge">
            <el-tag @click="searchKeyword(kw.word)" style="cursor: pointer">{{ kw.word }}</el-tag>
          </el-badge>
        </div>
        <el-empty v-else description="暂无搜索数据" />
      </el-card>

      <!-- 热门下载文件 -->
      <el-card class="hot-card">
        <template #header>热门下载文件</template>
        <div ref="tableWrapRef" class="table-v2-wrap" v-loading="loading">
          <el-table-v2
            :columns="downloadColumns"
            :data="hotDownloads"
            :width="tableWidth"
            :height="tableHeight"
            :row-key="(row) => row.fullPath || row.fileName || row.id || row.storageKey"
          :row-height="32" />
        </div>
        <div v-if="hotDownloads.length > 0" class="pagination-wrapper">
          <el-pagination
            v-model:current-page="currentPage"
            v-model:page-size="pageSize"
            :total="total"
            :page-sizes="[10, 20, 50]"
            layout="total, sizes, prev, pager, next"
            @current-change="onPageChange"
            @size-change="onPageSizeChange"
          />
          <span class="total-info">共 {{ total }} 个文件</span>
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, h, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { MonitorApi } from '@/api'
import { TimeUtils, PathUtils } from '@/utils'

const router = useRouter()

// 热门搜索关键词
const keywords = ref([])

// 热门下载
const hotDownloads = ref([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const loading = ref(false)

// el-table-v2 需要数值宽高，实时测量容器
const tableWrapRef = ref(null)
const tableWidth = ref(600)
const tableHeight = ref(360)
let tableResizeObs = null
const updateTableSize = () => {
  const el = tableWrapRef.value
  if (el) {
    tableWidth.value = el.clientWidth || 600
    tableHeight.value = el.clientHeight || 360
  }
}

// 格式化
const formatFileSize = (bytes) => {
  if (!bytes || bytes === 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return size.toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

// 判断是否为目录
const isDir = (row) => row.type === 'dir' || row.type === 'directory'

// 获取文件类型图标类名
const getFileIconClass = (row) => {
  if (isDir(row)) return 'icon-filetype-folder'
  const ext = row.fileName?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

// 下载列表列
const downloadColumns = [
  {
    title: '#',
    key: 'index',
    width: 50,
    cellRenderer: ({ rowIndex }) => rowIndex + 1 + (currentPage.value - 1) * pageSize.value
  },
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
                  router.push('/files/' + segPath.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/'))
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
                  router.push('/files/' + dirPath.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/'))
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
    title: '时间',
    key: 'uploadTime',
    width: 180,
    cellRenderer: ({ rowData: row }) => {
      const text = TimeUtils.formatDateTime(row.uploadTime)
      if (TimeUtils.isRecent24h(row.uploadTime)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  },
  {
    title: '下载次数',
    key: 'count',
    width: 90
  }
]

// 分页事件
const onPageChange = (page) => {
  currentPage.value = page
  loadHotDownloads()
}

const onPageSizeChange = (size) => {
  pageSize.value = size
  currentPage.value = 1
  loadHotDownloads()
}

// 点击关键词跳转到文件页并搜索
const searchKeyword = (keyword) => {
  router.push({ path: '/files', query: { q: keyword } })
}

// 加载热门搜索关键词
const loadKeywords = async () => {
  try {
    const data = await MonitorApi.getKeywords(20)
    if (data.success) {
      keywords.value = data.data || []
    }
  } catch (error) {
    console.error('获取热门搜索词失败:', error)
  }
}

// 加载热门下载
const loadHotDownloads = async () => {
  loading.value = true
  try {
    const data = await MonitorApi.getHotDownloads(currentPage.value, pageSize.value)
    if (data.success && data.data) {
      hotDownloads.value = data.data.records || []
      total.value = data.data.total || 0
    }
  } catch (error) {
    ElMessage.error('获取热门下载失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadKeywords()
  loadHotDownloads()
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (tableWrapRef.value) {
    tableResizeObs.observe(tableWrapRef.value)
  }
})

onUnmounted(() => {
  tableResizeObs?.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.hot-view {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .hot-cards {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }

  .hot-card {
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @card-hover-shadow;
    }
  }

  .table-v2-wrap {
    height: 360px;
  }

  .pagination-wrapper {
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 16px;
    padding: 16px 0 0 0;

    .total-info {
      font-size: @font-size-sm;
      color: @text-color-secondary;
    }
  }
}

@media @tablet {
  .hot-view {
    padding: 12px;
  }
}

@media @mobile {
  .hot-view {
    padding: 8px;
  }

  .pagination-wrapper {
    flex-direction: column;
    gap: 8px;
  }
}
</style>