import { test, expect } from '@playwright/test'

/**
 * 部署向导（Setup）功能 E2E 测试
 *
 * 前置条件：server/fuzhan.yaml 中 initialized 必须为 false
 * 运行方式：单独运行（本测试需要服务器处于未初始化状态）
 *   npx playwright test e2e/00-setup-config.spec.js
 *
 * 业务场景：
 * 1. 访问部署向导页面
 * 2. 查看基本配置（应用名称、监听地址、端口）
 * 3. 查看 HTTPS 配置（开关、端口、证书上传）
 * 4. 查看数据库配置
 * 5. 查看共享目录配置
 * 6. 查看管理员配置
 */

test.describe('部署向导页面', () => {
  test.beforeEach(async ({ page }) => {
    // 直接导航到 setup 页面
    await page.goto('/#/setup')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)
  })

  test('页面可以访问并显示标题', async ({ page }) => {
    // 验证页面标题存在
    const subtitle = page.locator('text=部署向导')
    await expect(subtitle).toBeVisible({ timeout: 10000 })
  })

  test('基本配置区域可见', async ({ page }) => {
    // 基本配置区域标题
    const basicSection = page.locator('text=基本配置')
    await expect(basicSection).toBeVisible({ timeout: 5000 })

    // 应用名称输入框
    const appNameInput = page.locator('input[placeholder*="应用名称"]')
    await expect(appNameInput).toBeVisible()

    // 端口输入框
    const portInput = page.locator('.el-input-number').first()
    await expect(portInput).toBeVisible()
  })

  test('HTTPS 配置区域存在开关和端口设置', async ({ page }) => {
    // HTTPS 区域标题
    const httpsSection = page.locator('text=HTTPS 服务配置')
    await expect(httpsSection).toBeVisible({ timeout: 5000 })

    // HTTPS 开关存在
    const httpsSwitch = page.locator('.el-switch').first()
    await expect(httpsSwitch).toBeVisible()

    // HTTPS 提示文本存在
    const hint = page.locator('text=启用 HTTPS 加密传输')
    await expect(hint).toBeVisible()
  })

  test('启用 HTTPS 后显示端口和证书上传区域', async ({ page }) => {
    // 点击 HTTPS 开关
    const httpsSwitch = page.locator('.el-switch').first()
    await httpsSwitch.click()
    await page.waitForTimeout(500)

    // HTTPS 端口输入应可见（使用 el-form-item 的 label 精确定位）
    const httpsPortLabel = page.locator('.el-form-item__label', { hasText: 'HTTPS 端口' })
    await expect(httpsPortLabel).toBeVisible({ timeout: 3000 })

    // 证书文件上传区域可见
    const certLabel = page.locator('.el-form-item__label', { hasText: '证书文件' })
    await expect(certLabel).toBeVisible()

    // 密钥文件上传区域可见
    const keyLabel = page.locator('.el-form-item__label', { hasText: '密钥文件' })
    await expect(keyLabel).toBeVisible()

    // 上传证书按钮存在
    const certUploadBtn = page.getByRole('button', { name: '上传证书' })
    await expect(certUploadBtn).toBeVisible()

    // 上传密钥按钮存在
    const keyUploadBtn = page.getByRole('button', { name: '上传密钥' })
    await expect(keyUploadBtn).toBeVisible()
  })

  test('数据库配置区域显示数据库类型选择', async ({ page }) => {
    const dbSection = page.locator('text=数据库配置')
    await expect(dbSection).toBeVisible({ timeout: 5000 })

    // 默认选中 SQLite
    const sqliteRadio = page.locator('text=SQLite').first()
    await expect(sqliteRadio).toBeVisible()

    // MySQL 和 PostgreSQL 选项也存在
    const mysqlRadio = page.locator('text=MySQL').first()
    await expect(mysqlRadio).toBeVisible()

    const pgRadio = page.locator('text=PostgreSQL').first()
    await expect(pgRadio).toBeVisible()
  })

  test('共享目录区域可见且可添加目录', async ({ page }) => {
    const dirSection = page.getByRole('heading', { name: '共享目录' })
    await expect(dirSection).toBeVisible({ timeout: 5000 })

    // 至少有一个目录输入框
    const pathInputs = page.locator('input[placeholder]')
    const count = await pathInputs.count()
    expect(count).toBeGreaterThanOrEqual(1)

    // 添加共享目录按钮存在
    const addBtn = page.locator('text=添加共享目录')
    await expect(addBtn).toBeVisible()
  })

  test('管理员配置区域可见', async ({ page }) => {
    const adminSection = page.locator('text=管理员配置')
    await expect(adminSection).toBeVisible({ timeout: 5000 })

    // 管理员用户名输入框
    const usernameInput = page.locator('input[placeholder="admin"]')
    await expect(usernameInput).toBeVisible()

    // 管理员密码输入框
    const passwordInput = page.locator('input[placeholder*="管理员密码"]')
    await expect(passwordInput).toBeVisible()
  })

  test('页脚存在完成配置按钮', async ({ page }) => {
    const submitBtn = page.locator('text=完成配置')
    await expect(submitBtn).toBeVisible({ timeout: 5000 })
  })
})
