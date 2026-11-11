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
