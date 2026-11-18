// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import TopicsView from './TopicsView.vue'
import { createAuthState } from '../auth/state'
import { CUSTOM_TOPIC_BACKGROUND_MAX_CHARS, CUSTOM_TOPIC_TITLE_MAX_CHARS } from '../shared/limits'
import type { Debate, Topic } from '../types/api'

// V2-03 私有自定义辩题的界面回归：计数按 Unicode 码点、超限保留输入、
// 二选一请求形状、两类响应可读、题库为空/失败仍可自定义、切换账号清理草稿。

const fetchMock = vi.fn<typeof fetch>()
const topics: Topic[] = Array.from({ length: 5 }, (_, index) => ({ id: String(index + 1), title: `辩题 ${index + 1}`, description: `说明 ${index + 1}` }))
const mounted: Array<{ app: App, host: HTMLDivElement }> = []

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-custom' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-custom' },
  })
}
function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: 'private upstream detail' }, request_id: 'req-custom' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-custom' },
  })
}
function customDebate(title: string, background: string): Debate {
  return {
    id: '321',
    topic: { id: null, title, description: background },
    user_stance: 'pro',
    ai_stance: 'con',
    mode: 'standard_4',
    ai_style: 'logical',
    total_rounds: 4,
    status: 'active',
    completed_rounds: 0,
    created_at: '2026-11-16T00:00:00Z',
    updated_at: '2026-11-16T00:00:00Z',
  }
}
async function mount(handler: typeof fetch, disabled = false) {
  const auth = createAuthState()
  fetchMock.mockResolvedValueOnce(json({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf-test' }))
  await auth.login({ username: 'alice', password: 'example-password' })
  fetchMock.mockReset().mockImplementation(handler)
  const controls = reactive({ disabled })
  const created = vi.fn(), openHistory = vi.fn(), back = vi.fn()
  const host = document.createElement('div'); document.body.append(host)
  const app = createApp(() => h(TopicsView, { auth, disabled: controls.disabled, onCreated: created, 'onOpen-history': openHistory, 'onBack-to-debate': back }))
  app.mount(host); mounted.push({ app, host })
  return { app, host, auth, controls, created, openHistory, back }
}
function button(host: HTMLElement, label: string) {
  const value = [...host.querySelectorAll('button')].find(node => node.textContent?.includes(label))
  if (!value) throw new Error(`缺少按钮 ${label}`)
  return value
}
function textarea(host: HTMLElement, selector: string) {
  const node = host.querySelector<HTMLTextAreaElement>(selector)
  if (!node) throw new Error(`缺少输入框 ${selector}`)
  return node
}
function type(node: HTMLTextAreaElement, value: string) {
  node.value = value
  node.dispatchEvent(new Event('input'))
}
function submit(host: HTMLElement) { host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) }
async function chooseCustom(host: HTMLElement) {
  const radio = host.querySelector<HTMLInputElement>('input[data-source="custom"]')
  if (!radio) throw new Error('缺少自定义来源选项')
  radio.click(); await nextTick()
}
async function loaded(host: HTMLElement) { await vi.waitFor(() => expect(host.querySelectorAll('[data-topic-id]')).toHaveLength(5)) }

beforeEach(() => { fetchMock.mockReset(); vi.stubGlobal('fetch', fetchMock) })
afterEach(() => { for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }; vi.unstubAllGlobals() })

describe('V2-03 自定义辩题创建', () => {
  it('选择自定义来源后出现标题与背景输入，展示私有性提示与两个独立计数', async () => {
    const { host } = await mount(async () => json({ items: topics }))
    await loaded(host)
    expect(host.querySelector('[data-custom-title]')).toBeNull()
    await chooseCustom(host)
    expect(host.querySelector('[data-custom-topic]')).not.toBeNull()
    expect(host.textContent).toContain('自定义辩题仅你可在本站查看')
    expect(host.querySelector('[data-custom-title-count]')?.textContent).toContain(`0 / ${CUSTOM_TOPIC_TITLE_MAX_CHARS}`)
    expect(host.querySelector('[data-custom-background-count]')?.textContent).toContain(`0 / ${CUSTOM_TOPIC_BACKGROUND_MAX_CHARS}`)
    // 未填标题时不能提交。
    expect(button(host, '创建会话').disabled).toBe(true)
  })

  it('计数按 Unicode 码点：补充平面字符算一个，组合字符算多个', async () => {
    const { host } = await mount(async () => json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    const title = textarea(host, '[data-custom-title]')
    type(title, '𠮷𠮷𠮷'); await nextTick()
    expect(host.querySelector('[data-custom-title-count]')?.textContent).toContain('3 / 200')
    type(title, 'e\u0301e\u0301'); await nextTick()
    expect(host.querySelector('[data-custom-title-count]')?.textContent).toContain('4 / 200')
    // .length 会把补充平面字符算成 2，这里必须与后端码点口径一致。
    expect(title.value.length).toBe(4)
  })

  it.each([
    { name: '标题 201 码点', title: '题'.repeat(201), background: '', ok: false },
    { name: '标题 200 码点', title: '题'.repeat(200), background: '', ok: true },
    { name: '纯空白标题', title: '　 \ufeff', background: '', ok: false },
    { name: '空白包裹的有效标题', title: '  有效题目  ', background: '', ok: true },
    { name: '背景 1001 码点', title: '题', background: '背'.repeat(1001), ok: false },
    { name: '背景 1000 码点', title: '题', background: '背'.repeat(1000), ok: true },
  ])('边界：$name', async ({ title, background, ok }) => {
    const { host, created } = await mount(async (_url, init) => init?.method === 'POST'
      ? json(customDebate(title, background), 201) : json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), title)
    type(textarea(host, '[data-custom-background]'), background)
    await nextTick()
    expect(button(host, '创建会话').disabled).toBe(!ok)
    if (!ok) {
      // 超限时保留输入并提示，不静默截断。
      expect(textarea(host, '[data-custom-title]').value).toBe(title)
      expect(textarea(host, '[data-custom-background]').value).toBe(background)
      submit(host); await nextTick()
      expect(created).not.toHaveBeenCalled()
      return
    }
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
  })

  it('提交只带 custom_topic，不带 topic_id，并保留原文与空白', async () => {
    const title = '  是否应延长图书馆开放时间？\n'
    const background = '背景：期末周复习需求上升。\t'
    const { host, created } = await mount(async (_url, init) => init?.method === 'POST'
      ? json(customDebate(title, background), 201) : json({ items: topics }))
    await loaded(host)
    // 先在预设来源下选一个题，验证来源切到自定义后不会把 topic_id 一起发出。
    host.querySelector<HTMLInputElement>('input[value="2"]')!.click(); await nextTick()
    await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), title)
    type(textarea(host, '[data-custom-background]'), background)
    await nextTick()
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(post[0]).toBe('/api/v1/debates')
    const body = JSON.parse(String(post[1]?.body))
    expect(body).toEqual({ custom_topic: { title, background }, user_stance: 'pro', ai_style: 'logical' })
    expect('topic_id' in body).toBe(false)
    expect(new Headers(post[1]?.headers).get('X-CSRF-Token')).toBe('csrf-test')
  })

  it('背景可省略，请求里不出现空的 background 字段', async () => {
    const { host, created } = await mount(async (_url, init) => init?.method === 'POST'
      ? json(customDebate('仅标题', ''), 201) : json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), '仅标题'); await nextTick()
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
    const body = JSON.parse(String(fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')![1]?.body))
    expect(body.custom_topic).toEqual({ title: '仅标题', background: '' })
  })

  it('标题与背景都按纯文本预览，不渲染 HTML', async () => {
    const { host } = await mount(async () => json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), '<img src=x onerror=unsafe>')
    type(textarea(host, '[data-custom-background]'), '<script>unsafe</script>')
    await nextTick()
    const preview = host.querySelector('[data-topic-preview]')!
    expect(preview.textContent).toContain('<img src=x onerror=unsafe>')
    expect(preview.querySelector('img,script')).toBeNull()
  })

  it('题库为空或加载失败时仍可填写并提交自定义题', async () => {
    for (const [handler, expected] of [
      [async () => json({ items: [] }), '暂无可用辩题'],
      [async () => failure(503, 'dependency_unavailable'), '服务暂时不可用'],
    ] as const) {
      const { host, created, app } = await mount(handler)
      await vi.waitFor(() => expect(host.textContent).toContain(expected))
      // 加载完成（不是仍在加载）后才切换来源，否则禁用状态会拦住交互。
      await vi.waitFor(() => expect(button(host, '创建会话').disabled).toBe(true))
      await chooseCustom(host)
      type(textarea(host, '[data-custom-title]'), '题库不可用时的自定义题'); await nextTick()
      expect(button(host, '创建会话').disabled).toBe(false)
      fetchMock.mockImplementationOnce(async (_url, init) => init?.method === 'POST'
        ? json(customDebate('题库不可用时的自定义题', ''), 201) : json({ items: [] }))
      submit(host)
      await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
      app.unmount(); mounted.splice(mounted.findIndex(item => item.app === app), 1); host.remove()
    }
  })

  it('服务端明确拒绝时保留草稿并允许修正后重试', async () => {
    let posts = 0
    const title = '  会被临时拒绝的题目  '
    const { host, created } = await mount(async (_url, init) => {
      if (init?.method !== 'POST') return json({ items: topics })
      return ++posts === 1 ? failure(400, 'invalid_request') : json(customDebate(title, ''), 201)
    })
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), title)
    await nextTick(); submit(host)
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(created).not.toHaveBeenCalled()
    expect(textarea(host, '[data-custom-title]').value).toBe(title)
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
  })

  it('切换账号清理草稿与来源，不把旧用户输入带到新视图', async () => {
    const { host, auth } = await mount(async (_url, init) => init?.method === 'POST' ? json(customDebate('题', ''), 201) : json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), '旧用户草稿')
    type(textarea(host, '[data-custom-background]'), '旧用户背景')
    await nextTick()
    fetchMock.mockImplementation(async (url) => {
      if (String(url).endsWith('/auth/logout')) return new Response(null, { status: 204 })
      return json({ items: topics })
    })
    await auth.logout(); await nextTick()
    expect(host.querySelector('[data-custom-title]')).toBeNull()
    fetchMock.mockResolvedValueOnce(json({ user: { id: '43', username: 'bob' }, csrf_token: 'csrf-bob' }))
    await auth.login({ username: 'bob', password: 'example-password' })
    await vi.waitFor(() => expect(host.textContent).toContain('bob'))
    await loaded(host)
    await chooseCustom(host)
    expect(textarea(host, '[data-custom-title]').value).toBe('')
    expect(textarea(host, '[data-custom-background]').value).toBe('')
    expect(host.querySelector<HTMLInputElement>('input[data-source="custom"]')?.checked).toBe(true)
  })

  it('切换回预设来源后只提交 topic_id，不再带自定义字段', async () => {
    const { host, created } = await mount(async (_url, init) => init?.method === 'POST'
      ? json({ ...customDebate('预设', ''), id: '123', topic: { id: '2', title: '辩题 2', description: '说明 2' } }, 201)
      : json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), '临时草稿')
    await nextTick()
    host.querySelector<HTMLInputElement>('input[data-source="preset"]')!.click(); await nextTick()
    host.querySelector<HTMLInputElement>('input[value="2"]')!.click(); await nextTick()
    // 切回预设后草稿仍保留在状态里，但不会进入请求。
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
    const body = JSON.parse(String(fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')![1]?.body))
    expect(body).toEqual({ topic_id: '2', user_stance: 'pro', ai_style: 'logical' })
    expect('custom_topic' in body).toBe(false)
  })

  it('创建响应中的空 topic.id 被接受，不会误判为格式错误', async () => {
    const { host, created } = await mount(async (_url, init) => init?.method === 'POST'
      ? json(customDebate('空 ID 会话', '背景'), 201) : json({ items: topics }))
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), '空 ID 会话'); await nextTick()
    submit(host)
    await vi.waitFor(() => expect(created).toHaveBeenCalledOnce())
    expect(created.mock.calls[0]![0].topic.id).toBeNull()
  })

  it('请求结果不明时锁定创建并提示先查历史，不自动重发', async () => {
    const { host, created, openHistory } = await mount(async (_url, init) => {
      if (init?.method === 'POST') throw new ApiError('timeout', '请求超时，请稍后手动重试。')
      return json({ items: topics })
    })
    await loaded(host); await chooseCustom(host)
    type(textarea(host, '[data-custom-title]'), '结果不明的题目'); await nextTick()
    submit(host)
    await vi.waitFor(() => expect(host.textContent).toContain('创建结果尚未确认'))
    expect(host.textContent).not.toContain('手动重试')
    expect(textarea(host, '[data-custom-title]').value).toBe('结果不明的题目')
    submit(host); await nextTick()
    expect(created).not.toHaveBeenCalled()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
    button(host, '查看历史会话').click(); expect(openHistory).toHaveBeenCalledOnce()
  })
})
