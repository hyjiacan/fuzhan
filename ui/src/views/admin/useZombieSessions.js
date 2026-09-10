import { ref, reactive, computed, h } from 'vue'
import { ElMessage, ElButton, ElTag, ElCheckbox } from 'element-plus'
import { AdminApi } from '@/api'
import { TimeUtils } from '@/utils'
import { formatSize } from './uploadHelpers'

const statusTypeMap = { default: 'info', info: 'info', success: 'success', warning: 'warning', error: 'danger' }

// 会话状态文案
const sessionStatusMap = {
  pending: { text: '等待中', type: 'default' },
  in_progress: { text: '上传中', type: 'info' },
  completed: { text: '已完成', type: 'success' },
  cancelled: { text: '已取消', type: 'warning' },
  expired: { text: '已过期', type: 'error' }
}

// 判定为僵尸会话（可清理）的状态集合
const ZOMBIE_STATES = ['expired', 'pending', 'in_progress']

/**
 * 僵尸（过期）上传会话管理与清理。
 */
export const useZombieSessions = () => {
  const zombieLoading = ref(false)
  const zombieCleaning = ref(false)
  const zombieConfirmVisible = ref(false)
  const zombieSelectedRowKeys = ref([])
  const zombieSelectedSessions = ref([])
  const zombieSessions = ref([])

  const zombieStats = reactive({
    zombieSessions: 0,
    totalSessions: 0,
    totalSize: 0
  })

  // 勾选（el-table-v2 不内置选择列，手动实现）
  const zombieIsAllSelected = computed(() => {
    return zombieSessions.value.length > 0 &&
      zombieSessions.value.every(s => zombieSelectedRowKeys.value.includes(s.id))
  })
  const zombieIsIndeterminate = computed(() => {
    if (zombieSessions.value.length === 0) return false
    const count = zombieSessions.value.filter(s => zombieSelectedRowKeys.value.includes(s.id)).length
    return count > 0 && count < zombieSessions.value.length
  })
  const zombieToggleSelectAll = (val) => {
    zombieSelectedRowKeys.value = val ? zombieSessions.value.map(s => s.id) : []
  }
  const zombieToggleSingle = (row, val) => {
    if (val) {
      if (!zombieSelectedRowKeys.value.includes(row.id)) {
        zombieSelectedRowKeys.value = zombieSelectedRowKeys.value.concat(row.id)
      }
    } else {
      zombieSelectedRowKeys.value = zombieSelectedRowKeys.value.filter(key => key !== row.id)
    }
  }

  const zombieColumns = [
    {
      key: 'selection',
      width: 50,
      headerCellRenderer: () => h(ElCheckbox, {
        modelValue: zombieIsAllSelected.value,
        indeterminate: zombieIsIndeterminate.value,
        onChange: (val) => zombieToggleSelectAll(val)
      }),
      cellRenderer: ({ rowData: row }) => h(ElCheckbox, {
        modelValue: zombieSelectedRowKeys.value.includes(row.id),
        onChange: (val) => zombieToggleSingle(row, val)
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
      title: '状态',
      key: 'status',
      width: 100,
      cellRenderer: ({ rowData: row }) => {
        const status = sessionStatusMap[row.status] || { text: row.status, type: 'default' }
        return h(ElTag, { type: statusTypeMap[status.type], size: 'small' }, () => status.text)
      }
    },
    {
      title: '文件大小',
      key: 'fileSize',
      width: 120,
      cellRenderer: ({ rowData: row }) => formatSize(row.fileSize)
    },
    {
      title: '已上传',
      key: 'uploadedSize',
      width: 120,
      cellRenderer: ({ rowData: row }) => {
        const percent = row.fileSize > 0 ? Math.round((row.uploadedSize / row.fileSize) * 100) : 0
        return `${formatSize(row.uploadedSize)} (${percent}%)`
      }
    },
    {
      title: '创建时间',
      key: 'createdAt',
      width: 180,
      cellRenderer: ({ rowData: row }) => row.createdAt ? TimeUtils.formatDateTime(row.createdAt) : '-'
    },
    {
      title: '过期时间',
      key: 'expiredAt',
      width: 180,
      cellRenderer: ({ rowData: row }) => row.expiredAt ? TimeUtils.formatDateTime(row.expiredAt) : '-'
    },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      cellRenderer: ({ rowData: row }) => {
        if (row.status === 'completed' || row.status === 'cancelled') {
          return null
        }
        return h(ElButton, {
          size: 'small',
          type: 'danger',
          link: true,
          onClick: () => zombieCleanSession(row)
        }, () => '清理')
      }
    }
  ]

  const loadZombieSessions = async () => {
    zombieLoading.value = true
    try {
      const data = await AdminApi.getSessions()
      if (data.success) {
        const sessionList = (data.data && data.data.sessions) || []
        zombieSessions.value = sessionList
        zombieStats.totalSessions = sessionList.length
        zombieStats.zombieSessions = sessionList.filter(s => ZOMBIE_STATES.includes(s.status)).length
        zombieStats.totalSize = sessionList.reduce((sum, s) => sum + (s.uploadedSize || 0), 0)
      }
    } catch (e) {
      console.error('加载会话列表失败', e)
    } finally {
      zombieLoading.value = false
    }
  }

  const zombieCleanSelected = () => {
    if (zombieSelectedRowKeys.value.length === 0) {
      ElMessage.info('请先选择要清理的会话')
      return
    }
    const selected = zombieSessions.value.filter(s => zombieSelectedRowKeys.value.includes(s.id))
    zombieSelectedSessions.value = selected
    zombieConfirmVisible.value = true
  }

  const zombieCleanSession = (session) => {
    zombieSelectedSessions.value = [session]
    zombieConfirmVisible.value = true
  }

  const zombieCleanAllExpired = () => {
    const expiredSessions = zombieSessions.value.filter(s => ZOMBIE_STATES.includes(s.status))
    if (expiredSessions.length === 0) {
      ElMessage.info('没有需要清理的过期会话')
      return
    }
    zombieSelectedRowKeys.value = expiredSessions.map(s => s.id)
    zombieSelectedSessions.value = expiredSessions
    zombieConfirmVisible.value = true
  }

  const zombieConfirmClean = async () => {
    zombieCleaning.value = true
    try {
      const sessionIds = zombieSelectedSessions.value.map(s => s.id)
      const data = await AdminApi.cleanupSessions(sessionIds)
      if (data.success) {
        ElMessage.success(`成功清理 ${sessionIds.length} 个会话`)
        zombieConfirmVisible.value = false
        zombieSelectedRowKeys.value = []
        zombieSelectedSessions.value = []
        loadZombieSessions()
      } else {
        ElMessage.error(data.message || '清理失败')
      }
    } catch (e) {
      ElMessage.error('清理失败')
    } finally {
      zombieCleaning.value = false
    }
  }

  return {
    zombieLoading, zombieCleaning, zombieConfirmVisible,
    zombieSelectedRowKeys, zombieSelectedSessions, zombieSessions, zombieStats,
    zombieColumns, loadZombieSessions,
    zombieCleanSelected, zombieCleanAllExpired, zombieConfirmClean
  }
}