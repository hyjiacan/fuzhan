#!/usr/bin/env node
// 按需构建 scalar（OpenAPI 文档页）：
// 若 server/scalar/scalar.html 已存在，说明上次已生成稳定的独立产物，直接跳过构建；
// 否则（首次构建/被清理）自动执行 yarn build:scalar 生成。
// 这样无需每次随主应用全量重建 scalar。如需强制重建，请显式执行 yarn build:scalar。
import { existsSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const uiDir = resolve(__dirname, '..')
const scalarEntry = resolve(__dirname, '../../server/scalar/scalar.html')

if (existsSync(scalarEntry)) {
  console.log(`[scalar] 输出已存在，跳过构建: ${scalarEntry}`)
  process.exit(0)
}

console.log('[scalar] 未检测到输出，自动构建 scalar ...')
const yarn = process.platform === 'win32' ? 'yarn.cmd' : 'yarn'
const result = spawnSync(yarn, ['build:scalar'], {
  stdio: 'inherit',
  cwd: uiDir,
})
if (result.status !== 0) {
  console.error(`[scalar] 构建失败，退出码: ${result.status}`)
}
process.exit(result.status ?? 1)