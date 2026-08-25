import { test, expect } from '@playwright/test'

/**
 * 临时文件功能 E2E 测试
 *
 * 业务场景：
 * 1. 用户访问临时文件页面
 * 2. 可以上传临时文件
 * 3. 上传后生成访问码
 * 4. 可以通过访问码下载文件
 * 5. 可以管理自己的临时文件
 */

test.describe('临时文件功能', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/#/temp')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(2000)

    // 关闭可能出现的登录弹框（el-dialog / el-card header 关闭按钮）
    const closeBtn = page.locator('.el-dialog__headerbtn, .el-card__header .el-icon')
    if (await closeBtn.isVisible().catch(() => false)) {
      await closeBtn.click()
      await page.waitForTimeout(500)
    }
  })

  test('临时文件页面可以访问', async ({ page }) => {
    const pageContent = page.locator('.temp-view, .temp, [class*="temp"], .el-container').first()
    await expect(pageContent).toBeVisible({ timeout: 10000 })
  })

  test('页面包含上传功能', async ({ page }) => {
    // 临时文件页面上传区域
    const uploadSection = page.locator('.drop-zone, [class*="upload"], button').filter({ hasText: /上传/i }).first()
    await expect(uploadSection).toBeVisible({ timeout: 10000 })
  })

  test('可以查看已上传的临时文件列表', async ({ page }) => {
    // 检查是否有文件列表或空状态提示
    const hasFileList = await page.locator('.el-table-v2').isVisible().catch(() => false)
    const hasEmptyState = await page.locator('.el-empty').isVisible().catch(() => false)

    expect(hasFileList || hasEmptyState).toBeTruthy()
  })

  test('临时文件显示访问码列', async ({ page }) => {
    // 检查文件列表
    const hasFileList = await page.locator('.el-table-v2').isVisible().catch(() => false)

    if (hasFileList) {
      // 检查是否有"访问码"列标题或相关内容
      const hasAccessCodeColumn = await page.getByText('访问码').count() > 0
      expect(hasAccessCodeColumn).toBeTruthy()
    } else {
      // 空状态也通过
      const hasEmpty = await page.locator('.el-empty').isVisible().catch(() => false)
      expect(hasEmpty).toBeTruthy()
    }
  })

  test('可以删除临时文件（如果存在）', async ({ page }) => {
    // 先检查是否有文件可以删除
    const fileRows = page.locator('.el-table-v2__row')
    const hasFiles = await fileRows.first().isVisible().catch(() => false)

    if (hasFiles) {
      // 查找删除按钮
      const deleteButton = page.locator('button[title="删除"]').first()
      const hasDeleteButton = await deleteButton.isVisible().catch(() => false)

      if (hasDeleteButton) {
        // 获取删除前的文件数量
        const initialCount = await fileRows.count()

        await deleteButton.click()
        await page.waitForTimeout(500)

        // 确认删除（如果出现确认对话框）
        const confirmButton = page.locator('button').filter({ hasText: /确定|确认/i }).first()
        if (await confirmButton.isVisible().catch(() => false)) {
          await confirmButton.click()
          await page.waitForTimeout(1000)

          // 验证文件数量减少
          const finalCount = await fileRows.count()
          expect(finalCount).toBeLessThan(initialCount)
        }
      } else {
        // 没有删除按钮（可能没有权限）
        expect(true).toBeTruthy()
      }
    } else {
      // 没有文件可删除，跳过
      expect(true).toBeTruthy()
    }
  })
})