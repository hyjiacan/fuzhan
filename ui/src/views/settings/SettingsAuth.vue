<template>
  <div class="settings-auth">
    <el-card class="auth-card">
      <template #header><span>本地账户</span></template>
      <el-form autocomplete="off" label-width="120px">
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
        <el-form-item label="登录验证码">
          <el-radio-group v-model="settings.account.captcha.mode">
            <el-radio label="disabled">关闭</el-radio>
            <el-radio label="always">始终开启</el-radio>
            <el-radio label="after_n">失败后开启</el-radio>
          </el-radio-group>
          <div class="field-hint">
            开启后登录/注册需输入图形验证码，防止暴力尝试；验证码由服务端按 IP 判定并一次性校验。
          </div>
        </el-form-item>
        <el-form-item v-if="settings.account.captcha.mode === 'after_n'" label="触发阈值">
          <div style="display: flex; align-items: center; gap: 12px;">
            <el-input-number v-model="settings.account.captcha.triggerAfter" :min="1" :max="20" />
            <span>次</span>
          </div>
          <div class="field-hint">同一 IP 登录/注册失败达到该次数后，即要求输入验证码</div>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="auth-card">
      <template #header>
        <div class="card-head">
          <span>第三方身份认证</span>
          <span class="card-sub">选择一种外部身份系统接入</span>
        </div>
      </template>
      <el-form autocomplete="off" label-width="120px">
        <el-form-item label="接入方式">
          <el-select v-model="provider" class="provider-select">
            <el-option label="不启用（仅本地账户）" value="none" />
            <el-option label="LDAP / Active Directory" value="ldap" />
            <el-option label="OAuth2 / OIDC" value="oauth2" />
          </el-select>
          <div class="field-hint">
            同一时间只接入一种第三方认证系统；本地账户始终保留作为回退。
          </div>
        </el-form-item>
      </el-form>
    </el-card>

    <SettingsAuthLdap v-if="provider === 'ldap'" :settings="settings" />
    <SettingsAuthOAuth2 v-else-if="provider === 'oauth2'" :settings="settings" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import SettingsAuthLdap from './SettingsAuthLdap.vue'
import SettingsAuthOAuth2 from './SettingsAuthOAuth2.vue'

const props = defineProps({
  settings: { type: Object, required: true }
})

// 当前接入的第三方认证系统：由两个启用开关推导，选择时互斥启用
const provider = computed({
  get() {
    if (props.settings.auth.ldap.enabled) return 'ldap'
    if (props.settings.auth.oauth2.enabled) return 'oauth2'
    return 'none'
  },
  set(value) {
    props.settings.auth.ldap.enabled = value === 'ldap'
    props.settings.auth.oauth2.enabled = value === 'oauth2'
  }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.settings-auth {
  .auth-card { margin-bottom: 16px; }

  .card-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .card-sub { color: @text-color-secondary; font-size: 12px; }

  .provider-select { width: 260px; }

  .field-hint { color: @text-color-secondary; font-size: 12px; }
}
</style>
