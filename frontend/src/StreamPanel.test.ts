// @vitest-environment jsdom
import { createApp, h, nextTick, type App as VueApp } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from './App.vue'
import { createAuthState } from './auth/state'
import type { DebateDetail, Turn } from './types/api'

const fetcher = vi.fn<typeof fetch>()
const time = '2026-11-15T00:00:00Z'
let app: VueApp | undefined
let host: HTMLDivElement
function json(data: unknown, status = 200) { return new Response(JSON.stringify({ data, request_id: 'ui' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'ui' } }) }
async function mount() {
  let saved: DebateDetail = { debate: { id: '123', topic: { id: '1', title: '流式交互', description: '说明' }, user_stance: 'pro', ai_stance: 'con', ai_style: 'logical', mode: 'standard_4', total_rounds: 4, status: 'active', completed_rounds: 0, created_at: time, updated_at: time }, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
  let controller!: ReadableStreamDefaultController<Uint8Array>, key = '', content = '', posts = 0
  const frame = (event: string, data: unknown) => controller.enqueue(new TextEncoder().encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`))
  const turn = (status: Turn['status'], version = 1): Turn => ({ id: '9', round_no: 1, client_request_id: key, status, attempt_version: version, error_code: status === 'failed' ? 'generation_cancelled' : null, messages: [{ id: '10', role: 'user', content, created_at: time }, ...(status === 'completed' ? [{ id: '11', role: 'assistant' as const, content: '正式已保存回复', created_at: time }] : [])] })
  fetcher.mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.endsWith('/auth/me')) return json({ user: { id: '42', username: 'stream_test' }, csrf_token: 'synthetic-ui-csrf' })
    if (url.endsWith('/strategy')) return json({ status: 'not_started', attempt_version: null })
    if (url.endsWith('/debates/123')) return json(saved)
    if (url.endsWith('/turns/stream')) {
      posts++
      const body = JSON.parse(String(init?.body)) as { content: string; client_request_id: string }
      key = body.client_request_id; content = body.content
      return new Response(new ReadableStream<Uint8Array>({ start(value) { controller = value }, cancel() { saved = { ...saved, turns: [turn('failed', 5)] } } }), { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'ui' } })
    }
    if (url.endsWith('/turns/9/retry')) {
      expect(JSON.parse(String(init?.body))).toEqual({ expected_attempt_version: 5 })
      saved = { ...saved, debate: { ...saved.debate, completed_rounds: 1 }, turns: [turn('completed', 6)] }
      return json({ turn: saved.turns[0], completed_rounds: 1, query_url: '/api/v1/debates/123' })
    }
    throw new Error('不允许的测试请求')
  })
  host = document.createElement('div'); document.body.append(host)
  app = createApp(h(App, { auth: createAuthState() })); app.mount(host)
  await vi.waitFor(() => expect(host.querySelector('#first-content')).not.toBeNull())
  return { frame, posts: () => posts, finish() { saved = { ...saved, debate: { ...saved.debate, completed_rounds: 1 }, turns: [turn('completed')] }; frame('done', { turn: saved.turns[0], completed_rounds: 1, status: 'active' }); controller.close() } }
}
function button(text: string) { const found = [...host.querySelectorAll('button')].find(value => value.textContent?.trim() === text); if (!found) throw new Error(`缺少按钮 ${text}`); return found }
async function submit() { const input = host.querySelector<HTMLTextAreaElement>('#first-content')!; input.value = '用户的原文'; input.dispatchEvent(new Event('input', { bubbles: true })); await nextTick(); button('发送首轮').click(); await vi.waitFor(() => expect(host.querySelector('[data-stream-preview]')).not.toBeNull()) }
beforeEach(() => { fetcher.mockReset(); vi.stubGlobal('fetch', fetcher); history.replaceState(null, '', '/#debate=123') })
afterEach(() => { app?.unmount(); host?.remove(); vi.unstubAllGlobals() })

it('流式预览单独显示且转义，done 前不计轮或插入正式消息', async () => {
  const fixture = await mount(); await submit()
  fixture.frame('start', { turn_id: '9', request_id: 'ui' }); fixture.frame('delta', { text: '<img src=x>临时😀' })
  await vi.waitFor(() => expect(host.querySelector('[data-stream-text]')?.textContent).toContain('<img src=x>临时😀'))
  expect(host.querySelector('[data-stream-preview] img')).toBeNull()
  expect(host.querySelectorAll('.message')).toHaveLength(0)
  expect(host.textContent).toContain('已完成 0 / 4 轮')
  fixture.finish()
  await vi.waitFor(() => expect(host.querySelectorAll('.message')).toHaveLength(2))
  expect(host.querySelector('[data-stream-preview]')).toBeNull()
  expect(host.textContent).toContain('正式已保存回复')
  expect(host.textContent).not.toContain('<img src=x>临时😀')
  expect(fixture.posts()).toBe(1)
})
it('停止等待后查询失败状态，显示查询版本并仅显式 JSON 重试', async () => {
  const fixture = await mount(); await submit()
  fixture.frame('start', { turn_id: '9', request_id: 'ui' }); fixture.frame('delta', { text: '未完成片段' })
  await vi.waitFor(() => expect(host.querySelector('[data-stream-text]')?.textContent).toContain('未完成片段'))
  button('停止等待并查询').click()
  await vi.waitFor(() => expect(host.querySelector('[data-retry-turn]')?.textContent).toContain('v5'))
  expect(host.querySelector('[data-stream-preview]')).toBeNull()
  expect(host.querySelectorAll('.message')).toHaveLength(1)
  host.querySelector<HTMLButtonElement>('[data-retry-turn]')!.click()
  await vi.waitFor(() => expect(host.querySelectorAll('.message')).toHaveLength(2))
  expect(fixture.posts()).toBe(1)
})
