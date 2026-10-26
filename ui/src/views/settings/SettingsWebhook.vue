<template>
  <div class="settings-webhook">
    <el-card>
      <template #header>
        <div class="card-head">
          <span>Webhook 推送</span>
          <span class="card-sub">把系统关键事件（任务/安全/配置变更）异步推送给外部接收端</span>
        </div>
      </template>
      <el-form label-width="140px">
        <el-form-item label="启用 Webhook">
          <el-switch v-model="settings.webhook.enabled" />
          <div class="field-hint" style="margin-left: 12px;">启用后将关键事件推送到下方回调地址</div>
        </el-form-item>

        <el-form-item label="回调地址">
          <el-input
            v-model="urlsDisplay"
            type="textarea"
            :rows="5"
            placeholder="每行一个回调 URL，如 https://example.com/hook/fuzhan"
          />
          <div class="field-hint">事件将以 HTTP POST + JSON 发送到每个地址；接收方可用 X-Fuzhan-Signature 校验来源合法性</div>
        </el-form-item>

        <el-form-item label="签名密钥">
          <el-input v-model="settings.webhook.secret" type="password" show-password placeholder="留空则不签名（依赖 HTTPS 保护）" autocomplete="new-password" />
          <div class="field-hint">配置后每条推送体计算 HMAC-SHA256，写入响应头 X-Fuzhan-Signature（sha256=&lt;hex&gt;）</div>
        </el-form-item>

        <el-row :gutter="16">
          <el-col :span="8" :sm="8" :xs="24">
            <el-form-item label="超时(秒)">
              <el-input-number v-model="settings.webhook.timeoutSeconds" :min="1" :max="120" style="width: 100%;" />
              <div class="field-hint">单次 HTTP 请求超时时间</div>
            </el-form-item>
          </el-col>
          <el-col :span="8" :sm="8" :xs="24">
            <el-form-item label="失败重试">
              <el-input-number v-model="settings.webhook.maxRetries" :min="0" :max="10" style="width: 100%;" />
              <div class="field-hint">发送失败后按指数退避重试的次数</div>
            </el-form-item>
          </el-col>
          <el-col :span="8" :sm="8" :xs="24">
            <el-form-item label="队列容量">
              <el-input-number v-model="settings.webhook.queueSize" :min="1" :max="100000" style="width: 100%;" />
              <div class="field-hint">进程内事件队列上限，超限丢弃以保护服务</div>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item>
          <el-button type="warning" :loading="testing" @click="testConnection">测试连接</el-button>
          <span class="field-hint" style="margin-left: 12px;">向当前回调地址发送一条 test.ping 测试事件</span>
        </el-form-item>
        <el-form-item v-if="testResults.length" label="测试结果">
          <div class="test-results">
            <div v-for="(r, i) in testResults" :key="i" class="test-result-line" :class="{ ok: r.indexOf('成功') > -1 }">
              {{ r }}
            </div>
          </div>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card>
      <template #header><span>支持的事件</span></template>
      <div class="event-table">
        <div class="event-row" v-for="ev in supportedEvents" :key="ev.event">
          <div class="event-code">{{ ev.event }}</div>
          <div class="event-desc">{{ ev.desc }}</div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { ConfigApi } from '@/api'

const props = defineProps({
  settings: { type: Object, required: true }
})

const testing = ref(false)
const testResults = ref([])

// 回调地址显示转换（数组 <-> 文本框）
const urlsDisplay = computed({
  get: () => (props.settings.webhook?.urls || []).join('\n'),
  set: (val) => {
    props.settings.webhook.urls = val.split('\n').map(s => s.trim()).filter(Boolean)
  }
})

const supportedEvents = [
  { event: 'task.started', desc: '系统作业开始执行（扫描/哈希/清理/URL 下载等）' },
  { event: 'task.finished', desc: '系统作业完成' },
  { event: 'task.failed', desc: '系统作业失败（含进程重启中断的对账记录）' },
  { event: 'task.canceled', desc: '系统作业被取消' },
  { event: 'security.event', desc: '安全事件（登录暴破/越权拒绝/频率限制/DDoS 告警）' },
  { event: 'config.saved', desc: '配置保存成功' }
]

const testConnection = async () => {
  const wh = props.settings.webhook
  if (!wh.urls || !wh.urls.length) {
    ElMessage.warning('请先填写至少一个回调地址')
    return
  }
  testing.value = true
  testResults.value = []
  try {
    const res = await ConfigApi.testWebhook({
      enabled: wh.enabled,
      urls: wh.urls,
      secret: wh.secret,
      timeoutSeconds: wh.timeoutSeconds,
      maxRetries: wh.maxRetries,
      queueSize: wh.queueSize
    })
    if (res.success) {
      testResults.value = res.data || []
      const allOk = testResults.value.every(r => r.indexOf('成功') > -1)
      if (allOk) ElMessage.success('所有回调地址测试成功')
    } else {
      testResults.value = [res.message || '测试失败']
      ElMessage.error(res.message || '测试失败')
    }
  } catch (e) {
    console.error('测试 Webhook 失败', e)
    ElMessage.error('测试失败：' + (e.message || '网络错误'))
  } finally {
    testing.value = false
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
  align-items: baseline;
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
}

.event-table {
  display: flex;
  flex-direction: column;
}

.event-row {
  display: flex;
  align-items: baseline;
  gap: 16px;
  padding: 6px 0;
  border-bottom: 1px solid @border-color;

  &:last-child {
    border-bottom: none;
  }
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
