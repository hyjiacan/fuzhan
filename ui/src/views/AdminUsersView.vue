<template>
  <div class="admin-users-view">
    <div class="header-section">
      <h2>用户管理</h2>
      <p class="description">管理系统用户，重置密码等操作</p>
    </div>

    <n-card class="filter-card">
      <n-space>
        <n-input v-model:value="searchQuery" :maxlength="200" placeholder="过滤用户名..." clearable style="width: 200px" />
        <n-button @click="loadUsers" :loading="loading">
          <template #icon><n-icon><RefreshIcon /></n-icon></template>
          刷新
        </n-button>
      </n-space>
    </n-card>

    <n-data-table
      :columns="columns"
      :data="filteredUsers"
      :loading="loading"
      :pagination="pagination"
      :row-key="row => row.uuid"
      striped
    />

    <n-modal v-model:show="resetPasswordModalVisible" preset="card" title="重置用户密码" style="width: 400px">
      <n-form ref="resetFormRef" :model="resetPasswordForm" label-placement="left" label-width="100">
        <n-form-item label="用户名">
          {{ resetPasswordForm.username }}
        </n-form-item>
        <n-form-item label="新密码" path="newPassword">
          <n-input
            v-model:value="resetPasswordForm.newPassword"
            :maxlength="128"
            type="password"
            show-password-on="click"
            placeholder="请输入新密码"
          />
        </n-form-item>
        <n-form-item label="确认密码" path="confirmPassword">
          <n-input
            v-model:value="resetPasswordForm.confirmPassword"
            :maxlength="128"
            type="password"
            show-password-on="click"
            placeholder="请再次输入新密码"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="resetPasswordModalVisible = false">取消</n-button>
          <n-button type="primary" :loading="resettingPassword" @click="handleResetPassword">
            确认重置
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, h } from 'vue'
import { NDataTable, NCard, NButton, NSpace, NInput, NModal, NForm, NFormItem, NTag, NProgress, NIcon, useMessage, useDialog } from 'naive-ui'
import { AuthApi, AdminApi } from '@/api'
import { TimeUtils } from '@/utils'

const message = useMessage()
const dialog = useDialog()

const RefreshIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M17.65 6.35C16.2 4.9 14.21 4 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08c-.82 2.33-3.04 4-5.65 4-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z' })
])

const loading = ref(false)
const users = ref([])
const searchQuery = ref('')
const resetPasswordModalVisible = ref(false)
const resettingPassword = ref(false)
const resetFormRef = ref(null)
const resetPasswordForm = ref({
  uuid: '',
  username: '',
  newPassword: '',
  confirmPassword: ''
})

const pagination = { pageSize: 20 }

const filteredUsers = computed(() => {
  if (!searchQuery.value) return users.value
  const query = searchQuery.value.toLowerCase()
  return users.value.filter(user =>
    user.username.toLowerCase().includes(query)
  )
})

const formatSize = (bytes) => {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
  return `${size.toFixed(1)} ${units[i]}`
}

const columns = [
  {
    title: '用户名',
    key: 'username',
    width: 150
  },
  {
    title: '状态',
    key: 'disabled',
    width: 80,
    render: (row) => {
      const type = row.disabled ? 'error' : 'success'
      return h(NTag, { type, size: 'small' }, () => row.disabled ? '禁用' : '正常')
    }
  },
  {
    title: '用户ID',
    key: 'uuid',
    ellipsis: { tooltip: true },
    width: 250
  },
  {
    title: '注册时间',
    key: 'createdAt',
    width: 180,
    render: (row) => TimeUtils.formatDateTime(row.createdAt)
  },
  {
    title: '配额使用',
    key: 'quota',
    width: 200,
    render: (row) => {
      if (!row.quota) return h('span', { style: 'color: #999; font-size: 12px' }, '无限制')
      const percentage = Math.min(100, Math.round((row.usedStorage || 0) / row.quota * 100))
      const status = percentage >= 90 ? 'error' : percentage >= 70 ? 'warning' : 'success'
      return h('div', { style: 'display: flex; align-items: center; gap: 8px;' }, [
        h(NProgress, {
          type: 'line',
          status,
          percentage,
          indicatorPlacement: 'inside',
          height: 18,
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
    render: (row) => h('div', { style: { display: 'flex', gap: '4px' } }, [
      h(NButton, {
        size: 'small',
        type: row.disabled ? 'success' : 'warning',
        quaternary: true,
        onClick: () => toggleUserDisabled(row)
      }, () => row.disabled ? '启用' : '禁用'),
      h(NButton, {
        size: 'small',
        type: 'info',
        quaternary: true,
        onClick: () => showResetPasswordModal(row)
      }, () => '重置密码'),
      h(NButton, {
        size: 'small',
        type: 'error',
        quaternary: true,
        onClick: () => confirmDeleteUser(row)
      }, () => '删除')
    ])
  }
]

const loadUsers = async () => {
  loading.value = true
  try {
    const data = await AdminApi.getUsers()
    if (data.success) {
      users.value = (data.data && data.data.users) || []
    } else {
      message.error(data.message || '获取用户列表失败')
    }
  } catch (error) {
    message.error('获取用户列表失败')
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
    message.error('两次输入的密码不一致')
    return
  }
  if (resetPasswordForm.value.newPassword.length < 6) {
    message.error('密码至少6位')
    return
  }

  resettingPassword.value = true
  try {
    const data = await AdminApi.resetPassword(resetPasswordForm.value.uuid, resetPasswordForm.value.newPassword)
    if (data.success) {
      message.success('密码重置成功')
      resetPasswordModalVisible.value = false
    } else {
      message.error(data.message || '密码重置失败')
    }
  } catch (error) {
    message.error('密码重置失败')
    console.error(error)
  } finally {
    resettingPassword.value = false
  }
}

const toggleUserDisabled = (user) => {
  const action = user.disabled ? '启用' : '禁用'
  dialog.warning({
    title: `确认${action}`,
    content: `确定要${action}用户 "${user.username}" 吗？`,
    positiveText: action,
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const data = await AdminApi.setUserDisabled(user.uuid, !user.disabled)
        if (data.success) {
          message.success(`${action}成功`)
          loadUsers()
        } else {
          message.error(data.message || `${action}失败`)
        }
      } catch (error) {
        message.error(`${action}失败`)
        console.error(error)
      }
    }
  })
}

const confirmDeleteUser = (user) => {
  dialog.warning({
    title: '确认删除',
    content: `确定要删除用户 "${user.username}" 吗？此操作不可恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        const data = await AdminApi.deleteUser(user.uuid)
        if (data.success) {
          message.success('用户已删除')
          loadUsers()
        } else {
          message.error(data.message || '删除失败')
        }
      } catch (error) {
        message.error('删除失败')
        console.error(error)
      }
    }
  })
}

onMounted(() => {
  loadUsers()
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
}
</style>
