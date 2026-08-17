<template>
  <div class="config-step">
    <n-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-placement="top"
    >
      <n-form-item label="目标数据库类型" path="driver">
        <n-radio-group v-model:value="formData.driver" name="driver">
          <n-space>
            <n-radio value="mysql">MySQL</n-radio>
            <n-radio value="postgres">PostgreSQL</n-radio>
            <n-radio value="sqlite">SQLite</n-radio>
          </n-space>
        </n-radio-group>
      </n-form-item>

      <template v-if="formData.driver === 'mysql'">
        <n-grid :cols="2" :x-gap="12">
          <n-gi>
            <n-form-item label="主机地址" path="host">
              <n-input v-model:value="formData.host" :maxlength="255" placeholder="localhost" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="端口" path="port">
              <n-input-number v-model:value="formData.port" :min="1" :max="65535" placeholder="3306" style="width: 100%" />
            </n-form-item>
          </n-gi>
        </n-grid>

        <n-form-item label="用户名" path="user">
          <n-input v-model:value="formData.user" :maxlength="64" placeholder="root" />
        </n-form-item>

        <n-form-item label="密码" path="password">
          <n-input v-model:value="formData.password" :maxlength="128" type="password" placeholder="请输入密码" show-password-on="click" />
        </n-form-item>

        <n-form-item label="数据库名" path="database">
          <n-input v-model:value="formData.database" :maxlength="64" placeholder="fuzhan" />
        </n-form-item>
      </template>

      <template v-else-if="formData.driver === 'postgres'">
        <n-grid :cols="2" :x-gap="12">
          <n-gi>
            <n-form-item label="主机地址" path="host">
              <n-input v-model:value="formData.host" :maxlength="255" placeholder="localhost" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="端口" path="port">
              <n-input-number v-model:value="formData.port" :min="1" :max="65535" placeholder="5432" style="width: 100%" />
            </n-form-item>
          </n-gi>
        </n-grid>

        <n-form-item label="用户名" path="user">
          <n-input v-model:value="formData.user" :maxlength="64" placeholder="postgres" />
        </n-form-item>

        <n-form-item label="密码" path="password">
          <n-input v-model:value="formData.password" :maxlength="128" type="password" placeholder="请输入密码" show-password-on="click" />
        </n-form-item>

        <n-form-item label="数据库名" path="database">
          <n-input v-model:value="formData.database" :maxlength="64" placeholder="fuzhan" />
        </n-form-item>
      </template>

      <template v-else>
        <n-form-item label="数据库文件路径" path="dsn">
          <n-input v-model:value="formData.dsn" :maxlength="1024" placeholder="fuzhan.db" />
        </n-form-item>
      </template>
    </n-form>

    <div class="step-actions">
      <n-button @click="$emit('cancel')">取消</n-button>
      <n-button type="primary" @click="handleNext">下一步</n-button>
    </div>
  </div>
</template>

<script>
import { ref, reactive, watch } from 'vue'
import { NForm, NFormItem, NRadioGroup, NRadio, NSpace, NInput, NInputNumber, NGrid, NGi, NButton } from 'naive-ui'

export default {
  name: 'ConfigStep',

  components: {
    NForm, NFormItem, NRadioGroup, NRadio, NSpace, NInput, NInputNumber, NGrid, NGi, NButton
  },

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