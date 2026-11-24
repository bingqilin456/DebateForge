import { expect, test, type Page, type Route } from '@playwright/test'
import type { Debate, Topic } from '../src/types/api'

const topics: Topic[] = Array.from({ length: 5 }, (_, i) => ({ id: String(i + 1), title: `浏览器辩题 ${i + 1}`, description: '讨论这项主张的利弊。' }))
const session = { user: { id: '42', username: 'browser_user' }, csrf_token: 'synthetic-csrf' }
function debate(id = '11', legacy = false): Debate {
  return {
    id, topic: topics[0]!, user_stance: 'pro', ai_stance: 'con',
    mode: legacy ? 'quick_3' : 'standard_4', total_rounds: legacy ? 3 : 4, ai_style: 'logical',
    status: 'completed', completed_rounds: legacy ? 3 : 4,
    created_at: '2026-11-14T00:00:00Z', updated_at: '2026-11-14T00:00:00Z',
  }
}
async function json(route: Route, data: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'X-Request-ID': 'browser-request' }, body: JSON.stringify({ data, request_id: 'browser-request' }) })
}
async function failure(route: Route, status = 503, code = 'dependency_unavailable') {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'X-Request-ID': 'browser-request' }, body: JSON.stringify({ error: { code, message: 'private upstream detail' }, request_id: 'browser-request' }) })
}
async function api(page: Page, handler: (route: Route, url: URL) => Promise<void>) {
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/auth/me') return json(route, session)
    if (url.pathname.endsWith('/strategy') && route.request().method() === 'GET') return json(route, { status: 'not_started', attempt_version: null })
    await handler(route, url)
  })
}

for (const stance of ['pro', 'con'] as const) {
  test(`T08 五题、${stance} 立场创建、详情交接`, async ({ page }) => {
    let creates = 0
    const result = { ...debate(), status: 'active', completed_rounds: 0, user_stance: stance, ai_stance: stance === 'pro' ? 'con' : 'pro' }
    await api(page, async (route, url) => {
      if (url.pathname.endsWith('/topics')) return json(route, { items: topics })
      if (route.request().method() === 'POST') {
        creates++
        expect(route.request().postDataJSON()).toEqual({ topic_id: '2', user_stance: stance, ai_style: 'logical' })
        expect(route.request().headers()['x-csrf-token']).toBe(session.csrf_token)
        return json(route, result, 201)
      }
      return json(route, { debate: result, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
    })
    await page.goto('/#topics')
    await expect(page.locator('[data-topic-id]')).toHaveCount(5)
    await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
    await page.locator('input[value="2"]').check()
    await page.locator(`input[value="${stance}"]`).check()
    await page.getByRole('button', { name: '创建会话', exact: true }).click()
    await expect(page).toHaveURL(/#debate=11$/)
    await expect(page.getByText('已完成 0 / 4 轮')).toBeVisible()
    expect(creates).toBe(1)
  })
}

test('T11 两页、三/四轮状态、上一页、刷新和打开详情', async ({ page }) => {
  const pages: number[] = []
  const items = Array.from({ length: 21 }, (_, i) => debate(String(i + 1), i === 1))
  await api(page, async (route, url) => {
    if (url.pathname === '/api/v1/debates') {
      const pageNo = Number(url.searchParams.get('page'))
      expect(url.searchParams.get('page_size')).toBe('20')
      pages.push(pageNo)
      return json(route, { items: items.slice((pageNo - 1) * 20, pageNo * 20), total: 21, page: pageNo, page_size: 20 })
    }
    // 使用进行中的历史会话验证交接，避免把已有总结请求混入分页测试。
    return json(route, { debate: { ...debate('21'), status: 'active', completed_rounds: 0 }, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
  })
  await page.goto('/#history')
  await expect(page.locator('[data-history-id]')).toHaveCount(20)
  await expect(page.locator('[data-history-id="1"]')).toContainText('4 / 4 轮')
  await expect(page.locator('[data-history-id="2"]')).toContainText('3 / 3 轮')
  await expect(page.getByRole('button', { name: '上一页', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.locator('[data-history-id]')).toHaveCount(1)
  await expect(page.locator('[data-history-id="21"]')).toBeVisible()
  await expect(page.getByRole('button', { name: '下一页', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '刷新历史', exact: true }).click()
  await expect.poll(() => pages.length).toBe(3)
  await page.getByRole('button', { name: '上一页', exact: true }).click()
  await expect(page.locator('[data-history-id]')).toHaveCount(20)
  await page.reload()
  await expect(page.locator('[data-history-id]')).toHaveCount(20)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await page.locator('[data-debate-id="21"]').click()
  await expect(page).toHaveURL(/#debate=21$/)
  await expect(page.getByText('已完成 0 / 4 轮')).toBeVisible()
  expect(pages).toEqual([1, 2, 2, 1, 1, 2])
})

test('空辩题与空历史、移动端无横向溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await api(page, async (route, url) => json(route, url.pathname.endsWith('/topics') ? { items: [] } : { items: [], total: 0, page: 1, page_size: 20 }))
  await page.goto('/#topics')
  await expect(page.getByText('暂无可用辩题，请稍后刷新。')).toBeVisible()
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '菜单', exact: true }).click()
  await page.locator('[data-view-tab="history"]').click()
  await expect(page.getByText('暂无历史会话，前往辩题列表开始一次练习吧。')).toBeVisible()
  await expect(page.getByRole('button', { name: '上一页', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '下一页', exact: true })).toBeDisabled()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

for (const kind of ['503', 'network', 'malformed'] as const) {
  test(`辩题与历史 ${kind} 异常不冒充空列表、手动恢复`, async ({ page }) => {
    const calls = { topics: 0, history: 0 }
    await api(page, async (route, url) => {
      const view = url.pathname.endsWith('/topics') ? 'topics' : 'history'
      if (++calls[view] === 1) {
        if (kind === '503') return failure(route)
        if (kind === 'network') return route.abort('failed')
        return json(route, { unexpected: true })
      }
      return json(route, view === 'topics' ? { items: topics } : { items: [debate()], total: 1, page: 1, page_size: 20 })
    })
    await page.goto('/#topics')
    await expect(page.locator('[data-view="topics"] [role="alert"]')).toBeVisible()
    await expect(page.getByText('暂无可用辩题，请稍后刷新。')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText('private upstream detail')
    expect(calls.topics).toBe(1)
    await page.getByRole('button', { name: '重试加载辩题', exact: true }).click()
    await expect(page.locator('[data-topic-id]')).toHaveCount(5)
    await page.locator('[data-view-tab="history"]').click()
    await expect(page.locator('[data-view="history"] [role="alert"]')).toBeVisible()
    await expect(page.getByText('暂无历史会话', { exact: false })).toHaveCount(0)
    expect(calls.history).toBe(1)
    await page.getByRole('button', { name: '重试加载历史', exact: true }).click()
    await expect(page.locator('[data-history-id]')).toHaveCount(1)
    expect(calls).toEqual({ topics: 2, history: 2 })
  })
}

test('V2-03 自定义辩题：私有提示、码点计数、超限保留输入、只提交 custom_topic', async ({ page }) => {
  const title = '是否应把远程办公设为默认？'
  const background = '讨论协作成本与人才半径'
  const custom: Debate = {
    ...debate('77'), topic: { id: null, title, description: background }, status: 'active', completed_rounds: 0,
  }
  const posts: unknown[] = []
  await api(page, async (route, url) => {
    if (url.pathname.endsWith('/topics')) return json(route, { items: topics })
    if (url.pathname === '/api/v1/debates' && route.request().method() === 'POST') {
      posts.push(route.request().postDataJSON())
      return json(route, custom, 201)
    }
    return json(route, { debate: custom, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
  })
  await page.goto('/#topics')
  await expect(page.locator('[data-topic-id]')).toHaveCount(5)
  await page.locator('input[data-source="custom"]').check()

  // 私有性与两个独立计数可见。
  await expect(page.getByText('自定义辩题仅你可在本站查看', { exact: false })).toBeVisible()
  await expect(page.locator('[data-custom-title-count]')).toContainText('0 / 200')
  await expect(page.locator('[data-custom-background-count]')).toContainText('0 / 1000')

  // 超限时保留输入并阻止提交，不静默截断。
  const over = '题'.repeat(201)
  await page.locator('[data-custom-title]').fill(over)
  await expect(page.locator('[data-custom-title]')).toHaveValue(over)
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
  await expect(page.locator('[data-custom-title-count]')).toContainText('201 / 200')

  // 补充平面字符按一个码点计数。
  await page.locator('[data-custom-title]').fill('𠮷𠮷𠮷')
  await expect(page.locator('[data-custom-title-count]')).toContainText('3 / 200')

  await page.locator('[data-custom-title]').fill(title)
  await page.locator('[data-custom-background]').fill(background)
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect.poll(() => posts.length).toBe(1)
  expect(posts[0]).toEqual({ custom_topic: { title, background }, user_stance: 'pro', ai_style: 'logical' })
  await expect(page).toHaveURL(/#debate=77$/)
})

test('V2-03 题库为空时仍可创建自定义辩题，预览按纯文本展示', async ({ page }) => {
  const title = '<img src=x onerror=unsafe>'
  await api(page, async (route, url) => {
    if (url.pathname.endsWith('/topics')) return json(route, { items: [] })
    if (url.pathname === '/api/v1/debates' && route.request().method() === 'POST') {
      return json(route, { ...debate('78'), topic: { id: null, title, description: '' }, status: 'active', completed_rounds: 0 }, 201)
    }
    return json(route, { status: 'not_started', attempt_version: null })
  })
  await page.goto('/#topics')
  await expect(page.getByText('暂无可用辩题，请稍后刷新。')).toBeVisible()
  await page.locator('input[data-source="custom"]').check()
  await page.locator('[data-custom-title]').fill(title)
  await expect(page.locator('[data-topic-preview]')).toContainText(title)
  await expect(page.locator('[data-topic-preview] img')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeEnabled()
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('创建响应丢失锁定提交并引导历史核实', async ({ page }) => {
  let creates = 0
  await api(page, async (route, url) => {
    if (url.pathname.endsWith('/topics')) return json(route, { items: topics })
    if (route.request().method() === 'POST') { creates++; return route.abort('failed') }
    return json(route, { items: [debate()], total: 1, page: 1, page_size: 20 })
  })
  await page.goto('/#topics')
  await page.locator('input[value="1"]').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect(page.getByText('创建结果尚未确认。', { exact: false })).toBeVisible()
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
  await page.locator('[data-nav-history]').click()
  await expect(page.locator('[data-history-id]')).toHaveCount(1)
  expect(creates).toBe(1)
})

test('历史刷新遇到 401 清除记录并回到登录', async ({ page }) => {
  let calls = 0
  await api(page, async route => ++calls === 1
    ? json(route, { items: [debate()], total: 1, page: 1, page_size: 20 })
    : failure(route, 401, 'unauthenticated'))
  await page.goto('/#history')
  await expect(page.locator('[data-history-id]')).toHaveCount(1)
  await page.getByRole('button', { name: '刷新历史', exact: true }).click()
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  await expect(page.locator('[data-history-id]')).toHaveCount(0)
})

test('统一入口：双击只创建一次，锁定导航，刷新重登和历史均读取保存详情', async ({ page }) => {
  let creates = 0, details = 0, loggedIn = true
  let release!: () => void
  const saved = { ...debate(), status: 'active', completed_rounds: 0 }
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/auth/me')) return loggedIn ? json(route, session) : failure(route, 401, 'unauthenticated')
    if (path.endsWith('/auth/logout')) { loggedIn = false; return route.fulfill({ status: 204 }) }
    if (path.endsWith('/auth/login')) { loggedIn = true; return json(route, session) }
    if (path.endsWith('/topics')) return json(route, { items: topics })
    if (path.endsWith('/strategy')) return json(route, { status: 'not_started', attempt_version: null })
    if (path === '/api/v1/debates' && route.request().method() === 'POST') {
      creates++
      await new Promise<void>(resolve => { release = resolve })
      return json(route, saved, 201)
    }
    if (path.endsWith('/debates/11')) { details++; return json(route, { debate: saved, turns: [], summary_status: { status: 'not_started', attempt_version: null } }) }
    return json(route, { items: [saved], total: 1, page: 1, page_size: 20 })
  })
  await page.goto('/')
  await expect(page.locator('[data-view="topics"]')).toBeVisible()
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
  await page.locator('input[value="1"]').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).evaluate((button: HTMLButtonElement) => { button.click(); button.click() })
  await expect.poll(() => creates).toBe(1)
  await expect(page.locator('[data-view-tab="history"]')).toBeDisabled()
  await expect(page.getByLabel('AI 陪练流派', { exact: true })).toBeDisabled()
  release()
  await expect(page.locator('#first-content')).toBeVisible()
  expect(details).toBe(1)
  await page.reload()
  await expect(page.locator('#first-content')).toBeVisible()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.getByLabel('用户名', { exact: true }).fill('browser_user')
  await page.getByLabel('密码', { exact: true }).fill('synthetic-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('#first-content')).toBeVisible()
  await page.locator('[data-view-tab="history"]').click()
  await page.locator('[data-debate-id="11"]').click()
  await expect(page.locator('#first-content')).toBeVisible()
  await page.getByRole('button', { name: '开始新的练习', exact: true }).click()
  await expect(page.locator('[data-view="topics"]')).toBeVisible()
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
  expect(creates).toBe(1)
  expect(details).toBe(4)
})

for (const kind of ['400', '401', '503', 'malformed', 'timeout'] as const) {
  test(`统一创建 ${kind}：明确输入失败可修正，未知结果先查历史，认证失败回登录`, async ({ page }) => {
    let creates = 0
    await api(page, async (route, url) => {
      if (url.pathname.endsWith('/topics')) return json(route, { items: topics })
      if (route.request().method() === 'POST') {
        creates++
        if (kind === 'timeout') { await new Promise(resolve => setTimeout(resolve, 5500)); return route.abort('timedout') }
        if (kind === 'malformed') return json(route, { unexpected: true }, 201)
        return failure(route, Number(kind), kind === '400' ? 'invalid_request' : kind === '401' ? 'unauthenticated' : 'dependency_unavailable')
      }
      return json(route, { items: [], total: 0, page: 1, page_size: 20 })
    })
    await page.goto('/')
    await page.locator('input[value="1"]').check()
    await page.getByRole('button', { name: '创建会话', exact: true }).click()
    if (kind === '401') {
      await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
    } else if (kind === '400') {
      await expect(page.locator('[data-view="topics"] [role="alert"]')).toBeVisible()
      await page.locator('input[value="2"]').check()
      await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeEnabled()
    } else {
      await expect(page.getByText('创建结果尚未确认。', { exact: false })).toBeVisible()
      await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
      await page.locator('[data-nav-history]').click()
      await page.locator('[data-view-tab="topics"]').click()
      await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeDisabled()
    }
    expect(creates).toBe(1)
  })
}

test('V2-02 从历史恢复训练赛并按服务端状态进入结辩', async ({ page }) => {
  const match = { ...debate(), mode: 'training_v2', total_rounds: 10, completed_rounds: 1, status: 'active' }
  const queryUrl = '/api/v1/debates/11'
  const steps = Array.from({ length: 6 }, (_, i) => ({
    id: String(i + 1), step_no: i + 1, stage: ['opening', 'rebuttal', 'free_debate'][Math.floor(i / 2)],
    stage_step: i % 2 + 1, free_debate_round: i >= 4 ? 1 : 0,
    speaker: i % 2 === 0 ? 'user' : 'assistant',
    client_request_id: `550e8400-e29b-41d4-a716-${String(i + 1).padStart(12, '0')}`,
    content: `已保存发言 ${i + 1}`, created_at: match.created_at,
  }))
  let closing = false
  let endRequests = 0
  const progress = () => ({
    stage: closing ? 'closing' : 'free_debate', stage_step: 0, free_debate_rounds: 1,
    free_debate_max_rounds: 10, committed_steps: 6, total_steps: closing ? 8 : 0,
    next_step_no: 7, next_speaker: closing ? 'assistant' : 'user', can_submit_step: true,
    can_retry_ai: false, can_end_free_debate: !closing, end_blocked_reason: closing ? 'not_free_debate' : null,
    pending_ai: null, abandoned_ai: null,
  })
  await api(page, async (route, url) => {
    if (url.pathname.endsWith('/topics')) return json(route, { items: topics })
    if (url.pathname === '/api/v1/debates') return json(route, { items: [match], total: 1, page: 1, page_size: 20 })
    if (url.pathname.endsWith('/free-debate/end')) {
      closing = true
      endRequests++
      return json(route, { step: null, progress: progress(), debate_status: 'active', query_url: queryUrl })
    }
    if (url.pathname.endsWith('/annotations')) return json(route, { items: [] })
    if (url.pathname.endsWith('/issue-cards')) return json(route, { state: 'not_ready', display_cap: 5, analysis: { status: 'not_started', attempt_version: 0 }, items: [] })
    expect(url.pathname).toBe(queryUrl)
    return json(route, { debate: match, turns: [], training: { progress: progress(), steps, query_url: queryUrl } })
  })
  await page.goto('/#history')
  await page.getByRole('button', { name: '查看会话：浏览器辩题 1' }).click()
  await expect(page).toHaveURL(/#training=11$/)
  await expect(page.getByText('已保存发言 6')).toBeVisible()
  await page.getByRole('button', { name: '进入结辩', exact: true }).click()
  await expect(page.getByRole('button', { name: '进入结辩', exact: true })).toHaveCount(0)
  await page.reload()
  await expect(page.getByText('已保存发言 6')).toBeVisible()
  await expect(page.getByRole('button', { name: '进入结辩', exact: true })).toHaveCount(0)
  expect(endRequests).toBe(1)
})

test('训练赛退出续练、争点回应定位、复盘失败与重练刷新恢复', async ({ page }) => {
  const path = '/api/v1/debates/11'
  const match = { ...debate(), mode: 'training_v2', total_rounds: 10, completed_rounds: 1 }
  const steps = Array.from({ length: 8 }, (_, i) => ({
    id: String(201 + i), step_no: i + 1, stage: ['opening', 'rebuttal', 'free_debate', 'closing'][Math.floor(i / 2)],
    stage_step: i % 2 + 1, free_debate_round: i === 4 || i === 5 ? 1 : 0,
    speaker: i % 2 === 0 || i === 7 ? (i === 6 ? 'assistant' : 'user') : 'assistant',
    client_request_id: `550e8400-e29b-41d4-a716-${String(i + 1).padStart(12, '0')}`,
    content: `依据与回应 ${i + 1} 🌱`, created_at: match.created_at,
  }))
  const anchor = (index: number) => ({
    message_id: steps[index]!.id, quote: steps[index]!.content, start_cp: 0, end_cp: [...steps[index]!.content].length, resolved: true,
  })
  const reviewContent = {
    dimensions: [
      { key: 'argument_structure', label: '论证结构', level: 3, level_label: '较稳', basis: '有理由支撑', improvement: '补前提', insufficient_reason: null, quote: anchor(0) },
      ...[['responsiveness', '回应针对性'], ['evidence_use', '证据使用'], ['concept_scope', '概念与范围']].map(([key, label]) => ({
        key, label, level: null, level_label: null, basis: '观察机会有限', improvement: '', insufficient_reason: '本场材料不足', quote: null,
      })),
    ],
    key_issues: [{ text: '双方如何评价论据？', quote: anchor(2) }],
    strengths: [{ text: '给出了依据', quote: anchor(0) }],
    improvements: [{ text: '补前提', quote: anchor(0) }],
  }
  let completed = false
  let reviewState: 'not_started' | 'failed' | 'completed' = 'not_started'
  let rewrites: unknown[] = []
  let reviewWrites = 0
  const review = () => reviewState === 'completed'
    ? { status: 'completed', attempt_version: 2, schema_version: 1, content: reviewContent }
    : reviewState === 'failed'
      ? { status: 'failed', attempt_version: 1, error_code: 'model_error' }
      : { status: 'not_started', attempt_version: null }
  await api(page, async (route, url) => {
    const method = route.request().method()
    if (url.pathname === '/api/v1/debates') return json(route, { items: [{ ...match, status: completed ? 'completed' : 'active' }], total: 1, page: 1, page_size: 20 })
    if (url.pathname === path) return json(route, {
      debate: { ...match, status: completed ? 'completed' : 'active' }, turns: [],
      summary_status: { status: 'not_started', attempt_version: null },
      training: { query_url: path, steps: completed ? steps : steps.slice(0, 6), progress: {
        stage: completed ? 'closing' : 'free_debate', stage_step: completed ? 2 : 0,
        free_debate_rounds: 1, free_debate_max_rounds: 10, committed_steps: completed ? 8 : 6,
        total_steps: completed ? 8 : 0, next_step_no: completed ? null : 7,
        next_speaker: completed ? null : 'user', can_submit_step: !completed, can_retry_ai: false,
        can_end_free_debate: !completed, end_blocked_reason: completed ? 'not_free_debate' : null,
        pending_ai: null, abandoned_ai: null,
      } },
    })
    if (url.pathname === path + '/annotations') return json(route, { items: [{
      turn_id: steps[5]!.id, round_no: 6, status: 'completed', attempt_version: 1,
      items: [{ category: 'strength', label: '论证有效', reason: '给出了依据', improvement: '可补充边界', anchor: anchor(4), turn_no: 5, role: 'user' }],
    }] })
    if (url.pathname === path + '/issue-cards') return json(route, {
      state: 'ready', display_cap: 5, analysis: { status: 'completed', attempt_version: completed ? 3 : 2 }, items: [{
        id: '1', title: '评价标准', title_source: 'ai', explanation: '核对双方论证',
        origin: { anchor: anchor(2), speaker: 'user', turn_no: 3, note: '提出位置' },
        responses: [{ anchor: anchor(3), speaker: 'assistant', turn_no: 4, note: '相关回应' }],
        pending: [], notices: [], version: 1,
      }],
    })
    if (url.pathname === path + '/review') {
      if (method === 'GET') return json(route, review())
      reviewWrites++
      if (reviewWrites === 1) {
        reviewState = 'failed'
        return failure(route, 502, 'model_error')
      }
      expect(route.request().postDataJSON()).toEqual({ retry: true, expected_attempt_version: 1 })
      reviewState = 'completed'
      return json(route, review())
    }
    if (url.pathname === path + '/repractice') {
      if (method === 'GET') return json(route, rewrites)
      expect(route.request().postDataJSON()).toMatchObject({ message_id: steps[0]!.id, quote: steps[0]!.content, rewrite: '改写后的依据' })
      const result = { id: '1', attempt_version: 1, status: 'completed', rewrite: '改写后的依据', feedback: {
        change: '已保存', reason: '补充了前提', new_problem: '核对适用范围',
      } }
      rewrites = [result]
      return json(route, result)
    }
    return failure(route, 404, 'not_found')
  })

  await page.goto('/#training=11')
  await expect(page.locator('[data-annotation-round="6"]')).toBeVisible()
  await expect(page.locator('[data-review-panel]')).toHaveCount(0)
  await page.getByRole('button', { name: '保存并退出' }).click()
  await page.getByRole('button', { name: '查看会话：浏览器辩题 1' }).click()
  await expect(page.locator('[data-step-no="6"]')).toBeVisible()
  await expect(page.getByText('评价标准', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /相关回应.*依据与回应 4/ }).click()
  await expect(page.locator('[data-step-no="4"] [data-located-quote]')).toHaveAttribute('data-located-quote', steps[3]!.content)

  completed = true
  await page.reload()
  await expect(page.locator('[data-step-no="8"]')).toBeVisible()
  await expect(page.locator('[data-review-panel]')).toBeVisible()
  await page.getByRole('button', { name: /生成复盘/ }).click()
  await expect(page.getByText('复盘生成失败', { exact: true })).toBeVisible()
  await page.reload()
  await expect(page.locator('[data-step-no="8"]')).toBeVisible()
  await expect(page.getByText('评价标准', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /重试生成复盘/ }).click()
  await expect(page.locator('[data-review-gap-note]')).toBeVisible()
  await page.locator('[data-legend-locate="argument_structure"]').click()
  await expect(page.locator('[data-step-no="1"] [data-located-quote]')).toHaveAttribute('data-located-quote', steps[0]!.content)
  await page.locator('[data-repractice="argument_structure"]').click()
  await page.getByLabel('改写原句').fill('改写后的依据')
  await page.getByRole('button', { name: '提交改写' }).click()
  await expect(page.locator('[data-training-repractice-item]')).toContainText('改写后的依据')
  await page.reload()
  await expect(page.locator('[data-training-repractice-item]')).toContainText('改写后的依据')
  await expect(page.locator('[data-review-gap-note]')).toBeVisible()
  expect(reviewWrites).toBe(2)
})
