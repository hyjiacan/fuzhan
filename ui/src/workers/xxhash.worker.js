// 计算 xxh3 哈希的模块型 Web Worker。
// 接收主线程 postMessage({ id, data })，data 为 Uint8Array（缓冲被 transfer 移交，避免拷贝）；
// 完成后回传 { id, hash } 或 { id, error }。
import { computeXxh3 } from '@/utils/xxhash-core'

self.onmessage = async (e) => {
  const { id, data } = e.data
  try {
    const hash = await computeXxh3(data)
    self.postMessage({ id, hash })
  } catch (error) {
    self.postMessage({ id, error: error && error.message ? error.message : String(error) })
  }
}