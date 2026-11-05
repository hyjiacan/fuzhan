<template>
  <div class="admin-webhook">
    <div class="header-section">
      <div class="header-left">
        <h2>Webhook 推送</h2>
        <p class="description">把系统关键事件（任务/安全/文件/认证等）异步推送给外部接收端</p>
      </div>
      <div class="header-right">
        <div class="dirty-tip" v-if="isDirty">
          <span class="dot"></span>有未保存的更改
        </div>
        <el-button :disabled="!loaded" @click="openGlobalModal">
          <el-icon><Setting /></el-icon>
          全局参数
        </el-button>
        <el-button :disabled="!loaded || !isDirty" @click="handleReset">重置</el-button>
        <el-button type="primary" :loading="saving" :disabled="!loaded || !isDirty" @click="saveConfig">
          保存配置{{ isDirty ? '' : ' ✓' }}
        </el-button>
      </div>
    </div>

    <el-skeleton v-if="!loaded && !loadError" :rows="6" animated style="padding: 24px;" />
    <el-empty v-else-if="loadError" description="配置加载失败，请稍后重试">
      <el-button type="primary" @click="loadConfig">重新加载</el-button>
    </el-empty>
    <template v-else>
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

        <el-form-item v-if="allTestResults.length" label-width="0">
          <div class="test-results">
            <div v-for="(r, i) in allTestResults" :key="i" class="test-result-line" :class="{ ok: r.success }">
              <span class="test-status">{{ r.success ? '成功' : '失败' }}</span>
              [{{ r.name || r.url }}] HTTP {{ r.statusCode || '-' }} · {{ r.elapsedMs }}ms
              <span v-if="r.error" class="test-error">—— {{ r.error }}</span>
            </div>
          </div>
        </el-form-item>
      </el-card>
    </template>

    <el-dialog v-model="showGlobalModal" title="Webhook 全局参数" width="640px" :close-on-click-modal="false">
      <el-form autocomplete="off" label-width="140px">
        <el-form-item label="启用 Webhook">
          <el-switch v-model="globalDraft.enabled" />
          <div class="field-hint" style="margin-left: 12px;">启用后将关键事件推送到各回调地址</div>
        </el-form-item>

        <el-form-item label="默认签名密钥">
          <el-input v-model="globalDraft.secret" type="password" show-password placeholder="留空则不签名（依赖 HTTPS 保护）" autocomplete="new-password" />
          <div class="field-hint">端点密钥留空时继承此处；配置后每条推送体计算 HMAC-SHA256，写入响应头 X-Fuzhan-Signature（sha256=&lt;hex&gt;）</div>
        </el-form-item>

        <el-form-item label="默认超时(秒)">
          <el-input-number v-model="globalDraft.timeoutSeconds" :min="1" :max="120" style="width: 200px;" />
          <div class="field-hint">单次 HTTP 请求超时时间，端点可覆盖</div>
        </el-form-item>
        <el-form-item label="默认失败重试">
          <el-input-number v-model="globalDraft.maxRetries" :min="0" :max="10" style="width: 200px;" />
          <div class="field-hint">发送失败后按指数退避重试的次数，端点可覆盖</div>
        </el-form-item>
        <el-form-item label="队列容量">
          <el-input-number v-model="globalDraft.queueSize" :min="1" :max="100000" style="width: 200px;" />
          <div class="field-hint">进程内事件队列上限，超限丢弃以保护服务（重启生效）</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-space>
          <el-button @click="showGlobalModal = false">取消</el-button>
          <el-button type="primary" :loading="globalSaving" @click="saveGlobal">保存</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Setting } from '@element-plus/icons-vue'
import { ConfigApi, SystemApi } from '@/api'
import store from '@/store'

const loaded = ref(false)
const loadError = ref(false)
const saving = ref(false)

// 全局参数 + 端点列表（等效原 settings.webhook 切片）
const data = reactive({
  enabled: false,
  urls: [],
  endpoints: [],
  secret: '',
  timeoutSeconds: 10,
  maxRetries: 3,
  queueSize: 1024
})

// 脏标记基线
const baseline = ref('')
const serialize = (v) => JSON.stringify(v, (key, value) => (key.startsWith('_') ? undefined : value))
const isDirty = computed(() => baseline.value !== '' && serialize(data) !== baseline.value)
const markClean = () => { baseline.value = serialize(data) }

// 端点待保存 DTO（剔除 UI 临时字段）
const webhookEndpointPayload = (ep) => ({
  name: ep.name,
  url: ep.url,
  enabled: ep.enabled,
  events: Array.isArray(ep.events) ? ep.events : [],
  secret: ep.secret,
  timeoutSeconds: ep.timeoutSeconds,
  maxRetries: ep.maxRetries,
  headers: ep.headers || {},
  query: ep.query || {}
})

// 全局参数弹窗：草稿编辑，取消即丢弃；保存时连同当前端点一并提交
const showGlobalModal = ref(false)
const globalSaving = ref(false)
const globalDraft = ref({
  enabled: false,
  secret: '',
  timeoutSeconds: 10,
  maxRetries: 3,
  queueSize: 1024
})

const openGlobalModal = () => {
  globalDraft.value = {
    enabled: data.enabled,
    secret: data.secret,
    timeoutSeconds: data.timeoutSeconds,
    maxRetries: data.maxRetries,
    queueSize: data.queueSize
  }
  showGlobalModal.value = true
}

const loadConfig = async () => {
  loadError.value = false
  try {
    const res = await ConfigApi.get()
    if (!res.success || !res.data) throw new Error('配置数据为空')
    const cfg = res.data
    const wh = cfg.webhook || {}
    data.enabled = wh.enabled ?? false
    data.urls = wh.urls || []
    data.endpoints = (wh.endpoints || []).map((ep) => ({
      ...ep,
      enabled: ep.enabled !== false,
      events: Array.isArray(ep.events) ? ep.events : [],
      headers: ep.headers || {},
      query: ep.query || {}
    }))
    data.secret = wh.secret || ''
    data.timeoutSeconds = wh.timeoutSeconds || 10
    data.maxRetries = wh.maxRetries ?? 3
    data.queueSize = wh.queueSize ?? 1024
    markClean()
    loaded.value = true
  } catch (e) {
    console.error('加载 Webhook 配置失败', e)
    ElMessage.error('加载配置失败')
    loaded.value = false
    loadError.value = true
  }
}

// 组装 Webhook 提交切片：全局参数 + 当前端点（保存整个 webhook 节点时需一并提交端点）
const webhookPayload = (globals) => ({
  enabled: globals.enabled,
  urls: data.urls,
  endpoints: data.endpoints.map(webhookEndpointPayload),
  secret: globals.secret,
  timeoutSeconds: globals.timeoutSeconds,
  maxRetries: globals.maxRetries,
  queueSize: globals.queueSize
})

const saveConfig = async () => {
  if (!validateWebhookPermissions()) return
  saving.value = true
  try {
    const res = await ConfigApi.save({ webhook: webhookPayload(data) })
    if (res.success) {
      ElMessage.success(res.message || '配置已保存并生效')
      markClean()
      refreshStoreOptions()
    } else {
      ElMessage.error(res.message || '保存失败')
    }
  } catch (e) {
    console.error('保存 Webhook 配置失败', e)
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

// 弹窗内保存全局参数：成功后回写 data 并同步基线
const saveGlobal = async () => {
  if (!validateWebhookPermissions(globalDraft.value.enabled)) return
  globalSaving.value = true
  try {
    const res = await ConfigApi.save({ webhook: webhookPayload({ ...data, ...globalDraft.value }) })
    if (res.success) {
      Object.assign(data, globalDraft.value)
      markClean()
      refreshStoreOptions()
      showGlobalModal.value = false
      ElMessage.success(res.message || '全局参数已保存并生效')
    } else {
      ElMessage.error(res.message || '保存失败')
    }
  } catch (e) {
    console.error('保存 Webhook 全局参数失败', e)
    ElMessage.error('保存失败')
  } finally {
    globalSaving.value = false
  }
}

const handleReset = () => {
  loadConfig()
}

// 保存后刷新前端公共配置（webhook 开关不直接影响 store 字段，统一走 getOptions 刷新一次）
const refreshStoreOptions = async () => {
  try {
    const optionsRes = await SystemApi.getOptions()
    if (optionsRes.data) {
      store.setConfig({
        appName: optionsRes.data.app?.name || '',
        privateStorageEnabled: optionsRes.data.privateStorage?.enabled ?? false,
        tempFilesEnabled: optionsRes.data.tempFiles?.enabled ?? true,
        ftpEnabled: optionsRes.data.ftp?.enabled ?? false,
        ftpPort: optionsRes.data.ftp?.port ?? null,
        webdavEnabled: optionsRes.data.webdav?.enabled ?? false,
        openApiEnabled: optionsRes.data.openApi?.enabled ?? false,
        allowRegistration: optionsRes.data.account?.allowRegistration ?? false,
        requireLogin: optionsRes.data.account?.requireLogin ?? false,
        captchaMode: optionsRes.data.account?.captcha?.mode ?? 'disabled',
        captchaTriggerAfter: optionsRes.data.account?.captcha?.triggerAfter ?? 3,
        anonymousUsername: optionsRes.data.webdav?.publicUsername || optionsRes.data.account?.anonymous?.username || ''
      })
    }
  } catch (e) {
    // 刷新失败静默处理，不影响保存结果
  }
}

// Webhook 事件权限校验：启用 Webhook 时启用中的端点必须显式指定事件
const validateWebhookPermissions = (enabled = data.enabled) => {
  if (!enabled) return true
  const bad = data.endpoints.find((ep) => ep.enabled !== false && !(ep.events && ep.events.length))
  if (!bad) return true
  ElMessage.error(`回调地址「${bad.name || bad.url || '未命名'}」未选择任何事件，请在「事件权限」中勾选事件或开启「接收全部事件」`)
  return false
}

// —— 以下端点操作逻辑与原 SettingsWebhook 保持一致 ——
const testingIndex = ref(-1)
const testingAll = ref(false)
const allTestResults = ref([])

const events = ref([])

const loadEvents = async () => {
  try {
    const res = await ConfigApi.webhookEvents()
    if (res.success && res.data) events.value = res.data
  } catch (e) {
    console.error('加载 Webhook 事件目录失败:', e)
  }
}

const eventGroups = computed(() => {
  const map = {}
  for (const ev of events.value) {
    const cat = ev.category || '其他'
    if (!map[cat]) map[cat] = { label: cat, items: [] }
    map[cat].items.push(ev)
  }
  return Object.values(map)
})

const endpoints = computed(() => data.endpoints || [])

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
    ep._allEvents = Array.isArray(ep.events) && ep.events.includes('*')
    if (!Array.isArray(ep.events)) ep.events = []
    ep._testResult = null
  }
}

const toggleAllEvents = (ep, all) => {
  ep.events = all ? ['*'] : []
}

const setGroupEvents = (ep, cat, checked) => {
  const names = cat.items.map((i) => i.event)
  const rest = (ep.events || []).filter((e) => e !== '*' && !names.includes(e))
  ep.events = checked ? [...rest, ...names] : rest
}

const catalogEventNames = computed(() => new Set((events.value || []).map((e) => e.event)))
const customEvents = (ep) => (ep.events || []).filter((e) => e !== '*' && !catalogEventNames.value.has(e))
const removeEvent = (ep, ev) => {
  ep.events = (ep.events || []).filter((e) => e !== ev)
}

watch(endpoints, (list) => {
  ;(list || []).forEach(syncFromEndpoint)
}, { immediate: true })

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
  data.endpoints.push(ep)
  syncFromEndpoint(ep)
}

const removeEndpoint = (idx) => {
  data.endpoints.splice(idx, 1)
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

onMounted(() => {
  loadConfig()
  loadEvents()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-webhook {
  padding: @spacing-md @spacing-lg;

  .header-section {
    margin-bottom: 16px;

    .description {
      font-size: @font-size-xs;
    }
  }
}

.field-hint {
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
  width: 100%;
}

.event-group {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: @border-radius;
  padding: 10px 12px;

  .event-group-head {
    display: flex;
    align-items: center;
    color: @text-color-secondary;
    font-size: 12px;
    margin-bottom: 6px;
    .event-group-title { font-weight: 600; }
    .event-group-actions { margin-left: auto; }
  }

  .el-checkbox { margin-right: 16px; }
  .event-name { font-family: @font-family-mono; }
  .event-desc { color: @text-color-secondary; margin-left: 6px; }
}

.event-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.endpoint-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.endpoint-card {
  &.is-disabled { opacity: .7; }
  .endpoint-head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    .endpoint-url { flex: 1; min-width: 120px; color: @text-color-secondary; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .endpoint-actions { display: inline-flex; gap: 4px; }
  }
  .auth-wrapper { display: flex; align-items: center; }
}

.kv-editor {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;

  .kv-row { display: flex; align-items: center; gap: 8px; }
}

.endpoint-test-result,
.test-result-line {
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 12px;
  padding: 6px 10px;
  border-radius: @border-radius-sm;
  background: rgba(208, 48, 80, 0.08);
  color: @error-color;

  &.ok {
    background: rgba(24, 160, 88, 0.08);
    color: @success-color;
  }
  .test-status { font-weight: 600; }
  .test-url { font-family: @font-family-mono; }
  .test-error { color: @error-color; }
}

.test-results {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
}
</style>
