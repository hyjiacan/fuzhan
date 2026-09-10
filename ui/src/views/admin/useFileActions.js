import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AdminApi, IndexApi } from '@/api'
import { PathUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import { isDir } from './fileTableColumns'

// 移动/重命名弹框宽度：桌面 800px，小屏按百分比自适应
const MOVE_DIALOG_MAX_WINDOW = 860

/**
 * 文件操作：索引扫描、移动/重命名、预览/下载、删除。
 * currentPath/currentFile/checkedRowKeys/loadCurrentDir/displayList 由视图注入。
 */
export const useFileActions = ({
  currentPath, currentFile, checkedRowKeys, loadCurrentDir, displayList
}) => {
  // ============ 索引扫描 ============
  const scanning = ref(false)
  const scanProgress = ref({ status: 'idle', scannedFiles: 0, totalFiles: 0, currentFile: '', errorMessage: '' })
  let progressTimer = null

  async function loadScanProgress() {
    try {
      const res = await IndexApi.getScanProgress()
      if (res.success) {
        scanProgress.value = res.data
      }
    } catch (err) {
      // 静默失败
    }
  }

  function startPollProgress() {
    loadScanProgress()
    progressTimer = setInterval(() => {
      loadScanProgress()
    }, 3000)
  }

  function stopPollProgress() {
    if (progressTimer) {
      clearInterval(progressTimer)
      progressTimer = null
    }
  }

  async function handleScan() {
    scanning.value = true
    try {
      const res = await IndexApi.triggerScan()
      if (res.success) {
        ElMessage.success('扫描已启动')
        startPollProgress()
      }
    } catch (err) {
      ElMessage.error(formatErrorMessage(err, '启动扫描失败'))
    } finally {
      scanning.value = false
    }
  }

  // ============ 移动/重命名 ============
  const moveModalVisible = ref(false)
  const targetRootName = ref('')
  const targetSubPath = ref('')
  const moving = ref(false)
  const moveDialogWidth = ref('800px')
  const updateMoveDialogWidth = () => {
    moveDialogWidth.value = window.innerWidth < MOVE_DIALOG_MAX_WINDOW ? '92%' : '800px'
  }

  const openMoveModal = (file) => {
    currentFile.value = file
    targetRootName.value = file.rootName || ''
    // 预填相对根目录的路径（去除 rootName 前缀），避免与 handleMove 拼接时出现重复嵌套
    let rel = file.path || ''
    const prefix = '/' + (file.rootName || '')
    if (rel.startsWith(prefix + '/')) {
      rel = rel.slice(prefix.length + 1)
    }
    targetSubPath.value = rel
    moveModalVisible.value = true
  }

  const handleMove = async () => {
    if (!targetSubPath.value || !targetSubPath.value.trim()) {
      ElMessage.warning('请输入目标路径（子目录或新文件名）')
      return
    }

    // 验证目标路径不能包含 .. 等越权路径
    const subPath = targetSubPath.value.trim()
    if (subPath.includes('..')) {
      ElMessage.error('目标路径无效，不能包含 ..')
      return
    }

    // 目标路径 = 根目录名 + 子路径
    const target = targetRootName.value + '/' + subPath
    // 源路径 = 根目录名 + 原相对路径
    const oldFullPath = currentFile.value.path

    moving.value = true
    try {
      const data = await AdminApi.move(oldFullPath, target)
      if (data.success) {
        ElMessage.success('操作成功')
        moveModalVisible.value = false
        targetSubPath.value = ''
        loadCurrentDir()
      } else {
        ElMessage.error(data.message || '操作失败')
      }
    } catch (e) {
      ElMessage.error('操作失败')
    } finally {
      moving.value = false
    }
  }

  // ============ 预览/下载 ============
  const previewDialogVisible = ref(false)
  const previewMaximized = ref(false)
  const previewFileData = ref({})

  // 管理下载走 /admin/download（带鉴权）；新窗口打不开无法带请求头，故在 URL 上附 token
  const adminDownloadHref = (fullPath, pathEncoded) => {
    const p = pathEncoded || PathUtils.encodeFilePath(fullPath)
    const t = localStorage.getItem('token')
    return `/api/v1/admin/download/${p}${t ? `?token=${encodeURIComponent(t)}` : ''}`
  }

  const previewFile = (file) => {
    previewFileData.value = file
    previewDialogVisible.value = true
  }

  const downloadFile = (file) => {
    if (file?.path) {
      const fullPath = file.path
      window.open(adminDownloadHref(fullPath), '_blank')
    }
  }

  // ============ 删除 ============
  const deleting = ref(false)
  const handleDelete = async (file) => {
    const isDirectory = isDir(file)
    try {
      await ElMessageBox.confirm(
        isDirectory
          ? `确定要删除目录 "${file.name}" 及其所有内容吗？此操作不可恢复。`
          : `确定要删除文件 "${file.name}" 吗？此操作不可恢复。`,
        '确认删除',
        {
          type: 'warning',
          confirmButtonText: '删除',
          cancelButtonText: '取消'
        }
      )
    } catch {
      return
    }
    deleting.value = true
    try {
      const deletePath = file.path
      const data = await AdminApi.delete(deletePath)
      if (data.success) {
        ElMessage.success('删除成功')
        loadCurrentDir()
      } else {
        ElMessage.error(data.message || '删除失败')
      }
    } catch (e) {
      ElMessage.error('删除失败')
    } finally {
      deleting.value = false
    }
  }

  const handleBatchDelete = async () => {
    const selected = displayList.value.filter(f => checkedRowKeys.value.includes(f.path))
    if (selected.length === 0) return
    const dirCount = selected.filter(f => isDir(f)).length
    const fileCount = selected.length - dirCount
    let contentText
    if (dirCount > 0 && fileCount > 0) {
      contentText = `确定要删除选中的 ${fileCount} 个文件和 ${dirCount} 个目录（含目录下所有内容）吗？此操作不可恢复。`
    } else if (dirCount > 0) {
      contentText = `确定要删除选中的 ${dirCount} 个目录（含目录下所有内容）吗？此操作不可恢复。`
    } else {
      contentText = `确定要删除选中的 ${fileCount} 个文件吗？此操作不可恢复。`
    }
    try {
      await ElMessageBox.confirm(contentText, '确认删除', {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消'
      })
    } catch {
      return
    }
    deleting.value = true
    try {
      let successCount = 0
      for (const file of selected) {
        const data = await AdminApi.delete(file.path)
        if (data.success) {
          successCount++
        }
      }
      ElMessage.success(`已删除 ${successCount} 个文件`)
      checkedRowKeys.value = []
      loadCurrentDir()
    } catch (e) {
      ElMessage.error('删除失败')
    } finally {
      deleting.value = false
    }
  }

  return {
    scanning, scanProgress, loadScanProgress, startPollProgress, stopPollProgress, handleScan,
    moveModalVisible, targetRootName, targetSubPath, moving, moveDialogWidth,
    updateMoveDialogWidth, openMoveModal, handleMove,
    previewDialogVisible, previewMaximized, previewFileData, adminDownloadHref, previewFile, downloadFile,
    deleting, handleDelete, handleBatchDelete
  }
}