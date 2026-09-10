import { ref, reactive, computed, h } from 'vue'
import { ElMessage, ElButton, ElTag, ElCheckbox } from 'element-plus'
import { AdminApi } from '@/api'
import { TimeUtils } from '@/utils'
import store from '@/store'
import { formatSize } from './uploadHelpers'

const statusTypeMap = { default: 'info', info: 'info', success: 'success', warning: 'warning', error: 'danger' }

// URL 任务状态文案
const urlStatusTextMap = {
  pending: { text: '等待中', type: 'default' },
  downloading: { text: '上传中', type: 'info' },
  completed: { text: '已完成', type: 'success' },
  failed: { text: '失败', type: 'error' }
}

const storageTypeMap = { temp: '临时', private: '私有', regular: '普通' }

/**
 * URL 上传任务列表：加载分页、状态过滤、批量删除/重试。
 */
export const useUrlTasks = () => {
  const urlLoading = ref(false)
  const deleting = ref(false)
  const retrying = ref(false)
  const deleteModalVisible = ref(false)
  const retryModalVisible = ref(false)
  const toDeleteTasks = ref([])
  const toRetryTasks = ref([])
  const urlSelectedRowKeys = ref([])
  const urlStatusFilter = ref('')
  const urlPage = ref(1)
  const urlPageSize = ref(20)
  const urlTotal = ref(0)
  const urlTasks = ref([])

  const urlStats = reactive({
    total: 0,
    completed: 0,
    failed: 0,
    downloading: 0
  })

  // 勾选（el-table-v2 不内置选择列，手动实现）
  const urlIsAllSelected = computed(() => {
    return urlTasks.value.length > 0 &&
      urlTasks.value.every(t => urlSelectedRowKeys.value.includes(t.id))
  })
  const urlIsIndeterminate = computed(() => {
    if (urlTasks.value.length === 0) return false
    const count = urlTasks.value.filter(t => urlSelectedRowKeys.value.includes(t.id)).length
    return count > 0 && count < urlTasks.value.length
  })
  const urlToggleSelectAll = (val) => {
    urlSelectedRowKeys.value = val ? urlTasks.value.map(t => t.id) : []
  }
  const urlToggleSingle = (row, val) => {
    if (val) {
      if (!urlSelectedRowKeys.value.includes(row.id)) {
        urlSelectedRowKeys.value = urlSelectedRowKeys.value.concat(row.id)
      }
    } else {
      urlSelectedRowKeys.value = urlSelectedRowKeys.value.filter(key => key !== row.id)
    }
  }

  const urlColumns = [
    {
      key: 'selection',
      width: 50,
      headerCellRenderer: () => h(ElCheckbox, {
        modelValue: urlIsAllSelected.value,
        indeterminate: urlIsIndeterminate.value,
        onChange: (val) => urlToggleSelectAll(val)
      }),
      cellRenderer: ({ rowData: row }) => h(ElCheckbox, {
        modelValue: urlSelectedRowKeys.value.includes(row.id),
        onChange: (val) => urlToggleSingle(row, val)
      })
    },
    {
      title: '文件名',
      key: 'fileName',
      minWidth: 160,
      flexGrow: 1,
      cellRenderer: ({ rowData: row }) => h('div', { class: 'file-name-cell', title: row.fileName || '' }, [
        h('span', { class: 'file-link' }, row.fileName || '-')
      ])
    },
    {
      title: 'URL',
      key: 'url',
      width: 200,
      flexShrink: 1,
      cellRenderer: ({ rowData: row }) => {
        return h('div', { class: 'file-name-cell', title: row.url }, [
          h('span', { class: 'file-link', style: 'color: rgba(0,0,0,0.6); font-size: 12px;' }, row.url || '-')
        ])
      }
    },
    {
      title: '状态',
      key: 'status',
      width: 100,
      cellRenderer: ({ rowData: row }) => {
        const s = urlStatusTextMap[row.status] || { text: row.status, type: 'default' }
        return h(ElTag, { type: statusTypeMap[s.type], size: 'small' }, () => s.text)
      }
    },
    {
      title: '存储类型',
      key: 'storageType',
      width: 90,
      cellRenderer: ({ rowData: row }) => {
        return storageTypeMap[row.storageType] || row.storageType
      }
    },
    {
      title: '文件大小',
      key: 'fileSize',
      width: 100,
      cellRenderer: ({ rowData: row }) => formatSize(row.fileSize)
    },
    {
      title: '上传进度',
      key: 'downloadedBytes',
      width: 150,
      cellRenderer: ({ rowData: row }) => {
        if (row.status === 'completed') return '100%'
        if (row.fileSize <= 0) return '-'
        const pct = Math.round((row.downloadedBytes / row.fileSize) * 100)
        return h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } }, [
          h('div', {
            style: {
              width: '80px', height: '6px', background: 'rgba(0,0,0,0.1)',
              borderRadius: '3px', overflow: 'hidden'
            }
          }, [
            h('div', {
              style: {
                width: `${Math.min(pct, 100)}%`, height: '100%',
                background: row.status === 'failed' ? '#ff4d4f' : '#1890ff',
                borderRadius: '3px', transition: 'width 0.3s'
              }
            })
          ]),
          h('span', { style: { fontSize: '12px', color: 'rgba(0,0,0,0.6)' } },
            `${formatSize(row.downloadedBytes)} / ${formatSize(row.fileSize)}`)
        ])
      }
    },
    {
      title: '错误信息',
      key: 'errorMessage',
      width: 180,
      cellRenderer: ({ rowData: row }) => {
        if (!row.errorMessage) return null
        return h('span', { style: { color: '#ff4d4f', fontSize: '12px' } }, row.errorMessage)
      }
    },
    {
      title: '创建时间',
      key: 'createdAt',
      width: 170,
      cellRenderer: ({ rowData: row }) => {
        const text = TimeUtils.formatDateTime(row.createdAt)
        if (TimeUtils.isRecent24h(row.createdAt)) {
          return h('span', { style: 'color: #18a058' }, text)
        }
        return text
      }
    },
    {
      title: '完成时间',
      key: 'completedAt',
      width: 170,
      cellRenderer: ({ rowData: row }) => row.completedAt ? TimeUtils.formatDateTime(row.completedAt) : '-'
    },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      cellRenderer: ({ rowData: row }) => {
        const btns = []
        if (row.status === 'failed') {
          btns.push(h(ElButton, {
            size: 'small', type: 'warning', link: true,
            onClick: () => retrySingle(row)
          }, () => '重试'))
        }
        btns.push(h(ElButton, {
          size: 'small', type: 'danger', link: true,
          onClick: () => deleteSingle(row)
        }, () => '删除'))
        return h('div', { style: { display: 'flex', gap: '8px' } }, btns)
      }
    }
  ]

  const loadURLTasks = (onLoaded) => {
    urlLoading.value = true
    AdminApi.getURLTasks(urlPage.value, urlPageSize.value, urlStatusFilter.value)
      .then(res => {
        if (res.success) {
          urlTasks.value = res.data.items || []
          urlPage.value = res.data.page
          urlPageSize.value = res.data.pageSize
          urlTotal.value = res.data.total
          updateURLStats(res.data.items || [])
        } else {
          ElMessage.error(res.message || '加载失败')
        }
      })
      .catch(() => ElMessage.error('加载任务列表失败'))
      .finally(() => {
        urlLoading.value = false
        onLoaded?.()
      })
  }

  function updateURLStats(items) {
    urlStats.total = items.length
    urlStats.completed = items.filter(t => t.status === 'completed').length
    urlStats.failed = items.filter(t => t.status === 'failed').length
    urlStats.downloading = items.filter(t => t.status === 'pending' || t.status === 'downloading').length
  }

  function onURLStatusFilterChange() {
    urlPage.value = 1
    urlSelectedRowKeys.value = []
    loadURLTasks()
  }

  function onURLPageSizeChange() {
    urlPage.value = 1
    loadURLTasks()
  }

  function deleteSingle(row) {
    toDeleteTasks.value = [row]
    deleteModalVisible.value = true
  }

  function retrySingle(row) {
    toRetryTasks.value = [row]
    retryModalVisible.value = true
  }

  function batchDelete() {
    const selected = urlTasks.value.filter(t => urlSelectedRowKeys.value.includes(t.id))
    if (selected.length === 0) {
      ElMessage.info('请先选择任务')
      return
    }
    toDeleteTasks.value = selected
    deleteModalVisible.value = true
  }

  function batchRetry() {
    const selected = urlTasks.value.filter(t =>
      urlSelectedRowKeys.value.includes(t.id) && t.status === 'failed'
    )
    if (selected.length === 0) {
      ElMessage.info('没有可重试的失败任务')
      return
    }
    toRetryTasks.value = selected
    retryModalVisible.value = true
  }

  async function confirmDelete() {
    deleting.value = true
    try {
      let successCount = 0
      for (const task of toDeleteTasks.value) {
        try {
          const res = await AdminApi.deleteURLTask(task.id)
          if (res.success) successCount++
        } catch (e) {
          console.error('删除任务失败:', task.id, e)
        }
      }
      ElMessage.success(`成功删除 ${successCount} 个任务`)
      deleteModalVisible.value = false
      urlSelectedRowKeys.value = []
      toDeleteTasks.value = []
      loadURLTasks()
    } catch (e) {
      ElMessage.error('删除失败')
    } finally {
      deleting.value = false
    }
  }

  async function confirmRetry() {
    retrying.value = true
    store.setActiveUrlTasks(true)
    try {
      let successCount = 0
      for (const task of toRetryTasks.value) {
        try {
          const res = await AdminApi.retryURLTask(task.id)
          if (res.success) successCount++
        } catch (e) {
          console.error('重试任务失败:', task.id, e)
        }
      }
      ElMessage.success(`成功重试 ${successCount} 个任务`)
      retryModalVisible.value = false
      urlSelectedRowKeys.value = []
      toRetryTasks.value = []
      loadURLTasks()
    } catch (e) {
      ElMessage.error('重试失败')
    } finally {
      retrying.value = false
    }
  }

  return {
    urlLoading, deleting, retrying, deleteModalVisible, retryModalVisible,
    toDeleteTasks, toRetryTasks, urlSelectedRowKeys, urlStatusFilter,
    urlPage, urlPageSize, urlTotal, urlTasks, urlStats, urlColumns,
    loadURLTasks, onURLStatusFilterChange, onURLPageSizeChange,
    batchDelete, batchRetry, confirmDelete, confirmRetry
  }
}