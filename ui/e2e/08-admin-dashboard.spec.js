import { test, expect } from '@playwright/test'

/**
 * 管理后台 - 仪表盘 E2E 测试
 *
 * 业务场景：
 * 1. 管理员登录后访问看板页面
 * 2. 查看系统统计信息（用户数、文件数、存储使用）
 * 3. 查看热门搜索词
 * 4. 查看最近上传/下载记录
 */

// 登录管理员辅助函数
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

test.describe('管理后台 - 仪表盘（已登录）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page)
  })

  test('仪表盘页面可以访问', async ({ page }) => {
    // 导航到仪表盘
    await page.goto('/#/admin/dashboard')
    await page.waitForLoadState('networkidle')

    // 验证页面
    const dashboard = page.locator('.admin-dashboard, [class*="dashboard"], .el-container').first()
    await expect(dashboard).toBeVisible({ timeout: 15000 })
  })

  test('仪表盘显示统计卡片', async ({ page }) => {
    await page.goto('/#/admin/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)

    // 检查统计卡片
    const statCards = page.locator('.stat-card, [class*="stat"]')
    const count = await statCards.count()

    // 应该有用户总数、文件总数、存储等信息
    expect(count).toBeGreaterThan(0)
  })

  test('仪表盘显示存储使用进度', async ({ page }) => {
    await page.goto('/#/admin/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)

    const progress = page.locator('.el-progress, [class*="progress"]')
    await expect(progress.first()).toBeVisible({ timeout: 10000 })
  })

  test('仪表盘显示热门搜索词', async ({ page }) => {
    await page.goto('/#/admin/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)

    const keywordSection = page.locator('text=热门搜索词')
    await expect(keywordSection).toBeVisible({ timeout: 10000 })
  })

  test('仪表盘显示最近上传记录', async ({ page }) => {
    await page.goto('/#/admin/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)

    const uploadSection = page.locator('text=最近上传')
    await expect(uploadSection).toBeVisible({ timeout: 10000 })
  })

  test('仪表盘显示最近下载记录', async ({ page }) => {
    await page.goto('/#/admin/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(3000)

    const downloadSection = page.locator('text=最近下载')
    await expect(downloadSection).toBeVisible({ timeout: 10000 })
  })
})

test.describe('管理后台 - 仪表盘（未登录）', () => {
  test('未登录用户访问仪表盘显示登录弹框', async ({ page }) => {
    // 清除登录状态后直接访问管理后台
    await page.goto('/#/')
    await page.waitForLoadState('networkidle')

    // 清除所有登录相关的 localStorage
    await page.evaluate(() => {
      localStorage.removeItem('token')
      localStorage.removeItem('username')
      localStorage.removeItem('userRole')
    })

    // 刷新页面让路由守卫生效
    await page.reload()
    await page.waitForTimeout(1000)

    // 尝试访问管理后台
    await page.goto('/#/admin/dashboard')
    await page.waitForTimeout(2000)

    // 验证登录弹框出现（实现：未登录时显示登录弹框而非重定向）
    const loginDialog = page.locator('.el-overlay, .el-dialog').first()
    await expect(loginDialog).toBeVisible({ timeout: 5000 })
  })
})
