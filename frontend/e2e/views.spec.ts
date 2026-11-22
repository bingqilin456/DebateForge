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
