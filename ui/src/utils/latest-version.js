/**
 * 最新版本文件识别的 Worker 客户端。
 * 与 xxhash.js 相同的单例 Worker + pending Map + 递增 id 模式；
 * 结果回传后按 id 找到对应 Promise。Worker 不可用时降级为同步计算，保证功能可用。
 */
import latestVersionModule from '@/assets/js/filelist-latest-version-algm.js'

const syncAnalyze = (files, config) => {
  const result = latestVersionModule.analyzeLatestVersions(files || [], config || {})
  return new Set(result.latestPaths)
}

let worker = null
let nextId = 0
const pending = new Map()
// 一旦 Worker 创建/运行失败，后续请求直接走同步，避免反复新建
let degraded = false

const getWorker = () => {
  if (worker) return worker
  if (typeof Worker === 'undefined') return null
  try {
    worker = new Worker(new URL('../workers/latest-version.worker.js', import.meta.url), { type: 'module' })
    worker.onmessage = ({ data }) => {
      const p = pending.get(data.id)
      if (!p) return
      pending.delete(data.id)
      if (data.error) p.reject(new Error(data.error))
      else p.resolve(new Set(data.latestPaths))
    }
    worker.onerror = (err) => {
      const entries = Array.from(pending.values())
      pending.clear()
      entries.forEach((p) => p.reject(err.error || new Error('最新版本分析 worker 异常')))
      degraded = true
      worker = null
    }
    return worker
  } catch {
    degraded = true
    return null
  }
}

/**
 * 异步分析文件列表的"最新版本" path 集合
 * @param {Array} files - 文件对象数组（含 path/name/type/modifiedTime）
 * @param {Object} [config] - 算法三态分组配置
 * @returns {Promise<Set<string>>}
 */
export function analyzeLatestVersionsAsync(files, config = {}) {
  const payload = (files || []).map((f) => ({
    path: f.path,
    name: f.name,
    type: f.type,
    modifiedTime: f.modifiedTime
  }))

  return new Promise((resolve) => {
    if (!degraded) {
      const w = getWorker()
      if (w) {
        const id = ++nextId
        pending.set(id, { resolve, reject: () => resolve(syncAnalyze(files, config)) })
        try {
          w.postMessage({ id, files: payload, config })
          return
        } catch {
          pending.delete(id)
          degraded = true
          worker = null
        }
      }
    }
    resolve(syncAnalyze(files, config))
  })
}