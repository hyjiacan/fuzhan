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

          <el-form-item label="事件权限">
            <div class="event-perm">
              <el-switch
                v-model="ep._allEvents"
                active-text="接收全部事件"
                @change="(v) => toggleAllEvents(ep, v)"
              />
              <div v-if="!ep._allEvents" class="event-groups">
                <div v-for="cat in eventGroups" :key="cat.label" class="event-group">
                  <div class="event-group-head">
                    <span class="event-group-title">{{ cat.label }}</span>
                    <div class="event-group-actions">
                      <el-button link size="small" @click="setGroupEvents(ep, cat, true)">全选</el-button>
                      <el-button link size="small" @click="setGroupEvents(ep, cat, false)">清空</el-button>
                    </div>
                  </div>
                  <el-checkbox-group v-model="ep.events">
                    <el-checkbox v-for="ev in cat.items" :key="ev.event" :label="ev.event">
                      <span class="event-name">{{ ev.event }}</span>
                      <span class="event-desc">{{ ev.desc }}</span>
                    </el-checkbox>
                  </el-checkbox-group>
                </div>
                <div v-if="customEvents(ep).length" class="event-group">
                  <div class="event-group-head">
                    <span class="event-group-title">自定义规则</span>
                  </div>
                  <div class="event-tags">
                    <el-tag v-for="ev in customEvents(ep)" :key="ev" size="small" closable @close="removeEvent(ep, ev)">{{ ev }}</el-tag>
                  </div>
                </div>
              </div>
              <div v-if="ep._allEvents" class="field-hint">
                已显式订阅全部事件（YAML 中为 <code>*</code>），新增事件同样会被推送。
              </div>
              <div v-else-if="!ep.events || !ep.events.length" class="field-hint is-error">
                未选择任何事件：该回调不会收到任何推送（默认拒绝）。
              </div>
              <div v-else class="field-hint">
                仅推送所勾选的事件；后续新增事件需手动勾选。
              </div>
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

          <el-form-item v-if="ep._authPreset !== 'none' && ep._authPreset !== 'custom'" :label="authPresetLabel(ep)">
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
      <el-alert
        v-if="eventsError"
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 12px;"
      >
        事件目录加载失败，无法展示可用事件；请检查后端服务后刷新页面重试。
      </el-alert>
      <el-empty v-else-if="eventsLoaded && !events.length" description="后端未配置可用事件" :image-size="60" />
      <div v-else class="event-table">
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

// 事件目录：由后端 /admin/config/webhook/events 提供；
// 加载失败保持为空——事件目录缺失即后端异常态，不回退到前端内置清单
const events = ref([])
const eventsLoaded = ref(false)
const eventsError = ref(false)

const loadEvents = async () => {
  try {
    const res = await ConfigApi.webhookEvents()
    if (res.success && res.data) events.value = res.data
    // 空列表为后端返回的真实结果，保持为空
    eventsLoaded.value = true
    eventsError.value = false
  } catch (e) {
    // 加载失败保持为空，并标记加载失败以便界面给出错误提示
    console.error('加载 Webhook 事件目录失败:', e)
    eventsLoaded.value = true
    eventsError.value = true
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
  _allEvents: false,
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
    // 事件权限：显式 `*` 视为接收全部；空列表表示不接收任何事件（默认拒绝）
    ep._allEvents = Array.isArray(ep.events) && ep.events.includes('*')
    if (!Array.isArray(ep.events)) ep.events = []
    ep._testResult = null
  }
}

// 接收全部事件：写入显式 `*` 规则，避免依赖“留空即全部”的隐式语义
const toggleAllEvents = (ep, all) => {
  ep.events = all ? ['*'] : []
}

// 事件权限按分类勾选：整组全选 / 清空（通配规则仍由后端支持，界面不再直接输入）
const setGroupEvents = (ep, cat, checked) => {
  const names = cat.items.map((i) => i.event)
  const rest = (ep.events || []).filter((e) => e !== '*' && !names.includes(e))
  ep.events = checked ? [...rest, ...names] : rest
}

// 目录之外的自定义规则（如历史配置中的 file.*）：单独展示，避免不可见残留
const catalogEventNames = computed(() => new Set((events.value || []).map((e) => e.event)))
const customEvents = (ep) => (ep.events || []).filter((e) => e !== '*' && !catalogEventNames.value.has(e))
const removeEvent = (ep, ev) => {
  ep.events = (ep.events || []).filter((e) => e !== ev)
}

watch(endpoints, (list) => {
  ;(list || []).forEach(syncFromEndpoint)
}, { immediate: true })

// 行数组变化时同步写回正式字段（供 SettingsView 统一保存）。
// 写入前先比对，避免自身写入再次触发深度监听形成无限递归（Maximum recursive updates）。
const sameMap = (cur, next) => {
  const keys = Object.keys(cur || {})
  if (keys.length !== Object.keys(next).length) return false
  return keys.every((k) => cur[k] === next[k])
}

watch(endpoints, (list) => {
  ;(list || []).forEach((ep) => {
    if (!ep._initialized) return
    const headers = mapFromRows(ep._headersRows || [])
    if (!sameMap(ep.headers, headers)) ep.headers = headers
    const query = mapFromRows(ep._queryRows || [])
    if (!sameMap(ep.query, query)) ep.query = query
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

const authPresetLabel = (ep) => {
  const preset = ep?._authPreset
  if (preset === 'bearer') return 'Bearer Token'
  if (preset === 'apikey') return 'API Key'
  if (preset === 'urltoken') return 'Token'
  return 'Token 值'
}

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

  &.is-error {
    color: @error-color;
  }
}

.event-perm {
  width: 100%;
}

.event-groups {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}

.event-group {
  border: 1px solid @border-color-light;
  border-radius: @border-radius-sm;
  padding: 6px 10px 8px;

  :deep(.el-checkbox-group) {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  :deep(.el-checkbox) {
    height: auto;
    margin-right: 0;
  }

  :deep(.el-checkbox__label) {
    display: inline-flex;
    align-items: baseline;
    gap: 8px;
    white-space: normal;
    line-height: 1.6;
  }
}

.event-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.event-group-title {
  font-size: @font-size-xs;
  font-weight: 600;
  color: @text-color;
}

.event-group-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.event-name {
  font-family: @font-family-mono;
  font-size: @font-size-xs;
}

.event-desc {
  color: @text-color-secondary;
  font-size: @font-size-xs;
}

.event-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 4px;
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
