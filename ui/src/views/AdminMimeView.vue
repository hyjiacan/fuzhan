<template>
  <div class="admin-mime">
    <div class="header-section">
      <div class="header-left">
        <h2>MIME 映射</h2>
        <p class="description">维护「扩展名 → MIME 类型」映射，替代/补充硬编码的内置表</p>
      </div>
      <div class="header-right">
        <div class="dirty-tip" v-if="isDirty">
          <span class="dot"></span>有未保存的更改
        </div>
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
      <!-- 自定义映射列表 -->
      <el-card class="mime-card" shadow="never">
        <template #header>
          <div class="card-head">
            <span class="card-title">自定义条目（{{ mimeList.length }}）</span>
            <div class="card-actions">
              <el-input
                v-model="keyword"
                class="search-input"
                placeholder="搜索扩展名或 MIME"
                clearable
              />
              <el-button type="primary" @click="openAdd">添加映射</el-button>
            </div>
          </div>
        </template>
        <el-table :data="filteredList" style="width: 100%" :empty-text="emptyText">
          <el-table-column label="扩展名" prop="ext" min-width="160">
            <template #default="{ row }">
              <el-tag type="info" effect="plain" class="mime-ext">.{{ row.ext }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="MIME 类型" prop="mime" min-width="300">
            <template #default="{ row }">
              <span class="mime-value">{{ row.mime }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="90" align="right">
            <template #default="{ row }">
              <el-button link type="danger" @click="removeEntry(row.ext)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <!-- 添加映射弹框 -->
      <el-dialog v-model="addVisible" title="添加 MIME 映射" width="600px">
        <el-input
          autocomplete="off"
          v-model="batchText"
          type="textarea"
          placeholder="每行一条，如：&#10;epub=application/epub+zip&#10;exe application/x-msdownload&#10;ipa:application/octet-stream"
          :rows="8"
        />
        <div class="field-hint">
          每行一个条目，支持 <code>扩展名=MIME</code>、<code>扩展名 MIME</code> 或 <code>扩展名:MIME</code>，重复扩展名将覆盖；自动去除点号前缀并忽略空行
        </div>
        <template #footer>
          <div class="dialog-footer">
            <el-button @click="batchText = ''">清空</el-button>
            <div class="dialog-footer-right">
              <el-button @click="addVisible = false">取消</el-button>
              <el-button type="primary" @click="handleBatchAdd">添加</el-button>
            </div>
          </div>
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { ConfigApi } from '@/api'

const loaded = ref(false)
const loadError = ref(false)
const saving = ref(false)

const addVisible = ref(false)
const batchText = ref('')
const keyword = ref('')

// 自定义条目表（等效原 settings.mimeTable）
const mimeTable = reactive({})

// 脏标记基线
const baseline = ref('')
const isDirty = computed(() => baseline.value !== '' && JSON.stringify(mimeTable) !== baseline.value)
const markClean = () => { baseline.value = JSON.stringify(mimeTable) }

// 自定义条目列表
const mimeList = computed(() => {
  return Object.keys(mimeTable)
    .sort()
    .map(ext => ({ ext, mime: mimeTable[ext] }))
})

// 搜索过滤
const filteredList = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return mimeList.value
  return mimeList.value.filter(item =>
    item.ext.includes(kw) || item.mime.toLowerCase().includes(kw)
  )
})

const emptyText = computed(() => keyword.value.trim() ? '未找到匹配条目' : '暂无自定义条目')

const loadConfig = async () => {
  loadError.value = false
  try {
    const res = await ConfigApi.get()
    if (!res.success || !res.data) throw new Error('配置数据为空')
    const cfg = res.data
    const mt = cfg.mime_table || {}
    for (const k of Object.keys(mimeTable)) delete mimeTable[k]
    Object.assign(mimeTable, mt)
    markClean()
    loaded.value = true
  } catch (e) {
    console.error('加载 MIME 配置失败', e)
    ElMessage.error('加载配置失败')
    loaded.value = false
    loadError.value = true
  }
}

const saveConfig = async () => {
  saving.value = true
  try {
    const res = await ConfigApi.save({ mime_table: { ...mimeTable } })
    if (res.success) {
      ElMessage.success(res.message || '配置已保存并生效')
      markClean()
    } else {
      ElMessage.error(res.message || '保存失败')
    }
  } catch (e) {
    console.error('保存 MIME 配置失败', e)
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

const handleReset = () => {
  loadConfig()
}

const openAdd = () => {
  addVisible.value = true
}

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
    mimeTable[entry.ext] = entry.mime
    added++
  })
  if (added > 0) {
    ElMessage.success(`成功添加 ${added} 条映射`)
    batchText.value = ''
    addVisible.value = false
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
  delete mimeTable[ext]
  ElMessage.success(`已删除 .${ext}`)
}

loadConfig()
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-mime {
  padding: @spacing-md @spacing-lg;

  .header-section {
    margin-bottom: 16px;

    .description {
      font-size: @font-size-xs;
    }
  }
}

.mime-card {
  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }

  .card-title {
    font-weight: 500;
  }

  .card-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .search-input {
    width: 240px;
  }
}

.mime-ext {
  font-family: var(--el-font-family-mono, monospace);
}

.mime-value {
  font-family: var(--el-font-family-mono, monospace);
  word-break: break-all;
}

.dialog-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;

  .dialog-footer-right {
    display: flex;
    gap: 8px;
  }
}

@media (max-width: 768px) {
  .mime-card .search-input {
    width: 100%;
  }
}
</style>
