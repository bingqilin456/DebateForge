import { expect, test, type Route } from '@playwright/test'
import type { Debate, StrategyStatus } from '../src/types/api'

// 隔离浏览器响应测试，不连接 Go/MySQL；真实 Fake 链路另见 style-live.spec.ts。
const session = { user: { id: '42', username: 'style_browser_mock' }, csrf_token: 'synthetic-style-csrf' }
const debate: Debate = {
  id: '11', topic: { id: '1', title: '浏览器策略辩题', description: '比较条件与证据。' },
  user_stance: 'pro', ai_stance: 'con', ai_style: 'data', mode: 'standard_4', total_rounds: 4,
  status: 'active', completed_rounds: 0, created_at: '2026-11-15T00:00:00Z', updated_at: '2026-11-15T00:00:00Z',
}
const completed: StrategyStatus = { status: 'completed', attempt_version: 3, schema_version: 1, prompt_version: 'strategy-v1', ai_style: 'data', content: { thesis: '<img src=x>明确核心主张', arguments: ['给出可检验的证据'], risks: ['不要把相关性当成因果'] } }
async function json(route: Route, data: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'X-Request-ID': 'style-browser-mock' }, body: JSON.stringify({ data, request_id: 'style-browser-mock' }) })
}

test('T14 浏览器 Mock：失败先查新版本，409 只查询，二次确认才重试', async ({ page }) => {
  let version = 1, gets = 0
  let finishRetry!: () => void
  const bodies: unknown[] = []
  let state: StrategyStatus = { status: 'failed', attempt_version: version, error_code: 'model_timeout' }
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path.endsWith('/auth/me')) return json(route, session)
    if (!path.endsWith('/strategy')) return json(route, { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
    if (request.method() === 'GET') { gets++; return json(route, state) }
    expect(request.headers()['x-csrf-token']).toBe(session.csrf_token)
    bodies.push(request.postDataJSON())
    if (bodies.length === 1) {
      version = 3
      state = { status: 'failed', attempt_version: version, error_code: 'model_timeout' }
      return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'attempt_version_conflict', message: 'upstream private detail' }, request_id: 'style-browser-mock' }) })
    }
    state = completed
    await new Promise<void>(resolve => { finishRetry = resolve })
    return json(route, completed)
  })
  await page.goto('/#debate=11')
  const card = page.getByRole('region', { name: '赛前策略卡', exact: true })
  await expect(card).toContainText('当前版本 v1')
  expect(bodies).toHaveLength(0)
  state = { status: 'failed', attempt_version: 2, error_code: 'model_timeout' }
  await card.locator('[data-prepare-strategy-retry]').click()
  await expect(card.locator('[data-retry-strategy]')).toHaveText('确认重试策略（v2）')
  expect(bodies).toHaveLength(0)
  await card.locator('[data-retry-strategy]').click()
  await expect(card).toContainText('当前版本 v3')
  await expect(card).toContainText('不会自动重试')
  expect(bodies).toEqual([{ retry: true, expected_attempt_version: 2 }])
  await expect(card.locator('[data-retry-strategy]')).toHaveCount(0)
  await expect(card).not.toContainText('upstream private detail')
  expect(gets).toBe(3)
  await card.locator('[data-prepare-strategy-retry]').click()
  const retry = card.locator('[data-retry-strategy]')
  await expect(retry).toHaveText('确认重试策略（v3）')
  await retry.evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  await expect(card.locator('[data-query-strategy]')).toBeDisabled()
  finishRetry()
  await expect(card.locator('[data-strategy-completed]')).toBeVisible()
  expect(bodies).toEqual([{ retry: true, expected_attempt_version: 2 }, { retry: true, expected_attempt_version: 3 }])
  await expect(card).toContainText('<img src=x>明确核心主张')
  await expect(card.locator('img')).toHaveCount(0)
  await page.reload()
  await expect(card.locator('[data-strategy-completed]')).toBeVisible()
  expect(bodies).toHaveLength(2)
})

test('T14 浏览器 Mock：202 只轮询 GET，策略等待不阻挡首轮入口', async ({ page }) => {
  let posts = 0, polls = 0
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path.endsWith('/auth/me')) return json(route, session)
    if (!path.endsWith('/strategy')) return json(route, { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
    if (request.method() === 'POST') {
      posts++
      expect(request.postDataJSON()).toEqual({})
      return json(route, { status: 'generating', attempt_version: 3 }, 202)
    }
    if (posts === 0) return json(route, { status: 'not_started', attempt_version: null })
    return json(route, ++polls >= 2 ? completed : { status: 'generating', attempt_version: 3 })
  })
  await page.goto('/#debate=11')
  const card = page.getByRole('region', { name: '赛前策略卡', exact: true })
  const generate = card.locator('[data-generate-strategy]')
  await expect(generate).toBeEnabled()
  await generate.evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  await expect(card).toContainText('策略生成中（v3）')
  await page.locator('#first-content').fill('先明确讨论范围，再提供可以检验的证据。')
  await expect(page.getByRole('button', { name: '发送首轮', exact: true })).toBeEnabled()
  await expect(card.locator('[data-strategy-completed]')).toBeVisible()
  expect(posts).toBe(1)
  expect(polls).toBe(2)
})
