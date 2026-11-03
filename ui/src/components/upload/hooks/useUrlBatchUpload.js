import { ref, computed } from 'vue'
import { UploadApi, FileRecordApi, DependencyApi } from '@/api'
import { formatErrorMessage } from '@/utils/error'
import store from '@/store'

// 本地条目状态 -> 展示文案/标签（naive 标签类型，经 elTagType 映射）
const ITEM_STATUS_TEXT = {
  draft: '待上传',
  pending: '排队中',
  uploading: '上传中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消'
}
const ITEM_STATUS_TAG = {
  draft: 'info',
  pending: 'warning',
  uploading: 'info',
  completed: 'success',
  failed: 'error',
  cancelled: 'default'
}

// 后端任务状态 -> 本地条目状态
const mapTaskStatus = (s) => {
  switch (s) {
    case 'pending': return 'pending'
    case 'downloading': return 'uploading'
    case 'completed': return 'completed'
    case 'failed': return 'failed'
    case 'cancelled': return 'cancelled'
    default: return 'uploading'
  }
}

let localSeq = 0
const nextId = () => `url-${Date.now()}-${++localSeq}`

// 支持的校验算法（用于「计算校验值」选择）
export const CHECKSUM_ALGORITHMS = [
  { value: 'md5', label: 'MD5' },
  { value: 'sha1', label: 'SHA1' },
  { value: 'sha256', label: 'SHA256' }
]

// URL 批量上传：单条/多行添加、探测去重、逐条或整体入队（后端全局队列调度）、SSE 进度推送（失败降级轮询）
export function useUrlBatchUpload({
  form,
  getApiUrl,
  needsRootSelection,
  uploadMessage,
  uploadMessageType,
  emit
}) {
  const items = ref([])
  const batchMode = ref(false)
  const batchText = ref('')
  const singleUrl = ref('')
  const probing = ref(false)
  const enqueuing = ref(false)
  const pollTimers = new Map()
  const MAX_POLL_RETRIES = 400 // ~10 分钟

  // 共享 SSE 进度通道：fetch 流式解析（EventSource 无法携带 X-Anonymous-ID）；
  // 连续失败 2 次后降级为原有逐条轮询，降级期间不破坏既有查询接口。
  const URL_TASK_STREAM_URL = '/api/v1/web/uploads/url-tasks/stream'
  const STREAM_RETRY_MS = 1500
  const STREAM_MAX_ERRORS = 2
  let streamCtrl = null
  let streamRetryTimer = null
  let streamConnected = false
  let streamErrors = 0
  let streamFallback = false

  const storageType = computed(() => {
    const createUrl = getApiUrl('createSession')
    if (createUrl?.includes('/temp/')) return 'temp'
    if (createUrl?.includes('/private/')) return 'private'
    return ''
  })

  const hasActive = computed(() => items.value.some(i => i.status === 'pending' || i.status === 'uploading'))
  const draftCount = computed(() => items.value.filter(i => i.status === 'draft').length)
  const completedCount = computed(() => items.value.filter(i => i.status === 'completed').length)
  const failedCount = computed(() => items.value.filter(i => i.status === 'failed').length)

  const statusText = (s) => ITEM_STATUS_TEXT[s] || s
  const statusTag = (s) => ITEM_STATUS_TAG[s] || 'default'

  const normalizeUrl = (u) => (u || '').trim()
  const existsInList = (url) => items.value.some(i => normalizeUrl(i.url) === url)

  const setMessage = (text, type = 'info') => {
    if (uploadMessage) uploadMessage.value = text
    if (uploadMessageType) uploadMessageType.value = type
  }

  // 探测一批 URL 并追加到列表（列表内去重）
  const addUrls = async (urls) => {
    const unique = []
    let dup = 0
    for (const raw of urls) {
      const u = normalizeUrl(raw)
      if (!u) continue
      if (existsInList(u) || unique.includes(u)) { dup++; continue }
      unique.push(u)
    }
    if (unique.length === 0) {
      if (dup > 0) setMessage('URL 已在列表中', 'warning')
      return
    }

    probing.value = true
    let warning = dup > 0 ? `已跳过 ${dup} 个重复 URL` : ''
    try {
      const dir = form.uploadDir || ''
      const rootName = needsRootSelection.value ? (form.rootName || '') : ''
      const res = await UploadApi.probeUrls(unique, dir, rootName, storageType.value)
      const got = res?.data?.items || []
      let added = 0
      for (const it of got) {
        if (it.error) {
          warning = `${it.url}: ${it.error}`
          continue
        }
        const u = normalizeUrl(it.url)
        if (existsInList(u)) continue
        items.value.push({
          id: nextId(),
          url: u,
          fileName: it.fileName || '',
          fileSize: it.fileSize || 0,
          targetExists: !!it.targetExists,
          alreadyUploaded: !!it.alreadyUploaded,
          notes: '',
          depFileName: '',
          depFileRecordId: null,
          depRelation: 'requires',
          depDescription: '',
          status: 'draft',
          progress: 0,
          retryCount: 0,
          displayText: '',
          error: '',
          taskId: null,
          // 校验值：用户勾选的算法、计算结果、加载/错误状态（不落库，仅回显）
          checksumAlgorithms: [],
          checksums: null,
          checksumLoading: false,
          checksumError: ''
        })
        added++
      }
      if (warning) {
        setMessage(warning, 'warning')
      } else if (added > 0) {
        setMessage(`已添加 ${added} 个 URL`, 'success')
      } else {
        setMessage('')
      }
    } catch (e) {
      setMessage(formatErrorMessage(e, '探测 URL 失败'), 'error')
    } finally {
      probing.value = false
    }
  }

  const addSingle = async () => {
    const u = normalizeUrl(singleUrl.value)
    if (!u) return
    await addUrls([u])
    singleUrl.value = ''
  }

  const addBatch = async () => {
    const urls = batchText.value.split(/\r?\n/)
    if (!urls.some(u => normalizeUrl(u))) return
    await addUrls(urls)
    batchText.value = ''
    batchMode.value = false
  }

  const toggleBatchMode = () => { batchMode.value = !batchMode.value }

  const removeItem = (id) => {
    const item = items.value.find(i => i.id === id)
    // 队列中的任务已持久化在后端，删除条目时需一并取消，避免留下无人跟踪的后台任务
    if (item && item.taskId && (item.status === 'pending' || item.status === 'uploading')) {
      UploadApi.cancelURLTask(item.taskId).catch(() => { /* 取消失败不阻塞删除 */ })
    }
    stopPolling(id)
    const idx = items.value.findIndex(i => i.id === id)
    if (idx >= 0) items.value.splice(idx, 1)
    syncStream()
  }

  // 入队单个条目：任务持久化到后端，由全局队列按并行上限调度
  const enqueue = async (item) => {
    if (item.status !== 'draft') return false
    setMessage('正在准备上传...', 'info')
    try {
      let data
      if (needsRootSelection.value) {
        data = await UploadApi.uploadFromUrlLocal(item.url, item.fileName, form.uploadDir || '/', form.rootName || '_apps')
      } else {
        data = await UploadApi.uploadFromUrl(item.url, item.fileName, form.uploadDir || '', storageType.value)
      }
      if (data.success && data.data?.taskId) {
        store.setActiveUrlTasks(true)
        item.taskId = data.data.taskId
        item.status = 'pending'
        item.displayText = '排队中...'
        item.error = ''
        item.progress = 0
        if (data.data.fileSize) item.fileSize = data.data.fileSize
        if (data.data.fileName) item.fileName = item.fileName || data.data.fileName
        setMessage('已加入队列，等待上传...', 'info')
        syncStream()
        return true
      }
      item.status = 'failed'
      item.error = data.message || '入队失败'
      setMessage(item.error, 'error')
      return false
    } catch (e) {
      item.status = 'failed'
      item.error = formatErrorMessage(e, '入队失败')
      setMessage(item.error, 'error')
      return false
    }
  }

  const uploadItem = async (id) => {
    const item = items.value.find(i => i.id === id)
    if (!item) return
    await enqueue(item)
  }

  // 整体上传：串行入队所有待上传条目，避免瞬间大量探测请求
  const uploadAll = async () => {
    const drafts = items.value.filter(i => i.status === 'draft')
    if (drafts.length === 0) return
    enqueuing.value = true
    try {
      for (const it of drafts) {
        await enqueue(it)
      }
    } finally {
      enqueuing.value = false
    }
  }

  const retryItem = async (id) => {
    const item = items.value.find(i => i.id === id)
    if (!item) return
    // 重试后文件内容可能变化，清空上次校验结果
    item.checksums = null
    item.checksumError = ''
    if (!item.taskId) {
      item.status = 'draft'
      return uploadItem(id)
    }
    try {
      await UploadApi.retryURLTask(item.taskId)
      item.status = 'pending'
      item.error = ''
      item.progress = 0
      item.displayText = '排队中...'
      setMessage('已重新加入队列', 'info')
      syncStream()
    } catch (e) {
      item.error = formatErrorMessage(e, '重试失败')
    }
  }

  // 计算已完成任务的校验值：按条目勾选的算法请求后端流式计算，结果仅回显不落库
  const computeChecksum = async (id) => {
    const item = items.value.find(i => i.id === id)
    if (!item || !item.taskId) return
    const algorithms = item.checksumAlgorithms || []
    if (algorithms.length === 0) {
      setMessage('请至少选择一种校验算法', 'warning')
      return
    }
    item.checksumLoading = true
    item.checksumError = ''
    try {
      const res = await UploadApi.computeChecksum(item.taskId, algorithms)
      item.checksums = res?.data?.checksums || null
    } catch (e) {
      item.checksumError = formatErrorMessage(e, '计算校验值失败')
      setMessage(item.checksumError, 'error')
    } finally {
      item.checksumLoading = false
    }
  }

  const cancelItem = async (id) => {
    const item = items.value.find(i => i.id === id)
    if (!item) return
    if (item.taskId && (item.status === 'pending' || item.status === 'uploading')) {
      try { await UploadApi.cancelURLTask(item.taskId) } catch (e) { /* 忽略取消失败 */ }
    }
    stopPolling(item.id)
    item.status = 'cancelled'
    item.displayText = ''
    syncStream()
  }

  // applyTaskState 将后端任务状态映射到条目（SSE 与轮询共用；终态提示只触发一次）
  const applyTaskState = (item, task) => {
    if (!item || !task) return
    item.progress = task.fileSize > 0 ? Math.round(task.downloadedBytes / task.fileSize * 100) : 0
    item.retryCount = task.retryCount || 0
    if (task.fileSize) item.fileSize = task.fileSize
    if (task.fileName) item.fileName = task.fileName

    const retrySuffix = item.retryCount > 0 ? `（第 ${item.retryCount} 次重试）` : ''
    const st = mapTaskStatus(task.status)
    const prev = item.status
    item.status = st
    if (st === 'pending') {
      item.displayText = `排队中${retrySuffix}...`
      setMessage('已加入队列，等待上传...', 'info')
    } else if (st === 'uploading') {
      item.displayText = `上传中 ${item.progress}%${retrySuffix}`
      setMessage(`正在上传 ${item.fileName}...`, 'info')
    } else if (st === 'completed') {
      item.displayText = '上传完成'
      item.progress = 100
      stopPolling(item.id)
      if (prev !== 'completed') {
        setMessage('文件上传成功', 'success')
        submitNotesAndDeps(item)
        emit?.('upload-success')
      }
    } else if (st === 'failed') {
      item.error = task.errorMessage || '上传失败'
      stopPolling(item.id)
      if (prev !== 'failed') setMessage('文件上传失败', 'error')
    } else if (st === 'cancelled') {
      stopPolling(item.id)
    }
    syncStream()
  }

  const startPolling = (item) => {
    stopPolling(item.id)
    let count = 0
    const timer = setInterval(async () => {
      count++
      if (count > MAX_POLL_RETRIES) {
        stopPolling(item.id)
        item.status = 'failed'
        item.error = '上传超时'
        setMessage('文件上传超时，请重试', 'error')
        return
      }
      try {
        const res = await UploadApi.getURLTask(item.taskId)
        if (!res.success) return
        applyTaskState(item, res.data.task)
      } catch (e) {
        // 轮询失败不中断，继续尝试
        console.warn('轮询URL上传进度失败:', e)
      }
    }, 1500)
    pollTimers.set(item.id, timer)
  }

  const stopPolling = (id) => {
    const t = pollTimers.get(id)
    if (t) {
      clearInterval(t)
      pollTimers.delete(id)
    }
  }

  const clearPollTimers = () => {
    for (const id of Array.from(pollTimers.keys())) stopPolling(id)
  }

  // ============ 共享 SSE 进度通道 ============
  const activeTaskItems = () =>
    items.value.filter(i => i.taskId && (i.status === 'pending' || i.status === 'uploading'))

  const anonHeaders = () => {
    try {
      const id = localStorage.getItem('anonymous_id')
      if (id) return { 'X-Anonymous-ID': id }
    } catch (e) { /* 忽略存储不可用 */ }
    return {}
  }

  const startStream = () => {
    if (typeof window === 'undefined' || !window.fetch) {
      streamFallback = true
      startPollingActive()
      return
    }
    closeStream()
    const ctrl = new AbortController()
    streamCtrl = ctrl
    // 同源 fetch 自动携带登录 Cookie；匿名身份通过 X-Anonymous-ID 头传递
    fetch(URL_TASK_STREAM_URL, { signal: ctrl.signal, headers: anonHeaders() })
      .then(async (resp) => {
        if (ctrl.signal.aborted) return
        if (!resp.ok || !resp.body) {
          onStreamFailure()
          return
        }
        streamConnected = true
        streamErrors = 0
        // 连接可用后停掉逐条轮询，避免重复请求；并立即对活跃任务做一次对账，
        // 避免任务在建立连接前已完成而错过推送（服务端只推送活跃/近期变化的任务）
        clearPollTimers()
        reconcileActiveItems()
        const reader = resp.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        try {
          for (;;) {
            const { done, value } = await reader.read()
            if (done || ctrl.signal.aborted) break
            buffer += decoder.decode(value, { stream: true })
            let sep
            while ((sep = buffer.indexOf('\n\n')) >= 0) {
              const block = buffer.slice(0, sep)
              buffer = buffer.slice(sep + 2)
              const line = block.split('\n').find(l => l.startsWith('data:'))
              if (!line) continue
              try {
                const payload = JSON.parse(line.slice(5).trim())
                applyStreamTasks(payload && payload.tasks)
              } catch (e) { /* 忽略异常帧 */ }
              streamErrors = 0
            }
          }
          if (!ctrl.signal.aborted) onStreamFailure()
        } catch (e) {
          if (!ctrl.signal.aborted) onStreamFailure()
        }
      })
      .catch(() => {
        if (!ctrl.signal.aborted) onStreamFailure()
      })
  }

  const applyStreamTasks = (tasks) => {
    for (const task of tasks || []) {
      const item = items.value.find(i => i.taskId === task.id)
      if (item) applyTaskState(item, task)
    }
  }

  // reconcileActiveItems 建连后对活跃任务逐条补一次状态，弥补推送的时间窗
  const reconcileActiveItems = async () => {
    for (const item of activeTaskItems()) {
      try {
        const res = await UploadApi.getURLTask(item.taskId)
        if (res.success && res.data && res.data.task) applyTaskState(item, res.data.task)
      } catch (e) {
        // 单条对账失败忽略，后续由推送或降级轮询补齐
      }
    }
  }

  const onStreamFailure = () => {
    streamConnected = false
    closeStream()
    streamErrors += 1
    if (streamErrors >= STREAM_MAX_ERRORS) {
      streamFallback = true
      streamErrors = 0
      startPollingActive()
      return
    }
    if (!hasActive.value) return
    if (streamRetryTimer) clearTimeout(streamRetryTimer)
    streamRetryTimer = setTimeout(() => {
      streamRetryTimer = null
      if (!streamFallback && hasActive.value) startStream()
    }, STREAM_RETRY_MS)
  }

  // closeStream 仅中断当前连接，保留失败计数供重试/降级判断
  const closeStream = () => {
    if (streamCtrl) {
      streamCtrl.abort()
      streamCtrl = null
    }
  }

  const stopStream = () => {
    closeStream()
    streamConnected = false
    streamErrors = 0
    if (streamRetryTimer) {
      clearTimeout(streamRetryTimer)
      streamRetryTimer = null
    }
  }

  const startPollingActive = () => {
    for (const item of activeTaskItems()) {
      if (!pollTimers.has(item.id)) startPolling(item)
    }
  }

  // syncStream 依据活跃任务维持共享通道：有活跃任务时确保连接（或降级轮询），否则关闭
  const syncStream = () => {
    if (!hasActive.value) {
      stopStream()
      streamFallback = false
      return
    }
    if (streamFallback) {
      startPollingActive()
      return
    }
    if (streamConnected || streamCtrl || streamRetryTimer) return
    startStream()
  }

  const stopAllPolling = () => {
    clearPollTimers()
    stopStream()
    streamFallback = false
  }

  // 上传完成后提交备注/依赖（异步，轮询文件记录就绪后写入）
  const submitNotesAndDeps = (item) => {
    const notes = item.notes
    const depFileRecordId = item.depFileRecordId
    const depRelation = item.depRelation || 'requires'
    const depDescription = item.depDescription || ''
    if (!notes && !depFileRecordId) return
    const fileName = item.fileName
    if (!fileName) return

    setTimeout(async () => {
      try {
        const dir = form.uploadDir && form.uploadDir !== '/' ? form.uploadDir.replace(/^\/+/, '') : ''
        const filePath = dir ? '/' + dir + '/' + fileName : '/' + fileName
        const rootName = form.rootName
        if (!rootName) return

        let record = null
        for (let i = 0; i < 2; i++) {
          const res = await FileRecordApi.findRecord(fileName, rootName, filePath)
          if (res.success && res.data.record) { record = res.data.record; break }
          await new Promise(r => setTimeout(r, 3000))
        }
        if (!record) {
          console.warn('未找到文件记录，无法设置 URL 备注/依赖:', fileName)
          return
        }
        if (notes) await FileRecordApi.updateNotes(record.id, notes)
        if (depFileRecordId) await DependencyApi.createPublic(record.id, depFileRecordId, depRelation, depDescription)
      } catch (e) {
        console.warn('提交 URL 备注/依赖失败:', e)
      }
    }, 2000)
  }

  const clearAll = () => {
    stopAllPolling()
    items.value = []
  }

  return {
    items,
    batchMode,
    batchText,
    singleUrl,
    probing,
    enqueuing,
    hasActive,
    draftCount,
    completedCount,
    failedCount,
    statusText,
    statusTag,
    addSingle,
    addBatch,
    toggleBatchMode,
    removeItem,
    uploadItem,
    uploadAll,
    retryItem,
    cancelItem,
    computeChecksum,
    stopPolling,
    stopAllPolling,
    clearAll
  }
}