<template>
  <div class="settings-mime">
    <!-- 添加 -->
    <el-row :gutter="16" style="margin-top: 16px;">
      <el-col :xs="24">
        <el-card class="mime-card">
          <template #header><span>添加</span></template>
          <el-input
            v-model="batchText"
            type="textarea"
            placeholder="每行一条，如：&#10;epub=application/epub+zip&#10;exe application/x-msdownload&#10;ipa:application/octet-stream"
            :rows="8"
          />
          <div class="field-hint">每行一个条目，支持 <code>扩展名=MIME</code>、<code>扩展名 MIME</code> 或 <code>扩展名:MIME</code>，重复扩展名将覆盖；自动去除点号前缀并忽略空行</div>
          <div style="margin-top: 12px; text-align: right;">
            <el-button type="primary" @click="handleBatchAdd">添加</el-button>
            <el-button @click="batchText = ''">清空</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 自定义映射列表 -->
    <el-row :gutter="16">
      <el-col :span="24">
        <el-card class="mime-card">
          <template #header>
            <span>自定义条目（{{ mimeList.length }}）</span>
          </template>
          <el-table :data="mimeList" style="width: 100%" empty-text="暂无自定义条目">
            <el-table-column label="扩展名" prop="ext" min-width="140">
              <template #default="{ row }">
                <span class="mime-ext">.{{ row.ext }}</span>
              </template>
            </el-table-column>
            <el-table-column label="MIME 类型" prop="mime" min-width="260" />
            <el-table-column label="操作" width="90" align="right">
              <template #default="{ row }">
                <el-button link type="danger" @click="removeEntry(row.ext)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'

const props = defineProps({
  settings: { type: Object, required: true }
})

const batchText = ref('')

// 自定义条目列表（从 settings.mimeTable 派生）
const mimeList = computed(() => {
  const table = props.settings.mimeTable || {}
  return Object.keys(table)
    .sort()
    .map(ext => ({ ext, mime: table[ext] }))
})

// 解析单个「扩展名 与 MIME」条目
const parseEntry = (line) => {
  const text = (line || '').trim()
  if (!text) return null
  let ext = ''
  let mime = ''
  const arrow = text.indexOf('->')
  const eq = text.indexOf('=')
  const colon = text.indexOf(':')
  let sep = -1
  let skip = 1
  if (arrow !== -1 && (eq === -1 || arrow < eq) && (colon === -1 || arrow < colon)) {
    sep = arrow
    skip = 2
  } else if (eq !== -1 && (colon === -1 || eq < colon)) {
    sep = eq
  } else if (colon !== -1) {
    sep = colon
  }
  if (sep !== -1) {
    ext = text.slice(0, sep)
    mime = text.slice(sep + skip)
  } else {
    const m = text.match(/^(\S+)\s+(\S.*)$/)
    if (m) {
      ext = m[1]
      mime = m[2]
    }
  }
  ext = ext.trim().replace(/^\.+/, '').toLowerCase()
  mime = mime.trim()
  if (!ext || !mime) return null
  return { ext, mime }
}

// 批量添加
const handleBatchAdd = () => {
  const lines = (batchText.value || '').split('\n')
  let added = 0
  let skipped = 0
  lines.forEach((line) => {
    const entry = parseEntry(line)
    if (!entry) {
      if (line.trim()) skipped++
      return
    }
    props.settings.mimeTable[entry.ext] = entry.mime
    added++
  })
  if (added > 0) {
    ElMessage.success(`成功添加 ${added} 条映射`)
    batchText.value = ''
  }
  if (skipped > 0) {
    ElMessage.warning(`${skipped} 行格式无法识别，已忽略（每行需为 扩展名 与 MIME，如 epub=application/epub+zip）`)
  }
  if (added === 0 && skipped === 0) {
    ElMessage.info('请输入要添加的映射')
  }
}

// 删除
const removeEntry = (ext) => {
  delete props.settings.mimeTable[ext]
  ElMessage.success(`已删除 .${ext}`)
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.mime-card {
  transition: transform @transition-smooth, box-shadow @transition-smooth;

  &:hover {
    box-shadow: @shadow-md;
  }
}

.mime-ext {
  font-family: var(--el-font-family-mono, monospace);
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;
  margin-top: 8px;
}
</style>