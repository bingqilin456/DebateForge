import { expect, test, type Page } from '@playwright/test'
import type { Debate, DebateDetail, StrategyStatus, Turn } from '../src/types/api'

// 仅使用浏览器 Mock；不连接数据库或模型。截图供桌面与窄屏人工复核。
const time = '2026-11-16T00:00:00Z'
const topic = { id: '1', title: '技术发展是否让表达更有力量', description: '先明确讨论范围，再比较证据与现实约束。' }
const debate: Debate = { id: '11', topic, user_stance: 'pro', ai_stance: 'con', ai_style: 'logical', mode: 'standard_4', total_rounds: 4, completed_rounds: 0, status: 'active', created_at: time, updated_at: time }
const strategy: StrategyStatus = { status: 'completed', attempt_version: 1, schema_version: 1, prompt_version: 'strategy-v1', ai_style: 'logical', content: { thesis: '技术扩大了表达的机会，也需要辨别信息质量。', arguments: ['比较不同人群的表达机会。'], risks: ['回应信息过载与传播失真的反例。'] } }
function turn(round: number): Turn {
  return { id: String(round), round_no: round, client_request_id: `550e8400-e29b-41d4-a716-44665544000${round}`, attempt_version: 1, error_code: null, status: 'completed', messages: [
    { id: String(round * 10), role: 'user', content: '技术降低了发声门槛，让更多人有机会参与公共讨论。', created_at: time },
    { id: String(round * 10 + 1), role: 'assistant', content: '表达机会增加是否意味着观点更容易被理解？请进一步比较传播范围与讨论质量。', created_at: time },
  ] }
}
async function capture(page: Page, path: string) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path, fullPage: true })
}

for (const width of [1440, 375]) {
  test(`共享值与基础样式：${width}px 页面和 Unicode 输入边界`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    let authenticated = false
    let detail: DebateDetail = { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
    let strategyState: StrategyStatus = strategy
    await page.route('**/api/v1/**', async route => {
      const path = new URL(route.request().url()).pathname
      if (path.endsWith('/auth/me') && !authenticated) {
        return route.fulfill({ status: 401, contentType: 'application/json', headers: { 'X-Request-ID': 'visual' }, body: JSON.stringify({ error: { code: 'unauthenticated', message: '请登录' }, request_id: 'visual' }) })
      }
      const data = path.endsWith('/auth/me')
        ? { user: { id: '42', username: 'visual_user' }, csrf_token: 'synthetic-visual-csrf' }
        : path.endsWith('/topics') ? { items: [topic] }
          : path.endsWith('/strategy') ? strategyState
            : path === '/api/v1/debates' ? { items: [detail.debate], total: 1, page: 1, page_size: 20 } : detail
      await route.fulfill({ status: 200, contentType: 'application/json', headers: { 'X-Request-ID': 'visual' }, body: JSON.stringify({ data, request_id: 'visual' }) })
    })
    await page.goto('/')
    await expect(page.locator('.auth-card')).toBeVisible()
    await expect(page.locator('.auth-card')).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    await capture(page, testInfo.outputPath('login.png'))
    authenticated = true
    await page.goto('/#topics')
    await page.reload()
    await expect(page.locator('[data-topic-id]')).toHaveCount(1)
    await page.locator('input[value="1"]').check()
    await capture(page, testInfo.outputPath('topics.png'))
    await page.goto('/#history')
    await expect(page.locator('[data-history-id]')).toHaveCount(1)
    await capture(page, testInfo.outputPath('history.png'))
    await page.goto('/#debate=11')
    await expect(page.locator('[data-strategy-completed]')).toBeVisible()
    await expect(page.locator('[data-query-strategy]')).toBeEnabled()
    await capture(page, testInfo.outputPath('strategy-input.png'))
    const input = page.locator('#first-content')
    const send = page.locator('form[data-first-turn] button[type="submit"]')
    await input.fill('😀'.repeat(2000))
    await expect(send).toBeEnabled()
    await expect(page.locator('#content-help')).toContainText('2000 / 2000')
    await input.fill('😀'.repeat(2001))
    await expect(send).toBeDisabled()
    await input.fill('\u0085\uFEFF')
    await expect(send).toBeDisabled()
    await page.evaluate(() => {
      const original = window.fetch.bind(window)
      window.fetch = async (input, init) => {
        if (!String(input).endsWith('/turns/stream')) return original(input, init)
        return new Response(new ReadableStream({ start(controller) {
          controller.enqueue(new TextEncoder().encode('event: start\ndata: {"turn_id":"9","request_id":"visual-stream"}\n\nevent: delta\ndata: {"text":"临时回复：请补充可比较的证据。😀"}\n\n'))
        } }), { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'visual-stream' } })
      }
    })
    await input.fill('技术让更多人有机会表达。')
    await send.click()
    await expect(page.locator('[data-stream-text]')).toContainText('临时回复')
    await capture(page, testInfo.outputPath('stream.png'))
    detail = { ...detail, turns: [{ ...turn(1), status: 'failed', error_code: 'model_timeout', messages: turn(1).messages.slice(0, 1) }] }
    strategyState = { status: 'failed', attempt_version: 1, error_code: 'model_timeout' }
    await page.reload()
    await expect(page.locator('[data-retry-turn]')).toBeVisible()
    await capture(page, testInfo.outputPath('failure.png'))
    detail = { debate: { ...debate, status: 'completed', completed_rounds: 4 }, turns: [1, 2, 3, 4].map(turn), summary_status: { status: 'completed', attempt_version: 1, schema_version: 1, content: {
      strengths: [{ round_no: 1, point: '立论明确', reason: '紧扣表达机会。' }],
      issues: [{ round_no: 2, point: '比较不足', reason: '需要区分覆盖面与质量。' }],
      suggestions: [{ round_no: 4, action: '补充反例分析', example: '比较信息过载对不同人群的影响。' }],
    } } }
    await page.reload()
    await expect(page.locator('[data-summary-group="strengths"]')).toBeVisible()
    await capture(page, testInfo.outputPath('messages-summary.png'))
    await page.goto('/auth-preview.html')
    await expect(page.locator('.auth-card')).toBeVisible()
    await expect(page.locator('.auth-card')).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    await expect(page.locator('.auth-card')).toHaveCSS('border-radius', '16px')
    await capture(page, testInfo.outputPath('auth-preview.png'))
  })
}
