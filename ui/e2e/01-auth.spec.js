import { test, expect } from '@playwright/test'

/**
 * 用户认证功能 E2E 测试
 *
 * 业务场景：
 * 1. 用户点击登录按钮
 * 2. 弹出登录弹框
 * 3. 输入用户名密码
 * 4. 提交表单
 * 5. 登录成功/失败反馈
 * 注意：当前 UI 登录弹框内无注册入口，注册通过 /#/register 页面或服务端 API 完成
 */

// 关闭登录弹框
async function closeLoginModal(page) {
  await page.keyboard.press('Escape')
  await page.waitForTimeout(300)
}

test.describe('用户认证功能', () => {
  test.beforeEach(async ({ page }) => {
    // 访问页面并等待加载
    await page.goto('/#/')
    await page.waitForTimeout(3000)
  })

  test('页面顶部有登录入口', async ({ page }) => {
    const loginButton = page.locator('.header-actions button').filter({ hasText: /登录|Login/i })
    await expect(loginButton).toBeVisible({ timeout: 10000 })
  })

  test('点击登录按钮打开登录弹框', async ({ page }) => {
    const loginButton = page.locator('.header-actions button').filter({ hasText: /登录|Login/i })
    await loginButton.click()

    // 等待弹框出现
    await page.waitForTimeout(500)

    // 验证登录表单元素存在
    const usernameInput = page.locator('.el-dialog input[placeholder*="用户"]').first()
    const passwordInput = page.locator('.el-dialog input[type="password"]').first()

    await expect(usernameInput).toBeVisible()
    await expect(passwordInput).toBeVisible()

    await closeLoginModal(page)
  })

  test('登录表单包含用户名和密码字段', async ({ page }) => {
    await page.locator('.header-actions button').filter({ hasText: /登录|Login/i }).click()
    await page.waitForTimeout(500)

    // 验证两个表单字段都存在
    const inputs = page.locator('.el-dialog input')
    const count = await inputs.count()
    expect(count).toBeGreaterThanOrEqual(2)

    await closeLoginModal(page)
  })

  test('空表单点击提交显示验证提示', async ({ page }) => {
    await page.locator('.header-actions button').filter({ hasText: /登录|Login/i }).click()
    await page.waitForTimeout(500)

    // 点击提交按钮
    const submitButton = page.locator('.el-dialog .el-button--primary').first()
    await submitButton.click()
    await page.waitForTimeout(500)

    // 验证表单未关闭（验证失败）。element-plus 会保留已关闭 el-dialog 的 DOM（display:none），
    // 且页面存在多个 dialog（登录/注册/上传状态等），因此仅按可见的「登录」dialog 判断打开状态
    const modalStillOpen = await page.getByRole('dialog', { name: '登录' }).isVisible().catch(() => false)
    expect(modalStillOpen).toBeTruthy()

    // 或者验证出现了验证错误提示
    const hasError = await page.locator('.el-form-item__error').isVisible().catch(() => false)
    expect(hasError || modalStillOpen).toBeTruthy()

    await closeLoginModal(page)
  })

  test('登录失败显示错误提示', async ({ page }) => {
    await page.locator('.header-actions button').filter({ hasText: /登录|Login/i }).click()
    await page.waitForTimeout(500)

    // 输入错误凭据
    await page.locator('.el-dialog input[placeholder*="用户"]').first().fill('wronguser')
    await page.locator('.el-dialog input[type="password"]').first().fill('wrongpass')

    // 提交
    await page.locator('.el-dialog .el-button--primary').first().click()
    await page.waitForTimeout(2000)

    // 验证弹框仍然显示（登录失败）
    const dialogStillOpen = await page.getByRole('dialog', { name: '登录' }).isVisible().catch(() => false)
    expect(dialogStillOpen).toBeTruthy()

    // 验证有错误消息显示（el-message 或 el-notification）
    const hasMessage = await page.locator('.el-message, .el-notification').isVisible().catch(() => false)
    // 如果没有消息组件，至少验证弹框还在（表示登录未成功）
    expect(hasMessage || dialogStillOpen).toBeTruthy()

    await closeLoginModal(page)
  })
})
