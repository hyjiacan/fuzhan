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
        <div class="card-head card-head--baseline">
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
            同一时间只接入一种第三方认证系统。
          </div>
          <div class="field-hint">
            LDAP 用户仅通过 LDAP 登录，不再回退本地密码；本地管理员账号作为应急入口。
          </div>
          <div class="field-hint">
            停用认证源后，已有账号会标记为待处理，可在用户管理中转为本地账号。
          </div>
        </el-form-item>
      </el-form>
    </el-card>

    <SettingsAuthLdap v-if="provider === 'ldap'" :settings="settings" />
    <SettingsAuthOAuth2 v-else-if="provider === 'oauth2'" :settings="settings" />

    <el-card class="auth-card">
      <template #header>
        <div class="card-head card-head--baseline">
          <span>同名冲突处理</span>
          <span class="card-sub">外部账号与本地同名账号冲突时的处理方式</span>
        </div>
      </template>
      <el-form autocomplete="off" label-width="120px">
        <el-form-item label="处理方式">
          <el-radio-group v-model="settings.auth.identityConflictMode" class="conflict-mode-group">
            <el-radio label="self_service">
              <div class="conflict-mode">
                <span class="conflict-mode__title">用户自行处理（推荐）</span>
                <span class="field-hint conflict-mode__hint">
                  外部账号与本地同名账号冲突时，用户可输入本地密码确认绑定，或使用独立账号登录。
                </span>
              </div>
            </el-radio>
            <el-radio label="admin">
              <div class="conflict-mode">
                <span class="conflict-mode__title">管理员处理</span>
                <span class="field-hint conflict-mode__hint">
                  冲突登录被拒绝并进入待处理列表，由管理员在用户管理中绑定或忽略。
                </span>
              </div>
            </el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
    </el-card>
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

  .provider-select { width: 260px; }

  .conflict-mode-group {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;

    .el-radio {
      align-items: flex-start;
      height: auto;
      margin-right: 0;
      white-space: normal;
    }

    .conflict-mode {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    .conflict-mode__title { font-weight: 500; }

    .conflict-mode__hint {
      display: block;
      margin-top: 2px;
    }
  }
}
</style>
