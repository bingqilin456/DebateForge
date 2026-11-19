import { expect, test } from '@playwright/test'

/**
 * 争点卡隔离前端 Mock 验收（独立端口 5273，全部数据来自内存夹具）。
 *
 * 覆盖：默认 5 卡上限、双方引用点击精确高亮、重复歧义显示待核对、
 * 人工改名后刷新仍保留、版本冲突弱提示且保留数据、越权 404 与整理失败提示。
 * 不连接 Go 后端、不调用真实模型；本结果不能替代真实链路联调。
 */

test.beforeEach(async ({ page }) => {
  await page.goto('/issue-card-preview.html')
  await expect(page.getByRole('heading', { name: '争点卡隔离 Mock 预览' })).toBeVisible()
  await expect(page.locator('[data-issue-card]')).toHaveCount(5)
})

test('默认最多 5 张，并展示提出原文、相关回应与待澄清', async ({ page }) => {
  await expect(page.locator('[data-issue-card-count]')).toHaveText('5 / 5')
  await expect(page.locator('[data-issue-card-cap]')).toContainText('默认只展示最相关的 5 张')
  const first = page.locator('[data-issue-card]').first()
  await expect(first).toContainText('提出原文')
  await expect(first).toContainText('相关回应')
  await expect(first).toContainText('回应是否切中问题')
  await expect(first).toContainText('卡片不判定“已驳倒”')
})

test('点击双方引用精确定位发言流中的原句', async ({ page }) => {
  const card = page.locator('[data-issue-card]').first()
  const markers = card.locator('[data-issue-card-citation]')
  await expect(markers).toHaveCount(2)
  const userKey = await markers.first().getAttribute('data-issue-card-citation')
  const aiKey = await markers.nth(1).getAttribute('data-issue-card-citation')
  // 引用本身的码点数＝应高亮的 span 数：定位只覆盖已复核区间，不整条发言。
  const userQuoteLength = Array.from((await markers.first().textContent()) ?? '').length
  const aiQuoteLength = Array.from((await markers.nth(1).textContent()) ?? '').length
  expect(userQuoteLength).toBeGreaterThan(0)
  expect(aiQuoteLength).toBeGreaterThan(0)

  await markers.first().click()
  await expect(page.locator('.message.user').first().locator('.is-located')).toHaveCount(userQuoteLength)
  await expect(page.locator('.message.assistant').first().locator('.is-located')).toHaveCount(0)

  await markers.nth(1).click()
  await expect(page.locator('.message.assistant').first().locator('.is-located')).toHaveCount(aiQuoteLength)
  await expect(page.locator('.message.user').first().locator('.is-located')).toHaveCount(0)

  // 定位键来自已复核坐标，双方互不相同。
  expect(userKey).not.toBe(aiKey)
})

test('重复歧义与失效引用显示待核对，不高亮、不猜位置', async ({ page }) => {
  // 第 5 张卡的提出原文“抽样”在原文中出现两次，只能待核对。
  const ambiguous = page.locator('[data-issue-card]').nth(4)
  await expect(ambiguous).toContainText('原文待核对')
  await expect(ambiguous.locator('[data-issue-card-citation]')).toHaveCount(1)
  await expect(ambiguous).toContainText('不猜测位置')
})

test('人工改名后刷新仍读取保存的人工标题', async ({ page }) => {
  const first = page.locator('[data-issue-card]').first()
  await first.locator('[data-issue-card-rename]').click()
  const input = first.locator('[data-issue-card-rename-form] input')
  await input.fill('我的人工争点')
  await first.locator('[data-issue-card-rename-save]').click()
  await expect(first.locator('h4')).toHaveText('我的人工争点')
  await expect(first).toContainText('人工标题')
  // 刷新后由 Mock 的已保存状态重建，不再回到 AI 标题。
  await page.reload()
  await expect(page.locator('[data-issue-card]').first().locator('h4')).toHaveText('我的人工争点')
  await expect(page.locator('[data-issue-card]').first()).toContainText('人工标题')
})

test('改名后重新整理只补兼容内容，不覆盖人工标题', async ({ page }) => {
  const first = page.locator('[data-issue-card]').first()
  await first.locator('[data-issue-card-rename]').click()
  await first.locator('[data-issue-card-rename-form] input').fill('人工标题优先')
  await first.locator('[data-issue-card-rename-save]').click()
  await expect(first.locator('h4')).toHaveText('人工标题优先')
  await page.locator('[data-issue-card-analyze]').click()
  await expect(first.locator('h4')).toHaveText('人工标题优先')
  await expect(first).toContainText('晚到分析未覆盖你的人工标题')
})

test('移除与合并后刷新仍按人工编辑渲染', async ({ page }) => {
  const cards = page.locator('[data-issue-card]')
  const firstTitle = await cards.first().locator('h4').textContent()
  await cards.first().locator('[data-issue-card-remove]').click()
  // 预览页用页面内确认层，不依赖框架弹窗。
  await page.locator('[data-preview-confirm-ok]').click()
  await expect(cards).toHaveCount(4)
  // 被合并卡不再单独显示，但目标卡保留双方原句链接。
  await cards.first().locator('[data-issue-card-merge]').click()
  await cards.first().locator('[data-issue-card-merge-form] button').first().click()
  await page.locator('[data-preview-confirm-ok]').click()
  await expect(cards).toHaveCount(3)
  await expect(page.locator('[data-issue-card-panel]')).not.toContainText(firstTitle ?? '')
  await page.reload()
  await expect(cards).toHaveCount(3)
})

test('版本冲突只弱提示并重新读取，不覆盖已保存数据', async ({ page }) => {
  await page.locator('[data-preview-scenario="编辑版本冲突一次"]').click()
  const first = page.locator('[data-issue-card]').first()
  const savedTitle = await first.locator('h4').textContent()
  await first.locator('[data-issue-card-rename]').click()
  await first.locator('[data-issue-card-rename-form] input').fill('会被冲突拦下的标题')
  await first.locator('[data-issue-card-rename-save]').click()
  await expect(page.locator('[data-issue-card-notice]')).toContainText('版本已变化')
  await expect(first.locator('h4')).toHaveText(savedTitle ?? '')
  await expect(page.locator('[data-issue-card]')).toHaveCount(5)
})

test('越权读取显示受控 404，不渲染任何卡片', async ({ page }) => {
  await page.locator('[data-preview-scenario="越权 404"]').click()
  await expect(page.locator('[data-issue-card-panel]')).toContainText('资源不存在或不可访问')
  await expect(page.locator('[data-issue-card]')).toHaveCount(0)
})

test('整理失败保留已保存卡片并给出重试入口', async ({ page }) => {
  await page.locator('[data-preview-scenario="整理失败一次"]').click()
  await page.locator('[data-issue-card-analyze]').click()
  await expect(page.locator('[data-issue-card-notice]')).toContainText('生成失败')
  await expect(page.locator('[data-issue-card]')).toHaveCount(5)
  // 再整理一次成功，卡片身份不变。
  await page.locator('[data-issue-card-analyze]').click()
  await expect(page.locator('[data-issue-card]')).toHaveCount(5)
})

test('窄屏下争点卡在主内容下方单列堆叠，不覆盖正文', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const transcript = page.getByRole('region', { name: '已完赛四轮发言（夹具）' })
  const panel = page.locator('[data-issue-card-panel]')
  const transcriptBox = await transcript.boundingBox()
  const panelBox = await panel.boundingBox()
  expect(panelBox!.y).toBeGreaterThanOrEqual(transcriptBox!.y + transcriptBox!.height)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
})

test('四轮赛后：选本人原句、失败保留、原版本重试，复盘与雷达独立恢复', async ({ page }) => {
  const requests: string[] = []
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) requests.push(request.url()) })
  await page.goto('/first-batch-preview.html')
  await expect(page.getByRole('heading', { name: '四轮练习 · 赛后回看' })).toBeVisible()
  await expect(page.getByRole('button', { name: '选句重练' })).toHaveCount(4)
  const first = page.locator('article.user').first()
  await first.getByRole('button', { name: '选句重练' }).click()
  await expect(page.getByRole('status').filter({ hasText: '请先在这条发言中选一段原文' })).toBeVisible()
  await first.locator('.message-text').evaluate(element => {
    const range = document.createRange()
    range.setStartBefore(element.children[8]!)
    range.setEndAfter(element.children[15]!)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)
  })
  await first.getByRole('button', { name: '选句重练' }).click()
  await expect(page.locator('[data-practice-quote]')).not.toBeEmpty()
  await expect(page.locator('[data-practice-version]')).toHaveCount(0)
  await page.getByLabel('演示下一次重练反馈失败').check()
  await page.getByLabel('你的改写').fill('先说明双方采用同一套标准，再比较结果。')
  await page.getByRole('button', { name: '提交改写，查看反馈' }).click()
  await expect(page.getByText('反馈未生成，改写已保留。')).toBeVisible()
  await page.reload()
  await expect(page.getByText('先说明双方采用同一套标准，再比较结果。', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '重试这次反馈' }).click()
  await expect(page.getByText('理由：', { exact: false })).toBeVisible()
  await expect(page.locator('[data-practice-version]')).toHaveCount(1)
  await page.getByLabel('演示下一次复盘失败').check()
  await page.getByRole('button', { name: '生成复盘', exact: true }).click()
  await expect(page.locator('[data-review-failure]')).toBeVisible()
  await page.getByRole('button', { name: /重试生成复盘/ }).click()
  await expect(page.locator('[data-review-radar]')).toBeVisible()
  await page.locator('[data-locate-quote="argument_structure"]').click()
  await expect(first.locator('.is-located').first()).toBeVisible()
  await page.reload()
  await expect(page.locator('[data-review-radar]')).toBeVisible()
  await page.locator('[data-repractice="argument_structure"]').click()
  await expect(page.locator('[data-practice-quote]')).toHaveText('评价标准应当可检验')
  await expect(page.getByRole('textbox', { name: '这次想改进什么（选填）' })).toHaveValue('补一句双方具体要用什么标准。')
  await page.getByLabel('你的改写').fill('明确采用统一的复核清单。')
  await page.getByRole('button', { name: '提交改写，查看反馈' }).click()
  await expect(page.locator('[data-practice-version]')).toHaveCount(2)
  await expect(first.locator('.message-text')).toHaveText('第一轮用户发言：评价标准应当可检验，否则双方无法比较。')
  await page.setViewportSize({ width: 375, height: 812 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(375)
  expect(requests).toEqual([])
})
