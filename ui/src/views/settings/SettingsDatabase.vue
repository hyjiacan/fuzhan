<template>
  <el-card>
    <el-alert type="warning" :show-icon="false" :closable="false" class="db-migration-alert">
      <template #title>切换数据库配置需手动迁移数据</template>
      <div>修改数据库类型或地址后，若要保留已有数据，请使用 <code>dbswitch</code> 等外部迁移工具手动迁移数据，本站不提供内置迁移。配置保存后需重启服务方可生效。</div>
    </el-alert>

    <el-divider />

    <el-form :model="settings" label-width="120px">
      <el-form-item label="数据库类型">
        <div class="db-driver-list">
          <label
            v-for="opt in dbDrivers"
            :key="opt.value"
            class="db-driver-option"
            :class="{ active: settings.database.driver === opt.value }"
          >
            <el-radio v-model="settings.database.driver" :label="opt.value" class="db-driver-radio">
              {{ opt.name }}
            </el-radio>
            <span class="db-driver-desc">{{ opt.desc }}</span>
          </label>
        </div>
      </el-form-item>

      <el-form-item v-if="settings.database.driver === 'sqlite'" label="数据库文件" prop="database.dsn">
        <el-input v-model="settings.database.dsn" :maxlength="1024" placeholder="fuzhan.db" />
        <div class="field-hint">SQLite 数据库文件路径</div>
      </el-form-item>

      <template v-if="settings.database.driver === 'mysql'">
        <el-form-item label="主机地址" prop="database.mysqlHost">
          <el-input v-model="settings.database.mysqlHost" :maxlength="255" placeholder="localhost" />
          <div class="field-hint">MySQL 服务器地址，本机用 localhost，远程写 IP 或域名</div>
        </el-form-item>
        <el-form-item label="端口" prop="database.mysqlPort">
          <el-input-number v-model="settings.database.mysqlPort" :min="1" :max="65535" />
          <div class="field-hint">MySQL 服务监听端口，默认 3306</div>
        </el-form-item>
        <el-form-item label="用户名" prop="database.mysqlUser">
          <el-input v-model="settings.database.mysqlUser" :maxlength="64" placeholder="root" />
          <div class="field-hint">连接数据库的账号，建议使用仅有该库读写权限的专用账号</div>
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="settings.database.mysqlPassword" :maxlength="128" type="password" placeholder="输入密码" show-password />
          <div class="field-hint">该账号对应的密码，仅用于连接数据库</div>
        </el-form-item>
        <el-form-item label="数据库名" prop="database.mysqlDatabase">
          <el-input v-model="settings.database.mysqlDatabase" :maxlength="64" placeholder="fuzhan" />
          <div class="field-hint">要使用的目标数据库，需在 MySQL 中提前创建好</div>
        </el-form-item>
      </template>

      <template v-if="settings.database.driver === 'postgres'">
        <el-form-item label="主机地址" prop="database.postgresHost">
          <el-input v-model="settings.database.postgresHost" :maxlength="255" placeholder="localhost" />
          <div class="field-hint">PostgreSQL 服务器地址，本机用 localhost，远程写 IP 或域名</div>
        </el-form-item>
        <el-form-item label="端口" prop="database.postgresPort">
          <el-input-number v-model="settings.database.postgresPort" :min="1" :max="65535" />
          <div class="field-hint">PostgreSQL 服务监听端口，默认 5432</div>
        </el-form-item>
        <el-form-item label="用户名" prop="database.postgresUser">
          <el-input v-model="settings.database.postgresUser" :maxlength="64" placeholder="postgres" />
          <div class="field-hint">连接数据库的账号，建议使用仅有该库读写权限的专用账号</div>
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="settings.database.postgresPassword" :maxlength="128" type="password" placeholder="输入密码" show-password />
          <div class="field-hint">该账号对应的密码，仅用于连接数据库</div>
        </el-form-item>
        <el-form-item label="数据库名" prop="database.postgresDatabase">
          <el-input v-model="settings.database.postgresDatabase" :maxlength="64" placeholder="fuzhan" />
          <div class="field-hint">要使用的目标数据库，需在 PostgreSQL 中提前创建好</div>
        </el-form-item>
      </template>
    </el-form>
  </el-card>
</template>

<script setup>
defineProps({
  settings: { type: Object, required: true }
})

const dbDrivers = [
  { value: 'sqlite', name: 'SQLite', desc: '轻量级，文件存储，适合小型部署' },
  { value: 'mysql', name: 'MySQL', desc: '适合大规模应用，需要 MySQL 5.7+' },
  { value: 'postgres', name: 'PostgreSQL', desc: '功能丰富，适合企业级应用，需要 PostgreSQL 10+' }
]
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.db-driver-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
  margin-bottom: 6px;
}

.db-driver-option {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 14px;
  border: 1px solid @border-color-light;
  border-radius: @border-radius-lg;
  cursor: pointer;
  transition: border-color @transition-fast, background-color @transition-fast, box-shadow @transition-fast;

  &:hover {
    border-color: @primary-accent;
    background-color: var(--el-fill-color-light);
  }

  &.active {
    border-color: @primary-color;
    background-color: var(--el-color-primary-light-9);
    box-shadow: @shadow-sm;
  }

  .db-driver-radio {
    margin-right: 0;
    flex: 0 0 auto;
    white-space: nowrap;
    font-weight: 500;
  }

  .db-driver-desc {
    color: @text-color-secondary;
    font-size: @font-size-sm;
  }
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;
}
</style>