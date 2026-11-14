// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HistoryView from './HistoryView.vue'
import { createAuthState } from '../auth/state'
import type { Debate, DebateListResult } from '../types/api'

const fetchMock = vi.fn<typeof fetch>()
const mounted: Array<{ app: App, host: HTMLDivElement }> = []
function debate(id = '11', completed = false): Debate {
  return { id, topic: { id: '7', title: `历史辩题 ${id}`, description: '历史快照' }, user_stance: completed ? 'con' : 'pro', ai_stance: completed ? 'pro' : 'con', status: completed ? 'completed' : 'active', completed_rounds: completed ? 3 : 1, created_at: '2026-11-11T23:05:06Z', updated_at: '2026-11-12T00:05:06Z' }
}
function json(data: unknown) {
  return new Response(JSON.stringify({ data, request_id: 'req-history' }), { headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-history' } })
}
function list(items = [debate()], page = 1, pageSize = 20, total = items.length) {
  return json({ items, total, page, page_size: pageSize } satisfies DebateListResult)
}
function failure(status = 503, code = 'dependency_unavailable') {
  return new Response(JSON.stringify({ error: { code, message: 'private upstream detail' }, request_id: 'req-history' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-history' },
  })
}
function deferred() {
  let resolve!: (response: Response) => void
  const promise = new Promise<Response>(done => { resolve = done })
  return { promise, resolve }
}
async function mount(handler: typeof fetch, pageSize?: number, disabled = false) {
  const auth = createAuthState()
  fetchMock.mockResolvedValueOnce(json({ user: { id: '42', username: 'alice' }, csrf_token: 'csrf-history' }))
  await auth.login({ username: 'alice', password: 'example-password' })
  fetchMock.mockReset().mockImplementation(handler)
  const controls = reactive<{ disabled: boolean, pageSize?: number }>({ disabled, pageSize })
  const selected = vi.fn(), openDebate = vi.fn(), openTopics = vi.fn(), back = vi.fn()
  const host = document.createElement('div'); document.body.append(host)
  const app = createApp(() => h(HistoryView, { auth, pageSize: controls.pageSize, disabled: controls.disabled, 'onSelect-debate': selected, 'onOpen-debate': openDebate, 'onOpen-topics': openTopics, 'onBack-to-debate': back }))
  app.mount(host); mounted.push({ app, host })
  return { app, host, auth, controls, selected, openDebate, openTopics, back }
}
function button(host: HTMLElement, text: string) {
  const result = [...host.querySelectorAll('button')].find(node => node.textContent?.includes(text))
  if (!result) throw new Error(`缺少按钮 ${text}`)
  return result
}
function query(input: Parameters<typeof fetch>[0]) { return new URL(String(input), 'http://127.0.0.1').searchParams }
async function loaded(host: HTMLElement, ids = ['11']) {
  await vi.waitFor(() => expect([...host.querySelectorAll('[data-history-id]')].map(node => node.getAttribute('data-history-id'))).toEqual(ids))
}
beforeEach(() => { fetchMock.mockReset(); vi.stubGlobal('fetch', fetchMock) })
afterEach(() => { for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }; vi.unstubAllGlobals() })

describe('T11 本人历史列表', () => {
  it('默认请求第 1 页每页 20 条，显示题目、双方立场、状态、轮数及本地时间', async () => {
    const pending = deferred()
    const { host } = await mount(() => pending.promise)
    expect(host.textContent).toContain('正在加载历史')
    pending.resolve(list([debate(), debate('12', true)]))
    await loaded(host, ['11', '12'])
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/debates?page=1&page_size=20')
    expect(host.textContent).toContain('正方'); expect(host.textContent).toContain('反方')
    expect(host.textContent).toContain('进行中'); expect(host.textContent).toContain('已完成')
    expect(host.textContent).toContain('1 / 3'); expect(host.textContent).toContain('3 / 3')
    const time = host.querySelector<HTMLTimeElement>('time[datetime="2026-11-11T23:05:06Z"]')!
    expect(time.textContent).toBe(new Date(debate().created_at).toLocaleString('zh-CN', { hour12: false }))
    expect(time.textContent).not.toBe(debate().created_at)
  })

  it('空列表显示引导且禁用前后翻页，不误报失败', async () => {
    const { host, openTopics } = await mount(async () => list([]))
    await vi.waitFor(() => expect(host.textContent).toContain('暂无历史会话'))
    expect(host.querySelector('[role="alert"]')).toBeNull()
    expect(button(host, '上一页').disabled).toBe(true); expect(button(host, '下一页').disabled).toBe(true)
    button(host, '前往辩题列表').click(); expect(openTopics).toHaveBeenCalledOnce()
  })

  it('翻页使用 pageSize，加载中双击不会重复请求，也不显示上一页记录', async () => {
    const pending = deferred()
    const { host } = await mount(async input => query(input).get('page') === '2' ? pending.promise : list([debate('11'), debate('12')], 1, 2, 3), 2)
    await loaded(host, ['11', '12'])
    button(host, '下一页').click(); button(host, '下一页').click(); await nextTick()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/debates?page=2&page_size=2')
    expect(host.querySelectorAll('[data-history-id]')).toHaveLength(0)
    expect(button(host, '上一页').disabled).toBe(true)
    pending.resolve(list([debate('13')], 2, 2, 3))
    await loaded(host, ['13'])
    expect(button(host, '下一页').disabled).toBe(true)
    button(host, '上一页').click(); await loaded(host, ['11', '12'])
  })

  it('刷新保留当前页并展示更新后的数据', async () => {
    let secondPageCalls = 0
    const { host } = await mount(async input => query(input).get('page') === '2'
      ? list([debate('13', ++secondPageCalls > 1)], 2, 2, 3) : list([debate('11'), debate('12')], 1, 2, 3), 2)
    await loaded(host, ['11', '12']); button(host, '下一页').click(); await loaded(host, ['13'])
    button(host, '刷新历史').click()
    await vi.waitFor(() => expect(host.querySelector('[data-history-id="13"]')?.textContent).toContain('已完成'))
    expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/v1/debates?page=2&page_size=2')
    expect(secondPageCalls).toBe(2)
  })

  it('加载失败不冒充空历史，且只在手动操作后重试', async () => {
    let calls = 0
    const { host } = await mount(async () => ++calls === 1 ? failure() : list())
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')?.textContent).toContain('暂时不可用'))
    expect(host.textContent).not.toContain('暂无历史会话'); expect(host.textContent).not.toContain('private')
    expect(fetchMock).toHaveBeenCalledOnce()
    button(host, '重试加载历史').click(); await loaded(host)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('某页失败后重试仍请求该页，不把旧页当新页显示', async () => {
    let secondPageCalls = 0
    const { host } = await mount(async input => query(input).get('page') === '2'
      ? ++secondPageCalls === 1 ? failure() : list([debate('13')], 2, 2, 3)
      : list([debate('11'), debate('12')], 1, 2, 3), 2)
    await loaded(host, ['11', '12']); button(host, '下一页').click()
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.querySelectorAll('[data-history-id]')).toHaveLength(0)
    button(host, '重试加载历史').click(); await loaded(host, ['13'])
    expect(secondPageCalls).toBe(2)
  })

  it('数据缩减使请求页越界时，只补查新的末页，不进行无限轮询', async () => {
    let calls = 0
    const { host } = await mount(async input => {
      calls++
      if (query(input).get('page') === '2') return list([], 2, 2, 1)
      return calls === 1 ? list([debate('11'), debate('12')], 1, 2, 3) : list([debate()], 1, 2, 1)
    }, 2)
    await loaded(host, ['11', '12']); button(host, '下一页').click(); await loaded(host)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(button(host, '上一页').disabled).toBe(true); expect(button(host, '下一页').disabled).toBe(true)
  })

  it('点击记录只发一次 select-debate，不同时发 open-debate 或读取详情', async () => {
    const { host, selected, openDebate } = await mount(async () => list())
    await loaded(host)
    const record = host.querySelector<HTMLButtonElement>('[data-debate-id="11"]')!
    record.click(); record.click(); await nextTick()
    expect(selected).toHaveBeenCalledExactlyOnceWith('11')
    expect(openDebate).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('pageSize 改变时从第 1 页重新加载', async () => {
    const { host, controls } = await mount(async input => list([debate()], Number(query(input).get('page')), Number(query(input).get('page_size')), 3), 2)
    await loaded(host); button(host, '下一页').click()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    controls.pageSize = 1
    await vi.waitFor(() => expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/v1/debates?page=1&page_size=1'))
    await loaded(host)
  })

  it('无效 pageSize 由共享 API 拒绝，不向后端发请求', async () => {
    const { host } = await mount(async () => list(), 0)
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')?.textContent).toContain('分页参数无效'))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('disabled 阻止加载/选择/导航，解除后再加载', async () => {
    const { host, controls, selected, openTopics, back } = await mount(async () => list(), 20, true)
    expect(fetchMock).not.toHaveBeenCalled()
    button(host, '前往辩题列表').click(); button(host, '返回辩论主视图').click()
    expect(openTopics).not.toHaveBeenCalled(); expect(back).not.toHaveBeenCalled()
    controls.disabled = false; await loaded(host)
    controls.disabled = true; await nextTick()
    host.querySelector<HTMLButtonElement>('[data-debate-id="11"]')!.click()
    expect(selected).not.toHaveBeenCalled()
  })

  it('历史题目只按文本展示，不执行 HTML', async () => {
    const item = debate(); item.topic.title = '<img src=x onerror=unsafe>'
    const { host } = await mount(async () => list([item]))
    await loaded(host)
    expect(host.textContent).toContain(item.topic.title); expect(host.querySelector('img')).toBeNull()
  })

  it('Session 失效时清空旧历史，不把 401 显示成空列表', async () => {
    let calls = 0
    const { host, auth } = await mount(async () => ++calls === 1 ? list() : failure(401, 'unauthenticated'))
    await loaded(host); button(host, '刷新历史').click()
    await vi.waitFor(() => expect(auth.state.status).toBe('anonymous'))
    expect(host.querySelectorAll('[data-history-id]')).toHaveLength(0)
    expect(host.textContent).toContain('请先登录'); expect(host.textContent).not.toContain('暂无历史会话')
  })

  it('切换账号后丢弃旧账号的迟到历史响应', async () => {
    const pending = deferred()
    let lists = 0
    const { host, auth } = await mount(async (input) => String(input).endsWith('/auth/login')
      ? json({ user: { id: '43', username: 'bob' }, csrf_token: 'csrf-bob' })
      : ++lists === 1 ? pending.promise : list([debate('22')]))
    await auth.login({ username: 'bob', password: 'example-password' })
    await loaded(host, ['22'])
    pending.resolve(list([debate('11')]))
    await new Promise(resolve => setTimeout(resolve, 0)); await nextTick()
    expect(host.querySelector('[data-history-id="11"]')).toBeNull()
    expect(host.querySelector('[data-history-id="22"]')).not.toBeNull()
  })
})
