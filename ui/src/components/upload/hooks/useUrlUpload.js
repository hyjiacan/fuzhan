import { ref, computed } from 'vue'
import { UploadApi, FileRecordApi, DependencyApi } from '@/api'
import { NumberUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'

// URL 上传：文件信息探测、异步下载任务发起与轮询、下载完成后的备注/依赖提交
export function useUrlUpload({
  form,
  getApiUrl,
  needsRootSelection,
  uploading,
  uploadMessage,
  uploadMessageType,
  emit
}) {
  const urlFileInfo = ref({})
  const urlUploadState = ref({
    fileName: '',
    fileSize: 0,
    progress: 0,
    status: '',      // '', 'uploading', 'completed', 'failed'
    displayText: '',
    error: '',
    taskId: null
  })
  let urlPollTimer = null
  const MAX_POLL_RETRIES = 200 // 最大轮询次数（~5分钟）

  const getUrlUploadStatusTag = computed(() => {
    switch (urlUploadState.value.status) {
      case 'uploading': return 'info'
      case 'completed': return 'success'
      case 'failed': return 'error'
      default: return 'default'
    }
  })
  const getUrlUploadStatusText = computed(() => {
    switch (urlUploadState.value.status) {
      case 'uploading': return '下载中'
      case 'completed': return '已完成'
      case 'failed': return '失败'
      default: return ''
    }
  })

  const stopUrlPolling = () => {
    if (urlPollTimer) {
      clearInterval(urlPollTimer)
      urlPollTimer = null
    }
  }

  const pollUrlTask = (taskId) => {
    stopUrlPolling()
    let pollCount = 0
    urlPollTimer = setInterval(async () => {
      pollCount++
      if (pollCount > MAX_POLL_RETRIES) {
        stopUrlPolling()
        urlUploadState.value.status = 'failed'
        urlUploadState.value.error = '下载超时'
        uploadMessage.value = '文件下载超时，请重试'
        uploadMessageType.value = 'error'
        return
      }

      try {
        const res = await UploadApi.getURLTask(taskId)
        if (!res.success) {
          stopUrlPolling()
          urlUploadState.value.status = 'failed'
          urlUploadState.value.error = '获取进度失败'
          return
        }
        const task = res.data.task
        const pct = task.fileSize > 0 ? Math.round(task.downloadedBytes / task.fileSize * 100) : 0
        urlUploadState.value.progress = pct

        if (task.status === 'completed') {
          stopUrlPolling()
          urlUploadState.value.status = 'completed'
          urlUploadState.value.displayText = '下载完成'
          uploadMessage.value = '文件上传成功'
          uploadMessageType.value = 'success'
          submitUrlNotesAndDeps()
          emit('upload-success')
        } else if (task.status === 'failed') {
          stopUrlPolling()
          urlUploadState.value.status = 'failed'
          urlUploadState.value.error = task.errorMessage || '下载失败'
          uploadMessage.value = '文件上传失败'
          uploadMessageType.value = 'error'
        } else {
          urlUploadState.value.displayText = `下载中 ${pct}%`
        }
      } catch (e) {
        // 轮询失败不中断，继续尝试
        console.warn('轮询URL下载进度失败:', e)
      }
    }, 1500)
  }

  const urlInputTimer = ref(null)
  const handleUrlInput = () => {
    if (form.url) {
      clearTimeout(urlInputTimer.value)
      urlInputTimer.value = setTimeout(() => {
        fetchUrlFileInfo()
      }, 500)
    } else {
      urlFileInfo.value = {}
      form.filename = ''
    }
  }

  const fetchUrlFileInfo = async () => {
    if (!form.url) return
    uploadMessage.value = '正在获取文件信息...'
    uploadMessageType.value = 'info'
    try {
      const response = await UploadApi.getUrlInfo(form.url)
      if (response.success) {
        const filename = response.data.filename || 'downloaded_file'
        form.filename = filename
        urlFileInfo.value = {
          name: filename,
          size: response.data.size ? NumberUtils.formatFileSize(response.data.size) : '未知'
        }
        uploadMessage.value = ''
      } else {
        urlFileInfo.value = {}
        uploadMessage.value = '获取文件信息失败: ' + response.message
        uploadMessageType.value = 'error'
      }
    } catch (e) {
      urlFileInfo.value = {}
      uploadMessage.value = formatErrorMessage(e, '获取文件信息失败')
      uploadMessageType.value = 'error'
    }
  }

  const handleUrlUpload = async () => {
    if (!form.url || !form.filename) return

    uploading.value = true
    uploadMessage.value = '正在准备下载...'
    uploadMessageType.value = 'info'

    try {
      let data
      if (needsRootSelection.value) {
        data = await UploadApi.uploadFromUrlLocal(form.url, form.filename, form.uploadDir || '/', form.rootName || '_apps')
      } else {
        // 根据 API 路径判断存储类型
        const createUrl = getApiUrl('createSession')
        let storageType = ''
        if (createUrl?.includes('/temp/')) {
          storageType = 'temp'
        } else if (createUrl?.includes('/private/')) {
          storageType = 'private'
        }
        data = await UploadApi.uploadFromUrl(form.url, form.filename, form.uploadDir || '', storageType)
      }

      if (data.success && data.data && data.data.taskId) {
        // 异步下载，开始轮询进度
        store.setActiveUrlTasks(true) // 激活通知轮询
        const taskData = data.data
        urlUploadState.value = {
          fileName: taskData.fileName || form.filename,
          fileSize: taskData.fileSize || 0,
          progress: 0,
          status: 'uploading',
          displayText: '正在下载...',
          error: '',
          taskId: taskData.taskId
        }
        pollUrlTask(taskData.taskId)
      } else {
        uploadMessage.value = data.message || '上传失败'
        uploadMessageType.value = 'error'
      }
    } catch (e) {
      uploadMessage.value = formatErrorMessage(e, '上传失败')
      uploadMessageType.value = 'error'
    } finally {
      uploading.value = false
    }
  }

  const submitUrlNotesAndDeps = () => {
    const notes = form.urlNotes
    const depFileRecordId = form.urlDepRecordId
    const depRelation = form.urlDepRelation || 'requires'
    const depDescription = form.urlDepDescription || ''
    if (!notes && !depFileRecordId) return

    const fileName = form.filename
    if (!fileName) return

    setTimeout(async () => {
      try {
        const dir = form.uploadDir && form.uploadDir !== '/' ? form.uploadDir.replace(/^\/+/, '') : ''
        const filePath = dir ? '/' + dir + '/' + fileName : '/' + fileName
        const rootName = form.rootName
        if (!rootName) return

        const res = await FileRecordApi.findRecord(fileName, rootName, filePath)
        if (!res.success || !res.data.record) {
          await new Promise(r => setTimeout(r, 3000))
          const retry = await FileRecordApi.findRecord(fileName, rootName, filePath)
          if (!retry.success || !retry.data.record) {
            console.warn('未找到文件记录，无法设置 URL 备注/依赖:', fileName)
            return
          }
          const record = retry.data.record
          if (notes) await FileRecordApi.updateNotes(record.id, notes)
          if (depFileRecordId) await DependencyApi.createPublic(record.id, depFileRecordId, depRelation, depDescription)
          return
        }
        const record = res.data.record
        if (notes) await FileRecordApi.updateNotes(record.id, notes)
        if (depFileRecordId) await DependencyApi.createPublic(record.id, depFileRecordId, depRelation, depDescription)
      } catch (e) {
        console.warn('提交 URL 备注/依赖失败:', e)
      }
    }, 2000)
  }

  return {
    urlFileInfo,
    urlUploadState,
    getUrlUploadStatusTag,
    getUrlUploadStatusText,
    handleUrlInput,
    fetchUrlFileInfo,
    handleUrlUpload,
    submitUrlNotesAndDeps,
    stopUrlPolling,
    urlInputTimer
  }
}