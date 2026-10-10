<template>
  <el-card class="auth-card">
    <template #header>
      <div class="card-head">
        <span>OAuth2 / OIDC 认证</span>
        <span class="card-sub">对接外部统一认证系统（授权码模式）</span>
      </div>
    </template>
    <el-form label-width="150px">
      <el-form-item label="启用外部认证">
        <el-switch v-model="settings.auth.oauth2.enabled" />
        <div class="field-hint" style="margin-left: 12px;">启用后登录页展示外部认证入口，认证成功即映射为本地用户</div>
      </el-form-item>

      <el-form-item label="提供方名称">
        <el-input v-model="settings.auth.oauth2.providerName" placeholder="如 企业 SSO / GitHub，用于登录页按钮文案" />
      </el-form-item>

      <el-row :gutter="16">
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="Client ID">
            <el-input v-model="settings.auth.oauth2.clientId" placeholder="外部系统分配的客户端 ID" />
          </el-form-item>
        </el-col>
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="Client Secret">
            <el-input v-model="settings.auth.oauth2.clientSecret" type="password" show-password placeholder="客户端密钥" />
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item label="OIDC Issuer">
        <el-input v-model="settings.auth.oauth2.issuer" placeholder="如 https://sso.example.com（可选）" />
        <div class="field-hint">配置后自动发现授权/令牌/用户信息端点，可留空下面的三个端点</div>
      </el-form-item>

      <el-form-item label="授权端点">
        <el-input v-model="settings.auth.oauth2.authUrl" placeholder="如 https://sso.example.com/oauth2/authorize" />
      </el-form-item>

      <el-form-item label="令牌端点">
        <el-input v-model="settings.auth.oauth2.tokenUrl" placeholder="如 https://sso.example.com/oauth2/token" />
      </el-form-item>

      <el-form-item label="用户信息端点">
        <el-input v-model="settings.auth.oauth2.userInfoUrl" placeholder="如 https://sso.example.com/oauth2/userinfo" />
        <div class="field-hint">留空时回退解析 id_token 载荷（需提供方返回 id_token）</div>
      </el-form-item>

      <el-form-item label="Scopes">
        <el-input
          v-model="scopesDisplay"
          type="textarea"
          :rows="2"
          placeholder="每行一个，如 openid / profile / email"
        />
        <div class="field-hint">留空默认请求 openid profile email</div>
      </el-form-item>

      <el-form-item label="回调地址">
        <el-input v-model="settings.auth.oauth2.redirectUrl" :placeholder="defaultCallback" />
        <div class="field-hint">
          需与外部系统登记的回调地址一致；留空时按当前访问域名推断为
          <code>{{ defaultCallback }}</code>
          <el-button link type="primary" size="small" @click="copyCallback">复制</el-button>
        </div>
      </el-form-item>

      <el-divider content-position="left">用户映射</el-divider>

      <el-row :gutter="16">
        <el-col :span="8" :sm="8" :xs="24">
          <el-form-item label="用户名取值字段">
            <el-input v-model="settings.auth.oauth2.usernameClaim" placeholder="preferred_username" />
          </el-form-item>
        </el-col>
        <el-col :span="8" :sm="8" :xs="24">
          <el-form-item label="显示名取值字段">
            <el-input v-model="settings.auth.oauth2.displayNameClaim" placeholder="name" />
          </el-form-item>
        </el-col>
        <el-col :span="8" :sm="8" :xs="24">
          <el-form-item label="邮箱取值字段">
            <el-input v-model="settings.auth.oauth2.emailClaim" placeholder="email" />
          </el-form-item>
        </el-col>
      </el-row>
      <div class="field-hint" style="margin: -6px 0 12px 150px;">
        用户名回退链：配置字段 → 邮箱前缀 → sub
      </div>

      <el-form-item label="自动创建本地用户">
        <el-switch v-model="settings.auth.oauth2.autoCreateUser" />
        <div class="field-hint" style="margin-left: 12px;">关闭时仅允许已存在的本地用户通过外部认证登录</div>
      </el-form-item>

      <el-form-item label="跳过证书校验">
        <el-switch v-model="settings.auth.oauth2.insecureSkipVerify" />
        <div class="field-hint" style="margin-left: 12px;">仅自签名/测试环境开启（默认关闭，更安全）</div>
      </el-form-item>

      <el-form-item>
        <el-button type="warning" :loading="testing" @click="testConnection">测试配置</el-button>
        <span class="field-hint" v-if="testResult" style="margin-left: 12px;">{{ testResult }}</span>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { ConfigApi } from '@/api'
import { copyToClipboard } from '@/utils/clipboard'

const props = defineProps({
  settings: { type: Object, required: true }
})

// 默认回调地址：当前域名 + 后端回调路径
const defaultCallback = computed(() => `${window.location.origin}/api/v1/auth/oauth2/callback`)

// Scopes 数组 <-> 多行文本
const scopesDisplay = computed({
  get: () => (props.settings.auth.oauth2.scopes || []).join('\n'),
  set: (val) => {
    props.settings.auth.oauth2.scopes = val.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean)
  }
})

const testing = ref(false)
const testResult = ref('')

const buildPayload = () => {
  const o = props.settings.auth.oauth2
  return {
    enabled: o.enabled,
    providerName: o.providerName,
    clientId: o.clientId,
    clientSecret: o.clientSecret,
    authUrl: o.authUrl,
    tokenUrl: o.tokenUrl,
    userInfoUrl: o.userInfoUrl,
    issuer: o.issuer,
    scopes: o.scopes || [],
    redirectUrl: o.redirectUrl,
    usernameClaim: o.usernameClaim,
    displayNameClaim: o.displayNameClaim,
    emailClaim: o.emailClaim,
    autoCreateUser: o.autoCreateUser,
    insecureSkipVerify: o.insecureSkipVerify
  }
}

const copyCallback = async () => {
  const ok = await copyToClipboard(defaultCallback.value)
  if (ok) {
    ElMessage.success('回调地址已复制')
  } else {
    ElMessage.warning('复制失败，请手动复制')
  }
}

const testConnection = async () => {
  const o = props.settings.auth.oauth2
  if (!o.clientId || !o.clientSecret) {
    ElMessage.warning('请先填写 Client ID 与 Client Secret')
    return
  }
  testing.value = true
  testResult.value = ''
  try {
    const res = await ConfigApi.testOAuth2(buildPayload())
    if (res.success) {
      testResult.value = res.message || '配置测试成功'
      ElMessage.success(testResult.value)
    } else {
      testResult.value = res.message || '配置测试失败'
      ElMessage.error(testResult.value)
    }
  } catch (e) {
    testResult.value = '配置测试失败：' + (e?.message || e)
    ElMessage.error(testResult.value)
  } finally {
    testing.value = false
  }
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.auth-card {
  :deep(.el-card__header) { padding: 14px 18px; }
}

.card-head {
  display: flex;
  align-items: baseline;
  gap: 10px;

  .card-sub { font-size: 12px; color: @text-color-secondary; }
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;

  code {
    padding: 1px 4px;
    border-radius: 3px;
    background: var(--el-fill-color-light);
  }
}
</style>