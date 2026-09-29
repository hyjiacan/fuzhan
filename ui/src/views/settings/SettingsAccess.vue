<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>IP 访问控制</span></template>
        <el-form label-width="120px">
          <el-form-item label="访问模式">
            <el-radio-group v-model="settings.openApi.ipAccessMode">
              <el-radio label="allow">白名单模式</el-radio>
              <el-radio label="deny">黑名单模式</el-radio>
              <el-radio label="none">不限制</el-radio>
            </el-radio-group>
            <div class="field-hint">白名单和黑名单不能同时生效</div>
          </el-form-item>
          <el-form-item label="IP 列表" v-if="settings.openApi.ipAccessMode !== 'none'">
            <el-input
              v-model="ipAccessListDisplay"
              type="textarea"
              :placeholder="settings.openApi.ipAccessMode === 'allow' ? '每行一个 IP 或 CIDR，如 192.168.1.0/24' : '每行一个 IP 或 CIDR'"
              :rows="5"
            />
            <div class="field-hint">{{ settings.openApi.ipAccessMode === 'allow' ? '白名单中的 IP 允许访问' : '黑名单中的 IP 将被拒绝访问' }}</div>
          </el-form-item>
          <el-form-item label="频率限制">
            <el-switch v-model="settings.openApi.rateLimitEnabled" />
            <div class="field-hint">限制调用频率，防止滥用</div>
          </el-form-item>
          <el-form-item label="请求频率" v-if="settings.openApi.rateLimitEnabled">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input-number v-model="settings.openApi.requestsPerMinute" :min="1" :max="10000" />
              <span>次/分钟</span>
            </div>
            <div class="field-hint">API 调用频率上限；超过后该请求会被拒绝</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>文件访问控制</span></template>
        <el-form label-width="120px">
          <el-form-item label="允许的扩展名">
            <el-input
              v-model="extensionsDisplay"
              type="textarea"
              placeholder="每行一个扩展名，如 txt、pdf、jpg"
              :rows="6"
            />
            <div class="field-hint">留空允许所有扩展名；每行输入一个允许的扩展名，不含点号</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>下载频率限制</span></template>
        <el-form label-width="120px">
          <el-form-item label="频率限制">
            <el-switch :model-value="rateLimitEnabled" @update:model-value="setRateLimitEnabled" />
            <div class="field-hint">限制免认证下载（分享码 / 临时下载）每个 IP 在窗口内的最大请求数</div>
          </el-form-item>
          <template v-if="rateLimitEnabled">
            <el-form-item label="窗口(分钟)">
              <el-input-number v-model="settings.download.rateLimit.windowMinutes" :min="1" :max="10080" />
              <div class="field-hint">在一个窗口内累计请求数，窗口结束后计数重置</div>
            </el-form-item>
            <el-form-item label="最大请求数">
              <el-input-number v-model="settings.download.rateLimit.maxRequests" :min="1" :max="100000" />
              <div class="field-hint">单个 IP 在窗口内允许的最大请求数，超出后暂时拒绝</div>
            </el-form-item>
          </template>
          <el-form-item label="失效锁定">
            <el-switch :model-value="lockEnabled" @update:model-value="setLockEnabled" />
            <div class="field-hint">窗口内无效访问（不存在的分享码/下载码）达阈值即锁定该 IP</div>
          </el-form-item>
          <template v-if="lockEnabled">
            <el-form-item label="失效阈值">
              <el-input-number v-model="settings.download.rateLimit.lockAfter" :min="1" :max="10000" />
              <span style="margin-left: 8px;">次</span>
              <div class="field-hint">过期失效的请求达到多少次即触发锁定</div>
            </el-form-item>
            <el-form-item label="锁定(分钟)">
              <el-input-number v-model="settings.download.rateLimit.lockMinutes" :min="1" :max="10080" />
              <div class="field-hint">达到阈值后 IP 被禁用的时长</div>
            </el-form-item>
          </template>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>安全配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="信任代理头">
            <el-switch v-model="settings.security.trustProxy" />
            <div class="field-hint">信任 X-Forwarded-For / X-Real-IP 以取真实客户端 IP，置于反向代理后时应开启</div>
          </el-form-item>
          <el-form-item label="CORS 来源白名单">
            <el-input v-model="allowedOriginsDisplay" type="textarea" :rows="3" :maxlength="2000"
              placeholder="每行一个来源，如 https://a.example.com、*" style="width: 100%;" />
            <div class="field-hint">允许跨域访问的来源列表，每行一个；留空或 <code>*</code> 表示允许所有来源</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    </el-row>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  settings: { type: Object, required: true }
})

// 下载频率限制开关：maxRequests>0 表示启用
const rateLimitEnabled = computed(() => (props.settings.download?.rateLimit?.maxRequests || 0) > 0)
const setRateLimitEnabled = (val) => {
  if (val && props.settings.download.rateLimit.maxRequests <= 0) {
    props.settings.download.rateLimit.maxRequests = 120
  } else if (!val) {
    props.settings.download.rateLimit.maxRequests = 0
  }
}

// 失效锁定开关：lockAfter>0 表示启用
const lockEnabled = computed(() => (props.settings.download?.rateLimit?.lockAfter || 0) > 0)
const setLockEnabled = (val) => {
  if (val && props.settings.download.rateLimit.lockAfter <= 0) {
    props.settings.download.rateLimit.lockAfter = 20
  } else if (!val) {
    props.settings.download.rateLimit.lockAfter = 0
  }
}

// 文件扩展名显示转换（数组 <-> 文本框）
const extensionsDisplay = computed({
  get: () => (props.settings.allowedExtensions || []).join('\n'),
  set: (val) => {
    props.settings.allowedExtensions = val.split('\n').map(s => s.trim()).filter(Boolean)
  }
})

// IP 访问列表显示转换（数组 <-> 文本框）
const ipAccessListDisplay = computed({
  get: () => {
    if (props.settings.openApi.ipAccessMode === 'allow') return (props.settings.openApi.ipWhitelist || '').split('\n').filter(s => s.trim()).join('\n')
    if (props.settings.openApi.ipAccessMode === 'deny') return (props.settings.openApi.ipBlacklist || '').split('\n').filter(s => s.trim()).join('\n')
    return ''
  },
  set: (val) => {
    const list = val.split('\n').map(s => s.trim()).filter(Boolean)
    if (props.settings.openApi.ipAccessMode === 'allow') {
      props.settings.openApi.ipWhitelist = list.join('\n')
    } else if (props.settings.openApi.ipAccessMode === 'deny') {
      props.settings.openApi.ipBlacklist = list.join('\n')
    }
  }
})

// CORS 来源列表显示转换（数组 <-> 文本框）
const allowedOriginsDisplay = computed({
  get: () => (props.settings.security?.allowedOrigins || []).join('\n'),
  set: (val) => {
    props.settings.security.allowedOrigins = val.split('\n').map(s => s.trim()).filter(Boolean)
  }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.el-card {
  transition: transform @transition-smooth, box-shadow @transition-smooth;

  &:hover {
    box-shadow: @shadow-md;
  }
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;
}

.full-col {
  margin-bottom: 16px;
}
</style>