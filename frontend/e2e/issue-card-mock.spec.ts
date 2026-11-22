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
  // 已核验区间在预览里只有一个精确片段外框：片段文本＝引用原文，不包整条发言。
  const userQuote = (await markers.first().textContent()) ?? ''
  const aiQuote = (await markers.nth(1).textContent()) ?? ''
  const userQuoteLength = Array.from(userQuote).length
  const aiQuoteLength = Array.from(aiQuote).length
  expect(userQuoteLength).toBeGreaterThan(0)
  expect(aiQuoteLength).toBeGreaterThan(0)

  await markers.first().click()
  // 定位语义仍复用正式工作台的 attribute（滚动/聚焦），可见外框只在精确片段上。
  const userParagraph = page.locator('[data-message-id="101"] [data-message-content]')
  await expect(userParagraph).toHaveAttribute('data-located-quote', userQuote)
  const userRun = page.locator('.message.user').first().locator('[data-located-run]')
  await expect(userRun).toHaveCount(1)
  await expect(userRun).toHaveText(userQuote)
  await expect(userRun).toHaveClass(/located-fragment/)
  await expect(page.locator('.message.assistant').first().locator('[data-located-quote]')).toHaveCount(0)
  // 内部逐字节点仍在（保留码点坐标），但不再逐个描边，也不带片段外框 class。
  await expect(userRun.locator('[data-preview-index]')).toHaveCount(userQuoteLength)
  await expect(userRun.locator('.located-fragment')).toHaveCount(0)
  await expect(page.locator('.message.user').first().locator('.located-fragment')).toHaveCount(1)

  await markers.nth(1).click()
  const aiParagraph = page.locator('[data-message-id="102"] [data-message-content]')
  await expect(aiParagraph).toHaveAttribute('data-located-quote', aiQuote)
  await expect(userParagraph).not.toHaveAttribute('data-located-quote')
  const aiRun = page.locator('.message.assistant').first().locator('[data-located-run]')
  await expect(aiRun).toHaveCount(1)
  await expect(aiRun).toHaveText(aiQuote)
  await expect(page.locator('.message.user').first().locator('[data-located-run]')).toHaveCount(0)
  await expect(page.locator('.message.assistant').first().locator('.located-fragment')).toHaveCount(1)

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

test('预览只在精确片段外层画一个框，整段原文不显示内置描边', async ({ page }) => {
  const marker = page.locator('[data-issue-card]').first().locator('[data-issue-card-citation]').first()
  const quote = (await marker.textContent()) ?? ''
  await marker.click()
  const paragraph = page.locator('[data-message-id="101"] [data-message-content]')
  // 语义保留：共享定位仍把引用文本写到整段原文上，用于滚动/聚焦。
  await expect(paragraph).toHaveAttribute('data-located-quote', quote)
  const run = page.locator('.message.user').first().locator('[data-located-run]')
  await expect(run).toHaveCount(1)
  await expect(run).toHaveText(quote)
  await expect(run).toHaveClass(/located-fragment/)

  const frames = await page.evaluate(() => {
    const paragraph = document.querySelector<HTMLElement>('[data-located-quote]')!
    const run = document.querySelector<HTMLElement>('[data-located-run]')!
    const chars = Array.from(run.querySelectorAll<HTMLElement>('[data-preview-index]'))
    const boxOf = (element: Element) => {
      const style = getComputedStyle(element)
      return {
        outlineStyle: style.outlineStyle,
        borderTopStyle: style.borderTopStyle,
        borderTopWidth: style.borderTopWidth,
        borderRightWidth: style.borderRightWidth,
        backgroundColor: style.backgroundColor,
      }
    }
    const runStyle = getComputedStyle(run) as CSSStyleDeclaration & { boxDecorationBreak?: string }
    return {
      locatedCount: document.querySelectorAll('[data-located-quote]').length,
      fragmentCount: document.querySelectorAll('.located-fragment').length,
      paragraph: boxOf(paragraph),
      run: boxOf(run),
      runDisplay: runStyle.display,
      runDecorationBreak: runStyle.boxDecorationBreak
        || runStyle.getPropertyValue('-webkit-box-decoration-break')
        || runStyle.getPropertyValue('box-decoration-break'),
      charStyles: chars.map(boxOf),
      // 一个 inline 片段跨几行就有几个 client rect：同一行一个框。
      lineRects: Array.from(run.getClientRects()).map(rect => ({ top: Math.round(rect.top), height: Math.round(rect.height), width: Math.round(rect.width) })),
      text: run.textContent ?? '',
    }
  })
  // 整段原文只留语义，预览不给它画可见外框（正式工作台仍由 theme.css 描边）。
  expect(frames.locatedCount).toBe(1)
  expect(frames.paragraph.outlineStyle).toBe('none')
  // 精确片段是唯一的外框，且是连续的 inline 外框；跨行时按行分段。
  expect(frames.fragmentCount).toBe(1)
  expect(frames.runDisplay).toBe('inline')
  expect(frames.run.borderTopStyle).toBe('solid')
  expect(frames.run.borderTopWidth).not.toBe('0px')
  expect(frames.runDecorationBreak).toBe('clone')
  // 逐字节点不描边、不画框、不加背景。
  expect(frames.charStyles.every(style => style.outlineStyle === 'none')).toBe(true)
  expect(frames.charStyles.every(style => style.borderTopStyle === 'none' && style.borderTopWidth === '0px')).toBe(true)
  expect(frames.charStyles.every(style => style.backgroundColor === 'rgba(0, 0, 0, 0)')).toBe(true)
  expect(frames.text).toBe(quote)
  // 片段不新增空格，行数由自然换行决定。
  const tops = new Set(frames.lineRects.map(rect => rect.top))
  expect(tops.size).toBe(frames.lineRects.length)
  expect(frames.lineRects.every(rect => rect.height > 0 && rect.width > 0)).toBe(true)

  // 窄屏富余不足时自然换行：强制收窄容器后片段分成多行，每行一个框，文本不插空格。
  await page.setViewportSize({ width: 360, height: 800 })
  await page.locator('[data-message-id="101"] [data-message-content]').evaluate(element => {
    (element as HTMLElement).style.maxWidth = '7ch'
  })
  const narrow = await page.evaluate(() => {
    const run = document.querySelector<HTMLElement>('[data-located-run]')!
    return {
      rects: Array.from(run.getClientRects()).map(rect => Math.round(rect.top)),
      text: run.textContent ?? '',
      scrollWidth: document.documentElement.scrollWidth,
    }
  })
  expect(narrow.rects.length).toBeGreaterThan(1)
  expect(new Set(narrow.rects).size).toBe(narrow.rects.length)
  expect(narrow.text).toBe(quote)
  expect(narrow.scrollWidth).toBeLessThanOrEqual(360)
})

test('四轮赛后：选本人原句、失败保留、原版本重试，复盘与雷达独立恢复', async ({ page }) => {
  const requests: string[] = []
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/')) requests.push(request.url()) })
  await page.goto('/first-batch-preview.html')
  await expect(page.getByRole('heading', { name: '四轮练习 · 赛后回看' })).toBeVisible()
  await expect(page.getByRole('button', { name: '选句重练' })).toHaveCount(4)
  const first = page.locator('article.user').first()
  await page.locator('[data-issue-card-citation^="101|"]').first().click()
  await expect(page.locator('[data-practice-quote]')).toHaveText('评价标准应当可检验')
  await expect(first.locator('[data-located-run]')).toHaveText('评价标准应当可检验')
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
  await expect(first.locator('[data-located-run]')).toBeVisible()
  await expect(first.locator('[data-located-run]')).toHaveClass(/located-fragment/)
  // 雷达轴端文字：四轴按固定顺序标注轴号与维度名，不放档位文字。
  await expect(page.locator('[data-radar-axis-label]')).toHaveText([
    '轴1 论证结构', '轴2 回应针对性', '轴3 证据使用', '轴4 概念与范围',
  ])
  const radarBox = await page.locator('[data-review-radar]').boundingBox()
  const labelBoxes = await page.locator('[data-radar-axis-label]').evaluateAll(nodes =>
    nodes.map(node => { const rect = node.getBoundingClientRect(); return { x: rect.x, y: rect.y, width: rect.width, height: rect.height } }))
  // 轴端文字都落在 SVG 图形盒内，且各占一块互不重叠的区域。
  for (const box of labelBoxes) {
    expect(box.x).toBeGreaterThanOrEqual(radarBox!.x - 1)
    expect(box.x + box.width).toBeLessThanOrEqual(radarBox!.x + radarBox!.width + 1)
    expect(box.y).toBeGreaterThanOrEqual(radarBox!.y - 1)
    expect(box.y + box.height).toBeLessThanOrEqual(radarBox!.y + radarBox!.height + 1)
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width)
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
