<template>
  <div class="admin-users-view">
    <div class="header-section">
      <h2>用户管理</h2>
      <p class="description">管理系统用户，重置密码等操作</p>
    </div>

    <el-card class="filter-card" shadow="never">
      <el-space>
        <el-input v-model="searchQuery" :maxlength="200" placeholder="过滤用户名..." clearable style="width: 200px" @keydown.enter="onSearch" @clear="onSearch" />
        <el-button @click="loadUsers" :loading="loading">
          <el-icon><RefreshIcon /></el-icon>
          刷新
        </el-button>
      </el-space>
    </el-card>

    <el-card class="table-card" shadow="never">
      <div ref="tableWrapRef" class="table-v2-wrap" v-loading="loading">
        <el-table-v2
          :columns="columns"
          :data="users"
          :width="tableWidth"
          :height="tableHeight"
          :row-height="32"
          row-key="uuid"
        />
      </div>
      <div class="pagination-wrap" v-if="total > 0">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadUsers"
          @size-change="onPageSizeChange"
        />
      </div>
    </el-card>

    <el-dialog v-model="resetPasswordModalVisible" title="重置用户密码" width="400px">
      <el-form ref="resetFormRef" :model="resetPasswordForm" label-width="100px">
        <el-form-item label="用户名">
          {{ resetPasswordForm.username }}
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="resetPasswordForm.newPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请输入新密码"
          />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="resetPasswordForm.confirmPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请再次输入新密码"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="resetPasswordModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="resettingPassword" @click="handleResetPassword">
            确认重置
          </el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, h } from 'vue'
import { ElMessage, ElMessageBox, ElButton, ElTag, ElProgress } from 'element-plus'
import { AuthApi, AdminApi } from '@/api'
import { TimeUtils } from '@/utils'

const RefreshIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M17.65 6.35C16.2 4.9 14.21 4 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08c-.82 2.33-3.04 4-5.65 4-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z' })
])

const loading = ref(false)
const users = ref([])
const searchQuery = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const resetPasswordModalVisible = ref(false)
const resettingPassword = ref(false)
const resetFormRef = ref(null)
const resetPasswordForm = ref({
  uuid: '',
  username: '',
  newPassword: '',
  confirmPassword: ''
})

const onSearch = () => {
  page.value = 1
  loadUsers()
}

const onPageSizeChange = () => {
  page.value = 1
  loadUsers()
}

const formatSize = (bytes) => {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
  return `${size.toFixed(1)} ${units[i]}`
}

// el-table-v2 需要数值宽高，实时测量容器
const tableWrapRef = ref(null)
const tableWidth = ref(600)
const tableHeight = ref(400)
let tableResizeObs = null
const updateTableSize = () => {
  const el = tableWrapRef.value
  if (el) {
    tableWidth.value = el.clientWidth || 600
    tableHeight.value = el.clientHeight || 400
  }
}

const columns = [
  {
    title: '用户名',
    key: 'username',
    minWidth: 150,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { class: 'file-name-cell', title: row.username || '' }, [
      h('span', { class: 'file-link' }, row.username || '-')
    ])
  },
  {
    title: '状态',
    key: 'disabled',
    width: 80,
    cellRenderer: ({ rowData: row }) => {
      const type = row.disabled ? 'danger' : 'success'
      return h(ElTag, { type, size: 'small' }, () => row.disabled ? '禁用' : '正常')
    }
  },
  {
    title: '用户ID',
    key: 'uuid',
    width: 250,
    cellRenderer: ({ rowData: row }) => row.uuid || '-'
  },
  {
    title: '注册时间',
    key: 'createdAt',
    width: 180,
    cellRenderer: ({ rowData: row }) => TimeUtils.formatDateTime(row.createdAt)
  },
  {
    title: '配额使用',
    key: 'quota',
    width: 200,
    cellRenderer: ({ rowData: row }) => {
      if (!row.quota) return h('span', { style: 'color: #999; font-size: 12px' }, '无限制')
      const percentage = Math.min(100, Math.round((row.usedStorage || 0) / row.quota * 100))
      const status = percentage >= 90 ? 'exception' : percentage >= 70 ? 'warning' : 'success'
      return h('div', { style: 'display: flex; align-items: center; gap: 8px;' }, [
        h(ElProgress, {
          type: 'line',
          status,
          percentage,
          textInside: true,
          strokeWidth: 18,
          style: 'flex: 1; min-width: 100px;'
        }),
        h('span', { style: 'font-size: 12px; white-space: nowrap;' },
          formatSize(row.usedStorage) + ' / ' + formatSize(row.quota)
        )
      ])
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    fixed: 'right',
    cellRenderer: ({ rowData: row }) => h('div', { style: 'display: flex; gap: 8px;' }, [
      h(ElButton, {
        size: 'small',
        type: row.disabled ? 'success' : 'warning',
        link: true,
        onClick: () => toggleUserDisabled(row)
      }, () => row.disabled ? '启用' : '禁用'),
      h(ElButton, {
        size: 'small',
        type: 'primary',
        link: true,
        onClick: () => showResetPasswordModal(row)
      }, () => '重置密码'),
      h(ElButton, {
        size: 'small',
        type: 'danger',
        link: true,
        onClick: () => confirmDeleteUser(row)
      }, () => '删除')
    ])
  }
]

const loadUsers = async () => {
  loading.value = true
  try {
    const data = await AdminApi.getUsers(page.value, pageSize.value, searchQuery.value)
    if (data.success) {
      users.value = (data.data && data.data.users) || []
      total.value = data.data?.total || 0
    } else {
      ElMessage.error(data.message || '获取用户列表失败')
    }
  } catch (error) {
    ElMessage.error('获取用户列表失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

const showResetPasswordModal = (user) => {
  resetPasswordForm.value = {
    uuid: user.uuid,
    username: user.username,
    newPassword: '',
    confirmPassword: ''
  }
  resetPasswordModalVisible.value = true
}

const handleResetPassword = async () => {
  if (resetPasswordForm.value.newPassword !== resetPasswordForm.value.confirmPassword) {
    ElMessage.error('两次输入的密码不一致')
    return
  }
  if (resetPasswordForm.value.newPassword.length < 6) {
    ElMessage.error('密码至少6位')
    return
  }

  resettingPassword.value = true
  try {
    const data = await AdminApi.resetPassword(resetPasswordForm.value.uuid, resetPasswordForm.value.newPassword)
    if (data.success) {
      ElMessage.success('密码重置成功')
      resetPasswordModalVisible.value = false
    } else {
      ElMessage.error(data.message || '密码重置失败')
    }
  } catch (error) {
    ElMessage.error('密码重置失败')
    console.error(error)
  } finally {
    resettingPassword.value = false
  }
}

const toggleUserDisabled = async (user) => {
  const action = user.disabled ? '启用' : '禁用'
  try {
    await ElMessageBox.confirm(`确定要${action}用户 "${user.username}" 吗？`, `确认${action}`, {
      type: 'warning',
      confirmButtonText: action,
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  try {
    const data = await AdminApi.setUserDisabled(user.uuid, !user.disabled)
    if (data.success) {
      ElMessage.success(`${action}成功`)
      loadUsers()
    } else {
      ElMessage.error(data.message || `${action}失败`)
    }
  } catch (error) {
    ElMessage.error(`${action}失败`)
    console.error(error)
  }
}

const confirmDeleteUser = async (user) => {
  try {
    await ElMessageBox.confirm(`确定要删除用户 "${user.username}" 吗？此操作不可恢复。`, '确认删除', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  try {
    const data = await AdminApi.deleteUser(user.uuid)
    if (data.success) {
      ElMessage.success('用户已删除')
      loadUsers()
    } else {
      ElMessage.error(data.message || '删除失败')
    }
  } catch (error) {
    ElMessage.error('删除失败')
    console.error(error)
  }
}

onMounted(() => {
  loadUsers()
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

.admin-users-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 8px;

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

  .table-card {
    .table-v2-wrap {
      height: 480px;
    }

    .pagination-wrap {
      display: flex;
      justify-content: flex-end;
      padding: 12px 0 0;
    }
  }
}
</style>