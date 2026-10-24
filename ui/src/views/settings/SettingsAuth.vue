<template>
  <div class="settings-auth">
    <el-tabs v-model="activeTab" class="auth-tabs">
      <el-tab-pane name="account">
        <template #label>
          <span class="tab-label">账户</span>
        </template>
        <el-row :gutter="16">
          <el-col :span="12" :sm="12" :xs="24">
            <el-card>
              <template #header><span>访问策略</span></template>
              <el-form label-width="120px">
                <el-form-item label="强制登录">
                  <el-switch v-model="settings.account.requireLogin" />
                  <div class="field-hint">
                    开启后 Web 界面（含精简版 lite）与 CLI 接口均需登录后才能访问与下载；私有分享的匿名直接下载、临时文件分享除外。保存后立即生效，无需重启。
                  </div>
                </el-form-item>
                <el-form-item label="允许自行注册">
                  <el-switch v-model="settings.account.allowRegistration" />
                  <div class="field-hint">
                    关闭后登录页不再显示「立即注册」入口，服务端同时拒绝注册请求；保存后立即生效，无需重启。
                  </div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>
      <el-tab-pane name="ldap">
        <template #label>
          <span class="tab-label">LDAP</span>
        </template>
        <SettingsAuthLdap :settings="settings" />
      </el-tab-pane>
      <el-tab-pane name="oauth2">
        <template #label>
          <span class="tab-label">OAuth2 / OIDC</span>
        </template>
        <SettingsAuthOAuth2 :settings="settings" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import SettingsAuthLdap from './SettingsAuthLdap.vue'
import SettingsAuthOAuth2 from './SettingsAuthOAuth2.vue'

defineProps({
  settings: { type: Object, required: true }
})

const activeTab = ref('ldap')
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.settings-auth {
  .auth-tabs {
    :deep(.el-tabs__header) { margin-bottom: 16px; }
  }

  .tab-label { font-size: 14px; }

  .field-hint { color: @text-color-secondary; font-size: 12px; }
}
</style>