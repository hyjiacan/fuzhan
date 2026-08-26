<template>
  <div class="admin-openapi">
    <div class="header-section">
      <h2>Open API 管理</h2>
      <p class="description">管理 Open API 服务配置和访问密钥</p>
    </div>

    <!-- OpenAPI 服务配置 -->
    <el-card header="服务配置" style="margin-bottom: 16px;">
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form label-width="120px">
            <el-form-item label="启用 Open API">
              <el-switch v-model="openApiConfig.enabled" />
              <div class="field-hint">开启后可通过 /api/open/v1 端点提供文件访问 API</div>
            </el-form-item>
          </el-form>
        </el-col>
        <el-col :span="12">
          <el-form label-width="120px">
            <el-form-item label="启用频率限制">
              <el-switch v-model="openApiConfig.rateLimitEnabled" />
              <div class="field-hint">限制 API 调用频率，防止滥用</div>
            </el-form-item>
          </el-form>
        </el-col>
        <el-col :span="12">
          <el-form label-width="120px">
            <el-form-item label="访问模式">
              <el-radio-group v-model="openApiConfig.ipAccessMode">
                <el-radio value="allow">白名单模式</el-radio>
                <el-radio value="deny">黑名单模式</el-radio>
                <el-radio value="none">不限制</el-radio>
              </el-radio-group>
              <div class="field-hint">白名单和黑名单不能同时生效</div>
            </el-form-item>
          </el-form>
        </el-col>
        <el-col :span="12">
          <el-form label-width="120px" v-if="openApiConfig.rateLimitEnabled">
            <el-form-item label="请求频率">
              <el-input-number v-model="openApiConfig.requestsPerMinute" :min="1" :max="10000" />
              <span>次/分钟</span>
            </el-form-item>
          </el-form>
        </el-col>
        <el-col :span="12" v-if="openApiConfig.ipAccessMode !== 'none'">
          <el-form label-width="120px">
            <el-form-item :label="openApiConfig.ipAccessMode === 'allow' ? 'IP 白名单' : 'IP 黑名单'">
              <el-input
                v-model="ipAccessListDisplay"
                type="textarea"
                :placeholder="openApiConfig.ipAccessMode === 'allow' ? '每行一个 IP 或 CIDR，如 192.168.1.0/24' : '每行一个 IP 或 CIDR'"
                :rows="4"
              />
            </el-form-item>
          </el-form>
        </el-col>
      </el-row>
      <el-space style="margin-top: 16px;">
        <el-button type="primary" :loading="savingConfig" @click="saveConfig">保存配置</el-button>
      </el-space>
    </el-card>

    <!-- API Key 管理 -->
    <el-card header="API Key 管理">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <span>API Key 管理</span>
          <el-space>
            <el-button size="small" @click="loadApiKeys" :loading="loading">刷新</el-button>
            <el-button size="small" type="primary" @click="showCreateModal = true">
              <el-icon><component :is="AddIcon" /></el-icon>
              创建 Key
            </el-button>
          </el-space>
        </div>
      </template>

      <div ref="tableWrapRef" class="table-v2-wrap" v-loading="loading">
        <el-table-v2
          :columns="columns"
          :data="apiKeys"
          :width="tableWidth"
          :height="tableHeight"
          :row-height="32"
          row-key="id"
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
          <el-select v-model="createForm.scopes" placeholder="选择权限范围">
            <el-option
              v-for="opt in scopeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
          <div class="field-hint">open_api:reader - 读取文件列表、搜索、下载<br>open_api:writer - 额外包含文件备注、依赖关系</div>
        </el-form-item>
        <el-form-item label="过期时间" prop="expiresIn">
          <el-select v-model="createForm.expiresIn" placeholder="选择过期时间">
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
        <el-space>
          <el-button @click="showCreateModal = false">取消</el-button>
          <el-button type="primary" :loading="creating" @click="handleCreate">创建</el-button>
        </el-space>
      </template>
    </el-dialog>

    <!-- 显示新创建的 Key 弹窗 -->
    <el-dialog v-model="showRawKeyModal" title="API Key 已创建" width="600px">
      <el-alert type="warning" :closable="false">
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
        <el-space>
          <el-button @click="copyRawKey">复制</el-button>
          <el-button type="primary" @click="showRawKeyModal = false">关闭</el-button>
        </el-space>
      </template>
    </el-dialog>

    <!-- 确认删除对话框 -->
    <el-dialog v-model="showDeleteModal" title="确认删除" width="400px">
      <el-alert type="error" :closable="false">
        确定要删除 API Key「{{ deleteTarget?.name }}」吗？此操作不可恢复。
      </el-alert>
      <template #footer>
        <el-space>
          <el-button @click="showDeleteModal = false">取消</el-button>
          <el-button type="danger" :loading="deleting" @click="handleDelete">删除</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElButton, ElTag } from 'element-plus'
import { ApiKeyApi, ConfigApi, AuthApi } from '@/api'
import { TimeUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'

// Icons
const AddIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' })
])

// State
const loading = ref(false)
const creating = ref(false)
const deleting = ref(false)
const savingConfig = ref(false)
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

const openApiConfig = ref({
  enabled: false,
  ipAccessMode: 'none',
  ipWhitelist: '',
  ipBlacklist: '',
  rateLimitEnabled: true,
  requestsPerMinute: 60
})

const ipAccessListDisplay = computed({
  get: () => {
    if (openApiConfig.value.ipAccessMode === 'allow') return openApiConfig.value.ipWhitelist
    if (openApiConfig.value.ipAccessMode === 'deny') return openApiConfig.value.ipBlacklist
    return ''
  },
  set: (val) => {
    if (openApiConfig.value.ipAccessMode === 'allow') {
      openApiConfig.value.ipWhitelist = val
    } else if (openApiConfig.value.ipAccessMode === 'deny') {
      openApiConfig.value.ipBlacklist = val
    }
  }
})

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

// Columns
const columns = computed(() => [
  { title: '名称', key: 'name', minWidth: 180, flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { class: 'file-name-cell', title: row.name || '' }, [
      h('span', { class: 'file-link' }, row.name || '-')
    ]) },
  { title: 'Key ID', key: 'keyId', dataKey: 'keyId', width: 140 },
  {
    title: '权限', key: 'scopes', width: 160,
    cellRenderer: ({ rowData: row }) => h(ElTag, { size: 'small', type: row.scopes === 'open_api:writer' ? 'success' : 'info' },
      () => row.scopes)
  },
  {
    title: '状态', key: 'status', width: 80,
    cellRenderer: ({ rowData: row }) => {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now
      if (isExpired) return h(ElTag, { size: 'small', type: 'danger' }, () => '已过期')
      return h(ElTag, { size: 'small', type: row.status === 'active' ? 'success' : 'info' },
        () => row.status === 'active' ? '活跃' : '禁用')
    }
  },
  {
    title: '创建时间', key: 'createdAt', width: 170,
    cellRenderer: ({ rowData: row }) => row.createdAt ? TimeUtils.formatDateTime(row.createdAt) : '-'
  },
  {
    title: '过期时间', key: 'expiresAt', width: 170,
    cellRenderer: ({ rowData: row }) => {
      if (!row.expiresAt) return '永不过期'
      return TimeUtils.formatDateTime(row.expiresAt)
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    fixed: 'right',
    cellRenderer: ({ rowData: row }) => {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now

      return h('div', { style: 'display: flex; gap: 8px;' }, [
        h(ElButton, {
          size: 'small',
          link: true,
          type: isExpired || row.status === 'disabled' ? 'success' : 'warning',
          disabled: isExpired,
          onClick: () => toggleStatus(row)
        }, () => isExpired ? '已过期' : (row.status === 'active' ? '禁用' : '启用')),
        h(ElButton, { size: 'small', link: true, type: 'danger', onClick: () => confirmDelete(row) }, () => '删除')
      ])
    }
  }
])

// Methods
async function loadConfig() {
  try {
    const data = await ConfigApi.get()
    if (data.success && data.data) {
      const cfg = data.data.openApi || {}
      openApiConfig.value = {
        enabled: cfg.enabled ?? false,
        ipAccessMode: cfg.ipAccessMode || 'none',
        ipWhitelist: (cfg.ipWhitelist || []).join('\n'),
        ipBlacklist: (cfg.ipBlacklist || []).join('\n'),
        rateLimitEnabled: cfg.rateLimitEnabled ?? true,
        requestsPerMinute: cfg.requestsPerMinute || 60
      }
    }
  } catch (e) {
    console.error('加载配置失败', e)
  }
}

async function saveConfig() {
  savingConfig.value = true
  try {
    const data = await ConfigApi.save({
      openApi: {
        enabled: openApiConfig.value.enabled,
        ipAccessMode: openApiConfig.value.ipAccessMode,
        ipWhitelist: openApiConfig.value.ipWhitelist.split('\n').filter(ip => ip.trim()),
        ipBlacklist: openApiConfig.value.ipBlacklist.split('\n').filter(ip => ip.trim()),
        rateLimitEnabled: openApiConfig.value.rateLimitEnabled,
        requestsPerMinute: openApiConfig.value.requestsPerMinute
      }
    })
    if (data.success) {
      ElMessage.success('配置已保存')
      // 更新 store 中的 openApiEnabled
      store.setConfig({ openApiEnabled: openApiConfig.value.enabled })
    } else {
      ElMessage.error(data.message || '保存失败')
    }
  } catch (e) {
    ElMessage.error(formatErrorMessage(e, '保存失败'))
  } finally {
    savingConfig.value = false
  }
}

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
      const usersRes = await (await import('@/api')).AdminApi.getUsers()
      if (usersRes.success) {
        const user = usersRes.data?.users?.find(u => u.uuid === res.data.uuid)
        if (user) currentUserId.value = user.id
      }
    }
  } catch (e) {
    console.error('获取当前用户失败', e)
  }
}

async function handleCreate() {
  try { await createFormRef.value?.validate() } catch { return }

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

onMounted(() => {
  loadConfig()
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

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-openapi {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 24px;

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

  .field-hint {
    font-size: 12px;
    color: @text-color-secondary;
    line-height: 1.5;
  }

  .table-v2-wrap {
    height: 420px;
  }
}

@media @tablet {
  .admin-openapi {
    padding: 12px;
  }
}

@media @mobile {
  .admin-openapi {
    padding: 8px;
  }
}
</style>