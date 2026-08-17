<template>
  <div class="user-password-section">
    <div class="header-section">
      <h2>修改密码</h2>
      <p class="description">修改您的登录密码</p>
    </div>

    <n-card class="password-card" style="max-width: 500px;">
      <n-form ref="formRef" :model="passwordForm" :rules="passwordRules" label-placement="left" label-width="120">
        <n-form-item label="当前密码" path="oldPassword">
          <n-input
            v-model:value="passwordForm.oldPassword"
            :maxlength="128"
            type="password"
            show-password-on="click"
            placeholder="请输入当前密码"
            autocomplete="current-password"
          />
        </n-form-item>
        <n-form-item label="新密码" path="newPassword">
          <n-input
            v-model:value="passwordForm.newPassword"
            :maxlength="128"
            type="password"
            show-password-on="click"
            placeholder="请输入新密码"
            autocomplete="new-password"
          />
        </n-form-item>
        <n-form-item label="确认密码" path="confirmPassword">
          <n-input
            v-model:value="passwordForm.confirmPassword"
            :maxlength="128"
            type="password"
            show-password-on="click"
            placeholder="请再次输入新密码"
            autocomplete="new-password"
          />
        </n-form-item>
        <n-form-item>
          <n-button type="primary" :loading="changingPassword" @click="handleChangePassword">
            确认修改
          </n-button>
        </n-form-item>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import {
  NCard, NForm, NFormItem, NInput, NButton, useMessage
} from 'naive-ui'
import { AuthApi } from '@/api'

const message = useMessage()

const formRef = ref(null)
const changingPassword = ref(false)

const passwordForm = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

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
      message.success('密码修改成功')
      passwordForm.oldPassword = ''
      passwordForm.newPassword = ''
      passwordForm.confirmPassword = ''
    } else {
      message.error(data.message || '密码修改失败')
    }
  } catch (error) {
    message.error('密码修改失败')
    console.error(error)
  } finally {
    changingPassword.value = false
  }
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-password-section {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

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
}
</style>
