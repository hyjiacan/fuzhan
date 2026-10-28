<template>
  <div class="user-login-logs-section">
    <div class="header-section">
      <div class="header-left">
        <h2>登录记录</h2>
        <p class="description">查看您的登录与注册记录</p>
      </div>
    </div>

    <el-card>
      <el-table
        v-loading="loading"
        :data="records"
        stripe
        empty-text="暂无登录记录"
      >
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="方式" width="110">
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

const methodLabel = (m) => {
  const map = { local: '账号密码', oauth2: 'OAuth2', register: '注册' }
  return map[m] || m || '未知'
}

const formatTime = (t) => t ? TimeUtils.formatDateTime(t) : '—'

const load = async () => {
  loading.value = true
  try {
    const data = await PersonalApi.getLoginLogs(page.value, pageSize.value)
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

  .header-section {
    margin-bottom: 16px;

    h2 {
      margin: 0 0 8px 0;
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .description {
      margin: 0;
      color: @text-color-secondary;
      font-size: @font-size-base;
    }
  }

  .pagination {
    margin-top: 16px;
    justify-content: flex-end;
  }
}
</style>
