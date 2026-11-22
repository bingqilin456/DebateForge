import { expect, test, type Page, type Route } from '@playwright/test'

/**
 * V2-08 局部重练浏览器 Mock 验收：不连接后端、不写数据库、不调用真实模型。
 *
 * 覆盖端到端用户路径：
 * 1. 完赛页无批注也能主动选段进入重练；选段本身不发请求。
 * 2. 提交改写 → 反馈展示“是否改善 / 理由 / 可能的新问题”，不出现评分或整段范文。
 * 3. 生成失败保留改写与失败版本；刷新后仍能从服务端恢复；按原版本显式重试同一版本。
 * 4. 历史版本可恢复到编辑区继续改写；恢复不创建新版本，再次提交才新建版本。
 * 5. 复盘的四维入口复用同一重练入口（带入原句与改进提示）。
 * 6. 原文（正式消息）始终不被改写；页面不出现任何会话/轮次写请求。
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

const firstUserContent = '我方认为开放式考核更能检验真实能力'
const turns = [
  turn(1, firstUserContent, '对方强调公平性，但未说明统一复核的成本来源'),
  turn(2, '统一标准同样可以公开透明', '公开透明的标准仍然需要具体可执行的复核流程'),
  turn(3, '评价标准应当可检验并可复核', '复核一致性依赖事前约定的判定细则'),
  turn(4, '综上，开放式考核的收益大于其成本', '收益与成本的比较还需要量化依据'),
]

const firstQuote = { message_id: '202', quote: '开放式考核', start_cp: 4, end_cp: 9, resolved: true }

// 与 review-mock 同形的合法总结快照：仅供详情校验通过，不参与重练断言。
const completedSummary = {
  status: 'completed',
  attempt_version: 1,
  schema_version: 1,
  content: {
    strengths: [{ round_no: 1, point: '优点', reason: '依据' }],
    issues: [{ round_no: 2, point: '问题', reason: '依据' }],
    suggestions: [{ round_no: 3, action: '行动', example: '示例' }],
  },
}

const completedReview = {
  status: 'completed',
  attempt_version: 1,
  schema_version: 1,
  prompt_version: 'review-v1',
  content: {
    dimensions: [
      { key: 'argument_structure', label: '论证结构', level: 3, level_label: '较稳', basis: '主要论证有理由支撑', improvement: '补一句判断依据', insufficient_reason: null, quote: firstQuote },
      { key: 'responsiveness', label: '回应针对性', level: null, level_label: null, basis: '可观察机会有限', improvement: '', insufficient_reason: '本场只有一轮完整交锋', quote: null },
      { key: 'evidence_use', label: '证据使用', level: 2, level_label: '发展中', basis: '有例子但支持关系有限', improvement: '说明例子覆盖范围', insufficient_reason: null, quote: firstQuote },
      { key: 'concept_scope', label: '概念与范围', level: null, level_label: null, basis: '核心标准未展开边界', improvement: '', insufficient_reason: '本场未出现需要界定边界的情形', quote: null },
    ],
    key_issues: [{ text: '主要争点在于评价标准是否可检验', quote: firstQuote }],
    strengths: [{ text: '把主张落在可检验标准上', quote: firstQuote }],
    improvements: [{ text: '先回应标准质疑再收束主线', quote: firstQuote }],
  },
}

interface VersionState {
  id: string
  status: 'generating' | 'completed' | 'failed'
  attempt_version: number
  source: { message_id: string; start_cp: number; end_cp: number; quote: string }
  goal: string
  rewrite: string
  feedback: { improved: boolean; change: string; reason: string; new_problem: string } | null
  error_code: string | null
  restored_from: string | null
}

async function json(route: Route, data: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: { 'X-Request-ID': 'browser-request' },
    body: JSON.stringify({ data, request_id: 'browser-request' }),
  })
}

interface Harness {
  versions: VersionState[]
  reviewRequests: Array<{ method: string; body: unknown }>
  repracticeWrites: Array<{ method: string; path: string; body: Record<string, unknown> }>
  /** 下一次提交返回失败版本（模拟生成失败）。 */
  failNextSubmit: boolean
  /** 记录所有非 repractice/review 的业务写请求：重练不得改写会话与轮次。 */
  forbiddenWrites: string[]
}

function newVersion(source: VersionState['source'], rewrite: string): VersionState {
  return {
    id: String(700 + Math.floor(Math.random() * 100)),
    status: 'completed',
    attempt_version: 1,
    source,
    goal: '',
    rewrite,
    feedback: { improved: true, change: '改写在原句后补了判断依据。', reason: '理由与原句主张直接衔接。', new_problem: '' },
    error_code: null,
    restored_from: null,
  }
}

async function install(page: Page, harness: Harness) {
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    if (url.pathname === '/api/v1/auth/me') return json(route, session)
    if (url.pathname.endsWith('/strategy')) return json(route, { status: 'not_started', attempt_version: null })
    if (url.pathname.endsWith('/annotations')) return json(route, { items: [] })
    if (url.pathname.endsWith('/review')) {
      if (method === 'POST') harness.reviewRequests.push({ method, body: request.postDataJSON() })
      return json(route, completedReview)
    }
    if (url.pathname.endsWith('/repractice')) {
      if (method === 'GET') return json(route, { available: true, items: harness.versions })
      const body = request.postDataJSON() as Record<string, unknown>
      harness.repracticeWrites.push({ method, path: url.pathname, body })
      if (harness.failNextSubmit) {
        harness.failNextSubmit = false
        const failed: VersionState = {
          id: '801',
          status: 'failed',
          attempt_version: 1,
          source: { message_id: String(body.message_id), start_cp: Number(body.start_cp), end_cp: Number(body.end_cp), quote: String(body.quote) },
          goal: String(body.goal ?? ''),
          rewrite: String(body.rewrite),
          feedback: null,
          error_code: 'model_timeout',
          restored_from: null,
        }
        harness.versions.push(failed)
        return json(route, failed)
      }
      const created = newVersion(
        { message_id: String(body.message_id), start_cp: Number(body.start_cp), end_cp: Number(body.end_cp), quote: String(body.quote) },
        String(body.rewrite),
      )
      created.goal = String(body.goal ?? '')
      created.restored_from = typeof body.restored_from === 'string' ? body.restored_from : null
      harness.versions.push(created)
      return json(route, created)
    }
    const retryMatch = /\/repractice\/(\d+)\/retry$/.exec(url.pathname)
    if (retryMatch) {
      const body = request.postDataJSON() as Record<string, unknown>
      harness.repracticeWrites.push({ method, path: url.pathname, body })
      const target = harness.versions.find(item => item.id === retryMatch[1])!
      target.status = 'completed'
      target.attempt_version += 1
      target.error_code = null
      target.feedback = { improved: true, change: '重试后补上了依据。', reason: '理由清楚了。', new_problem: '注意不要夸大范围。' }
      return json(route, target)
    }
    // 除上面明确匹配的读/写接口，其余业务写请求都记录下来：重练不应触发任何会话写。
    if (method !== 'GET') harness.forbiddenWrites.push(`${method} ${url.pathname}`)
    return json(route, { debate, turns, summary_status: completedSummary })
  })
}

/** 在第一条本人发言里选中一段原文（与真实用户选择同一条路径）。 */
async function selectFirstQuote(page: Page) {
  const paragraph = page.locator('[data-message-id="202"] [data-message-content]')
  await paragraph.evaluate(element => {
    // 原文可能被批注切成多个文本节点：找到第一个包含目标片段且非空的节点。
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
    let node = walker.nextNode() as Text | null
    while (node) {
      const text = node.textContent ?? ''
      const start = text.indexOf('开放式考核')
      if (start >= 0) {
        const range = document.createRange()
        range.setStart(node, start)
        range.setEnd(node, start + '开放式考核'.length)
        const selection = window.getSelection()!
        selection.removeAllRanges()
        selection.addRange(range)
        return
      }
      node = walker.nextNode() as Text | null
    }
    throw new Error('原文里找不到可选片段')
  })
}

test('完赛页主动选段重练：失败保留、刷新恢复、原版本重试与历史恢复', async ({ page }) => {
  const harness: Harness = { versions: [], reviewRequests: [], repracticeWrites: [], failNextSubmit: false, forbiddenWrites: [] }
  await install(page, harness)
  await page.goto('/#debate=11')
  await expect(page.getByRole('heading', { name: '大学课程是否应采用开放式考核' })).toBeVisible()

  // 无批注也能重练：选段前不出现原句，选段后只在本地出现，不产生请求。
  await expect(page.locator('[data-repractice-panel]')).toBeVisible()
  await expect(page.locator('[data-repractice-quote]')).toHaveCount(0)
  const requestsBefore = harness.repracticeWrites.length
  await selectFirstQuote(page)
  await page.locator('[data-select-quote]').click()
  await expect(page.locator('[data-repractice-quote]')).toHaveText('开放式考核')
  expect(harness.repracticeWrites).toHaveLength(requestsBefore)

  // 提交一次会失败的改写：失败版本保留改写，并提示可重试。
  harness.failNextSubmit = true
  await page.locator('[data-repractice-goal] input').fill('补一句判断依据')
  await page.locator('[data-repractice-rewrite] textarea').fill('开放的考核更能检验真实能力，因为标准可以事先公开。')
  await page.locator('[data-repractice-submit]').click()
  await expect(page.locator('[data-repractice-failure]')).toContainText('反馈未生成，改写已保留')
  await expect(page.locator('[data-repractice-version="801"]')).toContainText('反馈未生成')
  await expect(page.locator('[data-repractice-version="801"]')).toContainText('开放')

  // 刷新：历史从服务端恢复（不是前端本地记忆）。
  await page.reload()
  await expect(page.locator('[data-repractice-version="801"]')).toBeVisible()
  await expect(page.locator('[data-repractice-rewrite-text="801"]')).toContainText('因为标准可以事先公开')

  // 按原版本显式重试（带 expected_attempt_version=1），同一版本变为已生成。
  await page.locator('[data-repractice-retry="801"]').click()
  await expect(page.locator('[data-repractice-version="801"]')).toContainText('反馈已生成')
  await expect(page.locator('[data-repractice-feedback="801"]')).toContainText('是否改善')
  await expect(page.locator('[data-repractice-feedback="801"]')).toContainText('理由')
  await expect(page.locator('[data-repractice-feedback="801"]')).toContainText('可能的新问题')
  expect(harness.repracticeWrites.at(-1)).toMatchObject({ method: 'POST', body: { expected_attempt_version: 1 } })
  // 同一版本被原地推进：不新增版本行。
  await expect(page.locator('[data-repractice-version]')).toHaveCount(1)

  // 反馈不评分、不返回整段范文：页面里没有评分文案。
  await expect(page.locator('[data-repractice-panel]')).not.toContainText('分')
  // 正式原文保持原样。
  await expect(page.locator('[data-message-id="202"] [data-message-content]')).toHaveText(firstUserContent)

  // 历史版本恢复到编辑区：只填回输入框，不创建新版本。
  await page.locator('[data-repractice-restore="801"]').click()
  await expect(page.locator('[data-repractice-rewrite] textarea')).toHaveValue('开放的考核更能检验真实能力，因为标准可以事先公开。')
  await expect(page.locator('[data-repractice-restored]')).toBeVisible()
  const writesAfterRestore = harness.repracticeWrites.length

  // 继续改写并提交：新建版本，且带 restored_from。
  await page.locator('[data-repractice-rewrite] textarea').fill('开放的考核更能检验真实能力，因为标准事先公开且可以复核。')
  await page.locator('[data-repractice-submit]').click()
  await expect(page.locator('[data-repractice-version]')).toHaveCount(2)
  expect(harness.repracticeWrites).toHaveLength(writesAfterRestore + 1)
  expect(harness.repracticeWrites.at(-1)!.body.restored_from).toBe('801')
  // 原版本没有被覆盖：两条历史都在，第一条改写仍是原来的文字。
  await expect(page.locator('[data-repractice-rewrite-text="801"]')).toContainText('因为标准可以事先公开。')

  // 全流程只写 repractice 接口，没有任何会话/轮次写请求。
  expect(harness.forbiddenWrites).toEqual([])
})

test('复盘四维入口复用同一重练入口：带入原句与改进提示', async ({ page }) => {
  const harness: Harness = { versions: [], reviewRequests: [], repracticeWrites: [], failNextSubmit: false, forbiddenWrites: [] }
  await install(page, harness)
  await page.goto('/#debate=11')
  await expect(page.locator('[data-repractice="argument_structure"]')).toBeVisible()
  await page.locator('[data-repractice="argument_structure"]').click()
  // 原句与改进提示都带进来，且不触发生成。
  await expect(page.locator('[data-repractice-quote]')).toHaveText('开放式考核')
  await expect(page.locator('[data-repractice-goal] input')).toHaveValue('补一句判断依据')
  expect(harness.repracticeWrites).toHaveLength(0)
  // 正式原文仍可读且未被改写。
  await expect(page.locator('[data-message-id="202"] [data-message-content]')).toHaveText(firstUserContent)
})

test('能力关闭时整块重练区隐藏，不出现任何入口', async ({ page }) => {
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/auth/me') return json(route, session)
    if (url.pathname.endsWith('/strategy')) return json(route, { status: 'not_started', attempt_version: null })
    if (url.pathname.endsWith('/annotations')) return json(route, { items: [] })
    if (url.pathname.endsWith('/review')) return json(route, { status: 'not_started', attempt_version: null })
    if (url.pathname.endsWith('/repractice')) return json(route, { available: false, items: [] })
    return json(route, { debate, turns, summary_status: completedSummary })
  })
  await page.goto('/#debate=11')
  await expect(page.getByRole('heading', { name: '大学课程是否应采用开放式考核' })).toBeVisible()
  await expect(page.locator('[data-repractice-panel]')).toHaveCount(0)
  await expect(page.locator('[data-select-quote]')).toHaveCount(0)
  await expect(page.getByText('局部重练', { exact: false })).toHaveCount(0)
})
