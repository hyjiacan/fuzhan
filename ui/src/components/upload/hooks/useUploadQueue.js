import { ref, computed, nextTick } from 'vue'
import { FileRecordApi, DependencyApi, request } from '@/api'
import { NumberUtils, xxh3Hash } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import { safeStorage } from '@/utils/storage'
import { SESSION_KEY_PREFIX } from '../constants'

// 本地分片上传队列：入队/移除/暂停/重试/续传/会话持久化、串行分片上传与进度、备注依赖提交
export function useUploadQueue({
  form,
  getApiUrl,
  needsRootSelection,
  props,
  message,
  dialog,
  emit
}) {
  const uploadQueue = ref([])
  const uploading = ref(false)
  const queueItemRefs = {}
  let queueIdCounter = 0

  // ===== 上传会话持久化：每个会话独立 key（按 uploadId），只写自己的 key，不清理其它会话，
  //      避免多页面同时上传时互相覆盖或误删其它页签的会话 =====
  const saveUploadSessions = () => {
    const filtered = uploadQueue.value.filter(item => item.status === 'uploading' || item.status === 'pending' || item.status === 'paused')
    for (const item of filtered) {
      if (!item.uploadId) continue
      const session = {
        id: item.id,
        name: item.name,
        size: item.size,
        uploadId: item.uploadId,
        status: item.status,
        progress: item.progress,
        speed: item.speed,
        elapsed: item.elapsed,
        remaining: item.remaining,
        displayText: item.displayText,
        rootName: item.rootName || form.rootName,
        uploadDir: item.uploadDir || form.uploadDir
      }
      safeStorage.set(SESSION_KEY_PREFIX + item.uploadId, JSON.stringify(session))
    }
  }

  const loadUploadSessions = () => {
    const sessions = []
    let keys = []
    try {
      keys = Object.keys(localStorage)
    } catch {
      return sessions
    }
    for (const key of keys) {
      if (!key.startsWith(SESSION_KEY_PREFIX)) continue
      const saved = safeStorage.get(key)
      if (!saved) continue
      try {
        const session = JSON.parse(saved)
        if (session && session.uploadId) sessions.push(session)
      } catch (e) {
        safeStorage.remove(key)
      }
    }
    return sessions
  }

  const removeUploadSession = (uploadId) => {
    if (!uploadId) return
    safeStorage.remove(SESSION_KEY_PREFIX + uploadId)
  }

  const clearUploadSessions = () => {
    let keys = []
    try {
      keys = Object.keys(localStorage)
    } catch {
      return
    }
    for (const key of keys) {
      if (key.startsWith(SESSION_KEY_PREFIX)) {
        safeStorage.remove(key)
      }
    }
  }

  const restoreUploadSessions = async () => {
    const savedSessions = loadUploadSessions()

    if (savedSessions.length === 0) {
      return
    }

    for (const saved of savedSessions) {
      if (!saved.uploadId) continue

      try {
        const getSessionUrl = getApiUrl('getSession', saved.uploadId)
        if (!getSessionUrl) {
          continue
        }
        const sessionStatusData = await request.get(getSessionUrl, { baseURL: '' })

        if (sessionStatusData.success && sessionStatusData.data?.session) {
          const session = sessionStatusData.data.session
          if (session.status !== 'completed' && session.status !== 'cancelled') {
            let restoreStatus = saved.status
            if (restoreStatus === 'paused' && !saved.file) {
              restoreStatus = 'needFile'
            }
            const queueItem = {
              id: saved.id,
              name: saved.name,
              size: saved.size,
              uploadId: saved.uploadId,
              status: restoreStatus,
              progress: session.uploadedIndexes?.length
                ? Math.round((session.uploadedIndexes.length / session.totalChunks) * 100)
                : 0,
              error: '',
              speed: saved.speed || '',
              elapsed: saved.elapsed || '',
              remaining: saved.remaining || '',
              displayText: saved.displayText || '',
              rootName: saved.rootName,
              uploadDir: saved.uploadDir
            }
            uploadQueue.value.push(queueItem)
            const statusText = restoreStatus === 'needFile' ? '，请重新选择文件' : (restoreStatus === 'paused' ? '（已暂停）' : '')
            message.info(`已恢复上传会话: ${saved.name}${statusText}`)
          } else {
            removeUploadSession(saved.uploadId)
          }
        }
      } catch (e) {
        console.error('恢复上传会话失败:', e)
        removeUploadSession(saved.uploadId)
      }
    }
  }

  const calculateXXH3 = async (buffer) => xxh3Hash(buffer)
  const formatFileSize = (bytes) => NumberUtils.formatFileSize(bytes)

  const scrollToQueueItem = (itemId) => {
    nextTick(() => {
      const el = queueItemRefs[itemId]
      if (el) {
        el.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
      }
    })
  }

  const addFileToQueue = (fileData) => {
    const queueItem = {
      id: ++queueIdCounter,
      name: fileData.name,
      size: fileData.size,
      status: 'pending',
      progress: 0,
      error: '',
      file: fileData.file,
      rootName: form.rootName,
      uploadDir: form.uploadDir,
      notes: fileData.notes || '',
      depFileName: fileData.depFileName || '',
      depFileRecordId: fileData.depFileRecordId || null,
      depRelation: fileData.depRelation || 'requires',
      depDescription: fileData.depDescription || '',
      showNotes: false,
      showDeps: false,
      uploadDirPath: form.uploadDir
    }
    uploadQueue.value.push(queueItem)
    emit('upload-change', uploadQueue.value.length)
    scrollToQueueItem(queueItem.id)
  }

  const removeFromQueue = async (id) => {
    const index = uploadQueue.value.findIndex(item => item.id === id)
    if (index === -1) return

    const item = uploadQueue.value[index]

    if (item.status === 'uploading') {
      dialog.warning({
        title: '取消上传',
        content: `确定要取消上传 "${item.name}" 吗？`,
        positiveText: '确定取消',
        negativeText: '继续上传',
        onPositiveClick: async () => {
          item.status = 'paused'
          saveUploadSessions()
          const checkRemoved = setInterval(() => {
            if (item.status !== 'uploading') {
              clearInterval(checkRemoved)
              if (item.uploadId) {
                const cancelUrl = getApiUrl('cancel', item.uploadId)
                if (cancelUrl) {
                  request.delete(cancelUrl, { baseURL: '' }).catch((e) => {
                    console.error('取消上传请求失败:', e)
                  })
                }
              }
              const idx = uploadQueue.value.findIndex(i => i.id === id)
              if (idx !== -1) {
                uploadQueue.value.splice(idx, 1)
                saveUploadSessions()
                emit('upload-change', uploadQueue.value.length)
              }
            }
          }, 100)
          message.info(`${item.name} 已取消`)
        }
      })
      return
    }

    if (item.uploadId) {
      try {
        const cancelUrl = getApiUrl('cancel', item.uploadId)
        if (cancelUrl) {
          await request.delete(cancelUrl, { baseURL: '' })
        }
      } catch (e) {
        console.error('取消上传会话失败:', e)
      }
    }

    uploadQueue.value.splice(index, 1)
    saveUploadSessions()
    emit('upload-change', uploadQueue.value.length)
  }

  const retryUpload = (item) => {
    item.status = 'pending'
    item.error = ''
    item.progress = 0
    processQueue()
  }

  const pauseUpload = (item) => {
    item.status = 'paused'
    saveUploadSessions()
    message.info(`${item.name} 已暂停，可点击继续恢复上传`)
  }

  const resumeUpload = (item) => {
    if (!item.file) {
      item.status = 'needFile'
      message.warning('请先选择文件')
      return
    }
    item.status = 'pending'
    processQueue()
  }

  const selectFileForItem = (item) => {
    const input = document.createElement('input')
    input.type = 'file'
    input.onchange = (e) => {
      const file = e.target.files[0]
      if (file) {
        item.file = file
        item.name = file.name
        item.size = file.size
        item.status = 'pending'
        processQueue()
      }
    }
    input.click()
  }

  const processQueue = async () => {
    const nextItem = uploadQueue.value.find(item => item.status === 'pending')
    if (!nextItem) {
      if (uploading.value) {
        uploading.value = false
        const hasActiveItems = uploadQueue.value.some(item =>
          item.status === 'uploading' || item.status === 'paused'
        )
        if (hasActiveItems) {
          return
        }
        emit('upload-success')
        clearUploadSessions()
      }
      return
    }

    uploading.value = true
    nextItem.status = 'uploading'
    nextItem.progress = 0
    nextItem.error = ''
    scrollToQueueItem(nextItem.id)

    try {
      const result = await uploadSingleFile(nextItem)
      if (result === 'paused') {
        return
      }
      nextItem.status = 'completed'
      nextItem.progress = 100
      removeUploadSession(nextItem.uploadId)
    } catch (e) {
      nextItem.status = 'failed'
      nextItem.error = formatErrorMessage(e, '上传失败')
      emit('upload-error', e)
      saveUploadSessions()
    }

    processQueue()
  }

  const startUpload = () => {
    if (uploadQueue.value.length === 0) return
    emit('upload-start')
    processQueue()
  }

  const pendingCount = computed(() =>
    uploadQueue.value.filter(item => item.status === 'pending').length
  )

  const hasActiveUploads = computed(() =>
    uploadQueue.value.some(item => item.status === 'uploading')
  )

  const uploadSingleFile = async (queueItem) => {
    const { file, name, size, uploadId: savedUploadId, rootName: itemRootName, uploadDir: itemUploadDir } = queueItem
    const createSessionUrl = getApiUrl('createSession')
    const isPrivate = createSessionUrl?.includes('/private/')

    if (savedUploadId) {
      queueItem.uploadId = savedUploadId
    }

    // Step 1: Create upload session（或跳过如果已存在）
    const uploadId = savedUploadId || await (async () => {
      const createSessionBody = isPrivate
        ? { filename: name, fileSize: size, dir: itemUploadDir || '' }
        : needsRootSelection.value
          ? { filename: name, fileSize: size, dir: itemUploadDir || '/', rootName: itemRootName || '_apps', targetType: 'local' }
          : { filename: name, fileSize: size, dir: itemUploadDir || '', deleteOnDownload: props.deleteOnDownload }

      const response = await request.post(createSessionUrl, createSessionBody, {
        baseURL: '',
        skipSuccessToast: true,
        headers: { 'Content-Type': 'application/json' }
      })

      if (!response.success) {
        throw new Error(response.message || '创建上传会话失败')
      }

      const { uploadId: newUploadId, totalChunks, chunkSize, overwriteRequired } = response.data
      if (!chunkSize) {
        throw new Error('服务器未返回分片大小配置')
      }

      if (overwriteRequired) {
        let confirmed = false
        await new Promise((resolve, reject) => {
          dialog.warning({
            title: '文件已存在',
            content: `文件 "${name}" 已存在，是否覆盖？`,
            positiveText: '覆盖',
            negativeText: '取消',
            onPositiveClick: () => {
              if (confirmed) return
              confirmed = true
              resolve()
            },
            onNegativeClick: () => {
              if (confirmed) return
              confirmed = true
              const cancelUrl = getApiUrl('cancel', newUploadId)
              request.delete(cancelUrl, { baseURL: '' }).catch(() => {})
              reject(new Error('用户取消了上传'))
            },
            onMaskClick: () => {
              if (confirmed) return
              confirmed = true
              const cancelUrl = getApiUrl('cancel', newUploadId)
              request.delete(cancelUrl, { baseURL: '' }).catch(() => {})
              reject(new Error('用户取消了上传'))
            }
          })
        })
      }

      queueItem.uploadId = newUploadId
      queueItem.totalChunks = totalChunks
      queueItem.chunkSize = chunkSize
      saveUploadSessions()

      return newUploadId
    })()

    // 恢复的会话需从服务端获取 chunkSize
    if (!queueItem.chunkSize) {
      const getSessionUrl = getApiUrl('getSession', uploadId)
      const sessionStatusData = await request.get(getSessionUrl, { baseURL: '' })
      if (!sessionStatusData.success) {
        throw new Error(sessionStatusData.message || '获取上传状态失败')
      }
      const session = sessionStatusData.data.session
      if (!session.chunkSize) {
        throw new Error('服务器未返回分片大小配置')
      }
      queueItem.chunkSize = session.chunkSize
      queueItem.totalChunks = session.totalChunks
    }

    const CHUNK_SIZE = queueItem.chunkSize
    const { totalChunks } = queueItem

    // 进度追踪
    const uploadData = []
    let lastUploadTime = Date.now()
    let lastUploadedBytes = 0
    const uploadStartTime = Date.now()
    const updateProgress = (uploadedBytes) => {
      const currentTime = Date.now()
      uploadData.push({ time: currentTime, size: uploadedBytes })
      const fiveSecondsAgo = currentTime - 5000
      while (uploadData.length > 0 && uploadData[0].time < fiveSecondsAgo) {
        uploadData.shift()
      }

      let uploadSpeed = 0
      if (uploadData.length >= 2) {
        const first = uploadData[0]
        const last = uploadData[uploadData.length - 1]
        uploadSpeed = (last.size - first.size) / ((last.time - first.time) / 1000)
      } else {
        const elapsed = currentTime - lastUploadTime
        if (elapsed > 0) {
          uploadSpeed = (uploadedBytes - lastUploadedBytes) / (elapsed / 1000)
        }
      }

      lastUploadTime = currentTime
      lastUploadedBytes = uploadedBytes

      const percentage = ((uploadedBytes / size) * 100).toFixed(1)
      const uploadedSize = NumberUtils.formatFileSize(uploadedBytes)
      const totalSize = NumberUtils.formatFileSize(size)
      const speedStr = NumberUtils.formatFileSize(uploadSpeed) + '/s'

      const elapsedSeconds = (currentTime - uploadStartTime) / 1000
      let elapsedStr = ''
      if (elapsedSeconds > 3600) {
        elapsedStr = (elapsedSeconds / 3600).toFixed(1) + ' 时'
      } else if (elapsedSeconds > 60) {
        elapsedStr = (elapsedSeconds / 60).toFixed(1) + ' 分'
      } else {
        elapsedStr = elapsedSeconds.toFixed(1) + ' 秒'
      }

      let remainingStr = '-'
      if (uploadSpeed > 0 && uploadedBytes < size) {
        const remainingBytes = size - uploadedBytes
        const remainingTime = remainingBytes / uploadSpeed
        if (remainingTime > 3600) {
          remainingStr = (remainingTime / 3600).toFixed(1) + ' 时'
        } else if (remainingTime > 60) {
          remainingStr = (remainingTime / 60).toFixed(1) + ' 分'
        } else {
          remainingStr = remainingTime.toFixed(1) + ' 秒'
        }
      }

      queueItem.progress = Math.round(uploadedBytes / size * 100)
      queueItem.speed = speedStr
      queueItem.remaining = remainingStr
      queueItem.elapsed = elapsedStr
      queueItem.displayText = `${uploadedSize}/${totalSize} (${percentage}%) ${speedStr} | 已耗时 ${elapsedStr} | 剩余 ${remainingStr}`
    }

    // Step 2: 获取已上传分片并续传
    let uploadedIndexes = new Set()
    let uploadedBytes = 0

    const getSessionUrl = getApiUrl('getSession', uploadId)
    const sessionStatusData = await request.get(getSessionUrl, { baseURL: '' })

    if (sessionStatusData.success && sessionStatusData.data?.session) {
      const session = sessionStatusData.data.session
      const serverChunkSize = session.chunkSize
      if (!serverChunkSize) {
        throw new Error('服务器未返回分片大小配置')
      }
      if (serverChunkSize !== CHUNK_SIZE) {
        throw new Error('分片大小不匹配：服务器配置已更改，请重新开始上传')
      }
      if (session.uploadedIndexes) {
        session.uploadedIndexes.forEach(i => uploadedIndexes.add(i))
        uploadedBytes = uploadedIndexes.size * CHUNK_SIZE
        if (uploadedBytes > size) uploadedBytes = size
        updateProgress(uploadedBytes)
      }
    } else if (!sessionStatusData.success) {
      removeUploadSession(queueItem.uploadId)
      delete queueItem.uploadId
      throw new Error(sessionStatusData.message || '上传会话已失效，请重新开始上传')
    }

    const uploadChunk = async (index) => {
      const start = index * CHUNK_SIZE
      const end = Math.min(start + CHUNK_SIZE, size)
      const chunk = file.slice(start, end)

      const chunkBuffer = await chunk.arrayBuffer()
      const checksum = await calculateXXH3(chunkBuffer)

      const formData = new FormData()
      formData.append('uploadId', String(uploadId))
      formData.append('chunkIndex', String(index))
      formData.append('chunk', chunk)
      formData.append('checksum', checksum)

      const data = await request.post(getApiUrl('uploadChunk'), formData, { baseURL: '', skipSuccessToast: true })
      if (!data.success) {
        throw new Error(data.message || `分片 ${index} 上传失败`)
      }

      uploadedIndexes.add(index)
      uploadedBytes = Math.min((index + 1) * CHUNK_SIZE, size)
      updateProgress(uploadedBytes)
    }

    // 串行上传所有分片
    let paused = false
    for (let i = 0; i < totalChunks; i++) {
      if (uploadedIndexes.has(i)) continue
      if (queueItem.status === 'paused') {
        paused = true
        saveUploadSessions()
        break
      }
      await uploadChunk(i)
    }

    if (paused) {
      return 'paused'
    }

    // Step 3: Finalize
    const finalizeUrl = getApiUrl('finalize')
    const finalizeData = await request.post(finalizeUrl, { uploadId }, {
      baseURL: '',
      skipSuccessToast: true,
      headers: { 'Content-Type': 'application/json' }
    })
    if (!finalizeData.success) {
      throw new Error(finalizeData.message || '完成上传失败')
    }

    // 上传成功后提交备注和依赖
    await submitNotesAndDeps(queueItem, name, itemRootName, itemUploadDir)

    return finalizeData.data
  }

  // 上传成功后提交备注和依赖
  const submitNotesAndDeps = async (queueItem, fileName, rootName, uploadDir) => {
    if (!queueItem.notes && !queueItem.depFileRecordId) return

    try {
      await new Promise(r => setTimeout(r, 2000))
      const dir = uploadDir && uploadDir !== '/' ? uploadDir.replace(/^\/+/, '') : ''
      const filePath = dir ? '/' + dir + '/' + fileName : '/' + fileName

      let res = await FileRecordApi.findRecord(fileName, rootName, filePath)
      let record
      if (!res.success || !res.data.record) {
        await new Promise(r => setTimeout(r, 3000))
        const retry = await FileRecordApi.findRecord(fileName, rootName, filePath)
        if (!retry.success || !retry.data.record) {
          console.warn('未找到文件记录，无法设置备注/依赖:', fileName)
          return
        }
        record = retry.data.record
      } else {
        record = res.data.record
      }
      if (queueItem.notes) {
        await FileRecordApi.updateNotes(record.id, queueItem.notes)
      }
      if (queueItem.depFileRecordId) {
        await DependencyApi.createPublic(record.id, queueItem.depFileRecordId, queueItem.depRelation, queueItem.depDescription)
      }
    } catch (e) {
      console.warn('提交备注/依赖失败:', e)
    }
  }

  return {
    uploadQueue,
    uploading,
    queueItemRefs,
    pendingCount,
    hasActiveUploads,
    formatFileSize,
    scrollToQueueItem,
    addFileToQueue,
    removeFromQueue,
    retryUpload,
    pauseUpload,
    resumeUpload,
    selectFileForItem,
    processQueue,
    startUpload,
    restoreUploadSessions,
    saveUploadSessions,
    clearUploadSessions
  }
}