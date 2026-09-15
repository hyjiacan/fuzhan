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
  // 清理模式：selected=清理所选；single=单条删除；all=一键清理全部僵尸
  const zombieCleanMode = ref('selected')
  const zombieSelectedRowKeys = ref([])
  const zombieSelectedSessions = ref([])
  const zombieSessions = ref([])
  const zombiePage = ref(1)
  const zombiePageSize = ref(20)
  const zombieTotal = ref(0)

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
      const data = await AdminApi.getSessions(zombiePage.value, zombiePageSize.value)
      if (data.success) {
        zombieSessions.value = (data.data && data.data.sessions) || []
        zombieTotal.value = data.data?.total || 0
        const stats = data.data?.stats
        if (stats) {
          zombieStats.totalSessions = stats.totalSessions || 0
          zombieStats.zombieSessions = stats.zombieSessions || 0
          zombieStats.totalSize = stats.totalSize || 0
        } else {
          // 后端未返回聚合统计时退化为当前页估算
          zombieStats.totalSessions = zombieTotal.value
          zombieStats.zombieSessions = zombieSessions.value.filter(s => ZOMBIE_STATES.includes(s.status)).length
          zombieStats.totalSize = zombieSessions.value.reduce((sum, s) => sum + (s.uploadedSize || 0), 0)
        }
      }
    } catch (e) {
      console.error('加载会话列表失败', e)
    } finally {
      zombieLoading.value = false
    }
  }

  const onZombiePageChange = () => {
    zombieSelectedRowKeys.value = []
    loadZombieSessions()
  }

  const onZombiePageSizeChange = () => {
    zombiePage.value = 1
    zombieSelectedRowKeys.value = []
    loadZombieSessions()
  }

  const zombieCleanSelected = () => {
    if (zombieSelectedRowKeys.value.length === 0) {
      ElMessage.info('请先选择要清理的会话')
      return
    }
    const selected = zombieSessions.value.filter(s => zombieSelectedRowKeys.value.includes(s.id))
    zombieSelectedSessions.value = selected
    zombieCleanMode.value = 'selected'
    zombieConfirmVisible.value = true
  }

  const zombieCleanSession = (session) => {
    zombieSelectedSessions.value = [session]
    zombieCleanMode.value = 'single'
    zombieConfirmVisible.value = true
  }

  // 一键清理：不依赖当前分页，由后端重新查询全部僵尸会话决定清理哪些
  const zombieCleanAllExpired = () => {
    zombieSelectedRowKeys.value = []
    zombieSelectedSessions.value = []
    zombieCleanMode.value = 'all'
    zombieConfirmVisible.value = true
  }

  const zombieConfirmClean = async () => {
    zombieCleaning.value = true
    try {
      if (zombieCleanMode.value === 'all') {
        const data = await AdminApi.cleanupAllZombieSessions()
        if (data.success) {
          ElMessage.success(data.message || '清理完成')
          zombieConfirmVisible.value = false
          zombieSelectedRowKeys.value = []
          zombieSelectedSessions.value = []
          loadZombieSessions()
        } else {
          ElMessage.error(data.message || '清理失败')
        }
        return
      }

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
    zombieCleanMode,
    zombieSelectedRowKeys, zombieSelectedSessions, zombieSessions, zombieStats,
    zombiePage, zombiePageSize, zombieTotal,
    zombieColumns, loadZombieSessions,
    onZombiePageChange, onZombiePageSizeChange,
    zombieCleanSelected, zombieCleanAllExpired, zombieConfirmClean
  }
}