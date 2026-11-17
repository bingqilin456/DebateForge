// @vitest-environment jsdom
import { createApp, h, nextTick, type App as VueApp } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { createAuthState } from './auth/state'
import type { AIStyle, CreateDebateRequest, Debate } from './types/api'

const fetcher = vi.fn<typeof fetch>()
const topic = { id: '1', title: '流派验收辩题', description: '说明' }
let app: VueApp | undefined
let host: HTMLDivElement
function reply(data: unknown, status = 200) { return new Response(JSON.stringify({ data, request_id: 'req-style' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-style' } }) }
function saved(ai_style: AIStyle): Debate { return { id: '123', topic, user_stance: 'pro', ai_stance: 'con', mode: 'standard_4', total_rounds: 4, ai_style, status: 'active', completed_rounds: 0, created_at: '2026-11-15T00:00:00Z', updated_at: '2026-11-15T00:00:00Z' } }
async function mount(returnedStyle?: AIStyle) {
  let current = saved(returnedStyle ?? 'logical')
  const bodies: CreateDebateRequest[] = []
  fetcher.mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.endsWith('/auth/me')) return reply({ user: { id: '42', username: 'alice' }, csrf_token: 'memory-csrf' })
    if (url.endsWith('/topics')) return reply({ items: [topic] })
    if (url.endsWith('/strategy')) return reply({ status: 'not_started', attempt_version: null })
    if (url.startsWith('/api/v1/debates?')) return reply({ items: [current], total: 1, page: 1, page_size: 20 })
    if (url.endsWith('/debates/123')) return reply({ debate: current, turns: [], summary_status: { status: 'not_started', attempt_version: null } })
    if (url === '/api/v1/debates' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as CreateDebateRequest
      bodies.push(body); current = saved(returnedStyle ?? body.ai_style ?? 'logical')
      return reply(current, 201)
    }
    throw new Error('不允许的测试请求')
  })
  host = document.createElement('div'); document.body.append(host)
  app = createApp(h(App, { auth: createAuthState() })); app.mount(host)
  await vi.waitFor(() => expect(host.querySelector('[data-topic-id="1"] input')).not.toBeNull())
  return bodies
}
function button(name: string) { const node = [...host.querySelectorAll('button')].find(item => item.textContent?.trim() === name); if (!node) throw new Error(`缺少 ${name}`); return node }
beforeEach(() => { vi.stubGlobal('fetch', fetcher); history.replaceState(null, '', '/') })
afterEach(() => { app?.unmount(); host?.remove(); vi.unstubAllGlobals() })

describe('T14 统一创建入口与保存值展示', () => {
  it.each(['logical', 'aggressive', 'empathic', 'data'] as const)('辩题列表创建提交 %s，保留单次交接', async style => {
    const bodies = await mount()
    host.querySelector<HTMLButtonElement>('[data-view-tab="topics"]')!.click()
    await vi.waitFor(() => expect(host.querySelector('[data-topic-id="1"]')).not.toBeNull())
    host.querySelector<HTMLInputElement>('[data-topic-id="1"] input')!.click()
    const select = host.querySelector<HTMLSelectElement>('select[name="ai_style"]')!
    expect(select).not.toBeNull(); expect(select.value).toBe('logical')
    select.value = style; select.dispatchEvent(new Event('change', { bubbles: true })); await nextTick()
    const form = host.querySelector('form')!
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(host.querySelector('[data-ai-style]')?.getAttribute('data-ai-style')).toBe(style))
    expect(bodies).toHaveLength(1); expect(bodies[0]).toEqual({ topic_id: '1', user_stance: 'pro', ai_style: style })
  })
  it('详情和历史展示后端保存值，而不是未保存的表单变量', async () => {
    await mount('data')
    host.querySelector<HTMLInputElement>('[data-topic-id="1"] input')!.click(); await nextTick()
    button('创建会话').click()
    await vi.waitFor(() => expect(host.querySelector('[data-ai-style="data"]')).not.toBeNull())
    host.querySelector<HTMLButtonElement>('[data-view-tab="history"]')!.click()
    await vi.waitFor(() => expect(host.querySelector('[data-history-id="123"] [data-ai-style="data"]')).not.toBeNull())
    host.querySelector<HTMLButtonElement>('[data-debate-id="123"]')!.click()
    await vi.waitFor(() => expect(host.querySelector('[aria-label="首轮辩论面板"] [data-ai-style="data"]')).not.toBeNull())
  })
})
