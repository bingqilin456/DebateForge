// @vitest-environment jsdom
import { createApp, h, nextTick, type App as VueApp } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from './App.vue'
import { createAuthState } from './auth/state'

const fetchMock = vi.fn<typeof fetch>()
let app: VueApp | undefined
let host: HTMLDivElement
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-app' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-app' } })
}
function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: '受控错误' }, request_id: 'req-app' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-app' } })
}
function mount() {
  host = document.createElement('div'); document.body.append(host)
  const auth = createAuthState()
  app = createApp(h(App, { auth })); app.mount(host)
  return auth
}
async function settle() { await vi.waitFor(() => expect(fetchMock.mock.results.every(r => r.type === 'return')).toBe(true)); await new Promise(resolve => setTimeout(resolve, 20)); await nextTick() }
function click(label: string) {
  const button = [...host.querySelectorAll('button')].find(el => el.textContent?.includes(label))
  if (!button) throw new Error(`缺少按钮 ${label}`)
  button.click()
}
beforeEach(() => { fetchMock.mockReset(); vi.stubGlobal('fetch', fetchMock); history.replaceState(null, '', '/'); })
afterEach(() => { app?.unmount(); host?.remove(); vi.unstubAllGlobals() })

it('恢复失败提供显式恢复入口；503 不伪装未登录', async () => {
  fetchMock.mockResolvedValue(failure(503, 'dependency_unavailable'))
  const auth = mount()
  await vi.waitFor(() => expect(host.textContent).toContain('恢复登录状态'))
  await settle()
  expect(auth.state.status).toBe('unknown')
  expect(host.querySelector('input[name="password"]')).toBeNull()
  fetchMock.mockResolvedValue(failure(401, 'unauthenticated'))
  click('恢复登录状态')
  await vi.waitFor(() => expect(host.querySelector('input[name="password"]')).not.toBeNull())
})

it('正式入口恢复认证、读取真实形状辩题、创建首轮并以纯文本展示', async () => {
  const debate = { id: '123', topic: { id: '1', title: '开放考核', description: '讨论学习' }, user_stance: 'con', ai_stance: 'pro', status: 'active', completed_rounds: 0, created_at: '2026-11-09T00:00:00Z', updated_at: '2026-11-09T00:00:00Z' }
  fetchMock.mockImplementation(async (url, init) => {
    if (String(url).endsWith('/auth/me')) return response({ user: { id: '42', username: 'alice' }, csrf_token: 'memory-only' })
    if (String(url).endsWith('/topics')) return response({ items: [debate.topic] })
    if (String(url).endsWith('/auth/logout')) return failure(503, 'dependency_unavailable')
    if (String(url).endsWith('/debates/123')) return response({ debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
    if (String(url).endsWith('/turns')) {
      const body = JSON.parse(String(init?.body))
      return response({ turn: { id: '2', round_no: 1, client_request_id: body.client_request_id, status: 'completed', attempt_version: 1, error_code: null, messages: [ { id: '3', role: 'user', content: body.content, created_at: debate.created_at }, { id: '4', role: 'assistant', content: '<img src=x onerror=alert(1)>', created_at: debate.created_at } ] }, completed_rounds: 1, query_url: '/api/v1/debates/123' }, 201)
    }
    return response(debate, 201)
  })
  mount()
  await vi.waitFor(() => expect(host.querySelector('select[name="topic"] option[value="1"]')).not.toBeNull())
  const stance = host.querySelector<HTMLSelectElement>('select[name="stance"]')!
  stance.value = 'con'; stance.dispatchEvent(new Event('change', { bubbles: true })); await nextTick()
  click('创建会话'); click('创建会话')
  await vi.waitFor(() => expect(host.querySelector('textarea')).not.toBeNull())
  const textarea = host.querySelector('textarea')!
  textarea.value = '\u0085'; textarea.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  expect(host.querySelector<HTMLButtonElement>('form[data-first-turn] button[type="submit"]')?.disabled).toBe(true)
  textarea.value = '原文'; textarea.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  host.querySelector('form[data-first-turn]')!.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }))
  await vi.waitFor(() => expect(host.textContent).toContain('首轮已完成'))
  expect(host.textContent).toContain('<img src=x onerror=alert(1)>')
  expect(host.querySelector('img')).toBeNull()
  expect(location.hash).toBe('#debate=123')
  expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith('/debates'))).toHaveLength(1)
  expect(fetchMock.mock.calls.find(([url]) => String(url).endsWith('/debates'))?.[1]?.body).toBe('{"topic_id":"1","user_stance":"con"}')
  click('退出登录')
  await vi.waitFor(() => expect(host.textContent).toContain('退出结果'))
  click('恢复登录状态')
  await vi.waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/debates/123'))).toBe(true))
  await vi.waitFor(() => expect(host.textContent).toContain('会话 123'))
  click('开始新的练习')
  await vi.waitFor(() => expect(host.querySelector('select[name="topic"]')).not.toBeNull())
  expect(host.textContent).not.toContain('<img src=x onerror=alert(1)>')
  expect(location.hash).toBe('')
})

it('三轮连续发言、失败重试与 SummaryPanel 纯文本转义展示完整闭环', async () => {
  const time = '2026-11-09T00:00:00Z'
  const baseDebate = {
    id: '123',
    topic: { id: '1', title: '开放考核', description: '讨论学习' },
    user_stance: 'pro',
    ai_stance: 'con',
    status: 'active' as 'active' | 'completed',
    completed_rounds: 0,
    created_at: time,
    updated_at: time,
  }
  const turns: Array<{
    id: string; round_no: number; client_request_id: string; status: 'completed' | 'failed'
    attempt_version: number; error_code: string | null
    messages: Array<{ id: string; role: 'user' | 'assistant'; content: string; created_at: string }>
  }> = []
  let summaryAttempts = 0
  let summaryState: Record<string, unknown> = { status: 'not_started', attempt_version: null }

  fetchMock.mockImplementation(async (url, init) => {
    const u = String(url)
    if (u.endsWith('/auth/me')) return response({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf' })
    if (u.endsWith('/topics')) return response({ items: [baseDebate.topic] })
    if (u.endsWith('/debates') && init?.method === 'POST') return response(baseDebate, 201)
    if (u.endsWith('/debates/123')) {
      return response({ debate: baseDebate, turns, summary_status: summaryState })
    }
    if (u.endsWith('/debates/123/turns')) {
      const body = JSON.parse(String(init?.body))
      const rNo = Number(body.round_no)
      if (rNo === 2 && turns.length === 1) {
        turns.push({
          id: '22', round_no: 2, client_request_id: body.client_request_id,
          status: 'failed', attempt_version: 1, error_code: 'model_timeout',
          messages: [{ id: '201', role: 'user', content: body.content, created_at: time }],
        })
        return failure(504, 'model_timeout')
      }
      const turn = {
        id: String(rNo * 10),
        round_no: rNo,
        client_request_id: body.client_request_id,
        status: 'completed' as const,
        attempt_version: 1,
        error_code: null,
        messages: [
          { id: String(rNo * 100 + 1), role: 'user' as const, content: body.content, created_at: time },
          { id: String(rNo * 100 + 2), role: 'assistant' as const, content: `第${rNo}轮AI回复`, created_at: time },
        ],
      }
      turns.push(turn)
      baseDebate.completed_rounds = rNo
      if (rNo === 3) baseDebate.status = 'completed'
      return response({ turn, completed_rounds: rNo, query_url: '/api/v1/debates/123' }, 201)
    }
    if (u.endsWith('/debates/123/turns/22/retry')) {
      const turn = {
        ...turns[1]!,
        status: 'completed' as const,
        attempt_version: 2,
        error_code: null,
        messages: [
          turns[1]!.messages[0]!,
          { id: '202', role: 'assistant' as const, content: '第2轮重试成功回复', created_at: time },
        ],
      }
      turns[1] = turn
      baseDebate.completed_rounds = 2
      return response({ turn, completed_rounds: 2, query_url: '/api/v1/debates/123' }, 200)
    }
    if (u.endsWith('/debates/123/summary') && init?.method === 'POST') {
      summaryAttempts++
      if (summaryAttempts === 1) {
        summaryState = { status: 'failed', attempt_version: 1, error_code: 'model_error' }
        return failure(502, 'model_error')
      }
      summaryState = {
        status: 'completed',
        attempt_version: 2,
        schema_version: 1,
        content: {
          strengths: [{ round_no: 1, point: '<script>alert(1)</script>首轮优点', reason: '论据扎实' }],
          issues: [{ round_no: 2, point: '<img src=x onerror=alert(2)>二轮不足', reason: '可进一步细化' }],
          suggestions: [{ round_no: 3, action: '三轮收束', example: '直接对比双方标准' }],
        },
      }
      return response(summaryState, 200)
    }
    return failure(404, 'not_found')
  })

  mount()
  await vi.waitFor(() => expect(host.querySelector('select[name="topic"] option[value="1"]')).not.toBeNull())
  click('创建会话')
  await vi.waitFor(() => expect(host.querySelector('textarea')).not.toBeNull())

  // 第 1 轮成功
  let textarea = host.querySelector('textarea')!
  textarea.value = '第一轮论点'; textarea.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  host.querySelector('form[data-first-turn]')!.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }))
  await vi.waitFor(() => expect(host.textContent).toContain('第 2 轮发言'))

  // 第 2 轮首次超时失败 -> 展示重试按钮
  textarea = host.querySelector('textarea')!
  textarea.value = '第二轮论点'; textarea.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  host.querySelector('form[data-first-turn]')!.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }))
  await vi.waitFor(() => expect(host.querySelector('[data-retry-turn]')).not.toBeNull())
  expect(host.querySelector('form[data-first-turn]')).toBeNull()

  // 点击重试第 2 轮（携带 expected_attempt_version: 1）
  host.querySelector<HTMLButtonElement>('[data-retry-turn]')!.click()
  await vi.waitFor(() => expect(host.textContent).toContain('第 3 轮发言'))

  // 第 3 轮成功 -> 自动请求一次总结（模拟首次总结失败，不循环调用，展示手动重试按钮）
  textarea = host.querySelector('textarea')!
  textarea.value = '第三轮总结陈词'; textarea.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  host.querySelector('form[data-first-turn]')!.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }))
  await vi.waitFor(() => expect(host.querySelector('[data-retry-summary]')).not.toBeNull())
  expect(summaryAttempts).toBe(1)

  // 点击手动重试总结（携带 retry: true, expected_attempt_version: 1）
  host.querySelector<HTMLButtonElement>('[data-retry-summary]')!.click()
  await vi.waitFor(() => expect(host.querySelector('[data-summary-completed]')).not.toBeNull())
  expect(summaryAttempts).toBe(2)
  expect(host.textContent).toContain('<script>alert(1)</script>首轮优点')
  expect(host.textContent).toContain('<img src=x onerror=alert(2)>二轮不足')
  expect(host.querySelector('script')).toBeNull()
  expect(host.querySelector('img')).toBeNull()
})

it('退出失败保留认证和面板；显式恢复或退出成功才清理', async () => {
  fetchMock.mockImplementation(async url => String(url).endsWith('/auth/me')
    ? response({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf' })
    : String(url).endsWith('/topics') ? response({ items: [] }) : failure(503, 'dependency_unavailable'))
  const auth = mount()
  await vi.waitFor(() => expect(host.textContent).toContain('alice'))
  click('退出登录')
  await vi.waitFor(() => expect(host.textContent).toContain('退出结果'))
  expect(auth.state.status).toBe('authenticated')
  fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
  click('退出登录')
  await vi.waitFor(() => expect(host.querySelector('input[name="password"]')).not.toBeNull())
  expect(host.textContent).not.toContain('alice')
})

it('保护请求 401 清理旧用户和首轮内容，不把其数据带回登录页面', async () => {
  fetchMock.mockImplementation(async url => String(url).endsWith('/auth/me')
    ? response({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf' })
    : failure(401, 'unauthenticated'))
  mount()
  await vi.waitFor(() => expect(host.querySelector('input[name="password"]')).not.toBeNull())
  expect(host.textContent).not.toContain('alice')
  expect(host.querySelector('[aria-label="首轮辩论面板"]')).toBeNull()
  expect(location.hash).toBe('')
  fetchMock.mockImplementation(async url => String(url).endsWith('/auth/login')
    ? response({ user: { id: '42', username: 'alice' }, csrf_token: 'new-csrf' }) : response({ items: [] }))
  for (const [name, value] of Object.entries({ username: 'alice', password: 'example-password' })) {
    const field = host.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
    field.value = value; field.dispatchEvent(new Event('input', { bubbles: true }))
  }
  await nextTick()
  host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await vi.waitFor(() => expect(host.textContent).toContain('alice'))
  expect(host.textContent).not.toContain('登录已结束')
})

it('表单登录刚成功时即使 pending 尚在收尾，也必须读取辩题', async () => {
  fetchMock.mockImplementation(async url => String(url).endsWith('/auth/me') ? failure(401, 'unauthenticated')
    : String(url).endsWith('/auth/login') ? response({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf' })
    : response({ items: [{ id: '1', title: '真实选题', description: '描述' }] }))
  mount()
  await vi.waitFor(() => expect(host.querySelector('input[name="password"]')).not.toBeNull())
  for (const [name, value] of Object.entries({ username: 'alice', password: 'example-password' })) {
    const field = host.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
    field.value = value; field.dispatchEvent(new Event('input', { bubbles: true }))
  }
  await nextTick()
  host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await vi.waitFor(() => expect(host.querySelector('select[name="topic"] option[value="1"]')).not.toBeNull())
})

it('顶层导航支持辩论主视图、TopicsView 与 HistoryView 切换并保留当前会话', async () => {
  const debate = {
    id: '123',
    topic: { id: '1', title: '开放考核', description: '讨论学习' },
    user_stance: 'pro',
    ai_stance: 'con',
    status: 'active',
    completed_rounds: 0,
    created_at: '2026-11-09T00:00:00Z',
    updated_at: '2026-11-09T00:00:00Z',
  }
  fetchMock.mockImplementation(async url => {
    const u = String(url)
    if (u.endsWith('/auth/me')) return response({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf' })
    if (u.endsWith('/topics')) return response({ items: [debate.topic] })
    if (u.startsWith('/api/v1/debates?')) return response({ items: [debate], total: 1, page: 1, page_size: 20 })
    if (u.endsWith('/debates/123')) {
      return response({ debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
    }
    return response(debate, 201)
  })

  mount()
  await vi.waitFor(() => expect(host.querySelector('select[name="topic"] option[value="1"]')).not.toBeNull())

  // 从主视图切换到 TopicsView
  host.querySelector<HTMLButtonElement>('[data-view-tab="topics"]')!.click()
  await nextTick()
  expect(location.hash).toBe('#topics')
  expect(host.querySelector('[data-view="topics"]')).not.toBeNull()
  await vi.waitFor(() => expect(host.querySelector('[data-topic-id="1"]')).not.toBeNull())
  expect(host.textContent).toContain('辩题列表与立场选择')

  // 从 TopicsView 内部按钮切换到 HistoryView
  host.querySelector<HTMLButtonElement>('[data-nav-history]')!.click()
  await nextTick()
  expect(location.hash).toBe('#history')
  expect(host.querySelector('[data-view="history"]')).not.toBeNull()
  await vi.waitFor(() => expect(host.querySelector('[data-history-id="123"]')).not.toBeNull())
  expect(host.textContent).toContain('本人历史会话')

  // 从 HistoryView 返回辩论主视图并创建会话 123
  host.querySelector<HTMLButtonElement>('[data-nav-debate]')!.click()
  await vi.waitFor(() => expect(host.querySelector('select[name="topic"]')).not.toBeNull())
  click('创建会话')
  await vi.waitFor(() => expect(host.textContent).toContain('会话 123'))
  expect(location.hash).toBe('#debate=123')

  // 在已打开会话 123 时切到历史页，再切回辩论主视图时自动恢复 #debate=123
  host.querySelector<HTMLButtonElement>('[data-view-tab="history"]')!.click()
  await nextTick()
  expect(location.hash).toBe('#history')
  expect(host.querySelector('[data-view="history"]')).not.toBeNull()

  host.querySelector<HTMLButtonElement>('[data-view-tab="debate"]')!.click()
  await vi.waitFor(() => expect(host.textContent).toContain('会话 123'))
  expect(location.hash).toBe('#debate=123')
})
