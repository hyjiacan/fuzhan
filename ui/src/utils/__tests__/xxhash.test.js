import { describe, it, expect } from 'vitest'
import { xxh3Hash } from '../xxhash'

// Go 后端 zeebo/xxh3 测试结果
const goExpected = {
  '': '2D06800538D394C2',
  'hello': '9555E8555C62DCFD',
  'test data': '8F0FA94A1FE96CC4'
}

describe('XXH3 Hash Consistency', () => {
  it('should hash empty string correctly', async () => {
    const result = await xxh3Hash('')
    expect(result).toBe(goExpected[''])
  })

  it('should hash hello correctly', async () => {
    const result = await xxh3Hash('hello')
    expect(result).toBe(goExpected['hello'])
  })

  it('should hash test data correctly', async () => {
    const result = await xxh3Hash('test data')
    expect(result).toBe(goExpected['test data'])
  })
})