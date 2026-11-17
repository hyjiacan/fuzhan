<template>
  <el-card class="auth-card">
    <template #header>
      <div class="card-head card-head--baseline">
        <span>LDAP 认证</span>
        <span class="card-sub">Active Directory / OpenLDAP</span>
      </div>
    </template>
    <el-form autocomplete="off" label-width="140px">
      <el-row :gutter="16">
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="服务器地址">
            <el-input autocomplete="off" v-model="settings.auth.ldap.host" placeholder="如 ldap.example.com" />
          </el-form-item>
        </el-col>
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="端口">
            <el-input-number v-model="settings.auth.ldap.port" :min="1" :max="65535" style="width: 100%;" />
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item label="使用 LDAPS">
        <el-switch v-model="settings.auth.ldap.useSSL" @change="handleUseSSLChange" />
        <div class="field-hint" style="margin-left: 12px;">勾选后走 TLS 加密（通常端口 636）</div>
      </el-form-item>

      <el-form-item label="跳过证书校验" v-if="settings.auth.ldap.useSSL">
        <el-switch v-model="settings.auth.ldap.insecureSkipVerify" />
        <div class="field-hint" style="margin-left: 12px;">仅自签名/测试环境开启（默认关闭，更安全）</div>
      </el-form-item>

      <el-form-item label="Base DN">
        <el-input autocomplete="off" v-model="settings.auth.ldap.baseDN" placeholder="如 dc=example,dc=com" />
      </el-form-item>

      <el-row :gutter="16">
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="绑定 DN">
            <el-input v-model="settings.auth.ldap.bindDN" placeholder="服务账号，仅用于目录查询，可留空" autocomplete="off" />
          </el-form-item>
        </el-col>
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="绑定密码">
            <el-input v-model="settings.auth.ldap.bindPassword" type="password" show-password placeholder="服务账号密码" autocomplete="new-password" />
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item label="用户过滤器">
        <el-input autocomplete="off" v-model="settings.auth.ldap.userFilter" placeholder="如 (uid=%s) 或 AD 的 (sAMAccountName=%s)，%s 为用户名" />
        <div class="field-hint">%s 会被替换为登录用户名；留空则默认 (uid=%s)</div>
      </el-form-item>

      <el-form-item label="用户名属性">
        <el-input autocomplete="off" v-model="settings.auth.ldap.usernameAttribute" placeholder="如 uid 或 sAMAccountName，留空按用户过滤器推导" />
        <div class="field-hint">本地账号名与目录同步取该属性；留空时从用户过滤器推导，回退 uid</div>
      </el-form-item>

      <el-row :gutter="16">
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="目录同步间隔(秒)">
            <el-input-number v-model="settings.auth.ldap.syncInterval" :min="60" :step="60" style="width: 100%;" />
          </el-form-item>
        </el-col>
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="自动创建本地用户">
            <el-switch v-model="settings.auth.ldap.autoCreateUser" />
            <div class="field-hint" style="margin-left: 12px;">认证通过后自动建立本地账户</div>
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item>
        <el-button type="warning" :loading="testing" @click="testConnection">测试连接</el-button>
        <span class="field-hint" v-if="testResult" style="margin-left: 12px;">{{ testResult }}</span>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ConfigApi } from '@/api'

const props = defineProps({
  settings: { type: Object, required: true }
})

const testing = ref(false)
const testResult = ref('')

// 切换 LDAPS 时同步默认端口：仅在当前端口是对应协议的默认值时联动，保留自定义端口
// 关闭 LDAPS 时清除“跳过证书校验”，避免非 TLS 连接下残留无效配置
const handleUseSSLChange = (useSSL) => {
  const ld = props.settings.auth.ldap
  if (useSSL && ld.port === 389) ld.port = 636
  if (!useSSL && ld.port === 636) ld.port = 389
  if (!useSSL) ld.insecureSkipVerify = false
}

const testConnection = async () => {
  const ld = props.settings.auth.ldap
  if (!ld.host) {
    ElMessage.warning('请先填写服务器地址')
    return
  }
  testing.value = true
  testResult.value = ''
  try {
    const res = await ConfigApi.testLdap({
      enabled: ld.enabled,
      host: ld.host,
      port: ld.port,
      use_ssl: ld.useSSL,
      base_dn: ld.baseDN,
      bind_dn: ld.bindDN,
      bind_password: ld.bindPassword,
      user_filter: ld.userFilter,
      username_attribute: ld.usernameAttribute,
      sync_interval: ld.syncInterval,
      auto_create_user: ld.autoCreateUser,
      insecure_skip_verify: ld.insecureSkipVerify
    })
    if (res.success) {
      testResult.value = res.message || '连接成功'
      ElMessage.success(testResult.value)
    } else {
      testResult.value = res.message || '连接测试失败'
      ElMessage.error(testResult.value)
    }
  } catch (e) {
    testResult.value = '连接测试失败：' + (e?.message || e)
    ElMessage.error(testResult.value)
  } finally {
    testing.value = false
  }
}
</script>