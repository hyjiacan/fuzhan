<template>
  <div class="admin-users-view">
    <div class="header-section">
      <div class="header-left">
        <h2>用户管理</h2>
        <p class="description">管理系统用户，重置密码等操作</p>
      </div>
    </div>

    <el-card class="filter-card" shadow="never">
      <el-space>
        <el-input v-model="searchQuery" :maxlength="200" placeholder="过滤用户名..." clearable style="width: 200px" @keydown.enter="onSearch" @clear="onSearch" />
        <el-button @click="loadUsers" :loading="loading">
          <el-icon><RefreshIcon /></el-icon>
          刷新
        </el-button>
        <el-button type="primary" @click="showCreateUserModal">
          <el-icon><PlusIcon /></el-icon>
          添加用户
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
          :row-height="TableConst.ROW_HEIGHT"
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

    <el-dialog v-model="createUserModalVisible" title="添加用户" width="420px">
      <el-form ref="createFormRef" :model="createUserForm" label-width="80px">
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="createUserForm.username"
            :maxlength="32"
            placeholder="3-32位字符"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            v-model="createUserForm.password"
            :maxlength="128"
            type="password"
            show-password
            placeholder="至少6位"
          />
        </el-form-item>
        <el-form-item label="角色" prop="role">
          <el-select v-model="createUserForm.role" style="width: 100%">
            <el-option label="普通用户" value="user" />
            <el-option label="管理员" value="admin" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="createUserModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="creatingUser" @click="handleCreateUser">
            确认创建
          </el-button>
        </el-space>
      </template>
    </el-dialog>

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
import { TableConst, TimeUtils } from '@/utils'

const RefreshIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M17.65 6.35C16.2 4.9 14.21 4 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08c-.82 2.33-3.04 4-5.65 4-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z' })
])
const PlusIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' })
])

const loading = ref(false)
const users = ref([])
const searchQuery = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const createUserModalVisible = ref(false)
const creatingUser = ref(false)
const createFormRef = ref(null)
const createUserForm = ref({
  username: '',
  password: '',
  role: 'user'
})
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
      if (!row.quota) return h('span', { style: 'color: var(--el-text-color-secondary); font-size: 12px' }, '无限制')
      const used = row.usedStorage || 0
      const percentage = Math.min(100, Math.round(used / row.quota * 100))
      const status = percentage >= 90 ? 'exception' : percentage >= 70 ? 'warning' : 'success'
      return h('div', {
        style: 'display: flex; flex-direction: column; gap: 4px; justify-content: center; width: 100%;'
      }, [
        h('div', { style: 'display: flex; justify-content: space-between; align-items: baseline; gap: 8px;' }, [
          h('span', { style: 'font-size: 12px; white-space: nowrap;' },
            formatSize(used) + ' / ' + formatSize(row.quota)),
          h('span', { style: 'font-size: 12px; color: var(--el-text-color-secondary); white-space: nowrap;' },
            percentage + '%')
        ]),
        h(ElProgress, {
          type: 'line',
          status,
          percentage,
          showText: false,
          strokeWidth: 8,
          style: 'width: 100%;'
        })
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

const showCreateUserModal = () => {
  createUserForm.value = {
    username: '',
    password: '',
    role: 'user'
  }
  createUserModalVisible.value = true
}

const handleCreateUser = async () => {
  if (!createUserForm.value.username || createUserForm.value.username.trim().length < 3) {
    ElMessage.error('用户名至少3个字符')
    return
  }
  if (createUserForm.value.password.length < 6) {
    ElMessage.error('密码至少6位')
    return
  }

  creatingUser.value = true
  try {
    const data = await AdminApi.createUser(
      createUserForm.value.username.trim(),
      createUserForm.value.password,
      createUserForm.value.role
    )
    if (data.success) {
      ElMessage.success('用户创建成功')
      createUserModalVisible.value = false
      onSearch()
    } else {
      ElMessage.error(data.message || '用户创建失败')
    }
  } catch (error) {
    ElMessage.error('用户创建失败')
    console.error(error)
  } finally {
    creatingUser.value = false
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
