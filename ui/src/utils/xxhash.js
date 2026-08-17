/**
 * xxh3 哈希工具函数
 * 与 Go 后端 github.com/zeebo/xxh3 保持一致
 */

/**
 * 计算 xxh3 哈希
 * @param {string|Uint8Array|ArrayBuffer} input - 输入字符串、字节数组或 ArrayBuffer
 * @returns {Promise<string>} 16位十六进制大写哈希值
 */
export async function xxh3Hash(input) {
  const { xxhash3 } = await import('hash-wasm')

  let data
  if (typeof input === 'string') {
    data = new TextEncoder().encode(input)
  } else if (input instanceof ArrayBuffer) {
    data = new Uint8Array(input)
  } else {
    data = input
  }

  const hash = await xxhash3(data)
  return hash.toString(16).toUpperCase().padStart(16, '0')
}