// xxh3 哈希纯函数（供 xxhash.worker.js 与单元测试复用，不在主线程直接调用）。
// 与 Go 后端 github.com/zeebo/xxh3 保持一致，测试向量见 __tests__/xxhash.test.js。
import { xxhash3 } from 'hash-wasm'

export async function computeXxh3(data) {
  const hash = await xxhash3(data)
  return hash.toString(16).toUpperCase().padStart(16, '0')
}