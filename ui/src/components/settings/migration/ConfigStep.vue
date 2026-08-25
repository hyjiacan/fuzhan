<template>
  <div class="config-step">
    <el-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-position="top"
    >
      <el-form-item label="目标数据库类型" prop="driver">
        <el-radio-group v-model="formData.driver">
          <el-radio label="mysql">MySQL</el-radio>
          <el-radio label="postgres">PostgreSQL</el-radio>
          <el-radio label="sqlite">SQLite</el-radio>
        </el-radio-group>
      </el-form-item>

      <template v-if="formData.driver === 'mysql'">
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="主机地址" prop="host">
              <el-input v-model="formData.host" :maxlength="255" placeholder="localhost" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="端口" prop="port">
              <el-input-number v-model="formData.port" :min="1" :max="65535" placeholder="3306" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="用户名" prop="user">
          <el-input v-model="formData.user" :maxlength="64" placeholder="root" />
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input v-model="formData.password" :maxlength="128" type="password" show-password placeholder="请输入密码" />
        </el-form-item>

        <el-form-item label="数据库名" prop="database">
          <el-input v-model="formData.database" :maxlength="64" placeholder="fuzhan" />
        </el-form-item>
      </template>

      <template v-else-if="formData.driver === 'postgres'">
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="主机地址" prop="host">
              <el-input v-model="formData.host" :maxlength="255" placeholder="localhost" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="端口" prop="port">
              <el-input-number v-model="formData.port" :min="1" :max="65535" placeholder="5432" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="用户名" prop="user">
          <el-input v-model="formData.user" :maxlength="64" placeholder="postgres" />
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input v-model="formData.password" :maxlength="128" type="password" show-password placeholder="请输入密码" />
        </el-form-item>

        <el-form-item label="数据库名" prop="database">
          <el-input v-model="formData.database" :maxlength="64" placeholder="fuzhan" />
        </el-form-item>
      </template>

      <template v-else>
        <el-form-item label="数据库文件路径" prop="dsn">
          <el-input v-model="formData.dsn" :maxlength="1024" placeholder="fuzhan.db" />
        </el-form-item>
      </template>
    </el-form>

    <div class="step-actions">
      <el-button @click="$emit('cancel')">取消</el-button>
      <el-button type="primary" @click="handleNext">下一步</el-button>
    </div>
  </div>
</template>

<script>
import { ref, reactive, watch } from 'vue'

export default {
  name: 'ConfigStep',

  props: {
    databaseType: {
      type: String,
      default: 'mysql'
    },
    initialConfig: {
      type: Object,
      default: null
    }
  },

  emits: ['next', 'cancel'],

  setup(props, { emit }) {
    const formRef = ref(null)

    // 根据初始配置初始化表单
    const initConfig = props.initialConfig || {}
    const initDriver = initConfig.driver || props.databaseType || 'mysql'

    // 解析 MySQL DSN
    const parseMysqlDsn = (dsn) => {
      const match = dsn.match(/([^:@]+):([^@]*)@tcp\(([^:]+):(\d+)\)\/([^?]+)/)
      if (match) {
        return {
          user: match[1],
          password: match[2],
          host: match[3],
          port: parseInt(match[4]),
          database: match[5]
        }
      }
      return { host: 'localhost', port: 3306, user: 'root', password: '', database: 'fuzhan' }
    }

    // 解析 PostgreSQL DSN
    const parsePostgresDsn = (dsn) => {
      const getParam = (str, key) => {
        const match = str.match(new RegExp(`${key}=(\\S+)`))
        return match ? match[1] : ''
      }
      return {
        host: getParam(dsn, 'host') || 'localhost',
        port: parseInt(getParam(dsn, 'port')) || 5432,
        user: getParam(dsn, 'user') || 'postgres',
        password: getParam(dsn, 'password'),
        database: getParam(dsn, 'dbname') || 'fuzhan'
      }
    }

    // 根据初始配置构建初始值
    let initData = {
      driver: initDriver,
      host: 'localhost',
      port: initDriver === 'postgres' ? 5432 : 3306,
      user: '',
      password: '',
      database: 'fuzhan',
      dsn: ''
    }

    if (initDriver === 'mysql' && initConfig.dsn) {
      const parsed = parseMysqlDsn(initConfig.dsn)
      initData = { ...initData, ...parsed }
    } else if (initDriver === 'postgres' && initConfig.dsn) {
      const parsed = parsePostgresDsn(initConfig.dsn)
      initData = { ...initData, ...parsed }
    } else if (initDriver === 'sqlite') {
      initData.dsn = initConfig.dsn || 'fuzhan.db'
    }

    const formData = reactive(initData)

    const rules = {
      driver: { required: true, message: '请选择数据库类型' },
      host: { required: true, message: '请输入主机地址' },
      port: { required: true, type: 'number', message: '请输入端口' },
      user: { required: true, message: '请输入用户名' },
      database: { required: true, message: '请输入数据库名' }
    }

    const handleNext = async () => {
      try {
        await formRef.value?.validate()
        emit('next', { driver: formData.driver, dsn: buildDSN() })
      } catch {
        // 验证失败
      }
    }

    const buildDSN = () => {
      if (formData.driver === 'mysql') {
        return `${formData.user}:${formData.password}@tcp(${formData.host}:${formData.port})/${formData.database}`
      } else if (formData.driver === 'postgres') {
        return `host=${formData.host} port=${formData.port} user=${formData.user} password=${formData.password} dbname=${formData.database}`
      } else {
        return formData.dsn || 'fuzhan.db'
      }
    }

    return {
      formRef,
      formData,
      rules,
      handleNext
    }
  }
}
</script>

<style scoped>
.config-step {
  padding: 16px 0;
}

.step-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--border-color);
}
</style>