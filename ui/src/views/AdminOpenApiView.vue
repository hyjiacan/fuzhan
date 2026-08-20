<template>
  <div class="admin-openapi">
    <div class="header-section">
      <h2>Open API 管理</h2>
      <p class="description">管理 Open API 服务配置和访问密钥</p>
    </div>

    <!-- OpenAPI 服务配置 -->
    <n-card title="服务配置" style="margin-bottom: 16px;">
      <n-grid :cols="2" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-form label-placement="left" label-width="120">
            <n-form-item label="启用 Open API">
              <n-switch v-model:value="openApiConfig.enabled" />
              <template #feedback>
                <span class="field-hint">开启后可通过 /api/open/v1 端点提供文件访问 API</span>
              </template>
            </n-form-item>
          </n-form>
        </n-gi>
        <n-gi>
          <n-form label-placement="left" label-width="120">
            <n-form-item label="启用频率限制">
              <n-switch v-model:value="openApiConfig.rateLimitEnabled" />
              <template #feedback>
                <span class="field-hint">限制 API 调用频率，防止滥用</span>
              </template>
            </n-form-item>
          </n-form>
        </n-gi>
        <n-gi>
          <n-form label-placement="left" label-width="120">
            <n-form-item label="访问模式">
              <n-radio-group v-model:value="openApiConfig.ipAccessMode">
                <n-radio value="allow">白名单模式</n-radio>
                <n-radio value="deny">黑名单模式</n-radio>
                <n-radio value="none">不限制</n-radio>
              </n-radio-group>
              <template #feedback>
                <span class="field-hint">白名单和黑名单不能同时生效</span>
              </template>
            </n-form-item>
          </n-form>
        </n-gi>
        <n-gi>
          <n-form label-placement="left" label-width="120" v-if="openApiConfig.rateLimitEnabled">
            <n-form-item label="请求频率">
              <n-space>
                <n-input-number v-model:value="openApiConfig.requestsPerMinute" :min="1" :max="10000" />
                <span>次/分钟</span>
              </n-space>
            </n-form-item>
          </n-form>
        </n-gi>
        <n-gi v-if="openApiConfig.ipAccessMode !== 'none'">
          <n-form label-placement="left" label-width="120">
            <n-form-item :label="openApiConfig.ipAccessMode === 'allow' ? 'IP 白名单' : 'IP 黑名单'">
              <n-input
                v-model:value="ipAccessListDisplay"
                type="textarea"
                :placeholder="openApiConfig.ipAccessMode === 'allow' ? '每行一个 IP 或 CIDR，如 192.168.1.0/24' : '每行一个 IP 或 CIDR'"
                :rows="4"
              />
            </n-form-item>
          </n-form>
        </n-gi>
      </n-grid>
      <n-space justify="end" style="margin-top: 16px;">
        <n-button type="primary" :loading="savingConfig" @click="saveConfig">保存配置</n-button>
      </n-space>
    </n-card>

    <!-- API Key 管理 -->
    <n-card title="API Key 管理">
      <template #header-extra>
        <n-space>
          <n-button size="small" @click="loadApiKeys" :loading="loading">刷新</n-button>
          <n-button size="small" type="primary" @click="showCreateModal = true">
            <template #icon><n-icon><AddIcon /></n-icon></template>
            创建 Key
          </n-button>
        </n-space>
      </template>

      <n-data-table
        :columns="columns"
        :data="apiKeys"
        :loading="loading"
        :pagination="pagination"
        :row-key="row => row.id"
        :bordered="false"
        size="small"
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
            <span class="field-hint">open_api:reader - 读取文件列表、搜索、下载<br>open_api:writer - 额外包含文件备注、依赖关系</span>
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
  NButton, NCard, NDataTable, NForm, NFormItem, NInput, NInputNumber,
  NModal, NSelect, NSpace, NIcon, NGrid, NGi, NAlert, NTag, useMessage,
  NPopconfirm, NSwitch, NRadioGroup, NRadio
} from 'naive-ui'
import { ApiKeyApi, ConfigApi, AuthApi } from '@/api'
import { TimeUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'

const message = useMessage()

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

// Columns
const columns = computed(() => [
  { title: '名称', key: 'name', width: 180 },
  { title: 'Key ID', key: 'keyId', width: 140, ellipsis: { tooltip: true } },
  {
    title: '权限', key: 'scopes', width: 160,
    render: (row) => h(NTag, { size: 'small', type: row.scopes === 'open_api:writer' ? 'success' : 'info' },
      () => row.scopes)
  },
  {
    title: '状态', key: 'status', width: 80,
    render: (row) => {
      const now = new Date()
      const isExpired = row.expiresAt && new Date(row.expiresAt) < now
      if (isExpired) return h(NTag, { size: 'small', type: 'error' }, () => '已过期')
      return h(NTag, { size: 'small', type: row.status === 'active' ? 'success' : 'default' },
        () => row.status === 'active' ? '活跃' : '禁用')
    }
  },
  {
    title: '创建时间', key: 'createdAt', width: 170,
    render: (row) => row.createdAt ? TimeUtils.formatDateTime(row.createdAt) : '-'
  },
  {
    title: '过期时间', key: 'expiresAt', width: 170,
    render: (row) => {
      if (!row.expiresAt) return '永不过期'
      return TimeUtils.formatDateTime(row.expiresAt)
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
      message.success('配置已保存')
      // 更新 store 中的 openApiEnabled
      store.setConfig({ openApiEnabled: openApiConfig.value.enabled })
    } else {
      message.error(data.message || '保存失败')
    }
  } catch (e) {
    message.error(formatErrorMessage(e, '保存失败'))
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
    message.error(formatErrorMessage(err, '加载失败'))
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
      message.success('API Key 创建成功')
      showCreateModal.value = false
      rawKey.value = res.data?.rawKey || ''
      showRawKeyModal.value = true
      loadApiKeys()
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

onMounted(() => {
  loadConfig()
  loadApiKeys()
  loadCurrentUser()
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