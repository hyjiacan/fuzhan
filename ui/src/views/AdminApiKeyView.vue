<template>
  <div class="admin-api-key">
    <div class="header-section">
      <h2>API Key 管理</h2>
      <p class="description">管理 Open API 访问密钥，用于外部程序访问</p>
    </div>

    <!-- 统计卡片 -->
    <n-grid :cols="3" :x-gap="16" :y-gap="16" style="margin-bottom: 16px;">
      <n-gi>
        <n-card class="stat-card">
          <div class="stat-content">
            <div class="stat-value" style="color: #1890ff;">{{ stats.active }}</div>
            <div class="stat-label">活跃 Key</div>
          </div>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card class="stat-card">
          <div class="stat-content">
            <div class="stat-value" style="color: #faad14;">{{ stats.total }}</div>
            <div class="stat-label">总计</div>
          </div>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card class="stat-card">
          <div class="stat-content">
            <div class="stat-value" style="color: #f5222d;">{{ stats.expired }}</div>
            <div class="stat-label">已过期</div>
          </div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 操作栏 -->
    <n-space style="margin-bottom: 16px;">
      <n-button type="primary" @click="showCreateModal = true">
        <template #icon>
          <n-icon><AddIcon /></n-icon>
        </template>
        创建 API Key
      </n-button>
      <n-button @click="loadApiKeys" :loading="loading">刷新</n-button>
    </n-space>

    <!-- API Key 列表 -->
    <n-card>
      <n-data-table
        :columns="columns"
        :data="apiKeys"
        :loading="loading"
        :pagination="pagination"
        :row-key="row => row.id"
      />
    </n-card>

    <!-- 创建 API Key 弹窗 -->
    <n-modal v-model:show="showCreateModal" preset="card" title="创建 API Key" style="width: 500px">
      <n-form ref="createFormRef" :model="createForm" :rules="createRules" label-placement="left" label-width="100">
        <n-form-item label="名称" path="name">
          <n-input v-model:value="createForm.name" :maxlength="128" placeholder="给这个 Key 起个名字" />
        </n-form-item>
        <n-form-item label="权限范围" path="scopes">
          <n-select
            v-model:value="createForm.scopes"
            :options="scopeOptions"
            placeholder="选择权限范围"
          />
          <template #feedback>
            <span class="field-hint">open_api:reader - 读取文件列表、搜索、下载<br>
            open_api:writer - 额外包含文件备注、依赖关系</span>
          </template>
        </n-form-item>
        <n-form-item label="过期时间" path="expiresIn">
          <n-select
            v-model:value="createForm.expiresIn"
            :options="expiryOptions"
            placeholder="选择过期时间"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreateModal = false">取消</n-button>
          <n-button type="primary" :loading="creating" @click="handleCreate">创建</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 显示新创建的 Key 弹窗 -->
    <n-modal v-model:show="showRawKeyModal" preset="card" title="API Key 已创建" style="width: 600px">
      <n-alert type="warning" :show-icon="false">
        请立即复制保存此 Key，它只会显示这一次！
      </n-alert>
      <n-input
        :value="rawKey"
        type="textarea"
        readonly
        :rows="3"
        style="margin-top: 16px; font-family: monospace;"
      />
      <template #footer>
        <n-space justify="end">
          <n-button @click="copyRawKey">复制</n-button>
          <n-button type="primary" @click="showRawKeyModal = false">关闭</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 确认删除对话框 -->
    <n-modal v-model:show="showDeleteModal" preset="card" title="确认删除" style="width: 400px">
      <n-alert type="error">
        确定要删除 API Key「{{ deleteTarget?.name }}」吗？此操作不可恢复。
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDeleteModal = false">取消</n-button>
          <n-button type="error" :loading="deleting" @click="handleDelete">删除</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted } from 'vue'
import {
  NButton, NCard, NDataTable, NForm, NFormItem, NInput, NInputGroup,
  NModal, NSelect, NSpace, NIcon, NGrid, NGi, NAlert, NTag, useMessage,
  NPopconfirm
} from 'naive-ui'
import { ApiKeyApi, AuthApi } from '../api'
import { formatErrorMessage } from '@/utils/error'

const message = useMessage()

// Icons
const AddIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' })
])

// State
const loading = ref(false)
const creating = ref(false)
const deleting = ref(false)
const apiKeys = ref([])
const showCreateModal = ref(false)
const showRawKeyModal = ref(false)
const showDeleteModal = ref(false)
const rawKey = ref('')
const deleteTarget = ref(null)
const createFormRef = ref(null)
const currentUserId = ref(0)

const createForm = ref({
  name: '',
  scopes: 'open_api:reader',
  expiresIn: 0
})

const createRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }]
}

const pagination = ref({
  page: 1,
  pageSize: 20,
  pageSizes: [10, 20, 50],
  showSizePicker: true,
  itemCount: 0
})

const scopeOptions = [
  { label: '读取 (open_api:reader)', value: 'open_api:reader' },
  { label: '读写 (open_api:writer)', value: 'open_api:writer' }
]

const expiryOptions = [
  { label: '永不过期', value: 0 },
  { label: '7 天', value: 7 * 24 * 60 * 60 },
  { label: '30 天', value: 30 * 24 * 60 * 60 },
  { label: '90 天', value: 90 * 24 * 60 * 60 },
  { label: '1 年', value: 365 * 24 * 60 * 60 }
]

// Stats
const stats = computed(() => {
  const total = apiKeys.value.length
  const active = apiKeys.value.filter(k => k.status === 'active').length
  const now = new Date()
  const expired = apiKeys.value.filter(k => k.expiresAt && new Date(k.expiresAt) < now).length
  return { total, active, expired }
})

// Columns
const columns = computed(() => [
  { title: 'ID', key: 'id', width: 60 },
  { title: '名称', key: 'name', width: 180 },
  { title: 'Key ID', key: 'keyId', width: 140, ellipsis: { tooltip: true } },
  { title: '权限', key: 'scopes', width: 160,
    render: (row) => h(NTag, { size: 'small', type: row.scopes === 'open_api:writer' ? 'success' : 'info' },
      () => row.scopes)
  },
  { title: '状态', key: 'status', width: 80,
    render: (row) => {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now
      if (isExpired) return h(NTag, { size: 'small', type: 'error' }, () => '已过期')
      return h(NTag, { size: 'small', type: row.status === 'active' ? 'success' : 'default' },
        () => row.status === 'active' ? '活跃' : '禁用')
    }
  },
  { title: '创建时间', key: 'createdAt', width: 170,
    render: (row) => row.createdAt ? new Date(row.createdAt).toLocaleString('zh-CN') : '-'
  },
  { title: '过期时间', key: 'expiresAt', width: 170,
    render: (row) => {
      if (!row.expiresAt) return '永不过期'
      return new Date(row.expiresAt).toLocaleString('zh-CN')
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    fixed: 'right',
    render(row) {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now

      return h(NSpace, { size: 'small' }, [
        h(NButton, {
          size: 'tiny',
          quaternary: true,
          type: isExpired || row.status === 'disabled' ? 'success' : 'warning',
          disabled: isExpired,
          onClick: () => toggleStatus(row)
        }, () => isExpired ? '已过期' : (row.status === 'active' ? '禁用' : '启用')),
        h(NPopconfirm, {
          onPositiveClick: () => confirmDelete(row)
        }, {
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, () => '删除'),
          default: () => '确定删除？'
        })
      ])
    }
  }
])

// Methods
async function loadApiKeys() {
  loading.value = true
  try {
    const res = await ApiKeyApi.list(pagination.value.page, pagination.value.pageSize)
    if (res.success) {
      apiKeys.value = res.data?.keys || []
      pagination.value.itemCount = res.data?.total || 0
    }
  } catch (err) {
    message.error(formatErrorMessage(err, '加载失败'))
  } finally {
    loading.value = false
  }
}

async function loadCurrentUser() {
  try {
    const res = await AuthApi.getUserInfo()
    if (res.success && res.data?.uuid) {
      // 获取用户列表来找到 userId
      const usersRes = await (await import('../api')).AdminApi.getUsers()
      if (usersRes.success) {
        const user = usersRes.data?.users?.find(u => u.uuid === res.data.uuid)
        if (user) {
          currentUserId.value = user.id
        }
      }
    }
  } catch (e) {
    console.error('获取当前用户失败', e)
  }
}

async function handleCreate() {
  try {
    await createFormRef.value?.validate()
  } catch {
    return
  }

  creating.value = true
  try {
    const res = await ApiKeyApi.create({
      name: createForm.value.name,
      userId: currentUserId.value || 1,
      scopes: createForm.value.scopes,
      expiresIn: createForm.value.expiresIn
    })
    if (res.success) {
      message.success('API Key 创建成功')
      showCreateModal.value = false
      rawKey.value = res.data?.rawKey || ''
      showRawKeyModal.value = true
      loadApiKeys()
      // 重置表单
      createForm.value = { name: '', scopes: 'open_api:reader', expiresIn: 0 }
    } else {
      message.error(res.message || '创建失败')
    }
  } catch (err) {
    message.error(formatErrorMessage(err, '创建失败'))
  } finally {
    creating.value = false
  }
}

function copyRawKey() {
  navigator.clipboard.writeText(rawKey.value).then(() => {
    message.success('已复制到剪贴板')
  }).catch(() => {
    message.error('复制失败')
  })
}

async function toggleStatus(row) {
  try {
    const newStatus = row.status === 'active' ? 'disabled' : 'active'
    const res = await ApiKeyApi.updateStatus(row.id, newStatus)
    if (res.success) {
      message.success(newStatus === 'active' ? '已启用' : '已禁用')
      loadApiKeys()
    } else {
      message.error(res.message || '操作失败')
    }
  } catch (err) {
    message.error(formatErrorMessage(err, '操作失败'))
  }
}

function confirmDelete(row) {
  deleteTarget.value = row
  showDeleteModal.value = true
}

async function handleDelete() {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    const res = await ApiKeyApi.delete(deleteTarget.value.id)
    if (res.success) {
      message.success('已删除')
      showDeleteModal.value = false
      loadApiKeys()
    } else {
      message.error(res.message || '删除失败')
    }
  } catch (err) {
    message.error(formatErrorMessage(err, '删除失败'))
  } finally {
    deleting.value = false
  }
}

function handlePageChange(page) {
  pagination.value.page = page
  loadApiKeys()
}

function handlePageSizeChange(size) {
  pagination.value.pageSize = size
  pagination.value.page = 1
  loadApiKeys()
}

onMounted(() => {
  loadApiKeys()
  loadCurrentUser()
})
</script>

<style scoped>
.admin-api-key {
  padding: 24px;
}
.header-section {
  margin-bottom: 20px;
}
.header-section h2 {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
}
.description {
  color: #888;
  margin: 4px 0 0;
  font-size: 14px;
}
.stat-card .stat-content {
  display: flex;
  align-items: center;
  gap: 12px;
}
.stat-value {
  font-size: 24px;
  font-weight: 700;
}
.stat-label {
  font-size: 14px;
  color: #888;
}
.field-hint {
  font-size: 12px;
  color: #888;
  line-height: 1.5;
}
</style>