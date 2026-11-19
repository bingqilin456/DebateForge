import { expect, test, type Page, type Route } from '@playwright/test'

/**
 * V2-13 复盘浏览器 Mock 验收：不连接后端、不写数据库、不调用真实模型。
 *
 * 覆盖：
 * 1. 完赛但复盘失败：失败态独立展示、比赛仍为已完成、可手动重试并成功、刷新后恢复。
 * 2. 四维：1–4 档与观察不足并列；观察不足不按 0 画；雷达缺项留空；原文定位可点击。
 */

const session = { user: { id: '42', username: 'browser_user' }, csrf_token: 'synthetic-csrf' }

const debate = {
  id: '11',
  topic: { id: '1', title: '大学课程是否应采用开放式考核', description: '讨论学习效果与评价公平。' },
  user_stance: 'pro',
  ai_stance: 'con',
  mode: 'standard_4',
  total_rounds: 4,
  ai_style: 'logical',
  status: 'completed',
  completed_rounds: 4,
  created_at: '2026-11-19T00:00:00Z',
  updated_at: '2026-11-19T00:00:00Z',
}

function turn(roundNo: number, userContent: string, assistantContent: string) {
  return {
    id: String(100 + roundNo),
    round_no: roundNo,
    client_request_id: `550e8400-e29b-41d4-a716-44665544000${roundNo}`,
    status: 'completed',
    attempt_version: 1,
    error_code: null,
    messages: [
      { id: String(200 + roundNo * 2), role: 'user', content: userContent, created_at: '2026-11-19T00:00:00Z' },
      { id: String(201 + roundNo * 2), role: 'assistant', content: assistantContent, created_at: '2026-11-19T00:00:00Z' },
    ],
  }
}

const turns = [
  turn(1, '我方认为开放式考核更能检验真实能力', '对方强调公平性，但未说明统一复核的成本来源'),
  turn(2, '统一标准同样可以公开透明', '公开透明的标准仍然需要具体可执行的复核流程'),
  turn(3, '评价标准应当可检验并可复核', '复核一致性依赖事前约定的判定细则'),
  turn(4, '综上，开放式考核的收益大于其成本', '收益与成本的比较还需要量化依据'),
]

const userQuoteText = '我方认为开放式考核更能检验真实能力'
// 坐标是 Unicode 码点半开区间：按码点长度计算，不能用 UTF-16 下标。
const userQuote = { message_id: '202', quote: userQuoteText, start_cp: 0, end_cp: [...userQuoteText].length, resolved: true }

const completedReview = {
  status: 'completed',
  attempt_version: 2,
  schema_version: 1,
  prompt_version: 'review-v1',
  content: {
    dimensions: [
      { key: 'argument_structure', label: '论证结构', level: 3, level_label: '较稳', basis: '主要论证有理由支撑', improvement: '补一句关键前提', insufficient_reason: null, quote: userQuote },
      { key: 'responsiveness', label: '回应针对性', level: null, level_label: null, basis: '本场可观察到的回应机会有限', improvement: '', insufficient_reason: '本场只有一轮完整交锋', quote: null },
      { key: 'evidence_use', label: '证据使用', level: 2, level_label: '发展中', basis: '有例子但支持关系有限', improvement: '说明例子覆盖范围', insufficient_reason: null, quote: userQuote },
      { key: 'concept_scope', label: '概念与范围', level: null, level_label: null, basis: '核心标准未展开边界', improvement: '', insufficient_reason: '本场未出现需要界定边界的情形', quote: null },
    ],
    key_issues: [{ text: '主要争点在于评价标准是否可检验', quote: userQuote }],
    strengths: [{ text: '把主张落在可检验标准上', quote: userQuote }],
    improvements: [{ text: '先回应标准质疑再收束主线', quote: userQuote }],
  },
}

async function json(route: Route, data: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: { 'X-Request-ID': 'browser-request' },
    body: JSON.stringify({ data, request_id: 'browser-request' }),
  })
}

async function installApi(page: Page, reviewHandler: (route: Route, method: string) => Promise<void>) {
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    if (url.pathname === '/api/v1/auth/me') return json(route, session)
    if (url.pathname.endsWith('/strategy') && method === 'GET') return json(route, { status: 'not_started', attempt_version: null })
    if (url.pathname.endsWith('/annotations') && method === 'GET') return json(route, { items: [] })
    if (url.pathname.endsWith('/review')) return reviewHandler(route, method)
    return json(route, { debate, turns, summary_status: { status: 'completed', attempt_version: 1, schema_version: 1, content: { strengths: [{ round_no: 1, point: '优点', reason: '依据' }], issues: [{ round_no: 2, point: '问题', reason: '依据' }], suggestions: [{ round_no: 3, action: '行动', example: '示例' }] } } })
  })
}

const failedReview = { status: 'failed', attempt_version: 1, error_code: 'model_timeout' }

test('完赛但复盘失败：失败独立展示、手动重试成功、刷新恢复且原文可读', async ({ page }) => {
  let reviewRequests = 0
  let reviewStatus: unknown = failedReview
  await installApi(page, async (route, method) => {
    if (method === 'GET') return json(route, reviewStatus)
    reviewRequests++
    expect(route.request().headers()['x-csrf-token']).toBe(session.csrf_token)
    // 手动重试必须带当前尝试版本，不允许匿名为首次生成。
    expect(route.request().postDataJSON()).toEqual({ retry: true, expected_attempt_version: 1 })
    reviewStatus = completedReview
    return json(route, completedReview)
  })

  await page.goto('/#debate=11')
  await expect(page.getByRole('heading', { name: '大学课程是否应采用开放式考核' })).toBeVisible()

  // 失败态独立：比赛仍为已完成，原文仍可读。
  await expect(page.locator('[data-review-state-text]')).toHaveText('复盘生成失败')
  await expect(page.locator('[data-review-error-code]')).toContainText('model_timeout')
  await expect(page.locator('[data-review-failure]')).toContainText('比赛仍为已完成')
  await expect(page.getByText('已完成', { exact: true })).toBeVisible()
  await expect(page.getByText('我方认为开放式考核更能检验真实能力').first()).toBeVisible()

  // 手动重试成功后展示四维与争点。
  await page.locator('[data-retry-review]').click()
  await expect(page.locator('[data-review-completed]')).toBeVisible()
  await expect(page.locator('[data-dimension]')).toHaveCount(4)
  await expect(page.locator('[data-review-group="key_issues"]')).toContainText('主要争点在于评价标准是否可检验')
  expect(reviewRequests).toBe(1)

  // 刷新后从服务端恢复成功状态（不是本地猜测）。
  await page.reload()
  await expect(page.locator('[data-review-state-text]')).toHaveText('复盘已生成')
  await expect(page.locator('[data-dimension]')).toHaveCount(4)
})

test('四维观察不足不按 0 画，雷达缺项留空，原文定位可点击', async ({ page }) => {
  await installApi(page, async (route, method) => {
    if (method === 'GET') return json(route, completedReview)
    return json(route, completedReview)
  })

  await page.goto('/#debate=11')
  await expect(page.locator('[data-review-completed]')).toBeVisible()

  // 观察不足：文字说明原因、无档级徽标；雷达只画两个已观察点，两个缺口。
  await expect(page.locator('[data-dimension="responsiveness"] [data-level="insufficient"]')).toHaveText('观察不足')
  await expect(page.locator('[data-dimension="responsiveness"]')).toContainText('为什么观察不足')
  await expect(page.locator('[data-dimension="responsiveness"] [data-level="0"]')).toHaveCount(0)
  await expect(page.locator('[data-review-radar] .radar-dot')).toHaveCount(2)
  await expect(page.locator('[data-review-radar] .radar-gap')).toHaveCount(2)
  await expect(page.locator('[data-review-gap-note]')).toContainText('不是 0 分')

  // 图例按钮键盘可达；点击后把原文片段定位到正式消息。
  const legendLocate = page.locator('[data-legend-locate="argument_structure"]')
  await legendLocate.focus()
  await expect(legendLocate).toBeFocused()
  await legendLocate.click()
  await expect(page.locator('[data-message-id="202"] [data-message-content]')).toHaveAttribute('data-located-quote', '我方认为开放式考核更能检验真实能力')

  // 观察不足轴没有定位入口；PDF 与重练明确标为待整合而非死链。
  await expect(page.locator('[data-legend-locate="responsiveness"]')).toHaveCount(0)
  await expect(page.locator('[data-pdf-pending]')).toBeDisabled()
  await expect(page.locator('[data-pending-entry="repractice-argument_structure"]')).toContainText('待整合')
})

test('未开始复盘：只显示手动入口，不自动生成', async ({ page }) => {
  let posts = 0
  await installApi(page, async (route, method) => {
    if (method === 'POST') { posts++; return json(route, completedReview) }
    return json(route, { status: 'not_started', attempt_version: null })
  })

  await page.goto('/#debate=11')
  await expect(page.locator('[data-review-state-text]')).toHaveText('复盘尚未生成')
  await expect(page.locator('[data-generate-review]')).toBeVisible()
  // 等待一轮网络空闲，确认没有自动 POST。
  await page.waitForTimeout(500)
  expect(posts).toBe(0)
  await expect(page.locator('[data-review-completed]')).toHaveCount(0)
})
