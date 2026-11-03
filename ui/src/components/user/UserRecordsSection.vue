<template>
  <div class="user-records-section">
    <div class="header-section">
      <div class="header-left">
        <h2>操作记录</h2>
        <p class="description">查看您的上传、下载、删除等操作记录</p>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <div class="filter-row">
        <el-select
          v-model="filterAction"
          placeholder="全部操作"
          clearable
          style="width: 130px"
          @change="applyFilters"
        >
          <el-option label="上传" value="upload" />
          <el-option label="下载" value="download" />
          <el-option label="按哈希下载" value="download-by-hash" />
          <el-option label="搜索" value="search" />
          <el-option label="预览" value="preview" />
          <el-option label="删除" value="delete" />
        </el-select>
        <el-select
          v-model="filterSource"
          placeholder="全部来源"
          clearable
          style="width: 120px"
          @change="applyFilters"
        >
          <el-option label="公共" value="public" />
          <el-option label="私有" value="private" />
          <el-option label="临时" value="temp" />
        </el-select>
        <el-select
          v-model="filterStatus"
          placeholder="全部结果"
          clearable
          style="width: 120px"
          @change="applyFilters"
        >
          <el-option label="成功" value="success" />
          <el-option label="失败" value="failed" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          style="width: 360px"
          @change="applyFilters"
        />
        <el-input
          v-model="keyword"
          placeholder="搜索文件名 / 路径"
          clearable
          style="width: 220px"
          @keyup.enter="applyFilters"
          @clear="applyFilters"
        />
        <el-button type="primary" @click="applyFilters">查询</el-button>
        <el-button @click="resetFilters">重置</el-button>
      </div>
    </el-card>

    <el-card>
      <el-table
        v-loading="loading"
        :data="records"
        stripe
        empty-text="没有符合条件的操作记录"
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
          <template #default="{ row }">
            <span>{{ row.fileName }}</span>
            <el-tag v-if="row.overwritten" size="small" type="warning" effect="plain" style="margin: 0 6px">覆盖</el-tag>
            <span>{{ row.fullPath ? '（' + row.fullPath + '）' : '' }}</span>
          </template>
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

// 过滤条件：操作 / 来源 / 结果 / 时间范围 / 关键词
const filterAction = ref('')
const filterSource = ref('')
const filterStatus = ref('')
const keyword = ref('')
const dateRange = ref([])

const actionLabel = (a) => {
  const map = {
    upload: '上传',
    download: '下载',
    'download-by-hash': '下载',
    search: '搜索',
    preview: '预览',
    delete: '删除'
  }
  return map[a] || a || '—'
}

const sourceLabel = (s) => {
  const map = { public: '公共', private: '私有', temp: '临时' }
  return map[s] || s || '—'
}

const formatTime = (t) => t ? TimeUtils.formatDateTime(t) : '—'
const formatSize = (b) => (b == null || b === 0) ? '—' : NumberUtils.formatFileSize(b)

// 依据当前过滤条件组装查询参数（空值不传，由后端忽略）
const buildFilters = () => ({
  action: filterAction.value || undefined,
  sourceType: filterSource.value || undefined,
  status: filterStatus.value || undefined,
  keyword: keyword.value.trim() || undefined,
  from: dateRange.value?.[0] ? new Date(dateRange.value[0]).toISOString() : undefined,
  to: dateRange.value?.[1] ? new Date(dateRange.value[1]).toISOString() : undefined
})

const load = async () => {
  loading.value = true
  try {
    const data = await PersonalApi.getRecords(page.value, pageSize.value, buildFilters())
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

// 条件变更后从第一页重新查询
const applyFilters = () => {
  page.value = 1
  load()
}

const resetFilters = () => {
  filterAction.value = ''
  filterSource.value = ''
  filterStatus.value = ''
  keyword.value = ''
  dateRange.value = []
  applyFilters()
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
}
</style>
