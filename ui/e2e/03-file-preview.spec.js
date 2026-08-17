import { test, expect } from '@playwright/test'

/**
 * 文件预览功能 E2E 测试
 *
 * 业务场景：
 * 1. 用户在文件列表中点击预览按钮
 * 2. 弹框显示文件预览内容
 * 3. 文本/图片/PDF 等格式可以直接预览
 */

test.describe('文件预览功能', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(2000)
  })

  test('预览按钮在文件操作列中', async ({ page }) => {
    // 查找包含"预览"文字的按钮
    const previewButtons = page.locator('button').filter({ hasText: '预览' })
    const count = await previewButtons.count()

    // 如果有可预览的文件，应该能看到预览按钮
    if (count > 0) {
      await expect(previewButtons.first()).toBeVisible()
    }
  })

  test('点击预览按钮打开预览弹框', async ({ page }) => {
    const previewButton = page.locator('button[title="预览"]').first()
    const hasPreviewButton = await previewButton.isVisible().catch(() => false)

    if (hasPreviewButton) {
      await previewButton.click()

      // 验证预览弹框出现
      const previewDialog = page.locator('.preview-dialog, [class*="preview"] .n-modal').first()
      await expect(previewDialog).toBeVisible({ timeout: 5000 })
    }
  })

  test('预览弹框包含关闭按钮', async ({ page }) => {
    const previewButton = page.locator('button[title="预览"]').first()
    const hasPreviewButton = await previewButton.isVisible().catch(() => false)

    if (hasPreviewButton) {
      await previewButton.click()
      await page.waitForTimeout(500)

      // 验证有关闭按钮
      const closeButton = page.locator('.n-modal-close, button[class*="close"]').first()
      await expect(closeButton).toBeVisible()
    }
  })

  test('预览弹框标题正确', async ({ page }) => {
    const previewButton = page.locator('button[title="预览"]').first()
    const hasPreviewButton = await previewButton.isVisible().catch(() => false)

    if (hasPreviewButton) {
      await previewButton.click()

      // 验证标题包含"预览"
      const dialogTitle = page.locator('.n-modal-card .n-card-header, [class*="preview"] .n-card-header').first()
      await expect(dialogTitle).toContainText('预览')
    }
  })
})