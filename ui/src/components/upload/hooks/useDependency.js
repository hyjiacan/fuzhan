import { ref } from 'vue'
import { FileRecordApi } from '@/api'
import { DEP_RELATION_OPTIONS } from '../constants'

// 依赖文件搜索（el-autocomplete 建议来源），覆盖队列项 / URL / 剪贴板与文本三类输入
export function useDependency({ form }) {
  // 队列项依赖：按 item.id 缓存建议
  const depAutocompleteCache = {}
  const handleDepSearch = async (item, value) => {
    if (!value || value.length < 1) {
      depAutocompleteCache[item.id] = []
      return
    }
    try {
      const res = await FileRecordApi.searchFiles(value)
      if (res.success && res.data.records) {
        depAutocompleteCache[item.id] = res.data.records.map(r => ({
          label: `${r.fileName} (${r.fullPath})`,
          value: String(r.id)
        }))
      }
    } catch (e) {
      // 静默失败
    }
  }
  const fetchDepSuggestions = async (item, query, cb) => {
    await handleDepSearch(item, query)
    cb((depAutocompleteCache[item.id] || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
  }
  const handleDepSelect = (item, opt) => {
    item.depFileRecordId = opt.id
    item.depFileName = opt.value.split(' (')[0]
  }

  // URL 上传依赖（写入 form）
  const urlDepAutocompleteOptions = ref([])
  const handleUrlDepSearch = async (value) => {
    if (!value || value.length < 1) {
      urlDepAutocompleteOptions.value = []
      return
    }
    try {
      const res = await FileRecordApi.searchFiles(value)
      if (res.success && res.data.records) {
        urlDepAutocompleteOptions.value = res.data.records.map(r => ({
          label: `${r.fileName} (${r.fullPath})`,
          value: String(r.id)
        }))
      }
    } catch (e) {}
  }
  const fetchUrlDepSuggestions = async (query, cb) => {
    await handleUrlDepSearch(query)
    cb((urlDepAutocompleteOptions.value || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
  }
  const handleUrlDepSelect = (opt) => {
    form.urlDepRecordId = opt.id
    form.urlDepFileName = opt.value.split(' (')[0]
  }

  // 剪贴板 / 文本上传共用依赖
  const extraNotes = ref('')
  const extraDepFileName = ref('')
  const extraDepRecordId = ref(null)
  const extraDepRelation = ref('requires')
  const extraDepDescription = ref('')
  const extraDepAutocompleteOptions = ref([])
  const handleExtraDepSearch = async (value) => {
    if (!value || value.length < 1) {
      extraDepAutocompleteOptions.value = []
      return
    }
    try {
      const res = await FileRecordApi.searchFiles(value)
      if (res.success && res.data.records) {
        extraDepAutocompleteOptions.value = res.data.records.map(r => ({
          label: `${r.fileName} (${r.fullPath})`,
          value: String(r.id)
        }))
      }
    } catch (e) {}
  }
  const fetchExtraDepSuggestions = async (query, cb) => {
    await handleExtraDepSearch(query)
    cb((extraDepAutocompleteOptions.value || []).map(o => ({ value: o.label, id: parseInt(o.value) })))
  }
  const handleExtraDepSelect = (opt) => {
    extraDepRecordId.value = opt.id
    extraDepFileName.value = opt.value.split(' (')[0]
  }

  return {
    depRelationOptions: DEP_RELATION_OPTIONS,
    fetchDepSuggestions,
    handleDepSelect,
    fetchUrlDepSuggestions,
    handleUrlDepSelect,
    extraNotes,
    extraDepFileName,
    extraDepRecordId,
    extraDepRelation,
    extraDepDescription,
    fetchExtraDepSuggestions,
    handleExtraDepSelect
  }
}