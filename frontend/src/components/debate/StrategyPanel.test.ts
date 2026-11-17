// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import StrategyPanel from './StrategyPanel.vue'
import { createAuthState } from '../../auth/state'
import type { DebateDetail, StrategyStatus, Turn } from '../../types/api'

const fetcher = vi.fn<typeof fetch>()
const mounted: Array<{ app: App; host: HTMLDivElement }> = []
const notStarted = { status: 'not_started', attempt_version: null } as const
const failed = (version = 1): StrategyStatus => ({ status: 'failed', attempt_version: version, error_code: 'model_timeout' })
const complete = (version = 1, style: 'logical' | 'data' = 'logical'): StrategyStatus => ({ status: 'completed', attempt_version: version, schema_version: 1, prompt_version: 'strategy-v1', ai_style: style, content: { thesis: '<img src=x>为用户立场破题', arguments: ['有效论据'], risks: ['回应对手质疑'] } })
function detail(id = '123'): DebateDetail {
  return { debate: { id, topic: { id: '1', title: '开放考核', description: '说明' }, user_stance: 'pro', ai_stance: 'con', mode: 'standard_4', total_rounds: 4, ai_style: 'logical', status: 'active', completed_rounds: 0, created_at: '2026-11-15T00:00:00Z', updated_at: '2026-11-15T00:00:00Z' }, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
}
function withTurn(status: Turn['status']): DebateDetail {
  const value = detail()
  value.turns = [{ id: '9', round_no: 1, attempt_version: 1, status, error_code: status === 'failed' ? 'model_timeout' : null, client_request_id: '550e8400-e29b-41d4-a716-446655440000', messages: [{ id: '10', role: 'user', content: '发言', created_at: value.debate.created_at }] }]
  if (status === 'completed') value.turns[0]!.messages.push({ id: '11', role: 'assistant', content: '回复', created_at: value.debate.created_at })
  return value
}
function reply(data: unknown, status = 200) { return new Response(JSON.stringify({ data, request_id: 'req-card' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-card' } }) }
function failure(status: number, code: string) { return new Response(JSON.stringify({ error: { code, message: 'private upstream data' }, request_id: 'req-card' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-card' } }) }
function deferred() { let resolve!: (response: Response) => void; const promise = new Promise<Response>(done => { resolve = done }); return { resolve, promise } }
async function mount(handler: typeof fetch, initial = detail()) {
  const auth = createAuthState()
  fetcher.mockResolvedValueOnce(reply({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf-card' }))
  await auth.login({ username: 'alice', password: 'example-password' })
  fetcher.mockReset().mockImplementation(handler)
  const value = ref(initial), disabled = ref(false)
  const host = document.createElement('div'); document.body.append(host)
  const app = createApp(() => h(StrategyPanel, { auth, detail: value.value, disabled: disabled.value }))
  app.mount(host); mounted.push({ app, host })
  return { app, host, value, disabled, auth }
}
function button(host: HTMLElement, text: string) { const result = [...host.querySelectorAll('button')].find(node => node.textContent?.includes(text)); if (!result) throw new Error(`缺少按钮 ${text}`); return result }
function posts() { return fetcher.mock.calls.filter(([, init]) => init?.method === 'POST') }
async function ready(host: HTMLElement) { await vi.waitFor(() => expect(host.textContent).toContain('尚未生成赛前策略')) }
beforeEach(() => { fetcher.mockReset(); vi.stubGlobal('fetch', fetcher) })
afterEach(() => { for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }; vi.useRealTimers(); vi.unstubAllGlobals() })

describe('T14 策略卡的显式生成与恢复', () => {
  it('首次进入和同会话重渲染只 GET，策略可跳过', async () => {
    const { host, value } = await mount(async () => reply(notStarted))
    await ready(host); value.value = { ...value.value }; await nextTick()
    expect(posts()).toHaveLength(0); expect(fetcher).toHaveBeenCalledOnce()
    expect(host.textContent).toContain('可跳过')
  })
  it('点击才 POST {}，前置查询无 turn，连续点击只生成一次', async () => {
    const pending = deferred()
    const { host } = await mount((input, init) => init?.method === 'POST' ? pending.promise : Promise.resolve(reply(String(input).endsWith('/strategy') ? notStarted : detail())))
    await ready(host)
    const generate = button(host, '生成赛前策略'); generate.click(); generate.click()
    await vi.waitFor(() => expect(posts()).toHaveLength(1))
    expect(posts()[0]![1]?.body).toBe('{}')
    expect(host.textContent).toContain('正在生成')
    pending.resolve(reply(complete(), 201))
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(posts()).toHaveLength(1)
  })
  it('已有 completed 只重读三字段、版本和保存流派，内容按文本转义', async () => {
    const { host } = await mount(async () => reply(complete()))
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(host.textContent).toContain('<img src=x>'); expect(host.querySelector('img')).toBeNull()
    expect(host.textContent).toContain('strategy-v1'); expect(host.textContent).toContain('有效论据'); expect(host.textContent).toContain('回应对手质疑')
    expect(host.textContent).toContain('正方'); expect(posts()).toHaveLength(0)
    expect([...host.querySelectorAll('button')].some(node => node.textContent?.includes('生成赛前策略'))).toBe(false)
  })
  it('202 生成中只安排有界 GET 查询，不再次 POST', async () => {
    vi.useFakeTimers()
    let gets = 0
    const { host } = await mount(async (input, init) => {
      if (!String(input).endsWith('/strategy')) return reply(detail())
      if (init?.method === 'POST') return reply({ status: 'generating', attempt_version: 1 }, 202)
      return reply(++gets === 1 ? notStarted : complete())
    })
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(host.textContent).toContain('策略生成中'))
    await vi.advanceTimersByTimeAsync(2_000)
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(posts()).toHaveLength(1); expect(gets).toBe(2)
  })
  it('每 2 秒只读查询，120 秒预算耗尽后停止，手动查询才重启', async () => {
    vi.useFakeTimers()
    const { host } = await mount(async () => reply({ status: 'generating', attempt_version: 1 }))
    await vi.advanceTimersByTimeAsync(0)
    await nextTick()
    expect(host.textContent).toContain('策略生成中')
    const initial = fetcher.mock.calls.length
    await vi.advanceTimersByTimeAsync(1_999)
    expect(fetcher).toHaveBeenCalledTimes(initial)
    await vi.advanceTimersByTimeAsync(1)
    expect(fetcher).toHaveBeenCalledTimes(initial + 1)
    await vi.advanceTimersByTimeAsync(118_000)
    expect(host.textContent).toContain('稍后手动查询')
    const stopped = fetcher.mock.calls.length
    await vi.advanceTimersByTimeAsync(10_000)
    expect(fetcher).toHaveBeenCalledTimes(stopped)
    expect(posts()).toHaveLength(0)
    button(host, '查询策略状态').click()
    await vi.advanceTimersByTimeAsync(0)
    await vi.advanceTimersByTimeAsync(2_000)
    expect(fetcher.mock.calls.length).toBeGreaterThan(stopped + 1)
    expect(posts()).toHaveLength(0)
  })
  it('409 恢复查询是 generating，详情查询较慢时不会丢掉只读轮询', async () => {
    vi.useFakeTimers()
    let gets = 0, details = 0
    const slowDetail = deferred()
    const { host } = await mount(async (input, init) => {
      if (!String(input).endsWith('/strategy')) return ++details === 1 ? reply(detail()) : slowDetail.promise
      if (init?.method === 'POST') return failure(409, 'attempt_version_conflict')
      return reply(++gets === 1 ? notStarted : gets === 2 ? { status: 'generating', attempt_version: 1 } : complete())
    })
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(details).toBe(2))
    await vi.advanceTimersByTimeAsync(2_100)
    slowDetail.resolve(reply(detail()))
    await vi.waitFor(() => expect(button(host, '查询策略状态').disabled).toBe(false))
    await vi.advanceTimersByTimeAsync(2_000)
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(posts()).toHaveLength(1)
  })
  it('退出后再以同一用户登录，旧会话迟到响应也不能回填', async () => {
    const pending = deferred()
    const { host, auth } = await mount(async (input, init) => {
      const path = String(input)
      if (path.endsWith('/auth/logout')) return new Response(null, { status: 204 })
      if (path.endsWith('/auth/login')) return reply({ user: { id: '42', username: 'alice' }, csrf_token: 'new-synthetic-csrf' })
      if (init?.method === 'POST') return pending.promise
      return reply(path.endsWith('/strategy') ? notStarted : detail())
    })
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(posts()).toHaveLength(1))
    await auth.logout()
    await auth.login({ username: 'alice', password: 'example-password' })
    await ready(host)
    pending.resolve(reply(complete(), 201)); await new Promise(resolve => setTimeout(resolve, 0)); await nextTick()
    expect(host.querySelector('[data-strategy-completed]')).toBeNull()
    expect(host.textContent).toContain('尚未生成赛前策略')
  })
  it('失败重试先 GET 最新版本，查询后仍需第二次点击确认', async () => {
    let gets = 0
    const { host } = await mount(async (input, init) => {
      if (!String(input).endsWith('/strategy')) return reply(detail())
      if (init?.method === 'POST') return reply(complete(3))
      return reply(failed(++gets === 1 ? 1 : 2))
    })
    await vi.waitFor(() => expect(host.textContent).toContain('model_timeout'))
    button(host, '查询最新重试版本').click()
    await vi.waitFor(() => expect(host.textContent).toContain('确认重试策略（v2）'))
    expect(posts()).toHaveLength(0)
    button(host, '确认重试策略（v2）').click()
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(JSON.parse(String(posts()[0]![1]?.body))).toEqual({ retry: true, expected_attempt_version: 2 })
  })
  it('409 后只重新查询，不换版本自动重发', async () => {
    let gets = 0
    const { host } = await mount(async (input, init) => {
      if (!String(input).endsWith('/strategy')) return reply(detail())
      if (init?.method === 'POST') return failure(409, 'attempt_version_conflict')
      return reply(failed(++gets <= 2 ? 1 : 2))
    })
    await vi.waitFor(() => expect(host.textContent).toContain('model_timeout'))
    button(host, '查询最新重试版本').click()
    await vi.waitFor(() => expect(host.textContent).toContain('确认重试策略（v1）'))
    button(host, '确认重试策略（v1）').click()
    await vi.waitFor(() => expect(host.textContent).toContain('当前版本 v2'))
    expect(posts()).toHaveLength(1)
    expect(host.textContent).not.toContain('private')
    expect([...host.querySelectorAll('button')].some(node => node.textContent?.includes('确认重试策略'))).toBe(false)
  })
  it('POST 502 后查询 failed，不自动重试；再次查询后才能显式重试', async () => {
    let hasPosted = false
    const { host } = await mount(async (input, init) => {
      if (!String(input).endsWith('/strategy')) return reply(detail())
      if (init?.method === 'POST') { hasPosted = true; return failure(502, 'model_error') }
      return reply(hasPosted ? failed(1) : notStarted)
    })
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(host.textContent).toContain('当前版本 v1'))
    expect(posts()).toHaveLength(1)
    button(host, '查询最新重试版本').click()
    await vi.waitFor(() => expect(host.textContent).toContain('确认重试策略（v1）'))
    expect(posts()).toHaveLength(1)
  })
  it.each(['failed', 'generating', 'completed'] as const)('已有 %s turn 都不提供首次生成或失败重试', async status => {
    const { host } = await mount(async () => reply(failed()), withTurn(status))
    await vi.waitFor(() => expect(host.textContent).toContain('已存在发言'))
    expect([...host.querySelectorAll('button')].some(node => /生成赛前策略|确认重试|查询最新重试/.test(node.textContent ?? ''))).toBe(false)
    expect(posts()).toHaveLength(0)
  })
  it('旧页面无 turn 但最新详情已有 turn 时，前置查询阻止 POST', async () => {
    const { host } = await mount(async input => reply(String(input).endsWith('/strategy') ? notStarted : withTurn('failed')))
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(host.textContent).toContain('已存在发言'))
    expect(posts()).toHaveLength(0)
  })
  it('策略在途时出现首轮，不丢失已认领策略的完成结果', async () => {
    const pending = deferred()
    const { host, value } = await mount(async (input, init) => init?.method === 'POST' ? pending.promise : reply(String(input).endsWith('/strategy') ? notStarted : detail()))
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(posts()).toHaveLength(1))
    value.value = withTurn('generating'); await nextTick()
    pending.resolve(reply(complete(), 201))
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(posts()).toHaveLength(1)
  })
  it('切换会话后丢弃旧会话迟到 POST，不跨会话回填', async () => {
    const pending = deferred()
    const { host, value } = await mount(async (input, init) => init?.method === 'POST' ? pending.promise : reply(String(input).endsWith('/strategy') ? notStarted : detail()))
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(posts()).toHaveLength(1))
    value.value = detail('124'); await ready(host)
    pending.resolve(reply(complete(), 201)); await new Promise(resolve => setTimeout(resolve, 0)); await nextTick()
    expect(host.querySelector('[data-strategy-completed]')).toBeNull()
    expect(host.querySelector('[data-strategy-id="124"]')).not.toBeNull()
  })
  it('GET 失败不放行生成；显式查询恢复后才允许点击', async () => {
    let calls = 0
    const { host } = await mount(async () => ++calls === 1 ? failure(503, 'dependency_unavailable') : reply(notStarted))
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(posts()).toHaveLength(0)
    button(host, '查询策略状态').click(); await ready(host)
    expect(posts()).toHaveLength(0)
  })
  it('POST 503 后 GET 发现已完成，以保存结果为准，不再生成', async () => {
    let posted = false
    const { host } = await mount(async (input, init) => {
      if (!String(input).endsWith('/strategy')) return reply(detail())
      if (init?.method === 'POST') { posted = true; return failure(503, 'dependency_unavailable') }
      return reply(posted ? complete() : notStarted)
    })
    await ready(host); button(host, '生成赛前策略').click()
    await vi.waitFor(() => expect(host.querySelector('[data-strategy-completed]')).not.toBeNull())
    expect(posts()).toHaveLength(1)
  })
})
