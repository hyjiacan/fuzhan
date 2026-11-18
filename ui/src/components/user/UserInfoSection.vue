<template>
  <div class="user-info-section">
    <div class="header-section">
      <div class="header-left">
        <h2>个人信息</h2>
        <p class="description">查看并修改您的账号信息</p>
      </div>
    </div>

    <el-card class="info-card">
      <el-descriptions :column="1">
        <el-descriptions-item label="用户类型">
          <el-tag :type="userInfo.role === 'admin' ? 'info' : 'success'">
            {{ userInfo.role === 'admin' ? '管理员' : '普通用户' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="用户名">
          {{ userInfo.username }}
        </el-descriptions-item>
        <el-descriptions-item label="姓名">
          <span v-if="editingField === 'display_name'" class="inline-edit">
            <el-input
              ref="editInputRef"
              v-model="editValue"
              size="small"
              :maxlength="64"
              placeholder="用于对外显示，可留空"
              @keyup.enter="handleSaveField"
              @keyup.esc="cancelEdit"
            />
            <el-button size="small" type="primary" :loading="savingProfile" @click="handleSaveField">
              保存
            </el-button>
            <el-button size="small" :disabled="savingProfile" @click="cancelEdit">取消</el-button>
          </span>
          <span v-else class="inline-view">
            <span>{{ userInfo.display_name || '-' }}</span>
            <el-button link type="primary" :icon="Edit" title="编辑姓名" @click="startEdit('display_name')" />
          </span>
        </el-descriptions-item>
        <el-descriptions-item label="昵称">
          <span v-if="editingField === 'nickname'" class="inline-edit">
            <el-input
              ref="editInputRef"
              v-model="editValue"
              size="small"
              :maxlength="32"
              placeholder="优先级最高的显示名，可留空"
              @keyup.enter="handleSaveField"
              @keyup.esc="cancelEdit"
            />
            <el-button size="small" type="primary" :loading="savingProfile" @click="handleSaveField">
              保存
            </el-button>
            <el-button size="small" :disabled="savingProfile" @click="cancelEdit">取消</el-button>
          </span>
          <span v-else class="inline-view">
            <span>{{ userInfo.nickname || '-' }}</span>
            <el-button link type="primary" :icon="Edit" title="编辑昵称" @click="startEdit('nickname')" />
          </span>
        </el-descriptions-item>
        <el-descriptions-item label="用户ID">
          {{ userInfo.uuid }}
        </el-descriptions-item>
        <el-descriptions-item label="注册时间">
          {{ formatDate(userInfo.created_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <div class="account-actions">
      <el-button type="primary" :icon="Lock" @click="passwordDialogVisible = true">
        修改密码
      </el-button>
      <el-button
        v-if="userInfo.role !== 'admin'"
        type="danger"
        plain
        :icon="Delete"
        @click="deleteDialogVisible = true"
      >
        注销账户
      </el-button>
    </div>

    <el-dialog
      v-model="passwordDialogVisible"
      title="修改密码"
      width="480px"
      :close-on-click-modal="false"
      @closed="resetPasswordForm"
    >
      <el-form ref="formRef" :model="passwordForm" :rules="passwordRules" label-width="96">
        <el-form-item label="当前密码" prop="oldPassword">
          <el-input
            v-model="passwordForm.oldPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请输入当前密码"
            autocomplete="current-password"
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="passwordForm.newPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请输入新密码"
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="passwordForm.confirmPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请再次输入新密码"
            autocomplete="new-password"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="changingPassword" @click="passwordDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="changingPassword" @click="handleChangePassword">
          确认修改
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="deleteDialogVisible"
      title="注销账户"
      width="480px"
      :close-on-click-modal="false"
      @closed="resetDeleteForm"
    >
      <p class="danger-desc">
        注销后账号将无法恢复，您的私有文件与临时文件将被永久删除；登录记录与操作记录会作为审计数据保留。
      </p>
      <el-form ref="deleteFormRef" :model="deleteForm" :rules="deleteRules" label-width="96">
        <el-form-item label="当前密码" prop="password">
          <el-input
            v-model="deleteForm.password"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请输入当前密码以确认注销"
            autocomplete="current-password"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="deletingAccount" @click="deleteDialogVisible = false">取消</el-button>
        <el-button type="danger" :loading="deletingAccount" @click="handleDeleteAccount">
          注销账户
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, nextTick, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Edit, Lock, Delete } from '@element-plus/icons-vue'
import { AuthApi } from '@/api'
import { TimeUtils } from '@/utils'

const router = useRouter()

const userInfo = ref({
  username: '',
  display_name: '',
  nickname: '',
  uuid: '',
  role: 'user',
  created_at: ''
})

const formatDate = (dateStr) => TimeUtils.formatDateTime(dateStr)

// 行内编辑：一次只编辑一个字段（display_name / nickname）
const editingField = ref('')
const editValue = ref('')
const editInputRef = ref(null)
const savingProfile = ref(false)

const startEdit = async (field) => {
  editingField.value = field
  editValue.value = userInfo.value[field] || ''
  await nextTick()
  editInputRef.value?.focus()
}

const cancelEdit = () => {
  editingField.value = ''
  editValue.value = ''
}

const handleSaveField = async () => {
  const field = editingField.value
  if (!field) return

  const value = editValue.value.trim()
  const limits = { display_name: 64, nickname: 32 }
  if (value.length > limits[field]) {
    ElMessage.warning(field === 'display_name' ? '姓名不能超过64个字符' : '昵称不能超过32个字符')
    return
  }

  savingProfile.value = true
  try {
    const displayName = field === 'display_name' ? value : (userInfo.value.display_name || '')
    const nickname = field === 'nickname' ? value : (userInfo.value.nickname || '')
    const data = await AuthApi.updateProfile(displayName, nickname)
    if (data.success) {
      ElMessage.success('资料已更新')
      cancelEdit()
      await loadUserInfo()
    } else {
      ElMessage.error(data.message || '资料更新失败')
    }
  } catch (error) {
    ElMessage.error(error?.message || '资料更新失败')
  } finally {
    savingProfile.value = false
  }
}

const loadUserInfo = async () => {
  try {
    const data = await AuthApi.getUserInfo()
    if (data.success && data.data) {
      userInfo.value = data.data
      localStorage.setItem('userRole', data.data.role || 'user')
      // 同步本地展示名缓存，保证顶栏等处展示一致
      AuthApi.restoreSession(data.data)
      window.dispatchEvent(new CustomEvent('auth:changed'))
    }
  } catch (error) {
    console.error('获取用户信息失败:', error)
    ElMessage.error('获取用户信息失败')
  }
}

const formRef = ref(null)
const changingPassword = ref(false)
const passwordDialogVisible = ref(false)

const passwordForm = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const resetPasswordForm = () => {
  passwordForm.oldPassword = ''
  passwordForm.newPassword = ''
  passwordForm.confirmPassword = ''
  formRef.value?.clearValidate()
}

const validatePasswordSame = (rule, value) => {
  if (value !== passwordForm.newPassword) {
    return new Error('两次输入的密码不一致')
  }
  return true
}

const passwordRules = {
  oldPassword: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
  newPassword: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    { min: 6, message: '密码至少6位', trigger: 'blur' }
  ],
  confirmPassword: [
    { required: true, message: '请确认新密码', trigger: 'blur' },
    { validator: validatePasswordSame, trigger: 'blur' }
  ]
}

const handleChangePassword = async () => {
  try {
    await formRef.value?.validate()
  } catch {
    return
  }

  changingPassword.value = true
  try {
    const data = await AuthApi.changePassword(
      passwordForm.oldPassword,
      passwordForm.newPassword
    )
    if (data.success) {
      ElMessage.success('密码修改成功')
      passwordDialogVisible.value = false
    } else {
      ElMessage.error(data.message || '密码修改失败')
    }
  } catch (error) {
    ElMessage.error('密码修改失败')
    console.error(error)
  } finally {
    changingPassword.value = false
  }
}

onMounted(() => {
  loadUserInfo()
})

// 注销账户：双重确认 + 当前密码校验
const deleteFormRef = ref(null)
const deletingAccount = ref(false)
const deleteDialogVisible = ref(false)
const deleteForm = reactive({ password: '' })
const deleteRules = {
  password: [{ required: true, message: '请输入当前密码', trigger: 'blur' }]
}

const resetDeleteForm = () => {
  deleteForm.password = ''
  deleteFormRef.value?.clearValidate()
}

const handleDeleteAccount = async () => {
  try {
    await deleteFormRef.value?.validate()
  } catch {
    return
  }

  try {
    await ElMessageBox.confirm(
      '注销后账号无法恢复，您的私有文件与临时文件将被永久删除，确定继续吗？',
      '注销账户',
      { type: 'warning', confirmButtonText: '确定注销', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' }
    )
  } catch {
    return
  }

  deletingAccount.value = true
  try {
    const data = await AuthApi.deleteAccount(deleteForm.password)
    if (data.success) {
      ElMessage.success('账号已注销')
      deleteDialogVisible.value = false
      AuthApi.clearAuth()
      window.dispatchEvent(new CustomEvent('auth:changed'))
      router.push({ name: 'login' })
    } else {
      ElMessage.error(data.message || '注销失败')
    }
  } catch (error) {
    ElMessage.error(error?.message || '注销失败')
  } finally {
    deletingAccount.value = false
  }
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-info-section {
  padding: @container-padding;

  .info-card {
    max-width: 600px;
  }

  .account-actions {
    display: flex;
    gap: 12px;
    max-width: 600px;
    margin-top: 16px;
  }

  .inline-view {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    vertical-align: middle;

    .el-button {
      padding: 0;
      height: auto;
      vertical-align: middle;
    }
  }

  .inline-edit {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    vertical-align: middle;

    .el-input {
      width: 220px;
    }
  }
}

// 弹框内容会被 teleport 到 body，不能挂在 .user-info-section 下
.danger-desc {
  margin: 0 0 16px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}
</style>
