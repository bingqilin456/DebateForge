import { expect, test } from '@playwright/test'

test('V2-04 隔离 Fake 对局保存原文批注并在历史恢复', async ({ page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_ANNOTATIONS !== '1', '仅在显式隔离 Fake 批注环境执行')
  test.setTimeout(90_000)
  const username = `v204_${Date.now().toString(36)}`
  const password = 'annotation-browser-synthetic-password'
  const registered = await page.request.post('/api/v1/auth/register', {
    headers: { Origin: baseURL! },
    data: { username, password },
  })
  expect(registered.status()).toBe(201)

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/#history')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.locator('[data-view-tab="topics"]').click()
  await page.locator('[data-topic-id="1"] input').check()
  await page.locator('input[value="pro"]').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect(page.getByText('已完成 0 / 4 轮')).toBeVisible()
  const debateId = new URL(page.url()).hash.replace('#debate=', '')
  const annotationPanel = page.locator('[data-annotation-panel]')
  const firstBatch = annotationPanel.locator('[data-annotation-round="1"]')
  const firstMarker = page.locator('.message.user [data-annotation-markers-entry]').first()

  for (let round = 1; round <= 4; round++) {
    await page.locator('#first-content').fill(`第${round}轮：我先界定讨论范围🌱，再说明判断标准和理由。`)
    const send = round === 1 ? '发送首轮' : `发送第 ${round} 轮`
    await page.getByRole('button', { name: send, exact: true }).click()
    await expect(page.locator('.message')).toHaveCount(round * 2)
    await expect(annotationPanel.locator(`[data-annotation-round="${round}"]`)).toBeAttached()
    if (round === 1) {
      await expect(firstMarker).toBeVisible()
      await expect(firstBatch.locator('[data-annotation-detail]')).toHaveCount(0)
      await firstMarker.click()
      await expect(firstBatch.locator('[data-annotation-detail]')).toBeVisible()
      await firstBatch.getByRole('button', { name: '定位原文' }).click()
      await expect(firstMarker).toBeFocused()

      const transcript = page.getByRole('region', { name: '首轮辩论面板' })
      const desktopTranscript = await transcript.boundingBox()
      const desktopPanel = await annotationPanel.boundingBox()
      expect(desktopPanel!.x).toBeGreaterThanOrEqual(desktopTranscript!.x + desktopTranscript!.width)
      await page.setViewportSize({ width: 390, height: 844 })
      const mobileTranscript = await transcript.boundingBox()
      const mobilePanel = await annotationPanel.boundingBox()
      expect(mobilePanel!.y).toBeGreaterThanOrEqual(mobileTranscript!.y + mobileTranscript!.height)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)

      await page.getByRole('switch', { name: '显示批注' }).click()
      await expect(annotationPanel).toBeHidden()
      await expect(firstMarker).toHaveCount(0)
    }
  }
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  await page.locator('[data-view-tab="history"]').click()
  await page.locator(`[data-debate-id="${debateId}"]`).click()
  await page.reload()
  await expect(page.locator('.message')).toHaveCount(8)
  await expect(annotationPanel.locator('[data-annotation-round]')).toHaveCount(4)
  await expect(firstMarker).toBeVisible()
})
