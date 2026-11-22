// @vitest-environment jsdom
import { createApp, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import FirstTurnPanel from './FirstTurnPanel.vue'
import { createAuthState } from '../../auth/state'
import type { DebateDetail, Turn } from '../../types/api'

/**
 * 关闭复盘切片时的界面回归：GET /review 返回 not_started + available:false，
 * 工作台必须完全不渲染复盘区、不出现任何生成入口，也不显示“无复盘”误导文案。
 * 反之，省略 available（旧响应/Mock）时必须保持原有完整面板。
 */

const fetcher = vi.fn<typeof fetch>()
const time = '2026-11-19T00:00:00Z'
let app: App | undefined
let host: HTMLDivElement

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'ui' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'ui' },
  })
}

// 完赛四轮：isDetail 要求 completed_rounds 与实际 completed 轮数一致，否则详情被拒。
function turn(roundNo: number): Turn {
  return {
    id: String(100 + roundNo),
    round_no: roundNo,
    client_request_id: `550e8400-e29b-41d4-a716-44665544000${roundNo}`,
    status: 'completed',
    attempt_version: 1,
    error_code: null,
    messages: [
      { id: String(200 + roundNo * 2), role: 'user', content: `第 ${roundNo} 轮用户原文`, created_at: time },
      { id: String(201 + roundNo * 2), role: 'assistant', content: `第 ${roundNo} 轮 AI 回复`, created_at: time },
    ],
  }
}

function detail(): DebateDetail {
  return {
    debate: {
      id: '123', topic: { id: '1', title: '开放考核', description: '说明' },
      user_stance: 'pro', ai_stance: 'con', ai_style: 'logical', mode: 'standard_4',
      total_rounds: 4, status: 'completed', completed_rounds: 4, created_at: time, updated_at: time,
    },
    turns: [turn(1), turn(2), turn(3), turn(4)],
    summary_status: { status: 'not_started', attempt_version: null },
  }
}

async function mount(review: unknown) {
  fetcher.mockImplementation(async input => {
    const url = String(input)
    if (url.endsWith('/review')) return json(review)
    // 局部重练是独立切片：本文件只验证复盘降级，重练按关闭态返回。
    if (url.endsWith('/repractice')) return json({ available: false, items: [] })
    if (url.endsWith('/strategy')) return json({ status: 'not_started', attempt_version: null })
    if (url.endsWith('/annotations')) return json({ items: [] })
    if (url.endsWith('/debates/123')) return json(detail())
    throw new Error(`不允许的测试请求: ${url}`)
  })
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(h(FirstTurnPanel, { auth: createAuthState(), route: '#debate=123' }))
  app.mount(host)
  await vi.waitFor(() => expect(host.querySelector('[data-summary-panel]')).not.toBeNull())
  await nextTick()
}

beforeEach(() => {
  fetcher.mockReset()
  vi.stubGlobal('fetch', fetcher)
  history.replaceState(null, '', '/#debate=123')
})
afterEach(() => {
  app?.unmount()
  host?.remove()
  vi.unstubAllGlobals()
})

it('available:false 时隐藏复盘区并禁止生成入口', async () => {
  await mount({ status: 'not_started', attempt_version: null, available: false })
  expect(host.querySelector('[data-review-panel]')).toBeNull()
  expect(host.querySelector('[data-generate-review]')).toBeNull()
  expect(host.querySelector('[data-review-notice]')).toBeNull()
  expect(host.textContent).not.toContain('赛后复盘')
  // 关闭复盘不影响完赛后的原文与总结区。
  expect(host.querySelector('[data-summary-panel]')).not.toBeNull()
  expect(host.textContent).toContain('第 1 轮用户原文')
})

it('省略 available 的旧响应仍展示完整复盘面板', async () => {
  await mount({ status: 'not_started', attempt_version: null })
  expect(host.querySelector('[data-review-panel]')).not.toBeNull()
  await vi.waitFor(() => expect(host.querySelector('[data-generate-review]')).not.toBeNull())
})
