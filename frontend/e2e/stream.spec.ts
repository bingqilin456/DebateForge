import { expect, test } from '@playwright/test'
import type { DebateDetail, Turn } from '../src/types/api'

interface StreamHarness {
  streamPosts: number
  retryBodies: unknown[]
  key: string
  emit(name: string, data: unknown): void
  finish(): void
  lose(mode: 'completed' | 'failed' | 'missing'): void
  commit(): void
}
declare global { interface Window { __b2Stream: StreamHarness } }
const date = '2026-11-15T00:00:00Z'
const initial: DebateDetail = { debate: { id: '123', topic: { id: '1', title: '流式浏览器 Mock', description: '仅合成响应，不连接后端。' }, user_stance: 'pro', ai_stance: 'con', ai_style: 'logical', mode: 'standard_4', total_rounds: 4, status: 'active', completed_rounds: 0, created_at: date, updated_at: date }, turns: [], summary_status: { status: 'not_started', attempt_version: null } }

test.beforeEach(async ({ page }) => {
  await page.addInitScript(({ initial, date }) => {
    let saved = initial, controller: ReadableStreamDefaultController<Uint8Array> | null = null
    let content = '', conflict = true
    const json = (data: unknown, status = 200) => new Response(JSON.stringify({ data, request_id: 'browser-stream' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'browser-stream' } })
    const makeTurn = (status: Turn['status'], version: number): Turn => ({ id: '9', round_no: 1, client_request_id: window.__b2Stream.key, status, attempt_version: version, error_code: status === 'failed' ? 'model_timeout' : null, messages: [{ id: '10', role: 'user', content, created_at: date }, ...(status === 'completed' ? [{ id: '11', role: 'assistant' as const, content: '已提交的正式回复', created_at: date }] : [])] })
    const commit = () => { saved = { ...saved, debate: { ...saved.debate, completed_rounds: 1 }, turns: [makeTurn('completed', 9)] } }
    window.__b2Stream = {
      streamPosts: 0, retryBodies: [], key: '',
      emit(name, data) {
        const bytes = new TextEncoder().encode(`event: ${name}\r\ndata: ${JSON.stringify(data)}\r\n\r\n`)
        for (const byte of bytes) controller?.enqueue(Uint8Array.of(byte))
      },
      commit,
      finish() { commit(); this.emit('done', { turn: saved.turns[0], completed_rounds: 1, status: 'active' }); controller?.close() },
      lose(mode) {
        if (mode === 'completed') commit()
        if (mode === 'failed') saved = { ...saved, turns: [makeTurn('failed', 7)] }
        controller?.close()
      },
    }
    const fallback = window.fetch.bind(window)
    window.fetch = async (input, init) => {
      const pathname = new URL(String(input), location.origin).pathname
      if (!pathname.startsWith('/api/v1/')) return fallback(input, init)
      if (pathname.endsWith('/auth/me')) return json({ user: { id: '42', username: 'stream_mock' }, csrf_token: 'synthetic-browser-csrf' })
      if (pathname.endsWith('/strategy')) return json({ status: 'not_started', attempt_version: null })
      if (pathname === '/api/v1/debates/123') return json(saved)
      if (pathname.endsWith('/turns/stream')) {
        const body = JSON.parse(String(init?.body)) as { content: string; client_request_id: string }
        content = body.content; window.__b2Stream.key = body.client_request_id; window.__b2Stream.streamPosts++
        expectHeader(init?.headers)
        return new Response(new ReadableStream<Uint8Array>({ start(value) { controller = value } }), { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'browser-stream' } })
      }
      if (pathname.endsWith('/turns/9/retry')) {
        window.__b2Stream.retryBodies.push(JSON.parse(String(init?.body)) as unknown)
        if (conflict) {
          conflict = false; saved = { ...saved, turns: [makeTurn('failed', 8)] }
          return new Response(JSON.stringify({ error: { code: 'attempt_version_conflict', message: 'private provider error' }, request_id: 'browser-stream' }), { status: 409, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'browser-stream' } })
        }
        commit()
        return json({ turn: saved.turns[0], completed_rounds: 1, query_url: '/api/v1/debates/123' })
      }
      throw new Error('Mock 不允许该请求')
    }
    function expectHeader(headers?: HeadersInit) {
      const value = new Headers(headers)
      if (value.get('Accept') !== 'text/event-stream' || value.get('X-CSRF-Token') !== 'synthetic-browser-csrf') throw new Error('未复用流式 Accept/CSRF')
    }
  }, { initial, date })
  await page.goto('/#debate=123')
  await page.locator('#first-content').fill('浏览器原文😀')
  await page.getByRole('button', { name: '发送首轮', exact: true }).evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  await expect(page.locator('[data-stream-preview]')).toBeVisible()
})

test('B2 浏览器 Mock：逐字节中文/emoji 预览转义，done 后只显示正式消息', async ({ page }, testInfo) => {
  await page.evaluate(() => { window.__b2Stream.emit('start', { turn_id: '9', request_id: 'browser-stream' }); window.__b2Stream.emit('delta', { text: '<img src=x>临时😀' }) })
  await expect(page.locator('[data-stream-text]')).toHaveText('<img src=x>临时😀')
  await expect(page.locator('[data-stream-preview] img')).toHaveCount(0)
  await expect(page.locator('.message')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('mock-stream-preview.png'), fullPage: true })
  await page.evaluate(() => window.__b2Stream.finish())
  await expect(page.locator('.message')).toHaveCount(2)
  await expect(page.locator('[data-stream-preview]')).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('<img src=x>临时😀')
  expect(await page.evaluate(() => window.__b2Stream.streamPosts)).toBe(1)
})
test('B2 浏览器 Mock：丢 done 只查询，已提交结果不重新生成', async ({ page }) => {
  await page.evaluate(() => { window.__b2Stream.emit('start', { turn_id: '9', request_id: 'browser-stream' }); window.__b2Stream.emit('delta', { text: '未确认' }); window.__b2Stream.lose('completed') })
  await expect(page.locator('.message')).toHaveCount(2)
  await expect(page.locator('body')).toContainText('已完成 1 / 4 轮')
  expect(await page.evaluate(() => window.__b2Stream.streamPosts)).toBe(1)
})
test('B2 浏览器 Mock：failed 查询版本、409 只查，第二次明确点击才用新版本', async ({ page }) => {
  await page.evaluate(() => window.__b2Stream.lose('failed'))
  const retry = page.locator('[data-retry-turn]')
  await expect(retry).toContainText('v7')
  expect(await page.evaluate(() => window.__b2Stream.retryBodies)).toEqual([])
  await retry.click()
  await expect(retry).toContainText('v8')
  expect(await page.evaluate(() => window.__b2Stream.retryBodies)).toEqual([{ expected_attempt_version: 7 }])
  await expect(page.locator('body')).not.toContainText('private provider error')
  await retry.click()
  await expect(page.locator('.message')).toHaveCount(2)
  expect(await page.evaluate(() => window.__b2Stream.retryBodies)).toEqual([{ expected_attempt_version: 7 }, { expected_attempt_version: 8 }])
  expect(await page.evaluate(() => window.__b2Stream.streamPosts)).toBe(1)
})
test('B2 浏览器 Mock：未收 start 且暂未找到原键，继续只读查询直至正式结果', async ({ page }) => {
  await page.evaluate(() => window.__b2Stream.lose('missing'))
  await expect(page.locator('body')).toContainText('尚未匹配原请求标识')
  await expect(page.locator('form[data-first-turn] button[type="submit"]')).toBeDisabled()
  const key = await page.evaluate(() => window.__b2Stream.key)
  await page.evaluate(() => window.__b2Stream.commit())
  await expect(page.locator('.message')).toHaveCount(2)
  expect(await page.evaluate(() => window.__b2Stream.key)).toBe(key)
  expect(await page.evaluate(() => window.__b2Stream.streamPosts)).toBe(1)
})
