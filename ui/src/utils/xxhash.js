/**
 * xxh3 哈希工具函数
 * 与 Go 后端 github.com/zeebo/xxh3 保持一致。
 * 哈希计算始终在 Web Worker 内执行，避免大分片哈希阻塞主线程。
 */

let worker = null
let nextId = 0
const pending = new Map()

const getWorker = () => {
  if (!worker) {
    worker = new Worker(new URL('../workers/xxhash.worker.js', import.meta.url), { type: 'module' })
    worker.onmessage = ({ data }) => {
      const p = pending.get(data.id)
      if (!p) return
      pending.delete(data.id)
      if (data.error) p.reject(new Error(data.error))
      else p.resolve(data.hash)
    }
    worker.onerror = (err) => {
      // worker 层面异常时拒绝所有在途请求，避免悬挂的 Promise
      const entries = Array.from(pending.values())
      pending.clear()
      entries.forEach(p => p.reject(err.error || new Error('xxh3 worker 异常')))
    }
  }
  return worker
}

/**
 * 计算 xxh3 哈希（始终经 Web Worker，主线程不阻塞）
 * @param {string|Uint8Array|ArrayBuffer} input - 输入字符串、字节数组或 ArrayBuffer
 * @returns {Promise<string>} 16位十六进制大写哈希值
 */
export async function xxh3Hash(input) {
  let data
  if (typeof input === 'string') {
    data = new TextEncoder().encode(input)
  } else if (input instanceof ArrayBuffer) {
    data = new Uint8Array(input)
  } else {
    data = input
  }

  const id = ++nextId
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    // data 的底层缓冲通过 transfer 移交给 worker，避免跨线程拷贝，也利于大文件分片
    getWorker().postMessage({ id, data }, [data.buffer])
  })
}