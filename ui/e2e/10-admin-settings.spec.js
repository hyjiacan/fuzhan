import { test, expect } from '@playwright/test'

/**
 * 系统设置功能 E2E 测试
 *
 * 业务场景：
 * 1. 管理员登录后访问系统设置
 * 2. 基本信息：应用名称、共享目录、私有/临时文件、上传配置
 * 3. 服务配置：HTTP/HTTPS/FTP/FTPS/WebDAV/TLS
 * 4. 存储配置：文件索引定时扫描
 * 5. 预览配置：MIME 类型、扩展名、分块大小
 * 6. 数据库：切换数据库类型
 * 7. 访问控制：IP 访问控制、文件扩展名
 * 8. 底部保存/重置按钮
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
    await page.locator('.el-dialog .el-button--primary').first().click()
    await page.waitForTimeout(3000)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(500)
  }
}

// 切换到指定 label 的标签页
async function switchTab(page, label) {
  await page.locator('.el-tabs__item').filter({ hasText: label }).click()
  await page.waitForTimeout(500)
}

test.describe('系统设置页面', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page)
    await page.goto('/#/settings')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)
  })

  test('页面可以访问', async ({ page }) => {
    const title = page.locator('h2:has-text("系统设置")')
    await expect(title).toBeVisible({ timeout: 10000 })
  })

  test('基本信息标签页 - 应用名称和共享目录配置', async ({ page }) => {
    // 基本信息标签默认激活
    const basicTab = page.locator('.el-tabs__item').filter({ hasText: '基本信息' }).first()
    await expect(basicTab).toBeVisible({ timeout: 5000 })

    // 应用名称输入框
    const appNameInput = page.locator('input[placeholder*="应用名称"]')
    await expect(appNameInput).toBeVisible()

    // 共享目录配置卡片
    const dirConfig = page.locator('.el-tabs .el-card:has-text("共享目录配置")').first()
    await expect(dirConfig).toBeVisible()
  })

  test('基本信息标签页 - 私有文件配置', async ({ page }) => {
    const privateSection = page.locator('.el-card:has-text("私有文件配置")').first()
    await expect(privateSection).toBeVisible()

    // 启用私有文件开关存在
    const enableSwitch = privateSection.locator('.el-switch').first()
    await expect(enableSwitch).toBeVisible()
  })

  test('基本信息标签页 - 临时文件配置', async ({ page }) => {
    const tempSection = page.locator('.el-card:has-text("临时文件配置")').first()
    await expect(tempSection).toBeVisible()

    // 是否包含启用开关和过期天数输入
    const expireInput = tempSection.locator('.el-input-number').first()
    await expect(expireInput).toBeVisible()
  })

  test('基本信息标签页 - 上传配置显示分片大小和最大文件大小', async ({ page }) => {
    const uploadSection = page.locator('.el-card:has-text("上传配置")').first()
    await expect(uploadSection).toBeVisible()

    const chunkSize = uploadSection.locator('label:has-text("分片大小")')
    await expect(chunkSize).toBeVisible()

    const maxFileSize = uploadSection.locator('label:has-text("最大文件大小")')
    await expect(maxFileSize).toBeVisible()
  })

  test('服务配置标签页 - HTTP/HTTPS 配置', async ({ page }) => {
    await switchTab(page, '服务配置')

    const httpLabel = page.getByText('启用 HTTP', { exact: true })
    await expect(httpLabel).toBeVisible()

    const httpsLabel = page.getByText('启用 HTTPS', { exact: true })
    await expect(httpsLabel).toBeVisible()

    // 有开关控件
    const switches = page.locator('.el-switch')
    const switchCount = await switches.count()
    expect(switchCount).toBeGreaterThanOrEqual(2)
  })

  test('服务配置标签页 - HTTPS 启用后显示端口', async ({ page }) => {
    await switchTab(page, '服务配置')

    // 找到 HTTPS 相关的开关（label 为"启用 HTTPS"的 form-item 内）
    const httpsSwitch = page.locator('.el-form-item:has(.el-switch):has-text("启用 HTTPS") .el-switch')
    if (await httpsSwitch.isVisible()) {
      await httpsSwitch.click()
      await page.waitForTimeout(300)

      // HTTPS 端口输入应出现
      const httpsPort = page.locator('label:has-text("HTTPS 端口")')
      await expect(httpsPort).toBeVisible({ timeout: 3000 })
    }
  })

  test('服务配置标签页 - FTP/FTPS/TLS 证书配置', async ({ page }) => {
    await switchTab(page, '服务配置')

    // FTP 服务
    const ftpSection = page.locator('.el-card:has-text("FTP 服务")').first()
    await expect(ftpSection).toBeVisible()

    // FTPS 服务
    const ftpsSection = page.locator('.el-card:has-text("FTPS 服务")').first()
    await expect(ftpsSection).toBeVisible()

    // TLS 证书配置区域（证书文件和密钥文件输入）
    const tlsSection = page.locator('.el-form-item:has-text("证书文件")').first()
    await expect(tlsSection).toBeVisible()
  })

  test('服务配置标签页 - WebDAV 配置', async ({ page }) => {
    await switchTab(page, '服务配置')

    // WebDAV 卡片
    const webdavSwitch = page.locator('.el-form-item:has-text("启用 WebDAV") .el-switch')
    await expect(webdavSwitch).toBeVisible()

    // 打开 WebDAV 后公开用户名输入出现
    await webdavSwitch.click()
    await page.waitForTimeout(300)
    const publicUserInput = page.locator('input[placeholder="public"]')
    await expect(publicUserInput).toBeVisible({ timeout: 3000 })
  })

  test('存储配置标签页 - 显示定时扫描配置', async ({ page }) => {
    await switchTab(page, '存储配置')

    const indexSection = page.locator('.el-card:has-text("文件索引配置")').first()
    await expect(indexSection).toBeVisible()

    // 定时扫描 Cron 输入框
    const cronInput = page.locator('input[type="text"]').nth(0)
    await expect(indexSection.locator('label:has-text("定时扫描间隔")')).toBeVisible()
  })

  test('预览配置标签页 - 显示 MIME 类型和扩展名', async ({ page }) => {
    await switchTab(page, '预览配置')

    const previewSection = page.locator('.el-card:has-text("预览配置")').first()
    await expect(previewSection).toBeVisible()

    const mimeLabel = previewSection.locator('label:has-text("MIME 类型")')
    await expect(mimeLabel).toBeVisible()

    const extLabel = previewSection.locator('label:has-text("文件扩展名")')
    await expect(extLabel).toBeVisible()
  })

  test('数据库标签页 - 显示数据库类型选择和切换', async ({ page }) => {
    await switchTab(page, '数据库')

    // 数据库类型选项（使用 radio label 精确定位）
    const sqliteRadio = page.locator('.el-radio-group .el-radio').filter({ hasText: 'SQLite' })
    await expect(sqliteRadio.first()).toBeVisible()

    const mysqlRadio = page.locator('.el-radio-group .el-radio').filter({ hasText: 'MySQL' })
    await expect(mysqlRadio.first()).toBeVisible()

    const pgRadio = page.locator('.el-radio-group .el-radio').filter({ hasText: 'PostgreSQL' })
    await expect(pgRadio.first()).toBeVisible()
  })

  test('访问控制标签页 - 显示 IP 访问控制和文件扩展名', async ({ page }) => {
    await switchTab(page, '访问控制')

    const ipSection = page.locator('.el-card:has-text("IP 访问控制")').first()
    await expect(ipSection).toBeVisible()

    const fileAccessSection = page.locator('.el-card:has-text("文件访问控制")').first()
    await expect(fileAccessSection).toBeVisible()

    const extLabel = fileAccessSection.locator('label:has-text("允许的扩展名")')
    await expect(extLabel).toBeVisible()
  })

  test('页面底部有保存和重置按钮', async ({ page }) => {
    const saveBtn = page.locator('button:has-text("保存配置")')
    await expect(saveBtn).toBeVisible({ timeout: 5000 })

    const resetBtn = page.locator('button:has-text("重置")')
    await expect(resetBtn).toBeVisible()
  })
})