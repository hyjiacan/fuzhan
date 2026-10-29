<template>
  <div class="user-login-logs-section">
    <div class="header-section">
      <div class="header-left">
        <h2>登录记录</h2>
        <p class="description">查看您的登录与注册记录</p>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <div class="filter-row">
        <el-select
          v-model="filterMethod"
          placeholder="全部方式"
          clearable
          style="width: 130px"
          @change="applyFilters"
        >
          <el-option label="账号密码" value="local" />
          <el-option label="OAuth2" value="oauth2" />
          <el-option label="注册" value="register" />
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
          placeholder="搜索 IP / 失败原因 / 浏览器 / 用户名"
          clearable
          style="width: 260px"
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
        empty-text="没有符合条件的登录记录"
      >
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="方式" width="120">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ methodLabel(row.method) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="结果" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'success' ? 'success' : 'danger'">
              {{ row.status === 'success' ? '成功' : '失败' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="clientIP" label="IP" width="140" />
        <el-table-column prop="failReason" label="失败原因" show-overflow-tooltip />
        <el-table-column prop="userAgent" label="浏览器 / 设备" show-overflow-tooltip />
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
import { TimeUtils } from '@/utils'

const loading = ref(false)
const records = ref([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

// 过滤条件：方式 / 结果 / 时间范围 / 关键词
const filterMethod = ref('')
const filterStatus = ref('')
const keyword = ref('')
const dateRange = ref([])

const methodLabel = (m) => {
  const map = { local: '账号密码', oauth2: 'OAuth2', register: '注册' }
  return map[m] || m || '未知'
}

const formatTime = (t) => t ? TimeUtils.formatDateTime(t) : '—'

// 依据当前过滤条件组装查询参数（空值不传，由后端忽略）
const buildFilters = () => ({
  method: filterMethod.value || undefined,
  status: filterStatus.value || undefined,
  keyword: keyword.value.trim() || undefined,
  from: dateRange.value?.[0] ? new Date(dateRange.value[0]).toISOString() : undefined,
  to: dateRange.value?.[1] ? new Date(dateRange.value[1]).toISOString() : undefined
})

const load = async () => {
  loading.value = true
  try {
    const data = await PersonalApi.getLoginLogs(page.value, pageSize.value, buildFilters())
    if (data.success && data.data) {
      records.value = data.data.records || []
      total.value = data.data.total || 0
    }
  } catch (error) {
    console.error('获取登录记录失败:', error)
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
  filterMethod.value = ''
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

.user-login-logs-section {
  padding: @container-padding;

  .filter-card {
    margin-bottom: 14px;
  }

  .filter-row {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }

  .pagination {
    margin-top: 16px;
    justify-content: flex-end;
  }
}
</style>
