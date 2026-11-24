import { expect, test } from '@playwright/test'
import type { DebateDetail, Turn } from '../src/types/api'

test('竞技场浏览器 Mock：锦囊、零调用框架、发送进度与完轮图谱', async ({ page }) => {
  const date = '2026-11-24T00:00:00Z'
  const detail: DebateDetail = {
    debate: {
      id: '123', topic: { id: '1', title: '辩题', description: '比较成本' }, user_stance: 'pro', ai_stance: 'con',
      ai_style: 'logical', mode: 'standard_4', total_rounds: 4, status: 'active', completed_rounds: 0,
      created_at: date, updated_at: date, ...{ practice: true },
    },
    turns: [],
    summary_status: { status: 'not_started', attempt_version: null },
  }
  const posts: string[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    function reply(data: unknown) { return route.fulfill({ json: { data, request_id: 'mock-arena' }, headers: { 'x-request-id': 'mock-arena' } }) }
    if (path.endsWith('/auth/me')) return reply({ user: { id: '42', username: 'arena_mock' }, csrf_token: 'mock-arena-csrf' })
    if (path.endsWith('/debates/123')) return reply(detail)
    if (path.endsWith('/annotations')) return reply({ items: [] })
    if (path.endsWith('/strategy') || path.endsWith('/review')) return reply({ status: 'not_started', attempt_version: null })
    if (path.endsWith('/arena') && request.method() === 'GET') {
      const rounds = detail.turns.length ? [{
        round_no: 1, pro_percent: 63, source: 'local',
        user_message_id: '10', assistant_message_id: '11', user_point: '共同标准', assistant_point: '补充成本',
      }] : []
      return reply({ rounds, hints: [{ round_no: 1, text: '先找共同标准', source: 'local' }] })
    }
    if (path.endsWith('/arena') && request.method() === 'POST') {
      posts.push(request.postDataJSON().action)
      return reply({ round_no: 1, text: '先解释衡量方法', source: 'local' })
    }
    if (path.endsWith('/turns/stream')) {
      posts.push('turn')
      const { client_request_id, content } = request.postDataJSON()
      const turn: Turn = {
        id: '9', round_no: 1, client_request_id, status: 'completed', attempt_version: 1, error_code: null,
        messages: [{ id: '10', role: 'user', content, created_at: date }, { id: '11', role: 'assistant', content: '补充成本。', created_at: date }],
      }
      detail.turns.push(turn); detail.debate.completed_rounds = 1
      const frame = (event: string, data: unknown) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
      return route.fulfill({
        contentType: 'text/event-stream',
        headers: { 'x-request-id': 'mock-arena' },
        body: frame('start', { turn_id: '9', request_id: 'mock-arena' }) + frame('delta', { text: '补充成本。' })
          + frame('done', { turn, completed_rounds: 1, status: 'active' }),
      })
    }
    return route.abort()
  })

  await page.goto('/#debate=123')
  const hint = page.getByText('先找共同标准')
  await expect(hint).toBeAttached()
  await expect(hint).toBeHidden()
  await page.getByText('第 1 轮攻防锦囊').click()
  await expect(hint).toBeVisible()
  await page.getByRole('button', { name: 'AI 重新生成' }).click()
  await expect(page.getByText('先解释衡量方法')).toBeVisible()
  await expect(page.locator('[data-practice-label]')).toContainText('不计正式积分')
  await page.getByRole('button', { name: '三段论框架（AI）' }).click()
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '三段论框架（AI）' }).click()
  await page.getByRole('button', { name: '用本地框架' }).click()
  await expect(page.locator('#first-content')).toHaveValue(/\[填写本轮观点\]/)
  await page.getByRole('button', { name: '预设漏洞辩词' }).click()
  await expect(page.locator('#first-content')).toHaveValue(/适合每个人/)
  expect(posts).toEqual(['hints'])
  await page.getByRole('button', { name: '发送交锋' }).click()
  await expect(page.locator('.message')).toHaveCount(2)
  await expect(page.getByText('正方 63%')).toBeVisible()
  await page.getByRole('button', { name: 'AI 进度' }).click()
  await page.getByText('开发者模式').click()
  await expect(page.getByText(/首段延迟/)).toBeVisible()
  await page.getByRole('button', { name: '交锋图谱' }).click()
  await page.getByRole('button', { name: '对方：补充成本' }).click()
  await expect(page.locator('[data-message-id="11"] [data-message-content]')).toBeFocused()
  expect(posts).toEqual(['hints', 'turn'])
})
