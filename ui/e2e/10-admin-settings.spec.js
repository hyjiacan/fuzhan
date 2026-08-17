import { test, expect } from '@playwright/test'

/**
 * 系统设置功能 E2E 测试
 *
 * 业务场景：
 * 1. 管理员登录后访问系统设置
 * 2. 基础配置：应用名称、HTTP/HTTPS 服务配置
 * 3. 数据库配置：切换数据库类型
 * 4. 存储配置：共享目录、私有/临时文件
 * 5. 上传配置：分片大小、文件大小限制
 * 6. 高级配置：扩展名、预览 MIME 类型
 * 7. FTP/FTPS/TLS 配置：FTP 开关、TLS 证书上传
 * 8. WebDAV 配置：开关、公开用户名
 */

// 登录管理员辅助函数（复用 08 的模式）
async function adminLogin(page) {
  await page.goto('/#/')
  await page.waitForLoadState('networkidle')

  const loginButton = page.locator('.header-actions button').filter({ hasText: /登录|Login/i })
  if (await loginButton.isVisible()) {
    await loginButton.click()
    await page.waitForTimeout(500)
    await page.locator('input[placeholder*="用户"], input[type="text"]').first().fill('admin')
    await page.locator('input[type="password"]').first().fill('admin123')
    await page.locator('.n-modal .n-button--primary-type, .n-modal .n-button.primary').first().click()
    await page.waitForTimeout(3000)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(500)
  }
}

test.describe('系统设置页面', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page)
    await page.goto('/#/settings')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)
  })

  test('页面可以访问', async ({ page }) => {
    const title = page.locator('text=系统设置')
    await expect(title).toBeVisible({ timeout: 10000 })
  })

  test('基础配置标签页 - 应用名称和 HTTP/HTTPS 配置', async ({ page }) => {
    // 基础配置标签默认激活
    const basicTab = page.locator('text=基础配置').first()
    await expect(basicTab).toBeVisible({ timeout: 5000 })

    // 应用名称输入框
    const appNameInput = page.locator('input[placeholder*="应用名称"]')
    await expect(appNameInput).toBeVisible()

    // HTTP 区域
    const httpLabel = page.getByText('启用 HTTP', { exact: true })
    await expect(httpLabel).toBeVisible()

    // HTTPS 区域
    const httpsLabel = page.getByText('启用 HTTPS', { exact: true })
    await expect(httpsLabel).toBeVisible()

    // 有开关控件
    const switches = page.locator('.n-switch')
    const switchCount = await switches.count()
    expect(switchCount).toBeGreaterThanOrEqual(2)
  })

  test('基础配置 - HTTPS 启用后显示端口', async ({ page }) => {
    // 找到 HTTPS 相关的开关
    const httpsSwitches = page.getByText('启用 HTTPS', { exact: true }).locator('..').locator('.n-switch')
    if (await httpsSwitches.isVisible()) {
      await httpsSwitches.click()
      await page.waitForTimeout(300)

      // HTTPS 端口输入应出现
      const httpsPort = page.getByText('HTTPS 端口', { exact: true })
      await expect(httpsPort).toBeVisible({ timeout: 3000 })
    }
  })

  test('数据库标签页 - 显示数据库类型选择和切换', async ({ page }) => {
    // 点击数据库标签
    await page.locator('.n-tabs .n-tabs-nav .n-tabs-tab').filter({ hasText: '数据库' }).click()
    await page.waitForTimeout(500)

    // 数据库类型选项（使用 radio label 精确定位）
    const sqliteRadio = page.locator('.n-radio-group .n-radio').filter({ hasText: 'SQLite' })
    await expect(sqliteRadio.first()).toBeVisible()

    const mysqlRadio = page.locator('.n-radio-group .n-radio').filter({ hasText: 'MySQL' })
    await expect(mysqlRadio.first()).toBeVisible()

    const pgRadio = page.locator('.n-radio-group .n-radio').filter({ hasText: 'PostgreSQL' })
    await expect(pgRadio.first()).toBeVisible()
  })

  test('存储标签页 - 显示共享目录、私有/临时文件配置', async ({ page }) => {
    await page.locator('.n-tabs .n-tabs-nav .n-tabs-tab').filter({ hasText: '存储' }).click()
    await page.waitForTimeout(500)

    // 共享目录
    const dirConfig = page.locator('text=共享目录配置')
    await expect(dirConfig).toBeVisible()

    // 私有文件
    const privateSection = page.locator('text=私有文件配置')
    await expect(privateSection).toBeVisible()

    // 临时文件
    const tempSection = page.locator('text=临时文件配置')
    await expect(tempSection).toBeVisible()
  })

  test('上传标签页 - 显示分片大小和最大文件大小', async ({ page }) => {
    await page.locator('.n-tabs .n-tabs-nav .n-tabs-tab').filter({ hasText: '上传' }).click()
    await page.waitForTimeout(500)

    const chunkSize = page.locator('text=分片大小')
    await expect(chunkSize).toBeVisible()

    const maxFileSize = page.locator('text=最大文件大小')
    await expect(maxFileSize).toBeVisible()
  })

  test('高级标签页 - 显示扩展名和预览配置', async ({ page }) => {
    await page.locator('.n-tabs .n-tabs-nav .n-tabs-tab').filter({ hasText: '高级' }).click()
    await page.waitForTimeout(500)

    const extLabel = page.locator('text=允许的扩展名')
    await expect(extLabel).toBeVisible()

    const previewSection = page.locator('text=预览配置')
    await expect(previewSection).toBeVisible()
  })

  test('FTP 标签页 - 显示 FTP/FTPS/TLS 证书配置', async ({ page }) => {
    await page.locator('.n-tabs .n-tabs-nav .n-tabs-tab').filter({ hasText: 'FTP' }).click()
    await page.waitForTimeout(500)

    // FTP 服务
    const ftpSection = page.locator('text=FTP 服务')
    await expect(ftpSection).toBeVisible()

    // FTPS 服务
    const ftpsSection = page.locator('text=FTPS 服务')
    await expect(ftpsSection).toBeVisible()

    // TLS 证书配置（共享） — 新增的共享 TLS 证书区域
    const tlsSection = page.locator('text=TLS 证书配置（共享）')
    await expect(tlsSection).toBeVisible()

    // 证书文件和密钥文件输入
    const certInput = page.locator('input[placeholder="未配置"]').first()
    await expect(certInput).toBeVisible()

    // 上传按钮存在
    const uploadBtns = page.locator('button:has-text("上传")')
    const uploadCount = await uploadBtns.count()
    expect(uploadCount).toBeGreaterThanOrEqual(2)
  })

  test('WebDAV 标签页 - 显示 WebDAV 配置', async ({ page }) => {
    await page.locator('.n-tabs .n-tabs-nav .n-tabs-tab').filter({ hasText: 'WebDAV' }).click()
    await page.waitForTimeout(500)

    // WebDAV 区域标题
    const webdavSection = page.locator('text=WebDAV 服务').first()
    await expect(webdavSection).toBeVisible()

    // 启用 WebDAV 开关
    const webdavSwitch = page.locator('.n-switch').last()
    await expect(webdavSwitch).toBeVisible()

    // 打开 WebDAV 后公开用户名出现
    await webdavSwitch.click()
    await page.waitForTimeout(300)
    const publicUserInput = page.locator('input[placeholder="public"]')
    await expect(publicUserInput).toBeVisible({ timeout: 3000 })
  })

  test('页面底部有保存和重置按钮', async ({ page }) => {
    const saveBtn = page.locator('button:has-text("保存配置")')
    await expect(saveBtn).toBeVisible({ timeout: 5000 })

    const resetBtn = page.locator('button:has-text("重置")')
    await expect(resetBtn).toBeVisible()
  })
})
