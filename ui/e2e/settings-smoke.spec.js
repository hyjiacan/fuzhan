import { test, expect } from '@playwright/test'

// SettingsView 拆分冒烟测试：需管理员登录
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

test('SettingsView 各 tab 正常渲染', async ({ page }) => {
  // 1. 管理员登录，取回 token
  const login = await page.request.post('/api/v1/auth/login', {
    data: { username: 'testadmin', password: 'Test12345' }
  })
  expect(login.ok()).toBeTruthy()
  const body = await login.json()
  console.log('[settings] login body =', JSON.stringify(body))
  const token = body.token || body.data?.token || body.data?.accessToken
  expect(token, '登录应返回 token').toBeTruthy()

  // 2. 注入 token 到 localStorage（在应用启动前）
  await page.addInitScript((tok) => {
    localStorage.setItem('token', tok)
    localStorage.setItem('username', 'testadmin')
    localStorage.setItem('userRole', 'admin')
  }, token)

  // 3. 访问设置页
  await page.goto('/#/admin/settings', { waitUntil: 'networkidle' })
  await expect(page.getByText('系统设置', { exact: true })).toBeVisible({ timeout: 20000 })

  // 4. 全部 tab 可见
  const tabs = ['基本信息', '服务配置', '存储配置', '预览配置', '数据库', '访问控制', '资源监控']
  for (const t of tabs) {
    await expect(page.locator('.el-tabs__item', { hasText: t }).first()).toBeVisible()
  }

  // 5. 逐 tab 切换并校验各自核心字段渲染
  const checkCases = [
    ['服务配置', '监听地址'],
    ['存储配置', '定时扫描间隔'],
    ['预览配置', 'MIME 类型'],
    ['数据库', '数据库类型'],
    ['访问控制', 'IP 访问控制'],
    ['资源监控', '启用监控'] // 资源监控
  ]
  for (const [tab, expectText] of checkCases) {
    await page.locator('.el-tabs__item', { hasText: tab }).first().click()
    await expect(page.getByText(expectText, { exact: false }).first()).toBeVisible({ timeout: 8000 })
  }

  // 6. 回到基本信息也正常
  await page.locator('.el-tabs__item', { hasText: '基本信息' }).first().click()
  await expect(page.getByText('应用名称', { exact: false }).first()).toBeVisible()

  // 7. 保存按钮可用
  await expect(page.getByRole('button', { name: '保存配置' })).toBeVisible()

  expect(pageErrors, `页面 JS 异常: ${pageErrors.join(' | ')}`).toEqual([])
})