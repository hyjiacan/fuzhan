<template>
  <div class="admin-users-view page-container page-container--gap">
    <div class="header-section">
      <div class="header-left">
        <h2>用户管理</h2>
        <p class="description">管理系统用户，重置密码等操作</p>
      </div>
      <div class="header-right">
        <el-button @click="openConflictDialog">
          身份冲突
          <el-tag v-if="conflictCount > 0" type="danger" size="small" style="margin-left: 6px">{{ conflictCount }}</el-tag>
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="conflictCount > 0"
      type="warning"
      :closable="false"
      show-icon
      class="conflict-alert"
    >
      <template #title>
        <span>检测到 {{ conflictCount }} 个身份冲突待处理（外部账号与本地同名账号冲突，已拒绝登录）</span>
        <el-button type="warning" size="small" style="margin-left: 12px" @click="openConflictDialog">立即处理</el-button>
      </template>
    </el-alert>

    <el-card class="filter-card" shadow="never">
      <el-space wrap>
        <el-input v-model="searchQuery" :maxlength="200" placeholder="过滤用户名..." clearable style="width: 200px" @keydown.enter="onSearch" @clear="onSearch" />
        <el-select v-model="sourceFilter" placeholder="认证来源" clearable style="width: 140px" @change="onSearch">
          <el-option v-for="opt in AUTH_SOURCE_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
        <el-select v-model="createdViaFilter" placeholder="创建渠道" clearable style="width: 160px" @change="onSearch">
          <el-option v-for="opt in CREATED_VIA_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
        <el-select v-model="identityStateFilter" placeholder="身份状态" clearable style="width: 160px" @change="onSearch">
          <el-option v-for="opt in IDENTITY_STATE_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
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
        <el-form-item label="姓名" prop="displayName">
          <el-input
            v-model="createUserForm.displayName"
            :maxlength="64"
            placeholder="可选，如 张三"
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

    <el-dialog v-model="convertLocalModalVisible" title="转为本地账号" width="460px">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="转为本地后将删除该用户全部外部身份绑定，并改用新密码登录。"
        style="margin-bottom: 16px"
      />
      <el-form :model="convertLocalForm" label-width="90px">
        <el-form-item label="用户名">
          {{ convertLocalForm.username }}
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="convertLocalForm.newPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="8-128位"
          />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="convertLocalForm.confirmPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请再次输入新密码"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="convertLocalModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="convertingToLocal" @click="handleConvertToLocal">
            确认转为本地
          </el-button>
        </el-space>
      </template>
    </el-dialog>

    <el-dialog v-model="bindIdentityModalVisible" title="绑定外部身份" width="500px">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="绑定后该用户的本地密码将被清空，请确认认证源已启用且外部标识正确。"
        style="margin-bottom: 16px"
      />
      <el-form :model="bindIdentityForm" label-width="90px">
        <el-form-item label="用户名">
          {{ bindIdentityForm.username }}
        </el-form-item>
        <el-form-item label="认证类型" prop="authType">
          <el-select v-model="bindIdentityForm.authType" placeholder="请选择已启用的认证源" style="width: 100%">
            <el-option v-for="opt in AUTH_BIND_TYPE_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="外部标识" prop="externalId">
          <el-input
            v-model="bindIdentityForm.externalId"
            :maxlength="512"
            placeholder="必填，如 LDAP DN 或 OAuth2 用户标识"
          />
        </el-form-item>
        <el-form-item label="外部名称" prop="externalName">
          <el-input
            v-model="bindIdentityForm.externalName"
            :maxlength="128"
            placeholder="可选，如 张三"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="bindIdentityModalVisible = false">取消</el-button>
          <el-button type="primary" :loading="bindingIdentity" @click="handleBindIdentity">
            确认绑定
          </el-button>
        </el-space>
      </template>
    </el-dialog>

    <el-dialog v-model="conflictDialogVisible" title="身份冲突处理" width="900px">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="批量绑定会把同名本地账号切换为该外部身份（清空其本地密码），仅确认是同一人时使用。"
        style="margin-bottom: 12px"
      />
      <div class="conflict-toolbar">
        <el-button type="primary" :disabled="selectedConflictIds.length === 0" :loading="handlingConflicts" @click="handleBatchBind">
          批量绑定到同名账号
        </el-button>
        <el-button type="warning" plain :disabled="selectedConflictIds.length === 0" :loading="handlingConflicts" @click="handleBatchIgnore(selectedConflictIds)">
          批量忽略
        </el-button>
        <span class="conflict-selected">已选 {{ selectedConflictIds.length }} 条</span>
      </div>
      <el-table
        :data="conflicts"
        v-loading="conflictLoading"
        row-key="id"
        max-height="420"
        @selection-change="onConflictSelectionChange"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column prop="username" label="用户名" min-width="120" show-overflow-tooltip />
        <el-table-column label="认证源" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="AUTH_SOURCE_TAG_TYPES[row.auth_type] || 'info'">
              {{ formatAuthSource(row.auth_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="external_id" label="外部标识" min-width="180" show-overflow-tooltip />
        <el-table-column label="建议本地账号" min-width="130">
          <template #default="{ row }">
            <span v-if="row.suggested_username">{{ row.suggested_username }}</span>
            <span v-else class="conflict-none">无</span>
          </template>
        </el-table-column>
        <el-table-column label="最近尝试时间" width="170">
          <template #default="{ row }">{{ TimeUtils.formatDateTime(row.last_seen_at) }}</template>
        </el-table-column>
        <el-table-column prop="attempt_count" label="尝试次数" width="90" />
        <el-table-column label="操作" width="130">
          <template #default="{ row }">
            <el-button
              size="small"
              type="primary"
              link
              :disabled="!row.can_bind"
              @click="handleRowBind(row)"
            >绑定</el-button>
            <el-button size="small" type="warning" link @click="handleRowIgnore(row)">忽略</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-wrap" v-if="conflictTotal > 0">
        <el-pagination
          v-model:current-page="conflictPage"
          v-model:page-size="conflictPageSize"
          :total="conflictTotal"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          small
          @current-change="loadConflicts"
          @size-change="onConflictPageSizeChange"
        />
      </div>
      <el-alert
        v-if="conflictSummary"
        type="info"
        :closable="false"
        show-icon
        :title="conflictSummary"
        style="margin-top: 12px"
      />
      <el-collapse v-if="conflictIssueResults.length > 0" style="margin-top: 8px">
        <el-collapse-item title="查看跳过/失败原因" name="issues">
          <div v-for="item in conflictIssueResults" :key="item.id" class="conflict-issue">
            #{{ item.id }} {{ item.username || '-' }}：{{ item.message }}
            （{{ item.status === 'skipped' ? '跳过' : '失败' }}）
          </div>
        </el-collapse-item>
      </el-collapse>
      <template #footer>
        <el-space>
          <el-button @click="loadConflicts">刷新</el-button>
          <el-button @click="conflictDialogVisible = false">关闭</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, h } from 'vue'
import { ElMessage, ElMessageBox, ElButton, ElTag, ElProgress, ElTooltip } from 'element-plus'
import { AuthApi, AdminApi } from '@/api'
import { TableConst, TimeUtils, UserUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'

const RefreshIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M17.65 6.35C16.2 4.9 14.21 4 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08c-.82 2.33-3.04 4-5.65 4-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z' })
])
const PlusIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' })
])

const loading = ref(false)
const users = ref([])
const searchQuery = ref('')
const sourceFilter = ref('')
const createdViaFilter = ref('')
const identityStateFilter = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

// 认证来源选项：与后端 auth_source 取值保持一致
const AUTH_SOURCE_OPTIONS = [
  { value: 'local', label: '本地' },
  { value: 'ldap', label: 'LDAP' },
  { value: 'oauth2', label: 'OAuth2' }
]

// 可绑定的外部认证类型：provider 由后端按当前启用的认证源决定，前端不可手填
const AUTH_BIND_TYPE_OPTIONS = AUTH_SOURCE_OPTIONS.filter(opt => opt.value !== 'local')

// 创建渠道选项：与后端 created_via 取值保持一致
const CREATED_VIA_OPTIONS = [
  { value: 'self_register', label: '自助注册' },
  { value: 'admin_create', label: '管理员创建' },
  { value: 'ldap_jit', label: 'LDAP 自动创建' },
  { value: 'ldap_sync', label: 'LDAP 同步' },
  { value: 'oauth2_jit', label: 'OAuth2 自动创建' },
  { value: 'legacy', label: '历史数据' }
]

// 身份状态选项：identity_state 由后端派生，同时用于筛选与列表展示
const IDENTITY_STATE_OPTIONS = [
  { value: 'ok', label: '正常' },
  { value: 'orphaned', label: '认证源已停用' },
  { value: 'provider_mismatch', label: '认证源已变更' },
  { value: 'inconsistent', label: '身份数据异常' }
]

const AUTH_SOURCE_LABELS = Object.fromEntries(AUTH_SOURCE_OPTIONS.map(opt => [opt.value, opt.label]))
const CREATED_VIA_LABELS = Object.fromEntries(CREATED_VIA_OPTIONS.map(opt => [opt.value, opt.label]))
const IDENTITY_STATE_LABELS = Object.fromEntries(IDENTITY_STATE_OPTIONS.map(opt => [opt.value, opt.label]))
const AUTH_SOURCE_TAG_TYPES = { local: 'info', ldap: 'warning', oauth2: 'success' }
// 身份状态标签类型：ok 用普通标签，异常状态用警示色
const IDENTITY_STATE_TAG_TYPES = { ok: 'info', orphaned: 'warning', provider_mismatch: 'warning', inconsistent: 'danger' }
// 异常身份的说明：仅在非 ok 状态展示 tooltip
const IDENTITY_STATE_TIPS = {
  orphaned: '认证源已停用：该账号所属认证源当前未启用或配置不完整，可转为本地账号',
  provider_mismatch: '认证源已变更：绑定记录中的 provider 与当前配置不一致',
  inconsistent: '身份数据异常：账号归属与绑定记录不满足一致性约束，需管理员修复'
}

// 认证来源展示名：未知值原样显示，空值显示为占位符
const formatAuthSource = (value) => AUTH_SOURCE_LABELS[value] || value || '-'
// 创建方式展示名：未知值原样显示，空值显示为占位符
const formatCreatedVia = (value) => CREATED_VIA_LABELS[value] || value || '未知'
// 身份状态展示名：未知或空值显示为占位符
const formatIdentityState = (value) => IDENTITY_STATE_LABELS[value] || '-'
const createUserModalVisible = ref(false)
const creatingUser = ref(false)
const createFormRef = ref(null)
const createUserForm = ref({
  username: '',
  displayName: '',
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
// 转为本地账号弹窗
const convertLocalModalVisible = ref(false)
const convertingToLocal = ref(false)
const convertLocalForm = ref({
  uuid: '',
  username: '',
  newPassword: '',
  confirmPassword: ''
})
// 绑定外部身份弹窗
const bindIdentityModalVisible = ref(false)
const bindingIdentity = ref(false)
const bindIdentityForm = ref({
  uuid: '',
  username: '',
  authType: '',
  externalId: '',
  externalName: ''
})
// 身份冲突：常驻入口 + 批量处置弹窗
const conflictCount = ref(0)
const conflictDialogVisible = ref(false)
const conflictLoading = ref(false)
const handlingConflicts = ref(false)
const conflicts = ref([])
const conflictTotal = ref(0)
const conflictPage = ref(1)
const conflictPageSize = ref(20)
const selectedConflictIds = ref([])
const conflictSummary = ref('')
const conflictIssueResults = ref([])

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

// 重置密码仅本地账号可用：非本地账号置灰，并用 tooltip 提示先转为本地
const renderResetPasswordAction = (row) => {
  const isLocal = row.auth_source === 'local'
  return h(ElTooltip, {
    content: '请先将该用户转为本地账号',
    disabled: isLocal,
    placement: 'top'
  }, {
    // 禁用的按钮不触发鼠标事件，需外包一层元素承载 tooltip
    default: () => h('span', { style: 'display: inline-flex;' }, [
      h(ElButton, {
        size: 'small',
        type: 'primary',
        link: true,
        disabled: !isLocal,
        onClick: () => showResetPasswordModal(row)
      }, () => '重置密码')
    ])
  })
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
    title: '姓名',
    key: 'display_name',
    minWidth: 120,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { title: row.display_name || '' }, row.display_name || '-')
  },
  {
    title: '昵称',
    key: 'nickname',
    minWidth: 120,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { title: row.nickname || '' }, row.nickname || '-')
  },
  {
    title: '认证来源',
    key: 'auth_source',
    width: 140,
    cellRenderer: ({ rowData: row }) => h(ElTooltip, {
      content: `创建方式：${formatCreatedVia(row.created_via)}`,
      placement: 'top'
    }, {
      default: () => h(ElTag, {
        size: 'small',
        type: AUTH_SOURCE_TAG_TYPES[row.auth_source] || 'info'
      }, () => formatAuthSource(row.auth_source))
    })
  },
  {
    title: '身份状态',
    key: 'identity_state',
    width: 140,
    cellRenderer: ({ rowData: row }) => {
      const state = row.identity_state
      if (!state || !IDENTITY_STATE_LABELS[state]) return h('span', '-')
      const tag = h(ElTag, {
        size: 'small',
        type: IDENTITY_STATE_TAG_TYPES[state] || 'info'
      }, () => formatIdentityState(state))
      const tip = IDENTITY_STATE_TIPS[state]
      return tip
        ? h(ElTooltip, { content: tip, placement: 'top' }, { default: () => tag })
        : tag
    }
  },
  {
    title: '账号状态',
    key: 'disabled',
    width: 90,
    cellRenderer: ({ rowData: row }) => {
      const type = row.disabled ? 'danger' : 'success'
      return h(ElTag, { type, size: 'small' }, () => row.disabled ? '禁用' : '正常')
    }
  },
  {
    title: '注册时间',
    key: 'created_at',
    width: 180,
    cellRenderer: ({ rowData: row }) => TimeUtils.formatDateTime(row.created_at)
  },
  {
    title: '配额使用',
    key: 'quota',
    width: 200,
    cellRenderer: ({ rowData: row }) => {
      if (!row.quota) return h('span', { style: 'color: var(--el-text-color-secondary); font-size: 12px' }, '无限制')
      const used = row.used_storage || 0
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
    width: 380,
    cellRenderer: ({ rowData: row }) => h('div', { style: 'display: flex; gap: 8px; align-items: center;' }, [
      h(ElButton, {
        size: 'small',
        type: row.disabled ? 'success' : 'warning',
        link: true,
        onClick: () => toggleUserDisabled(row)
      }, () => row.disabled ? '启用' : '禁用'),
      renderResetPasswordAction(row),
      h(ElButton, {
        size: 'small',
        type: 'primary',
        link: true,
        onClick: () => showConvertLocalModal(row)
      }, () => '转为本地'),
      h(ElButton, {
        size: 'small',
        type: 'primary',
        link: true,
        onClick: () => showBindIdentityModal(row)
      }, () => '绑定身份'),
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
  // 同步刷新待处理冲突数量，用于顶部提醒与常驻入口角标
  loadConflictCount()
  try {
    const data = await AdminApi.getUsers(page.value, pageSize.value, searchQuery.value, {
      source: sourceFilter.value,
      createdVia: createdViaFilter.value,
      identityState: identityStateFilter.value
    })
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
    displayName: '',
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
      createUserForm.value.displayName.trim(),
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
  // 仅本地账号可重置密码，非本地账号需先转为本地
  if (user.auth_source !== 'local') {
    ElMessage.warning('请先将该用户转为本地账号')
    return
  }
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
  if (resetPasswordForm.value.newPassword.length < 8 || resetPasswordForm.value.newPassword.length > 128) {
    ElMessage.error('密码长度需为8-128位')
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
    // 后端限制非本地账号重置（400），保留原文案便于管理员按提示操作
    ElMessage.error(formatErrorMessage(error, '密码重置失败'))
  } finally {
    resettingPassword.value = false
  }
}

const showConvertLocalModal = (user) => {
  convertLocalForm.value = {
    uuid: user.uuid,
    username: user.username,
    newPassword: '',
    confirmPassword: ''
  }
  convertLocalModalVisible.value = true
}

const handleConvertToLocal = async () => {
  const { uuid, username, newPassword, confirmPassword } = convertLocalForm.value
  if (newPassword.length < 8 || newPassword.length > 128) {
    ElMessage.error('密码长度需为8-128位')
    return
  }
  if (newPassword !== confirmPassword) {
    ElMessage.error('两次输入的密码不一致')
    return
  }

  // 高风险操作：二次确认后再提交
  try {
    await ElMessageBox.confirm(
      `确定将用户 "${username}" 转为本地账号吗？该操作会删除其全部外部身份绑定。`,
      '确认转为本地账号',
      { type: 'warning', confirmButtonText: '转为本地', cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  convertingToLocal.value = true
  try {
    const data = await AdminApi.convertToLocal(uuid, newPassword)
    if (data.success) {
      ElMessage.success('已转为本地账号')
      convertLocalModalVisible.value = false
      loadUsers()
    } else {
      ElMessage.error(data.message || '转为本地账号失败')
    }
  } catch (error) {
    ElMessage.error(formatErrorMessage(error, '转为本地账号失败'))
  } finally {
    convertingToLocal.value = false
  }
}

const showBindIdentityModal = (user) => {
  bindIdentityForm.value = {
    uuid: user.uuid,
    username: user.username,
    authType: '',
    externalId: '',
    externalName: ''
  }
  bindIdentityModalVisible.value = true
}

const handleBindIdentity = async () => {
  const { uuid, username, authType, externalId, externalName } = bindIdentityForm.value
  if (!authType) {
    ElMessage.error('请选择认证类型')
    return
  }
  if (!externalId.trim()) {
    ElMessage.error('请输入外部标识')
    return
  }
  const typeLabel = AUTH_SOURCE_LABELS[authType] || authType

  // 高风险操作：二次确认后再提交
  try {
    await ElMessageBox.confirm(
      `确定将用户 "${username}" 绑定到 ${typeLabel} 身份吗？绑定后将清空其本地密码。`,
      '确认绑定外部身份',
      { type: 'warning', confirmButtonText: '绑定', cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  bindingIdentity.value = true
  try {
    const data = await AdminApi.bindAuthIdentity(uuid, authType, externalId.trim(), externalName.trim())
    if (data.success) {
      ElMessage.success('外部身份绑定成功')
      bindIdentityModalVisible.value = false
      loadUsers()
    } else {
      ElMessage.error(data.message || '绑定外部身份失败')
    }
  } catch (error) {
    // 409（已绑定/来源冲突）、400（认证源未启用等）展示后端原文案
    ElMessage.error(formatErrorMessage(error, '绑定外部身份失败'))
  } finally {
    bindingIdentity.value = false
  }
}

// 加载待处理身份冲突数量（page_size=1，仅取 total）
const loadConflictCount = async () => {
  try {
    const data = await AdminApi.getIdentityConflicts({ status: 'pending', page: 1, page_size: 1 })
    if (data.success) {
      conflictCount.value = data.data?.total || 0
    }
  } catch (error) {
    console.error(error)
  }
}

const loadConflicts = async () => {
  conflictLoading.value = true
  try {
    const data = await AdminApi.getIdentityConflicts({
      status: 'pending',
      page: conflictPage.value,
      page_size: conflictPageSize.value
    })
    if (data.success) {
      conflicts.value = (data.data && data.data.conflicts) || []
      conflictTotal.value = data.data?.total || 0
    } else {
      ElMessage.error(data.message || '获取身份冲突失败')
    }
  } catch (error) {
    ElMessage.error('获取身份冲突失败')
    console.error(error)
  } finally {
    conflictLoading.value = false
  }
}

const openConflictDialog = () => {
  conflictDialogVisible.value = true
  conflictPage.value = 1
  conflictSummary.value = ''
  conflictIssueResults.value = []
  selectedConflictIds.value = []
  loadConflicts()
}

const onConflictPageSizeChange = () => {
  conflictPage.value = 1
  loadConflicts()
}

const onConflictSelectionChange = (rows) => {
  selectedConflictIds.value = rows.map(row => row.id)
}

// 汇总批量处理结果：成功/跳过/失败数量，并保留跳过与失败明细
const applyConflictResults = (results) => {
  const bound = results.filter(item => item.status === 'bound').length
  const skipped = results.filter(item => item.status === 'skipped').length
  const failed = results.filter(item => item.status === 'failed').length
  conflictSummary.value = `成功 ${bound} 条，跳过 ${skipped} 条，失败 ${failed} 条`
  conflictIssueResults.value = results.filter(item => item.status !== 'bound')
}

const runBatchBind = async (ids) => {
  handlingConflicts.value = true
  try {
    const data = await AdminApi.batchBindIdentityConflicts(ids)
    if (data.success) {
      applyConflictResults((data.data && data.data.results) || [])
      ElMessage.success(conflictSummary.value)
      selectedConflictIds.value = []
      await Promise.all([loadConflicts(), loadConflictCount()])
      loadUsers()
    } else {
      ElMessage.error(data.message || '批量绑定失败')
    }
  } catch (error) {
    // 400/403 等业务错误展示后端原文案
    ElMessage.error(formatErrorMessage(error, '批量绑定失败'))
  } finally {
    handlingConflicts.value = false
  }
}

const handleBatchBind = async () => {
  const ids = [...selectedConflictIds.value]
  if (ids.length === 0) return
  try {
    await ElMessageBox.confirm(
      `确定将选中的 ${ids.length} 个外部身份绑定到同名本地账号吗？该操作会清空这些账号的本地密码，且不会自动回退。`,
      '确认批量绑定',
      { type: 'warning', confirmButtonText: '绑定', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  runBatchBind(ids)
}

const handleRowBind = async (row) => {
  if (!row.can_bind) {
    ElMessage.warning('该冲突的认证源未启用，无法绑定')
    return
  }
  try {
    await ElMessageBox.confirm(
      `确定将外部身份 "${row.external_id}" 绑定到本地账号 "${row.username}" 吗？绑定后将清空该账号的本地密码。`,
      '确认绑定',
      { type: 'warning', confirmButtonText: '绑定', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  runBatchBind([row.id])
}

const handleBatchIgnore = async (ids) => {
  if (!ids || ids.length === 0) return
  try {
    await ElMessageBox.confirm(
      `确定忽略选中的 ${ids.length} 个身份冲突吗？忽略后不再出现在待处理列表，外部账号再次尝试登录时会重新出现。`,
      '确认忽略',
      { type: 'warning', confirmButtonText: '忽略', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  handlingConflicts.value = true
  try {
    const data = await AdminApi.batchIgnoreIdentityConflicts([...ids])
    if (data.success) {
      const ignored = data.data?.ignored || 0
      ElMessage.success(`已忽略 ${ignored} 条身份冲突`)
      selectedConflictIds.value = []
      await Promise.all([loadConflicts(), loadConflictCount()])
    } else {
      ElMessage.error(data.message || '批量忽略失败')
    }
  } catch (error) {
    ElMessage.error(formatErrorMessage(error, '批量忽略失败'))
  } finally {
    handlingConflicts.value = false
  }
}

const handleRowIgnore = (row) => {
  handleBatchIgnore([row.id])
}

const toggleUserDisabled = async (user) => {
  const action = user.disabled ? '启用' : '禁用'
  try {
    await ElMessageBox.confirm(`确定要${action}用户 "${UserUtils.displayLabel(user)}" 吗？`, `确认${action}`, {
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
    await ElMessageBox.confirm(`确定要删除用户 "${UserUtils.displayLabel(user)}" 吗？此操作不可恢复。`, '确认删除', {
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
  .header-section {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
  }

  .conflict-alert {
    margin-bottom: 0;
  }

  .conflict-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
  }

  .conflict-selected {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }

  .conflict-none {
    color: var(--el-text-color-secondary);
  }

  .conflict-issue {
    font-size: 13px;
    color: var(--el-text-color-regular);
    line-height: 1.8;
  }

  .table-card {
    .table-v2-wrap {
      height: 480px;
    }
  }
}
</style>
