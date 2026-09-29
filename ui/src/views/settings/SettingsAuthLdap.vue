<template>
  <el-card class="auth-card">
    <template #header>
      <div class="card-head">
        <span>LDAP 认证</span>
        <span class="card-sub">Active Directory / OpenLDAP</span>
      </div>
    </template>
    <el-form label-width="140px">
      <el-form-item label="启用 LDAP">
        <el-switch v-model="settings.auth.ldap.enabled" />
        <div class="field-hint" style="margin-left: 12px;">启用后登录先走 LDAP 认证，失败再回退本地密码</div>
      </el-form-item>

      <el-row :gutter="16">
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="服务器地址">
            <el-input v-model="settings.auth.ldap.host" placeholder="如 ldap.example.com" />
          </el-form-item>
        </el-col>
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="端口">
            <el-input-number v-model="settings.auth.ldap.port" :min="1" :max="65535" style="width: 100%;" />
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item label="使用 LDAPS">
        <el-switch v-model="settings.auth.ldap.useSSL" />
        <div class="field-hint" style="margin-left: 12px;">勾选后走 TLS 加密（通常端口 636）</div>
      </el-form-item>

      <el-form-item label="Base DN">
        <el-input v-model="settings.auth.ldap.baseDN" placeholder="如 dc=example,dc=com" />
      </el-form-item>

      <el-row :gutter="16">
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="绑定 DN">
            <el-input v-model="settings.auth.ldap.bindDN" placeholder="服务账号，仅用于目录查询，可留空" />
          </el-form-item>
        </el-col>
        <el-col :span="12" :sm="12" :xs="24">
          <el-form-item label="绑定密码">
            <el-input v-model="settings.auth.ldap.bindPassword" type="password" show-password placeholder="服务账号密码" />
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item label="用户过滤器">
        <el-input v-model="settings.auth.ldap.userFilter" placeholder="如 (uid=%s) 或 AD 的 (sAMAccountName=%s)，%s 为用户名" />
        <div class="field-hint">%s 会被替换为登录用户名；留空则默认 (uid=%s)</div>
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

      <el-form-item label="跳过证书校验">
        <el-switch v-model="settings.auth.ldap.insecureSkipVerify" />
        <div class="field-hint" style="margin-left: 12px;">仅自签名/测试环境开启（默认关闭，更安全）</div>
      </el-form-item>

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
      useSSL: ld.useSSL,
      baseDN: ld.baseDN,
      bindDN: ld.bindDN,
      bindPassword: ld.bindPassword,
      userFilter: ld.userFilter,
      syncInterval: ld.syncInterval,
      autoCreateUser: ld.autoCreateUser,
      insecureSkipVerify: ld.insecureSkipVerify
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
}
</style>