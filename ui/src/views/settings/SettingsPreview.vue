<template>
  <el-row :gutter="16">
    <el-col :span="24">
      <el-card>
        <template #header><span>全局预览开关</span></template>
        <div class="switch-global">
          <el-switch v-model="settings.preview.enabled" />
          <div class="switch-global-text">
            <div class="switch-title">启用文件预览</div>
            <div class="field-hint">控制整个系统的文件预览功能。关闭后所有类型（含各分组与自定义）的预览入口均不可用，文件只能下载查看</div>
          </div>
        </div>
      </el-card>
    </el-col>

    <el-col v-for="g in previewGroups" :key="g.key" :span="24" :md="12" :xs="24">
      <el-card class="group-card" :class="{ disabled: !settings.preview.enabled }">
        <template #header>
          <div class="group-header">
            <span>{{ g.label }}</span>
            <div class="group-actions">
              <el-button link type="primary" size="small" @click="resetGroup(g.key)">恢复默认</el-button>
              <el-switch v-model="settings.preview.groups[g.key].enabled" :disabled="!settings.preview.enabled" />
            </div>
          </div>
        </template>
        <div class="field-hint group-desc">{{ g.desc }}</div>
        <el-form autocomplete="off" label-width="90px">
          <el-form-item label="MIME 类型">
            <el-input autocomplete="off"
              v-model="settings.preview.groups[g.key].mimes"
              :maxlength="1024"
              type="textarea"
              :rows="2"
              :placeholder="g.mimesPlaceholder"
              :disabled="!settings.preview.enabled"
            />
            <div class="field-hint">{{ g.mimesHint }}</div>
          </el-form-item>
          <el-form-item label="文件扩展名">
            <el-input autocomplete="off"
              v-model="settings.preview.groups[g.key].exts"
              :maxlength="1024"
              type="textarea"
              :rows="2"
              :placeholder="g.extsPlaceholder"
              :disabled="!settings.preview.enabled"
            />
            <div class="field-hint">{{ g.extsHint }}</div>
          </el-form-item>
          <template v-if="g.key === 'text'">
            <el-form-item label="最大内联大小">
              <div style="display: flex; align-items: center; gap: 12px;">
                <el-input autocomplete="off" v-model="maxInlineSizeDisplay" :maxlength="32" placeholder="如 1m, 2m" style="width: 200px;" :disabled="!settings.preview.enabled" />
                <span style="color: var(--el-text-color-placeholder);">{{ formatSize(settings.preview.groups.text.maxInlineSize) }}</span>
              </div>
              <div class="field-hint">文本直接返回内容的大小上限，超过则分块加载</div>
            </el-form-item>
            <el-form-item label="文本分块大小">
              <div style="display: flex; align-items: center; gap: 12px;">
                <el-input autocomplete="off" v-model="textChunkSizeDisplay" placeholder="如 100k, 200k" style="width: 200px;" :disabled="!settings.preview.enabled" />
                <span style="color: var(--el-text-color-placeholder);">{{ formatSize(settings.preview.groups.text.textChunkSize) }}</span>
              </div>
              <div class="field-hint">预览大文本文件时分块读取的大小</div>
            </el-form-item>
          </template>
        </el-form>
      </el-card>
    </el-col>
  </el-row>
</template>

<script setup>
import { computed } from 'vue'
import { NumberUtils } from '@/utils'
import { getPreviewDefaults } from '@/config/preview'
import { formatToUnit, formatSize } from './useSettingsUtils'

const props = defineProps({
  settings: { type: Object, required: true }
})

// 内置预览类型分组：与后端 utils/preview_utils.go 的分组定义一一对应。
// 各组列表为「完全可编辑」语义：命中该组已启用的 MIME 或扩展名列表即归入该组；
// 任一分组（含自定义组）列表留空即不命中任何文件，仅已配置的条目参与匹配。
const BUILTIN_LIST_HINT = '留空表示该组不命中任何文件，仅已配置条目参与匹配；分组开关关闭后本组列表不再参与判定'
const previewGroups = [
  {
    key: 'image', label: '图片',
    desc: '浏览器内嵌显示的图片文件，默认表仅含浏览器原生支持的图片类型',
    mimesPlaceholder: 'image/png,image/jpeg', mimesHint: BUILTIN_LIST_HINT,
    extsPlaceholder: 'png,jpg,jpeg,gif,webp', extsHint: BUILTIN_LIST_HINT
  },
  {
    key: 'text', label: '文本',
    desc: '文本/代码文件在线查看，Markdown 自动渲染，大文件分块加载；下方大小配置仅作用于文本预览',
    mimesPlaceholder: 'text/*,application/json', mimesHint: BUILTIN_LIST_HINT,
    extsPlaceholder: 'txt,md,log,json', extsHint: BUILTIN_LIST_HINT
  },
  {
    key: 'audio', label: '音频',
    desc: '浏览器原生音频播放器在线播放，默认表仅含浏览器原生支持的音频编码',
    mimesPlaceholder: 'audio/mpeg,audio/wav', mimesHint: BUILTIN_LIST_HINT,
    extsPlaceholder: 'mp3,wav,flac,m4a', extsHint: BUILTIN_LIST_HINT
  },
  {
    key: 'video', label: '视频',
    desc: '浏览器原生视频播放器在线播放，默认表仅含浏览器原生支持的视频编码',
    mimesPlaceholder: 'video/mp4,video/webm', mimesHint: BUILTIN_LIST_HINT,
    extsPlaceholder: 'mp4,webm,m4v,ogv', extsHint: BUILTIN_LIST_HINT
  },
  {
    key: 'document', label: '文档',
    desc: '浏览器内嵌阅读器预览的文档类型（如 PDF）',
    mimesPlaceholder: 'application/pdf', mimesHint: BUILTIN_LIST_HINT,
    extsPlaceholder: 'pdf', extsHint: BUILTIN_LIST_HINT
  },
  {
    key: 'custom', label: '自定义',
    desc: '兜底白名单：未命中任何内置分组的文件按本组列表放行（默认关闭，需显式启用）',
    mimesPlaceholder: 'application/vnd.openxmlformats-officedocument.*',
    mimesHint: '逗号分隔，支持通配符；仅已配置条目参与匹配，留空不命中任何文件',
    extsPlaceholder: 'bak,epub',
    extsHint: '逗号分隔；仅已配置条目参与匹配，留空不命中任何文件'
  }
]

// 将单个分组的开关与 MIME/扩展名列表恢复为内置默认值（默认表来自后端 options，
// 未加载成功时不做操作；文本组同时恢复大小配置）
const resetGroup = (key) => {
  const def = getPreviewDefaults()?.[key]
  if (!def) return
  const group = props.settings.preview.groups[key]
  group.enabled = def.enabled !== false
  group.mimes = def.mimes ?? ''
  group.exts = def.exts ?? ''
  if (key === 'text') {
    group.maxInlineSize = 1048576 // 1M
    group.textChunkSize = 102400 // 100K
  }
}

const maxInlineSizeDisplay = computed({
  get: () => formatToUnit(props.settings.preview.groups.text.maxInlineSize),
  set: (val) => { props.settings.preview.groups.text.maxInlineSize = NumberUtils.parseFileSize(val) }
})
const textChunkSizeDisplay = computed({
  get: () => formatToUnit(props.settings.preview.groups.text.textChunkSize),
  set: (val) => { props.settings.preview.groups.text.textChunkSize = NumberUtils.parseFileSize(val) }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.el-card {
  transition: transform @transition-smooth, box-shadow @transition-smooth;
  margin-bottom: 16px;

  &:hover {
    box-shadow: @shadow-md;
  }
}

.switch-global {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px;
  border: 1px solid @border-color-light;
  border-radius: @border-radius-lg;
  background: var(--el-fill-color-light);

  .switch-global-text {
    flex: 1;
    min-width: 0;
  }
}

.group-card {
  &.disabled {
    opacity: 0.6;
  }
}

.group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.group-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.group-desc {
  margin-bottom: 12px;
}

.switch-title {
  font-size: 14px;
  font-weight: 500;
  color: @text-color;
  margin-bottom: 2px;
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;
  line-height: 1.5;
}
</style>
