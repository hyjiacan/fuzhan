<template>
  <el-dialog v-model="visible" title="文件依赖树" width="960px" :close-on-click-modal="false">
    <!-- 当前文件 -->
    <div v-if="rootNode" :style="{ marginBottom: '12px', padding: '12px', background: '#f6f8fa', borderRadius: '8px' }">
      <div style="display: flex; align-items: center; justify-content: space-between;">
        <div style="display: flex; align-items: center; gap: 8px;">
          <span style="font-weight: 600;">{{ rootNode.fileName }}</span>
        </div>
        <el-button v-if="rootNode.downloadURL" size="small" link type="primary"
          @click="window.open(rootNode.downloadURL, '_blank')">
          下载
        </el-button>
      </div>
      <div v-if="recordInfo" :style="{ marginTop: '4px', fontSize: '12px', color: '#999' }">
        路径: {{ recordInfo.fullPath }}
      </div>
    </div>

    <!-- 依赖树 -->
    <div :style="{ maxHeight: '480px', overflow: 'auto', marginBottom: '12px' }">
      <el-tree
        v-if="!loading && treeData.length > 0"
        :data="treeData"
        :props="{ label: 'label', children: 'children' }"
        default-expand-all
        node-key="key"
      >
        <template #default="{ data }">
          <span v-if="data._raw === null" style="font-weight: bold; color: #888; font-size: 12px;">{{ data.label }}</span>
          <div v-else style="display: flex; align-items: center; gap: 8px; width: 100%; padding: 2px 0;">
            <span style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ data.label }}</span>
            <el-button v-if="data._raw?.downloadURL" size="small" link type="primary"
              @click.stop="window.open(data._raw.downloadURL, '_blank')">下载</el-button>
            <el-button v-if="data._raw?.dependencyId" size="small" link type="danger"
              @click.stop="deleteDependency(data._raw.dependencyId)">移除</el-button>
          </div>
        </template>
      </el-tree>
      <el-empty v-else-if="!loading && treeData.length === 0" description="暂无依赖关系" />
      <el-tag v-if="loading" type="info">加载中...</el-tag>
    </div>

    <!-- 添加依赖 -->
    <el-divider />
    <div style="display: flex; flex-direction: column; gap: 8px;">
      <h4 style="margin:0 0 4px">添加上游依赖</h4>
      <el-autocomplete
        v-model="searchName"
        :maxlength="255"
        :fetch-suggestions="fetchAutocomplete"
        placeholder="搜索并选择文件"
        clearable
        @select="handleSelect"
      />
      <el-select v-model="relValue" placeholder="依赖关系">
        <el-option v-for="opt in relationOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
      </el-select>
      <div style="display: flex; gap: 8px; align-items: center;">
        <el-input v-model="descValue" :maxlength="500" placeholder="关系描述（可选）" style="flex: 1" />
        <el-button type="primary" @click="handleAdd" :loading="saving">添加</el-button>
      </div>
    </div>

    <template #footer>
      <div style="display: flex; justify-content: space-between;">
        <el-button size="small" @click="handleDownloadAll" :disabled="!treeData.length && !rootNode?.downloadURL">
          <el-icon :size="14"><Download /></el-icon>
          <span style="margin-left: 4px;">下载全部</span>
        </el-button>
        <el-button @click="visible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Download } from '@element-plus/icons-vue'
import { DependencyApi, IndexApi } from '@/api'

const STORAGE_KEY = 'fuzhan_download_all_dont_remind'

const props = defineProps({
  recordId: { type: Number, default: null },
  recordInfo: { type: Object, default: null }
})

const emit = defineEmits(['close'])
const message = ElMessage
const dialog = {
  warning: (opts) => ElMessageBox.confirm(opts.content, opts.title, {
    confirmButtonText: opts.positiveText || '确定',
    cancelButtonText: opts.negativeText || '取消',
    type: 'warning',
    closeOnClickModal: !!opts.onMaskClick
  }).then(() => { opts.onPositiveClick?.() }).catch(() => { opts.onNegativeClick?.() }),
  info: (opts) => ElMessageBox.confirm(opts.content, opts.title, {
    confirmButtonText: opts.positiveText || '确定',
    cancelButtonText: opts.negativeText || '取消',
    type: 'info'
  }).then(() => { opts.onPositiveClick?.() }).catch(() => { opts.onNegativeClick?.() })
}

const visible = ref(false)
const loading = ref(false)
const saving = ref(false)
const treeData = ref([])
const rootNode = ref(null) // 当前文件节点，不在树中
const currentRecordId = ref(null)
const searchName = ref('')
const targetId = ref(null)
const relValue = ref('requires')
const descValue = ref('')
const autocompleteOptions = ref([])
const relationOptions = [
  { label: '依赖 (requires)', value: 'requires' },
  { label: '被引用 (referenced_by)', value: 'referenced_by' },
  { label: '关联 (related)', value: 'related' }
]

// 打开弹框
const open = (id) => {
  visible.value = true
  loading.value = true
  treeData.value = []
  rootNode.value = null
  currentRecordId.value = id || props.recordId
  loadTree(currentRecordId.value)
}

// 加载依赖树
const loadTree = async (recordId) => {
  if (!recordId) return
  try {
    const res = await DependencyApi.getTree(recordId)
    if (res.success && res.data && res.data.root) {
      const root = res.data.root
      // 当前文件信息单独保存，不放入树中
      rootNode.value = root
      // 树的顶层只包含依赖项（上游 children + 下游 downstream）
      const deps = []
      if (root.children && root.children.length > 0) {
        deps.push(...root.children.map((child, i) => convertNode(child, `up-${i}`)))
      }
      if (root.downstream && root.downstream.length > 0) {
        deps.push({
          key: 'downstream-group',
          label: '下游依赖',
          isLeaf: false,
          _raw: null,
          disabled: true,
          children: root.downstream.map((child, i) => convertNode(child, `down-${i}`))
        })
      }
      treeData.value = deps
    }
  } catch (err) {
    message.error('加载依赖树失败: ' + (err.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

// 转换树节点（仅处理依赖项节点，不含当前文件）
function convertNode(node, parentKey) {
  const key = parentKey ? `${parentKey}-${node.id}` : String(node.id)
  const result = {
    key,
    label: node.fileName,
    isLeaf: true,
    _raw: { ...node }
  }
  // 依赖项的子项为递归依赖
  if (node.children && node.children.length > 0) {
    result.children = node.children.map((child, i) => convertNode(child, key))
    result.isLeaf = false
  }
  return result
}

// 收集下载链接
function collectURLs() {
  const urls = []
  // 当前文件
  if (rootNode.value?.downloadURL) urls.push(rootNode.value.downloadURL)
  // 收集所有依赖项的下载链接
  const walkTree = (nodes) => {
    for (const node of nodes) {
      if (node._raw?.downloadURL) urls.push(node._raw.downloadURL)
      if (node.children) walkTree(node.children)
    }
  }
  walkTree(treeData.value)
  return [...new Set(urls)]
}

// 下载全部
function doDownloadAll() {
  const urls = collectURLs()
  if (urls.length === 0) { message.warning('没有可下载的文件'); return }
  window.open(urls[0], '_blank')
  for (let i = 1; i < urls.length; i++) {
    setTimeout(() => window.open(urls[i], '_blank'), i * 300)
  }
  message.success(`正在打开 ${urls.length} 个下载链接`)
}

// 确认下载弹框
function handleDownloadAll() {
  const dontRemind = localStorage.getItem(STORAGE_KEY)
  if (dontRemind === 'true') {
    doDownloadAll()
    return
  }

  const urls = collectURLs()
  const count = urls.length

  dialog.warning({
    title: '下载确认',
    content: `即将打开 ${count} 个下载链接。\n由于浏览器弹窗拦截机制，可能需要允许弹窗。`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: () => {
      dialog.info({
        title: '提示',
        content: '后续不再提示？',
        positiveText: '不再提示',
        negativeText: '仅本次',
        onPositiveClick: () => {
          localStorage.setItem(STORAGE_KEY, 'true')
        },
        onNegativeClick: () => {
          // 仅本次
        }
      })
      doDownloadAll()
    },
    onNegativeClick: () => {
      // 取消下载
    }
  })
}

// 删除依赖
const deleteDependency = async (depId) => {
  dialog.warning({
    title: '确认移除依赖',
    content: `确定要移除此依赖项吗？此操作不可恢复。`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      saving.value = true
      try {
        const res = await DependencyApi.deletePublic(depId)
        if (res.success) {
          message.success('依赖已移除')
          await loadTree(currentRecordId.value)
        }
      } catch (err) {
        message.error('移除依赖失败: ' + (err.message || '未知错误'))
      } finally {
        saving.value = false
      }
    }
  })
}

// 自动完成搜索（el-autocomplete fetch-suggestions）
const fetchAutocomplete = async (query, cb) => {
  await handleSearch(query)
  cb((autocompleteOptions.value || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
}

// 自动完成搜索
const handleSearch = async (value) => {
  if (!value || value.length < 1) { autocompleteOptions.value = []; return }
  try {
    const res = await IndexApi.searchRecords(value)
    if (res.success && res.data.records) {
      autocompleteOptions.value = res.data.records
        .filter(r => !r.isDir)
        .map(r => ({
        label: `${r.fileName} (${r.fullPath})`,
        value: String(r.id)
      }))
    }
  } catch (e) {}
}

const handleSelect = (item) => {
  targetId.value = item.id
  searchName.value = item.value.split(' (')[0]
}

// 添加依赖
const handleAdd = async () => {
  if (!targetId.value || !currentRecordId.value) return
  saving.value = true
  try {
    const res = await DependencyApi.createPublic(currentRecordId.value, targetId.value, relValue.value, descValue.value || '')
    if (res.success) {
      message.success('依赖已添加')
      targetId.value = null
      searchName.value = ''
      descValue.value = ''
      autocompleteOptions.value = []
      loadTree(currentRecordId.value)
    }
  } catch (err) {
    message.error('添加依赖失败: ' + (err.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

// 暴露方法
defineExpose({ open })
</script>