<template>
  <div class="settings-webhook">
    <el-card>
      <template #header>
        <div class="card-head">
          <span>Webhook 推送</span>
          <span class="card-sub">把系统关键事件（任务/安全/文件/认证等）异步推送给外部接收端</span>
        </div>
      </template>
      <el-form autocomplete="off" label-width="140px">
        <el-form-item label="启用 Webhook">
          <el-switch v-model="settings.webhook.enabled" />
          <div class="field-hint" style="margin-left: 12px;">启用后将关键事件推送到下方各回调地址</div>
        </el-form-item>

        <el-form-item label="默认签名密钥">
          <el-input v-model="settings.webhook.secret" type="password" show-password placeholder="留空则不签名（依赖 HTTPS 保护）" autocomplete="new-password" />
          <div class="field-hint">端点密钥留空时继承此处；配置后每条推送体计算 HMAC-SHA256，写入响应头 X-Fuzhan-Signature（sha256=&lt;hex&gt;）</div>
        </el-form-item>

        <el-row :gutter="16">
          <el-col :span="8" :sm="8" :xs="24">
            <el-form-item label="默认超时(秒)">
              <el-input-number v-model="settings.webhook.timeoutSeconds" :min="1" :max="120" style="width: 100%;" />
              <div class="field-hint">单次 HTTP 请求超时时间，端点可覆盖</div>
            </el-form-item>
          </el-col>
          <el-col :span="8" :sm="8" :xs="24">
            <el-form-item label="默认失败重试">
              <el-input-number v-model="settings.webhook.maxRetries" :min="0" :max="10" style="width: 100%;" />
              <div class="field-hint">发送失败后按指数退避重试的次数，端点可覆盖</div>
            </el-form-item>
          </el-col>
          <el-col :span="8" :sm="8" :xs="24">
            <el-form-item label="队列容量">
              <el-input-number v-model="settings.webhook.queueSize" :min="1" :max="100000" style="width: 100%;" />
              <div class="field-hint">进程内事件队列上限，超限丢弃以保护服务（重启生效）</div>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-head">
          <span>回调地址</span>
          <span class="card-sub">每个地址一条记录，可独立配置、测试</span>
          <el-button type="primary" size="small" style="margin-left: auto;" @click="addEndpoint">添加回调地址</el-button>
        </div>
      </template>

      <el-form-item v-if="!endpoints.length" label-width="0">
        <el-empty description="尚未配置回调地址" :image-size="60" />
      </el-form-item>

      <div v-else class="endpoint-list">
        <el-card
          v-for="(ep, idx) in endpoints"
          :key="idx"
          class="endpoint-card"
          :class="{ 'is-disabled': !ep.enabled }"
          shadow="never"
        >
          <div class="endpoint-head">
            <el-input v-model="ep.name" placeholder="端点名称（可选）" style="width: 220px;" />
            <el-switch v-model="ep.enabled" active-text="启用" style="margin-left: 12px;" />
            <span class="endpoint-url">{{ ep.url }}</span>
            <div class="endpoint-actions">
              <el-button size="small" :loading="idx === testingIndex" @click="testEndpoint(idx)">测试</el-button>
              <el-button size="small" type="danger" text @click="removeEndpoint(idx)">删除</el-button>
            </div>
          </div>

          <el-form-item label="回调 URL" style="margin-top: 8px;">
            <el-input v-model="ep.url" placeholder="https://example.com/hook/fuzhan" />
          </el-form-item>

          <el-form-item label="事件过滤">
            <el-select
              v-model="ep.events"
              multiple
              collapse-tags
              collapse-tags-tooltip
              filterable
              clearable
              placeholder="空 = 接收全部事件"
              style="width: 100%;"
            >
              <el-option-group v-for="cat in eventGroups" :key="cat.label" :label="cat.label">
                <el-option v-for="ev in cat.items" :key="ev.event" :label="`${ev.event} — ${ev.desc}`" :value="ev.event" />
              </el-option-group>
            </el-select>
            <div class="field-hint">
              留空表示全部事件；可精确选择，也可输入通配符（如
              <code>file.*</code>、<code>task.*</code>、<code>*</code>）匹配整类。
            </div>
          </el-form-item>

          <el-form-item label="认证方式">
            <div class="auth-wrapper">
              <el-select v-model="ep._authPreset" style="width: 220px;" placeholder="选择认证方式">
                <el-option label="无认证" value="none" />
                <el-option label="Bearer Token" value="bearer" />
                <el-option label="API Key" value="apikey" />
                <el-option label="URL Token" value="urltoken" />
                <el-option label="自定义" value="custom" />
              </el-select>
              <div class="field-hint" style="margin-left: 12px;">预设仅用于快速填充下方 headers / query 字段，不随配置提交</div>
            </div>
          </el-form-item>

          <el-form-item v-if="ep._authPreset !== 'none' && ep._authPreset !== 'custom'" :label="authPresetLabel">
            <template v-if="ep._authPreset === 'bearer'">
              <el-input v-model="ep._token" type="password" show-password placeholder="Bearer Token 值" autocomplete="new-password" style="width: 320px;" />
              <el-button size="small" style="margin-left: 8px;" @click="applyAuthPreset(ep)">填入 Headers</el-button>
            </template>
            <template v-else-if="ep._authPreset === 'apikey'">
              <el-input v-model="ep._token" type="password" show-password placeholder="API Key 值" autocomplete="new-password" style="width: 200px;" />
              <el-input v-model="ep._keyName" placeholder="Header 名（如 X-API-Key）" style="width: 180px; margin-left: 8px;" />
              <el-button size="small" style="margin-left: 8px;" @click="applyAuthPreset(ep)">填入 Headers</el-button>
            </template>
            <template v-else-if="ep._authPreset === 'urltoken'">
              <el-input v-model="ep._token" type="password" show-password placeholder="Token 值" autocomplete="new-password" style="width: 200px;" />
              <el-input v-model="ep._keyName" placeholder="Query 参数名（如 token）" style="width: 180px; margin-left: 8px;" />
              <el-button size="small" style="margin-left: 8px;" @click="applyAuthPreset(ep)">填入 Query</el-button>
            </template>
          </el-form-item>

          <el-form-item v-if="ep._authPreset === 'custom' || authVisible(ep)" label="Headers">
            <div class="kv-editor">
              <div v-for="(row, ri) in ep._headersRows" :key="ri" class="kv-row">
                <el-input v-model="row.k" placeholder="Header 名" style="width: 200px;" />
                <el-input v-model="row.v" type="password" show-password placeholder="值（如 Bearer xxx）" style="width: 260px;" autocomplete="new-password" />
                <el-button size="small" text type="danger" @click="removeKv(ep._headersRows, ri)">移除</el-button>
              </div>
              <el-button size="small" @click="addKv(ep._headersRows)">+ 添加 Header</el-button>
            </div>
            <div class="field-hint">接收方鉴权请求头，如 <code>Authorization: Bearer xxx</code>、<code>X-API-Key: xxx</code></div>
          </el-form-item>

          <el-form-item v-if="ep._authPreset === 'custom' || authVisible(ep)" label="Query">
            <div class="kv-editor">
              <div v-for="(row, qi) in ep._queryRows" :key="qi" class="kv-row">
                <el-input v-model="row.k" placeholder="参数名（如 token）" style="width: 200px;" />
                <el-input v-model="row.v" type="password" show-password placeholder="值" style="width: 260px;" autocomplete="new-password" />
                <el-button size="small" text type="danger" @click="removeKv(ep._queryRows, qi)">移除</el-button>
              </div>
              <el-button size="small" @click="addKv(ep._queryRows)">+ 添加参数</el-button>
            </div>
            <div class="field-hint">追加到回调 URL 的查询参数（URL 已含同名参数时保留原值）</div>
          </el-form-item>

          <el-row :gutter="16">
            <el-col :span="8" :sm="8" :xs="24">
              <el-form-item label="密钥">
                <el-input v-model="ep.secret" type="password" show-password placeholder="留空继承默认" autocomplete="new-password" />
              </el-form-item>
            </el-col>
            <el-col :span="8" :sm="8" :xs="24">
              <el-form-item label="超时(秒)">
                <el-input-number v-model="ep.timeoutSeconds" :min="0" :max="120" style="width: 100%;" />
                <div class="field-hint">0 = 继承默认</div>
              </el-form-item>
            </el-col>
            <el-col :span="8" :sm="8" :xs="24">
              <el-form-item label="重试次数">
                <el-input-number v-model="ep.maxRetries" :min="0" :max="10" style="width: 100%;" />
                <div class="field-hint">0 = 继承默认</div>
              </el-form-item>
            </el-col>
          </el-row>

          <div v-if="ep._testResult" class="endpoint-test-result" :class="{ ok: ep._testResult.success }">
            <span class="test-status">{{ ep._testResult.success ? '成功' : '失败' }}</span>
            <span class="test-meta">HTTP {{ ep._testResult.statusCode || '-' }} · {{ ep._testResult.elapsedMs }}ms</span>
            <span v-if="ep._testResult.error" class="test-error">{{ ep._testResult.error }}</span>
            <span class="test-url">{{ ep._testResult.url }}</span>
          </div>
        </el-card>
      </div>

      <el-form-item v-if="endpoints.length" label-width="0" style="margin-top: 8px;">
        <el-button type="warning" :loading="testingAll" @click="testAll">测试全部</el-button>
        <span class="field-hint" style="margin-left: 12px;">向每个地址发送一条 test 事件（禁用端点与事件过滤不影响测试）</span>
      </el-form-item>

      <el-form-item v-if="allTestResults.length" label-width="0" label="测试结果">
        <div class="test-results">
          <div v-for="(r, i) in allTestResults" :key="i" class="test-result-line" :class="{ ok: r.success }">
            <span class="test-status">{{ r.success ? '成功' : '失败' }}</span>
            [{{ r.name || r.url }}] HTTP {{ r.statusCode || '-' }} · {{ r.elapsedMs }}ms
            <span v-if="r.error" class="test-error">—— {{ r.error }}</span>
          </div>
        </div>
      </el-form-item>
    </el-card>

    <el-card>
      <template #header><span>支持的事件（{{ events.length }}）</span></template>
      <div class="event-table">
        <div class="event-group" v-for="g in eventGroups" :key="g.label">
          <div class="event-group-title">{{ g.label }}</div>
          <div class="event-row" v-for="ev in g.items" :key="ev.event">
            <div class="event-code">{{ ev.event }}</div>
            <div class="event-desc">{{ ev.desc }}</div>
          </div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ConfigApi } from '@/api'

const props = defineProps({
  settings: { type: Object, required: true }
})

const testingIndex = ref(-1)
const testingAll = ref(false)
const allTestResults = ref([])

// 事件目录：优先后端目录，失败回退内置列表
const events = ref([])
const fallbackEvents = [
  { event: 'task.started', desc: '系统作业开始执行（扫描/哈希/清理/URL 下载等）', category: '任务' },
  { event: 'task.finished', desc: '系统作业完成', category: '任务' },
  { event: 'task.failed', desc: '系统作业失败（含进程重启中断的对账记录）', category: '任务' },
  { event: 'task.canceled', desc: '系统作业被取消', category: '任务' },
  { event: 'security.event', desc: '安全事件（登录暴破/越权拒绝/频率限制/DDoS 告警）', category: '安全' },
  { event: 'config.saved', desc: '配置保存成功', category: '配置' },
  { event: 'file.upload_started', desc: '创建上传会话，开始上传文件', category: '文件' },
  { event: 'file.upload_canceled', desc: '取消上传会话', category: '文件' },
  { event: 'file.uploaded', desc: '上传会话完成，文件写入最终路径', category: '文件' },
  { event: 'file.renamed', desc: '文件/目录重命名', category: '文件' },
  { event: 'file.moved', desc: '文件/目录移动', category: '文件' },
  { event: 'file.deleted', desc: '文件/目录删除', category: '文件' },
  { event: 'auth.login_success', desc: '本地/OAuth2 登录成功', category: '认证' },
  { event: 'auth.login_failed', desc: '本地/OAuth2 登录失败（含限流锁定）', category: '认证' }
]

const loadEvents = async () => {
  try {
    const res = await ConfigApi.webhookEvents()
    if (res.success && res.data) events.value = res.data
    else events.value = fallbackEvents
  } catch (e) {
    console.error('加载 Webhook 事件目录失败', e)
    events.value = fallbackEvents
  }
}
loadEvents()

const eventGroups = computed(() => {
  const map = {}
  for (const ev of events.value) {
    const cat = ev.category || '其他'
    if (!map[cat]) map[cat] = { label: cat, items: [] }
    map[cat].items.push(ev)
  }
  return Object.values(map)
})

// —— 端点列表 ——
const endpoints = computed(() => props.settings.webhook.endpoints || [])

const newEndpoint = () => ({
  name: '',
  url: '',
  enabled: true,
  events: [],
  secret: '',
  timeoutSeconds: 0,
  maxRetries: 0,
  _authPreset: 'none',
  _token: '',
  _keyName: '',
  _headersRows: [],
  _queryRows: [],
  _testResult: null
})

// 反序列化：headers/query map <-> 行数组
const rowsFromMap = (m) => Object.entries(m || {}).map(([k, v]) => ({ k, v }))
const mapFromRows = (rows) => {
  const m = {}
  for (const r of rows) {
    if (r.k) m[r.k] = r.v || ''
  }
  return m
}

const syncFromEndpoint = (ep) => {
  if (!ep._initialized) {
    ep._initialized = true
    ep._headersRows = rowsFromMap(ep.headers)
    ep._queryRows = rowsFromMap(ep.query)
    ep._testResult = null
  }
}

watch(endpoints, (list) => {
  ;(list || []).forEach(syncFromEndpoint)
}, { immediate: true, deep: false })

// 行数组变化时同步写回正式字段（供 SettingsView 统一保存）
watch(endpoints, (list) => {
  ;(list || []).forEach((ep) => {
    if (ep._initialized) {
      ep.headers = mapFromRows(ep._headersRows || [])
      ep.query = mapFromRows(ep._queryRows || [])
    }
  })
}, { deep: true })

const addEndpoint = () => {
  const ep = newEndpoint()
  props.settings.webhook.endpoints.push(ep)
  syncFromEndpoint(ep)
}

const removeEndpoint = (idx) => {
  props.settings.webhook.endpoints.splice(idx, 1)
}

const addKv = (rows) => rows.push({ k: '', v: '' })
const removeKv = (rows, i) => rows.splice(i, 1)

const authVisible = (ep) => (ep._headersRows && ep._headersRows.length) || (ep._queryRows && ep._queryRows.length)

const authPresetLabel = computed(() => {
  const preset = endpoints.value[testingIndex.value]?._authPreset
  if (preset === 'bearer') return 'Bearer Token'
  if (preset === 'apikey') return 'API Key'
  if (preset === 'urltoken') return 'Token'
  return 'Token 值'
})

const applyAuthPreset = (ep) => {
  if (ep._authPreset === 'bearer') {
    if (!ep._token) { ElMessage.warning('请先填写 Token 值'); return }
    ep._headersRows = [{ k: 'Authorization', v: `Bearer ${ep._token}` }]
    ElMessage.success('已填入 Authorization 请求头')
  } else if (ep._authPreset === 'apikey') {
    const name = ep._keyName || 'X-API-Key'
    if (!ep._token) { ElMessage.warning('请先填写 API Key 值'); return }
    ep._headersRows = [{ k: name, v: ep._token }]
    ElMessage.success('已填入请求头')
  } else if (ep._authPreset === 'urltoken') {
    const name = ep._keyName || 'token'
    if (!ep._token) { ElMessage.warning('请先填写 Token 值'); return }
    ep._queryRows = [{ k: name, v: ep._token }]
    ElMessage.success('已填入 URL 查询参数')
  }
}

const endpointToDTO = (ep) => ({
  name: ep.name,
  url: ep.url,
  enabled: ep.enabled,
  events: ep.events || [],
  secret: ep.secret,
  timeoutSeconds: ep.timeoutSeconds,
  maxRetries: ep.maxRetries,
  headers: mapFromRows(ep._headersRows || []),
  query: mapFromRows(ep._queryRows || [])
})

// —— 测试 ——
const testEndpoint = async (idx) => {
  const ep = endpoints.value[idx]
  if (!ep.url) { ElMessage.warning('请先填写回调 URL'); return }
  testingIndex.value = idx
  ep._testResult = null
  try {
    const res = await ConfigApi.testWebhook({ endpoints: [endpointToDTO(ep)] })
    if (res.success && Array.isArray(res.data) && res.data.length) {
      ep._testResult = res.data[0]
      if (res.data[0].success) ElMessage.success('测试成功')
    } else {
      ep._testResult = { success: false, error: res.message || '测试失败' }
      ElMessage.error(res.message || '测试失败')
    }
  } catch (e) {
    console.error('测试 Webhook 失败', e)
    ep._testResult = { success: false, error: e.message || '网络错误' }
  } finally {
    testingIndex.value = -1
  }
}

const testAll = async () => {
  const list = endpoints.value
  if (!list.length) { ElMessage.warning('请先添加回调地址'); return }
  testingAll.value = true
  allTestResults.value = []
  try {
    const res = await ConfigApi.testWebhook({
      endpoints: list.map(endpointToDTO)
    })
    if (res.success) {
      allTestResults.value = res.data || []
    } else {
      allTestResults.value = [{ success: false, error: res.message || '测试失败' }]
      ElMessage.error(res.message || '测试失败')
    }
  } catch (e) {
    console.error('测试 Webhook 失败', e)
    allTestResults.value = [{ success: false, error: e.message || '网络错误' }]
  } finally {
    testingAll.value = false
  }
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.settings-webhook {
  display: flex;
  flex-direction: column;
  gap: 16px;

  .el-card {
    transition: transform @transition-smooth, box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }
  }
}

.card-head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.card-sub {
  color: @text-color-secondary;
  font-size: 12px;
  font-weight: normal;
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;
}

.endpoint-list {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.endpoint-card {
  border: 1px solid @border-color;
  background: @bg-color-secondary;

  &.is-disabled {
    opacity: 0.7;
  }
}

.endpoint-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 4px;
}

.endpoint-url {
  flex: 1;
  color: @text-color-secondary;
  font-family: monospace;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.endpoint-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.auth-wrapper {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
}

.kv-editor {
  display: flex;
  flex-direction: column;
  gap: 6px;

  .kv-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
}

.endpoint-test-result {
  margin-top: 8px;
  padding: 8px 12px;
  border-radius: @border-radius-sm;
  background: fade(@error-color, 8%);
  font-size: 13px;

  &.ok {
    background: fade(@success-color, 8%);
  }

  .test-status {
    font-weight: 600;
    color: @error-color;

    &.ok {
      color: @success-color;
    }
  }

  .test-meta {
    margin-left: 8px;
    font-family: monospace;
    color: @text-color-secondary;
  }

  .test-error {
    margin-left: 8px;
    color: @error-color;
  }

  .test-url {
    display: block;
    margin-top: 2px;
    font-family: monospace;
    color: @text-color-secondary;
    word-break: break-all;
  }
}

.test-results {
  width: 100%;
}

.test-result-line {
  padding: 4px 0;
  font-family: monospace;
  font-size: 13px;
  color: @error-color;

  &.ok {
    color: @success-color;
  }

  .test-status {
    font-weight: 600;
  }

  .test-error {
    color: @error-color;
  }
}

.event-table {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.event-group-title {
  font-weight: 600;
  color: @text-color;
  font-size: 14px;
  padding: 6px 0 2px;
  border-bottom: 1px solid @border-color;
}

.event-row {
  display: flex;
  align-items: baseline;
  gap: 16px;
  padding: 4px 0;
}

.event-code {
  font-family: monospace;
  font-size: 13px;
  min-width: 150px;
  color: @primary-color;
}

.event-desc {
  color: @text-color;
  font-size: 13px;
}
</style>
