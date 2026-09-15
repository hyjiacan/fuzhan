<template>
  <div class="admin-api-key">
    <div class="header-section">
      <h2>API Key 管理</h2>
      <p class="description">管理 Open API 访问密钥，用于外部程序访问</p>
    </div>

    <!-- 统计卡片 -->
    <el-row :gutter="16" style="margin-bottom: 16px;">
      <el-col :span="8">
        <el-card class="stat-card" shadow="never">
          <div class="stat-content">
            <div class="stat-value" style="color: #1890ff;">{{ stats.active }}</div>
            <div class="stat-label">活跃 Key</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stat-card" shadow="never">
          <div class="stat-content">
            <div class="stat-value" style="color: #faad14;">{{ stats.total }}</div>
            <div class="stat-label">总计</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stat-card" shadow="never">
          <div class="stat-content">
            <div class="stat-value" style="color: #f5222d;">{{ stats.expired }}</div>
            <div class="stat-label">已过期</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 操作栏 -->
    <el-space style="margin-bottom: 16px;">
      <el-button type="primary" @click="showCreateModal = true">
        <el-icon class="el-icon--left"><component :is="AddIcon" /></el-icon>
        创建 API Key
      </el-button>
      <el-button @click="loadApiKeys" :loading="loading">刷新</el-button>
    </el-space>

    <!-- API Key 列表 -->
    <el-card shadow="never">
      <div ref="tableWrapRef" class="table-v2-wrap">
        <el-table-v2
          :columns="columns"
          :data="apiKeys"
          :width="tableWidth"
          :height="tableHeight"
          row-key="id"
        :row-height="32" />
        <div v-if="loading" class="table-loading-mask">
          <el-icon class="is-loading" :size="22"><Loading /></el-icon>
        </div>
      </div>
      <div style="display: flex; justify-content: flex-end; margin-top: 16px;">
        <el-pagination
          v-if="pagination.itemCount > 0"
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.itemCount"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next, jumper"
          @current-change="handlePageChange"
          @size-change="handlePageSizeChange"
        />
      </div>
    </el-card>

    <!-- 创建 API Key 弹窗 -->
    <el-dialog v-model="showCreateModal" title="创建 API Key" width="500px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="100px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="createForm.name" :maxlength="128" placeholder="给这个 Key 起个名字" />
        </el-form-item>
        <el-form-item label="权限范围" prop="scopes">
          <el-select v-model="createForm.scopes" placeholder="选择权限范围" style="width: 100%;">
            <el-option
              v-for="opt in scopeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
          <span class="field-hint">open_api:reader - 读取文件列表、搜索、下载<br>
            open_api:writer - 额外包含文件备注、依赖关系</span>
        </el-form-item>
        <el-form-item label="过期时间" prop="expiresIn">
          <el-select v-model="createForm.expiresIn" placeholder="选择过期时间" style="width: 100%;">
            <el-option
              v-for="opt in expiryOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="showCreateModal = false">取消</el-button>
          <el-button type="primary" :loading="creating" @click="handleCreate">创建</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 显示新创建的 Key 弹窗 -->
    <el-dialog v-model="showRawKeyModal" title="API Key 已创建" width="600px">
      <el-alert type="warning" :show-icon="false">
        请立即复制保存此 Key，它只会显示这一次！
      </el-alert>
      <el-input
        :model-value="rawKey"
        type="textarea"
        readonly
        :rows="3"
        style="margin-top: 16px; font-family: monospace;"
      />
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="copyRawKey">复制</el-button>
          <el-button type="primary" @click="showRawKeyModal = false">关闭</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 确认删除对话框 -->
    <el-dialog v-model="showDeleteModal" title="确认删除" width="400px">
      <el-alert type="error" :closable="false">
        确定要删除 API Key「{{ deleteTarget?.name }}」吗？此操作不可恢复。
      </el-alert>
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <el-button @click="showDeleteModal = false">取消</el-button>
          <el-button type="danger" :loading="deleting" @click="handleDelete">删除</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox, ElTag, ElButton, ElPopconfirm } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { ApiKeyApi, AuthApi } from '../api'
import { TimeUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'

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

// Columns (el-table-v2)
const columns = [
  { key: 'id', dataKey: 'id', title: 'ID', width: 60 },
  { key: 'name', dataKey: 'name', title: '名称', minWidth: 180, flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { class: 'file-name-cell', title: row.name || '' }, [
      h('span', { class: 'file-link' }, row.name || '-')
    ]) },
  { key: 'keyId', dataKey: 'keyId', title: 'Key ID', width: 140 },
  {
    key: 'scopes', title: '权限', width: 160,
    cellRenderer: ({ rowData: row }) => h(ElTag, { size: 'small', type: row.scopes === 'open_api:writer' ? 'success' : 'info' },
      () => row.scopes)
  },
  {
    key: 'status', title: '状态', width: 80,
    cellRenderer: ({ rowData: row }) => {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now
      if (isExpired) return h(ElTag, { size: 'small', type: 'danger' }, () => '已过期')
      return h(ElTag, { size: 'small', type: row.status === 'active' ? 'success' : 'info' },
        () => row.status === 'active' ? '活跃' : '禁用')
    }
  },
  {
    key: 'createdAt', title: '创建时间', width: 170,
    cellRenderer: ({ rowData: row }) => row.createdAt ? TimeUtils.formatDateTime(row.createdAt) : '-'
  },
  {
    key: 'expiresAt', title: '过期时间', width: 170,
    cellRenderer: ({ rowData: row }) => {
      if (!row.expiresAt) return '永不过期'
      return TimeUtils.formatDateTime(row.expiresAt)
    }
  },
  {
    key: 'actions',
    title: '操作',
    width: 200,
    fixed: 'right',
    cellRenderer: ({ rowData: row }) => {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now

      return h('div', { style: 'display:flex;align-items:center;gap:8px;' }, [
        h(ElButton, {
          size: 'small',
          link: true,
          type: isExpired || row.status === 'disabled' ? 'success' : 'warning',
          disabled: isExpired,
          onClick: () => toggleStatus(row)
        }, () => isExpired ? '已过期' : (row.status === 'active' ? '禁用' : '启用')),
        h(ElPopconfirm, {
          title: '确定删除？',
          confirmButtonText: '删除',
          cancelButtonText: '取消',
          width: 160,
          onConfirm: () => confirmDelete(row)
        }, {
          reference: () => h(ElButton, { size: 'small', link: true, type: 'danger' }, () => '删除')
        })
      ])
    }
  }
]

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
    ElMessage.error(formatErrorMessage(err, '加载失败'))
  } finally {
    loading.value = false
  }
}

async function loadCurrentUser() {
  try {
    const res = await AuthApi.getUserInfo()
    if (res.success && res.data?.uuid) {
      // 获取用户列表来找到 userId
      const usersRes = await (await import('../api')).AdminApi.getUsers(1, 1000)
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
      ElMessage.success('API Key 创建成功')
      showCreateModal.value = false
      rawKey.value = res.data?.rawKey || ''
      showRawKeyModal.value = true
      loadApiKeys()
      // 重置表单
      createForm.value = { name: '', scopes: 'open_api:reader', expiresIn: 0 }
    } else {
      ElMessage.error(res.message || '创建失败')
    }
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '创建失败'))
  } finally {
    creating.value = false
  }
}

function copyRawKey() {
  navigator.clipboard.writeText(rawKey.value).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

async function toggleStatus(row) {
  try {
    const newStatus = row.status === 'active' ? 'disabled' : 'active'
    const res = await ApiKeyApi.updateStatus(row.id, newStatus)
    if (res.success) {
      ElMessage.success(newStatus === 'active' ? '已启用' : '已禁用')
      loadApiKeys()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '操作失败'))
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
      ElMessage.success('已删除')
      showDeleteModal.value = false
      loadApiKeys()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '删除失败'))
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
.stat-card :deep(.el-card__body),
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
.table-v2-wrap {
  position: relative;
  height: 480px;
  width: 100%;
}
.table-loading-mask {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.6);
  z-index: 5;
}
</style>