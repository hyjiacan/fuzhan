<template>
  <el-card>
    <el-alert type="warning" :show-icon="false" :closable="false" class="db-migration-alert">
      <template #title>切换数据库配置需手动迁移数据</template>
      <div>修改数据库类型或地址后，若要保留已有数据，请使用 <code>dbswitch</code> 等外部迁移工具手动迁移数据，本站不提供内置迁移。配置保存后需重启服务方可生效。</div>
    </el-alert>

    <el-divider />

    <el-form :model="settings" label-width="120px">
      <el-form-item label="数据库类型">
        <el-radio-group v-model="settings.database.driver">
          <el-radio label="sqlite">SQLite</el-radio>
          <el-radio label="mysql">MySQL</el-radio>
          <el-radio label="postgres">PostgreSQL</el-radio>
        </el-radio-group>
        <div class="field-hint">
          <strong>SQLite</strong>：轻量级，文件存储，适合小型部署<br>
          <strong>MySQL</strong>：适合大规模应用，需要 MySQL 5.7+<br>
          <strong>PostgreSQL</strong>：功能丰富，适合企业级应用，需要 PostgreSQL 10+
        </div>
      </el-form-item>

      <el-form-item v-if="settings.database.driver === 'sqlite'" label="数据库文件" prop="database.dsn">
        <el-input v-model="settings.database.dsn" :maxlength="1024" placeholder="fuzhan.db" />
        <div class="field-hint">SQLite 数据库文件路径</div>
      </el-form-item>

      <template v-if="settings.database.driver === 'mysql'">
        <el-form-item label="主机地址" prop="database.mysqlHost">
          <el-input v-model="settings.database.mysqlHost" :maxlength="255" placeholder="localhost" />
        </el-form-item>
        <el-form-item label="端口" prop="database.mysqlPort">
          <el-input-number v-model="settings.database.mysqlPort" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="用户名" prop="database.mysqlUser">
          <el-input v-model="settings.database.mysqlUser" :maxlength="64" placeholder="root" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="settings.database.mysqlPassword" :maxlength="128" type="password" placeholder="输入密码" show-password />
        </el-form-item>
        <el-form-item label="数据库名" prop="database.mysqlDatabase">
          <el-input v-model="settings.database.mysqlDatabase" :maxlength="64" placeholder="fuzhan" />
        </el-form-item>
      </template>

      <template v-if="settings.database.driver === 'postgres'">
        <el-form-item label="主机地址" prop="database.postgresHost">
          <el-input v-model="settings.database.postgresHost" :maxlength="255" placeholder="localhost" />
        </el-form-item>
        <el-form-item label="端口" prop="database.postgresPort">
          <el-input-number v-model="settings.database.postgresPort" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="用户名" prop="database.postgresUser">
          <el-input v-model="settings.database.postgresUser" :maxlength="64" placeholder="postgres" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="settings.database.postgresPassword" :maxlength="128" type="password" placeholder="输入密码" show-password />
        </el-form-item>
        <el-form-item label="数据库名" prop="database.postgresDatabase">
          <el-input v-model="settings.database.postgresDatabase" :maxlength="64" placeholder="fuzhan" />
        </el-form-item>
      </template>
    </el-form>
  </el-card>
</template>

<script setup>
defineProps({
  settings: { type: Object, required: true }
})
</script>

<style lang="less" scoped>
.field-hint {
  color: #999;
  font-size: 12px;
}
</style>