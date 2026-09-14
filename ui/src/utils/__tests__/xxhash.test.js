import { describe, it, expect } from 'vitest'
import { computeXxh3 } from '../xxhash-core'

// 说明：生产路径 xxh3Hash（utils/xxhash.js）始终在 Web Worker 内计算，
// 该环境依赖浏览器 Worker，故在此对 worker 内使用的纯函数 computeXxh3 做正确性验证。

// Go 后端 zeebo/xxh3 测试结果
const goExpected = {
  '': '2D06800538D394C2',
  'hello': '9555E8555C62DCFD',
  'test data': '8F0FA94A1FE96CC4'
}

const hash = async (input) => {
  const data = typeof input === 'string' ? new TextEncoder().encode(input) : input
  return computeXxh3(data)
}

describe('XXH3 Hash Consistency', () => {
  it('should hash empty string correctly', async () => {
    const result = await hash('')
    expect(result).toBe(goExpected[''])
  })

  it('should hash hello correctly', async () => {
    const result = await hash('hello')
    expect(result).toBe(goExpected['hello'])
  })

  it('should hash test data correctly', async () => {
    const result = await hash('test data')
    expect(result).toBe(goExpected['test data'])
  })
})