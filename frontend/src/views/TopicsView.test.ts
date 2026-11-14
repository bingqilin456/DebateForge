// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TopicsView from './TopicsView.vue'
import { createAuthState } from '../auth/state'
import type { Debate, Stance, Topic } from '../types/api'

const fetchMock = vi.fn<typeof fetch>()
const topics: Topic[] = Array.from({ length: 5 }, (_, index) => ({ id: String(index + 1), title: `辩题 ${index + 1}`, description: `说明 ${index + 1}` }))
const mounted: Array<{ app: App, host: HTMLDivElement }> = []
function json(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-topics' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-topics' },
  })
}
function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: 'private upstream detail' }, request_id: 'req-topics' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-topics' },
  })
}
function debate(stance: Stance = 'pro'): Debate {
  return { id: '123', topic: topics[1]!, user_stance: stance, ai_stance: stance === 'pro' ? 'con' : 'pro', mode: 'standard_4', ai_style: 'logical', total_rounds: 4, status: 'active', completed_rounds: 0, created_at: '2026-11-12T00:00:00Z', updated_at: '2026-11-12T00:00:00Z' }
}
function deferred() {
  let resolve!: (response: Response) => void
  const promise = new Promise<Response>(done => { resolve = done })
  return { promise, resolve }
}
async function mount(handler: typeof fetch, disabled = false) {
  const auth = createAuthState()
  fetchMock.mockResolvedValueOnce(json({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf-test' }))
  await auth.login({ username: 'alice', password: 'example-password' })
  fetchMock.mockReset().mockImplementation(handler)
  const controls = reactive({ disabled })
  const created = vi.fn(), openDebate = vi.fn(), openHistory = vi.fn(), back = vi.fn()
  const host = document.createElement('div'); document.body.append(host)
  const app = createApp(() => h(TopicsView, { auth, disabled: controls.disabled, onCreated: created, 'onOpen-debate': openDebate, 'onOpen-history': openHistory, 'onBack-to-debate': back }))
  app.mount(host); mounted.push({ app, host })
  return { app, host, auth, controls, created, openDebate, openHistory, back }
}
function button(host: HTMLElement, label: string) {
  const value = [...host.querySelectorAll('button')].find(node => node.textContent?.includes(label))
  if (!value) throw new Error(`缺少按钮 ${label}`)
  return value
}
async function choose(host: HTMLElement, value: string) {
  const radio = host.querySelector<HTMLInputElement>(`input[type="radio"][value="${value}"]`)
  if (!radio) throw new Error(`缺少选择项 ${value}`)
  radio.click(); await nextTick()
}
function submit(host: HTMLElement) { host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) }
async function loaded(host: HTMLElement) { await vi.waitFor(() => expect(host.querySelectorAll('[data-topic-id]')).toHaveLength(5)) }
beforeEach(() => { fetchMock.mockReset(); vi.stubGlobal('fetch', fetchMock) })
afterEach(() => { for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }; vi.unstubAllGlobals() })

describe('T08 辩题列表与立场选择', () => {
  it('加载五个辩题及说明，初始不能未选题就创建', async () => {
    const pending = deferred()
    const { host } = await mount(() => pending.promise)
    expect(host.textContent).toContain('正在加载辩题')
    pending.resolve(json({ items: topics }))
    await loaded(host)
    expect(host.textContent).toContain('说明 5')
    expect(button(host, '创建会话').disabled).toBe(true)
    submit(host)
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('加载失败只显示受控提示，用户点击才重试', async () => {
    let calls = 0
    const { host } = await mount(async () => ++calls === 1 ? failure(503, 'dependency_unavailable') : json({ items: topics }))
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')?.textContent).toContain('暂时不可用'))
    expect(host.textContent).not.toContain('private')
    expect(fetchMock).toHaveBeenCalledOnce()
    button(host, '重试加载辩题').click()
    await loaded(host)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('空列表给出明确提示且不能创建', async () => {
    const { host } = await mount(async () => json({ items: [] }))
    await vi.waitFor(() => expect(host.textContent).toContain('暂无可用辩题'))
    expect(button(host, '创建会话').disabled).toBe(true)
  })

  it.each(['pro', 'con'] as const)('使用选题及 %s 立场创建，经现有 client 携带 CSRF，成功只发 created', async stance => {
    const result = debate(stance)
    const { host, created, openDebate } = await mount(async (_url, init) => init?.method === 'POST' ? json(result, 201) : json({ items: topics }))
    await loaded(host); await choose(host, '2'); await choose(host, stance)
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledWith(result))
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(post[0]).toBe('/api/v1/debates')
    expect(JSON.parse(String(post[1]?.body))).toEqual({ topic_id: '2', user_stance: stance })
    expect(new Headers(post[1]?.headers).get('X-CSRF-Token')).toBe('csrf-test')
    expect(openDebate).not.toHaveBeenCalled()
    submit(host); await nextTick()
    expect(created).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
  })

  it('同步防重复创建，并在等待期间禁用选题、立场和导航', async () => {
    const pending = deferred()
    const { host, created, openHistory } = await mount(async (_url, init) => init?.method === 'POST' ? pending.promise : json({ items: topics }))
    await loaded(host); await choose(host, '2')
    submit(host); submit(host); await nextTick()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
    expect(button(host, '创建中').disabled).toBe(true)
    expect([...host.querySelectorAll('input[type="radio"]')].every(input => input.matches(':disabled'))).toBe(true)
    button(host, '查看历史会话').click(); expect(openHistory).not.toHaveBeenCalled()
    pending.resolve(json(debate(), 201))
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
  })

  it('明确的创建失败保留选择，允许用户手动重试', async () => {
    let posts = 0
    const { host, created } = await mount(async (_url, init) => init?.method === 'POST'
      ? ++posts === 1 ? failure(404, 'not_found') : json(debate('con'), 201) : json({ items: topics }))
    await loaded(host); await choose(host, '2'); await choose(host, 'con'); submit(host)
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(posts).toBe(1); expect(created).not.toHaveBeenCalled()
    expect(host.querySelector<HTMLInputElement>('input[value="2"]')?.checked).toBe(true)
    expect(host.querySelector<HTMLInputElement>('input[value="con"]')?.checked).toBe(true)
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
  })

  it('创建响应丢失不自动重发，提示先查看历史核实结果', async () => {
    const { host, created, openHistory } = await mount(async (_url, init) => {
      if (init?.method === 'POST') throw new TypeError('private network detail')
      return json({ items: topics })
    })
    await loaded(host); await choose(host, '2'); submit(host)
    await vi.waitFor(() => expect(host.textContent).toContain('创建结果尚未确认'))
    expect(host.textContent).not.toContain('private')
    expect(created).not.toHaveBeenCalled()
    expect(button(host, '创建会话').disabled).toBe(true)
    button(host, '查看历史会话').click(); expect(openHistory).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
  })

  it('外部 disabled 同时阻止请求和导航，解除后可正常加载', async () => {
    const { host, controls, openHistory, back } = await mount(async () => json({ items: topics }), true)
    expect(fetchMock).not.toHaveBeenCalled()
    button(host, '查看历史会话').click(); button(host, '返回辩论主视图').click()
    expect(openHistory).not.toHaveBeenCalled(); expect(back).not.toHaveBeenCalled()
    controls.disabled = false
    await loaded(host)
    button(host, '查看历史会话').click(); button(host, '返回辩论主视图').click()
    expect(openHistory).toHaveBeenCalledOnce(); expect(back).toHaveBeenCalledOnce()
  })

  it('刷新后移除已失效的选题', async () => {
    let calls = 0
    const { host } = await mount(async () => json({ items: ++calls === 1 ? topics : [topics[0]] }))
    await loaded(host); await choose(host, '2'); button(host, '刷新辩题').click()
    await vi.waitFor(() => expect(host.querySelectorAll('[data-topic-id]')).toHaveLength(1))
    expect(button(host, '创建会话').disabled).toBe(true)
  })

  it('文本转义，不把题目/描述当 HTML', async () => {
    const { host } = await mount(async () => json({ items: [{ id: '1', title: '<img src=x>', description: '<script>unsafe</script>' }] }))
    await vi.waitFor(() => expect(host.textContent).toContain('<img src=x>'))
    expect(host.querySelector('img,script')).toBeNull()
  })

  it('退出后不展示迟到的辩题响应', async () => {
    const pending = deferred()
    const { host, auth } = await mount((url) => String(url).endsWith('/auth/logout') ? Promise.resolve(new Response(null, { status: 204 })) : pending.promise)
    await auth.logout()
    pending.resolve(json({ items: topics })); await nextTick(); await new Promise(resolve => setTimeout(resolve, 0))
    expect(host.querySelectorAll('[data-topic-id]')).toHaveLength(0)
    expect(host.textContent).toContain('请先登录')
  })

  it('组件卸载后不发迟到的 created', async () => {
    const pending = deferred()
    const current = await mount(async (_url, init) => init?.method === 'POST' ? pending.promise : json({ items: topics }))
    await loaded(current.host); await choose(current.host, '2'); submit(current.host)
    current.app.unmount(); mounted.splice(mounted.findIndex(item => item.app === current.app), 1)
    pending.resolve(json(debate(), 201)); await new Promise(resolve => setTimeout(resolve, 0))
    expect(current.created).not.toHaveBeenCalled(); current.host.remove()
  })
})
