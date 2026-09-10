import { test, expect } from '@playwright/test'

// 组件拆分后的界面冒烟测试：HomeView + UploadManager（匿名可访问）
const consoleErrors = []
const pageErrors = []

test.beforeEach(async ({ page }) => {
  consoleErrors.length = 0
  pageErrors.length = 0
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text())
  })
  page.on('pageerror', (err) => pageErrors.push(String(err)))
})

test('HomeView 文件列表渲染、排序与搜索正常', async ({ page }) => {
  await page.goto('/#/files', { waitUntil: 'networkidle' })
  // 等待上传按钮出现，代表页面应用挂载完成
  await expect(page.getByRole('button', { name: '上传', exact: true })).toBeVisible({ timeout: 15000 })

  // 1. 列头渲染
  for (const col of ['文件名', '大小', '修改时间', '下载次数', '备注', '操作']) {
    await expect(page.getByText(col, { exact: true })).toBeVisible({ timeout: 8000 })
  }

  // 2. 文件表存在行数据（根目录非空）
  const rowCount = await page.locator('.el-table-v2__body .el-table-v2__row').count()
  console.log(`[home] 文件列表行数 = ${rowCount}`)
  expect(rowCount).toBeGreaterThan(0)
  // 名称单元格应有目录/文件链接
  await expect(page.locator('.file-name-cell').first()).toBeVisible()

  // 3. 点击「修改时间」列头排序（切换状态不报错）
  await page.getByText('修改时间', { exact: true }).click()
  await page.waitForTimeout(300)

  // 4. 搜索流程：输入触发，状态栏出现「搜索中/搜索完成」
  await page.getByPlaceholder('搜索文件...').fill('.pdf')
  await page.getByRole('button', { name: '搜索' }).click()
  await expect(page.locator('.search-status')).toBeVisible({ timeout: 10000 })

  // 清除搜索，回到目录
  await page.getByTitle('清除搜索').click()
  await expect(page.locator('.breadcrumb-root, .breadcrumb')).toBeVisible({ timeout: 8000 })

  expect(pageErrors, `页面 JS 异常: ${pageErrors.join(' | ')}`).toEqual([])
  // 允许少量良性 console error（如 favicon/404），但禁止与服务初始加载相关的异常
  const fatal = consoleErrors.filter(e => !/favicon/i.test(e))
  expect(fatal, `console error: ${fatal.join(' | ')}`).toEqual([])
})

test('UploadManager 上传弹窗正常渲染', async ({ page }) => {
  await page.goto('/#/files', { waitUntil: 'networkidle' })
  await expect(page.getByRole('button', { name: '上传', exact: true })).toBeVisible({ timeout: 15000 })

  await page.getByRole('button', { name: '上传', exact: true }).click()
  // 上传弹窗（对话框内部含拖放区）
  await expect(page.locator('.upload-dialog').first()).toBeVisible({ timeout: 8000 })
  await expect(page.getByText('拖拽文件到此处，或点击选择')).toBeVisible()
  // 上传方式 tabs（左侧面板，本地 tab 应默认激活并渲染）
  await expect(page.getByText('上传本地文件', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('从 URL 上传', { exact: true }).first()).toBeVisible()

  // 关闭弹窗（UploadManager 通过「关闭」按钮关闭）
  await page.getByRole('button', { name: '关闭' }).click()
  await expect(page.locator('.upload-dialog').first()).toBeHidden({ timeout: 8000 })

  expect(pageErrors, `页面 JS 异常: ${pageErrors.join(' | ')}`).toEqual([])
})