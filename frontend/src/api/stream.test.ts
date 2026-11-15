import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApiClient } from './client'
import { createDebateApi } from './debate'

const fetcher = vi.fn<typeof fetch>()
const key = '550e8400-e29b-41d4-a716-446655440000'
const content = '保留 原文 😀'
const time = '2026-11-15T00:00:00Z'
const turn = { id: '9', round_no: 1, client_request_id: key, status: 'completed', attempt_version: 1, error_code: null, messages: [{ id: '10', role: 'user', content, created_at: time }, { id: '11', role: 'assistant', content: '正式回复', created_at: time }] }
const done = { turn, completed_rounds: 1, status: 'active' }
const start = { turn_id: '9', request_id: 'stream-request' }
function frame(event: string, data: unknown) { return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n` }
function response(text: string) { return new Response(text, { headers: { 'Content-Type': 'text/event-stream; charset=utf-8', 'X-Request-ID': 'stream-request' } }) }
function json(data: unknown, status = 202) { return new Response(JSON.stringify({ data, request_id: 'stream-request' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'stream-request' } }) }
function api() { return createDebateApi(createApiClient({ getCsrfToken: () => 'synthetic-stream-csrf' })) }
beforeEach(() => { fetcher.mockReset(); vi.stubGlobal('fetch', fetcher) })
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })
describe('B2 流式发送与原 client 契约', () => {
  it('POST 同源/Session/CSRF 与原键原文，start 无版本，delta 只是通知，done 为正式结果', async () => {
    fetcher.mockResolvedValue(response(frame('start', start) + frame('delta', { text: '临时😀' }) + frame('done', done)))
    const update = vi.fn()
    await expect(api().stream('123', content, key, 1, 4, update)).resolves.toEqual({ kind: 'done', data: done })
    expect(update.mock.calls.map(call => call[0])).toEqual([{ type: 'start', ...start }, { type: 'delta', text: '临时😀' }])
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/123/turns/stream')
    expect(init).toMatchObject({ method: 'POST', credentials: 'same-origin', body: JSON.stringify({ round_no: 1, content, client_request_id: key }) })
    expect(new Headers(init?.headers).get('Accept')).toBe('text/event-stream')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('synthetic-stream-csrf')
  })
  it('202 必须走 JSON，不当 SSE 或成功完成；不自动发第二次 POST', async () => {
    const pending = { turn: { ...turn, status: 'generating', messages: turn.messages.slice(0, 1) }, completed_rounds: 0, query_url: '/api/v1/debates/123' }
    fetcher.mockResolvedValue(json(pending))
    await expect(api().stream('123', content, key, 1, 4, vi.fn())).resolves.toEqual({ kind: 'pending', data: pending })
    expect(fetcher).toHaveBeenCalledOnce()
  })
  it.each([
    frame('delta', { text: '无 start' }),
    frame('start', { ...start, attempt_version: 1 }),
    frame('start', { ...start, request_id: 'wrong-request' }),
    frame('start', start) + frame('start', start),
    frame('start', start) + frame('chunk', { text: '不改事件名' }),
    frame('start', start) + frame('done', { ...done, turn: { ...turn, id: '12' } }),
    frame('start', start) + frame('done', { ...done, turn: { ...turn, client_request_id: '550e8400-e29b-41d4-a716-446655440001' } }),
    frame('start', start) + frame('done', { ...done, status: 'completed' }),
  ])('拒绝不符合冻结契约或本请求关联的事件 %#', async text => {
    fetcher.mockResolvedValue(response(text))
    await expect(api().stream('123', content, key, 1, 4, vi.fn())).rejects.toMatchObject({ kind: 'format' })
  })
  it('completed 旧轮重放保留服务端当前进度快照，交给状态层 GET 补全', async () => {
    const replay = { ...done, completed_rounds: 2 }
    fetcher.mockResolvedValue(response(frame('start', start) + frame('done', replay)))
    await expect(api().stream('123', content, key, 1, 4, vi.fn())).resolves.toEqual({ kind: 'done', data: replay })
  })
  it.each([
    new Response('{}', { status: 202, headers: { 'Content-Type': 'text/event-stream' } }),
    json({ turn: { ...turn, status: 'generating', messages: turn.messages.slice(0, 1) }, completed_rounds: 0, query_url: 'https://untrusted.invalid/steal' }),
    new Response(null, { status: 200, headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'stream-request' } }),
  ])('拒绝错误 202 MIME、外站查询地址或空 SSE body %#', async value => {
    fetcher.mockResolvedValue(value)
    await expect(api().stream('123', content, key, 1, 4, vi.fn())).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).toHaveBeenCalledOnce()
  })
  it('已取消的操作不发送请求', async () => {
    const controller = new AbortController(); controller.abort()
    await expect(api().stream('123', content, key, 1, 4, vi.fn(), controller.signal)).rejects.toMatchObject({ kind: 'network' })
    expect(fetcher).not.toHaveBeenCalled()
  })
  it('流内 error 使用受控提示，不把上游原文带入 UI', async () => {
    fetcher.mockResolvedValue(response(frame('start', start) + frame('error', { error: { code: 'model_timeout', message: 'private provider response' }, request_id: 'stream-request' })))
    const error = await api().stream('123', content, key, 1, 4, vi.fn()).catch(value => value)
    expect(error.code).toBe('model_timeout')
    expect(error.message).not.toContain('private provider response')
  })
  it.each([frame('start', start) + frame('delta', { text: '未提交' }), frame('start', start) + frame('done', done).trimEnd()])('EOF 无完整 done 不报告完成 %#', async text => {
    fetcher.mockResolvedValue(response(text))
    await expect(api().stream('123', content, key, 1, 4, vi.fn())).rejects.toMatchObject({ kind: 'network' })
  })
  it('200 application/json 即使内容有效也不冒充流式结果', async () => {
    fetcher.mockResolvedValue(json(done, 200))
    await expect(api().stream('123', content, key, 1, 4, vi.fn())).rejects.toMatchObject({ kind: 'format' })
  })
  it('流开始前的 401 复用共享认证失效回调', async () => {
    const expired = vi.fn()
    fetcher.mockResolvedValue(new Response(JSON.stringify({ error: { code: 'unauthenticated', message: 'private' }, request_id: 'stream-request' }), { status: 401, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'stream-request' } }))
    const client = createDebateApi(createApiClient({ getCsrfToken: () => 'synthetic', onUnauthenticated: expired }))
    await expect(client.stream('123', content, key, 1, 4, vi.fn())).rejects.toMatchObject({ status: 401, code: 'unauthenticated' })
    expect(expired).toHaveBeenCalledOnce()
  })
  it('70 秒预算覆盖悬挂 body；外部取消也能退出', async () => {
    vi.useFakeTimers()
    fetcher.mockImplementation(async () => new Response(new ReadableStream({ start() {} }), { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'stream-request' } }))
    const check = expect(api().stream('123', content, key, 1, 4, vi.fn())).rejects.toMatchObject({ kind: 'timeout' })
    await vi.advanceTimersByTimeAsync(70_000); await check
    const controller = new AbortController()
    const cancelled = expect(api().stream('123', content, key, 1, 4, vi.fn(), controller.signal)).rejects.toMatchObject({ kind: 'network' })
    controller.abort(); await cancelled
  })
})
