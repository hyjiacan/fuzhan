<template>
  <div class="settings-auth">
    <el-tabs v-model="activeTab" class="auth-tabs">
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
}
</style>