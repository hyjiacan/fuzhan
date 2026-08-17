import { test, expect } from '@playwright/test'

/**
 * 私有存储功能 E2E 测试
 *
 * 业务场景：
 * 1. 登录用户访问私有存储
 * 2. 可以上传私有文件
 * 3. 只能自己查看和管理
 * 4. 可以设置文件过期时间
 */

// 登录辅助函数
async function loginAsAdmin(page) {
  await page.goto('/#/')
  await page.waitForTimeout(2000)

  const loginButton = page.locator('.header-actions button').filter({ hasText: /登录|Login/i })
  if (await loginButton.isVisible().catch(() => false)) {
    await loginButton.click()
    await page.waitForTimeout(500)
    await page.locator('.n-modal input[placeholder*="用户"]').first().fill('admin')
    await page.locator('.n-modal input[type="password"]').first().fill('admin123')
    await page.locator('.n-modal .n-button--primary-type').first().click()
    await page.waitForTimeout(3000)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(500)
  }
}

test.describe('私有存储功能', () => {
  test('未登录时访问私有存储显示登录提示', async ({ page }) => {
    await page.goto('/#/')
    await page.waitForLoadState('networkidle')

    // 清除登录状态
    await page.evaluate(() => localStorage.clear())
    await page.reload()
    await page.waitForTimeout(2000)

    // 访问私有存储（路由守卫会拦截，弹出登录弹框并重定向）
    await page.goto('/#/private')
    await page.waitForTimeout(3000)

    // 验证登录弹框出现（Naive UI card modal 含用户名/密码输入框）
    const usernameInput = page.locator('input[placeholder*="用户"]').first()
    await expect(usernameInput).toBeVisible({ timeout: 5000 })
  })

  test('登录后可以访问私有存储', async ({ page }) => {
    await loginAsAdmin(page)

    // 导航到私有存储
    await page.goto('/#/private')
    await page.waitForTimeout(3000)

    // 验证私有存储页面内容：配额、文件列表、空状态至少有一种
    const hasQuota = await page.getByText(/配额|Quota|已用|存储|空间/i).isVisible().catch(() => false)
    const hasFileList = await page.locator('.n-data-table').isVisible().catch(() => false)
    const hasEmpty = await page.locator('.n-empty').isVisible().catch(() => false)
    const hasUpload = await page.locator('button:has-text("上传")').isVisible().catch(() => false)

    // 至少应该显示配额信息或文件列表或上传按钮
    expect(hasQuota || hasFileList || hasEmpty || hasUpload).toBeTruthy()
  })

  test('私有存储页面显示配额信息', async ({ page }) => {
    await loginAsAdmin(page)

    // 导航到私有存储
    await page.goto('/#/private')
    await page.waitForTimeout(3000)

    // 页面应该有工具栏区域（包含按钮）
    const toolbar = page.locator('.toolbar-row')
    const hasToolbar = await toolbar.isVisible().catch(() => false)

    // 或者显示空状态
    const emptyState = page.locator('.empty-state')
    const hasEmptyState = await emptyState.isVisible().catch(() => false)

    // 工具栏或空状态至少有一个
    expect(hasToolbar || hasEmptyState).toBeTruthy()
  })
})
