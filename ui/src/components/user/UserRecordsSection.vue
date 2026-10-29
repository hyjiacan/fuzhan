<template>
  <div class="user-records-section">
    <div class="header-section">
      <div class="header-left">
        <h2>操作记录</h2>
        <p class="description">查看您的上传与下载等操作记录</p>
      </div>
    </div>

    <el-card>
      <el-table
        v-loading="loading"
        :data="records"
        stripe
        empty-text="暂无操作记录"
      >
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ actionLabel(row.action) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="90">
          <template #default="{ row }">{{ sourceLabel(row.sourceType) }}</template>
        </el-table-column>
        <el-table-column label="文件名 / 路径" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.fileName }}{{ row.fullPath ? '（' + row.fullPath + '）' : '' }}</template>
        </el-table-column>
        <el-table-column label="大小" width="120">
          <template #default="{ row }">{{ formatSize(row.fileSize) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'failed' ? 'danger' : 'success'">
              {{ row.status === 'failed' ? '失败' : '成功' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="failReason" label="原因" show-overflow-tooltip />
      </el-table>

      <el-pagination
        v-if="total > 0"
        class="pagination"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="handlePageChange"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { PersonalApi } from '@/api'
import { TimeUtils, NumberUtils } from '@/utils'

const loading = ref(false)
const records = ref([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

const actionLabel = (a) => {
  const map = {
    upload: '上传',
    download: '下载',
    'download-by-hash': '下载',
    search: '搜索',
    preview: '预览'
  }
  return map[a] || a || '—'
}

const sourceLabel = (s) => {
  const map = { public: '公共', private: '私有', temp: '临时' }
  return map[s] || s || '—'
}

const formatTime = (t) => t ? TimeUtils.formatDateTime(t) : '—'
const formatSize = (b) => (b == null || b === 0) ? '—' : NumberUtils.formatFileSize(b)

const load = async () => {
  loading.value = true
  try {
    const data = await PersonalApi.getRecords(page.value, pageSize.value)
    if (data.success && data.data) {
      records.value = data.data.records || []
      total.value = data.data.total || 0
    }
  } catch (error) {
    console.error('获取操作记录失败:', error)
  } finally {
    loading.value = false
  }
}

const handlePageChange = (p) => {
  page.value = p
  load()
}

onMounted(load)
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-records-section {
  padding: @container-padding;

  .pagination {
    margin-top: 16px;
    justify-content: flex-end;
  }
}
</style>
