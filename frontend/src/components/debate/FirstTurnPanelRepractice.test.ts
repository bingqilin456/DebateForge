// @vitest-environment jsdom
import { createApp, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import FirstTurnPanel from './FirstTurnPanel.vue'
import { createAuthState } from '../../auth/state'
import type { DebateDetail, RepracticeVersion, Turn } from '../../types/api'

/**
 * V2-08 局部重练在工作台里的接入回归。
 *
 * 覆盖：
 * 1. 主动选段：完赛后从本人发言选中一段 → 点“选句重练” → 面板出现原句，且不触发任何 POST。
 * 2. 复盘原文入口复用同一重练入口：改进提示带入目标、原句带入选段。
 * 3. 提交失败：保留改写并展示失败版本；按当前 attempt 显式重试同一版本。
 * 4. 历史版本恢复：填回编辑区且不产生请求，再次提交才新建版本。
 * 5. 能力关闭（available=false）：整块重练区隐藏，不出现任何入口。
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

function turn(roundNo: number): Turn {
  return {
    id: String(100 + roundNo),
    round_no: roundNo,
    client_request_id: `550e8400-e29b-41d4-a716-44665544000${roundNo}`,
    status: 'completed',
    attempt_version: 1,
    error_code: null,
    messages: [
      { id: String(200 + roundNo * 2), role: 'user', content: roundNo === 2 ? '甲😀乙第 1 轮发言原文' : `甲😀乙第 ${roundNo} 轮发言原文`, created_at: time },
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

function source(quote: string, startCp = 0) {
  return { message_id: '202', start_cp: startCp, end_cp: startCp + [...quote].length, quote }
}

function version(overrides: Partial<RepracticeVersion> = {}): RepracticeVersion {
  return {
    id: '1',
    status: 'completed',
    attempt_version: 1,
    source: source('甲😀乙'),
    goal: '',
    rewrite: '改写内容',
    feedback: { improved: true, change: '补了依据', reason: '理由更直接', new_problem: '' },
    error_code: null,
    restored_from: null,
    ...overrides,
  } as RepracticeVersion
}

interface Harness {
  versions: RepracticeVersion[]
  available: boolean
  submitted: Array<Record<string, unknown>>
  retried: Array<{ id: string; body: Record<string, unknown> }>
  submitResult?: RepracticeVersion
  review?: Record<string, unknown>
}

const session = { user: { id: '42', username: 'browser_user' }, csrf_token: 'synthetic-csrf' }

async function mount(harness: Harness) {
  fetcher.mockImplementation(async (input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url.endsWith('/auth/me')) return json(session)
    if (url.endsWith('/strategy')) return json({ status: 'not_started', attempt_version: null })
    if (url.endsWith('/annotations')) return json({ items: [] })
    if (url.endsWith('/review')) return json(harness.review ?? { status: 'not_started', attempt_version: null })
    if (url.endsWith('/repractice')) {
      if (method === 'GET') return json({ available: harness.available, items: harness.versions })
      const body = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>
      harness.submitted.push(body)
      return json(harness.submitResult ?? version({ id: '2' }))
    }
    if (url.includes('/repractice/') && url.endsWith('/retry')) {
      const body = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>
      harness.retried.push({ id: url, body })
      return json(version({ id: url.split('/').at(-2) ?? '1', status: 'completed', attempt_version: 2 }))
    }
    if (url.endsWith('/debates/123')) return json(detail())
    throw new Error(`不允许的测试请求: ${method} ${url}`)
  })
  host = document.createElement('div')
  document.body.append(host)
  const auth = createAuthState()
  app = createApp(h(FirstTurnPanel, { auth, route: '#debate=123' }))
  app.mount(host)
  // 写接口要求已恢复登录（CSRF）；读完会话后再等布局就绪。
  await auth.restore()
  await vi.waitFor(() => expect(host.querySelector('[data-summary-panel]')).not.toBeNull())
  await nextTick()
}

/** 在指定用户消息里模拟浏览器选区：按码点切出 quote。 */
async function selectInUserMessage(messageId: string, quote: string) {
  const paragraph = host.querySelector<HTMLElement>(`[data-message-id="${messageId}"] [data-message-content]`)
  if (!paragraph) throw new Error(`缺少用户消息 ${messageId} 的原文`)
  const text = paragraph.textContent ?? ''
  const codepoints = [...text]
  // 先按码点找起点，避免把 UTF-16 下标当成码点下标（emoji 会错位）。
  const codePoints = [...quote]
  let startCp = -1
  for (let index = 0; index + codePoints.length <= codepoints.length; index++) {
    if (codepoints.slice(index, index + codePoints.length).join('') === quote) { startCp = index; break }
  }
  if (startCp < 0) throw new Error('原文里找不到要选的片段')
  const target = codePoints.length
  // 逐字节点可能被批注切分；用文本节点定位，保证与用户真实选区一致。
  const walker = document.createTreeWalker(paragraph, NodeFilter.SHOW_TEXT)
  const nodes: Text[] = []
  let node = walker.nextNode()
  while (node) {
    if ((node.textContent ?? '').length > 0) nodes.push(node as Text)
    node = walker.nextNode()
  }
  let startNode: Text | null = null
  let endNode: Text | null = null
  let startOffset = 0
  let endOffset = 0
  let consumedCp = 0
  for (const current of nodes) {
    const runes = [...(current.textContent ?? '')]
    if (!startNode && consumedCp + runes.length > startCp) {
      startNode = current
      // Range 偏移是 UTF-16 下标：把节点内的码点偏移换算成 UTF-16 长度。
      startOffset = runes.slice(0, startCp - consumedCp).join('').length
    }
    if (startNode && consumedCp + runes.length >= startCp + target) {
      endNode = current
      endOffset = runes.slice(0, startCp + target - consumedCp).join('').length
      break
    }
    consumedCp += runes.length
  }
  if (!startNode || !endNode) throw new Error('无法在原文里定位选区')
  const range = document.createRange()
  range.setStart(startNode, startOffset)
  range.setEnd(endNode, endOffset)
  const selection = window.getSelection()!
  selection.removeAllRanges()
  selection.addRange(range)
  await nextTick()
}

async function click(selector: string) {
  const element = host.querySelector<HTMLElement>(selector)
  if (!element) throw new Error(`缺少元素 ${selector}`)
  element.click()
  await nextTick()
}
/** Arco 输入组件把 data 属性放在外层包装上，真正的输入元素是内部 input/textarea。 */
function inputInside(selector: string): HTMLInputElement | HTMLTextAreaElement | null {
  const wrapper = host.querySelector<HTMLElement>(selector)
  return wrapper?.querySelector<HTMLInputElement | HTMLTextAreaElement>('input, textarea')
    ?? wrapper as HTMLInputElement | null
}

/** Arco 输入组件把值写进内部 input/textarea；用原生事件触发更新。 */
async function type(selector: string, value: string) {
  const element = inputInside(selector)
  if (!element) throw new Error(`缺少输入 ${selector}`)
  element.value = value
  element.dispatchEvent(new Event('input', { bubbles: true }))
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

it('主动选中本人原句进入重练，打开面板不触发生成', async () => {
  const harness: Harness = { versions: [], available: true, submitted: [], retried: [] }
  await mount(harness)
  expect(host.querySelector('[data-repractice-panel]')).not.toBeNull()
  expect(host.querySelector('[data-repractice-quote]')).toBeNull()

  await selectInUserMessage('202', '甲😀乙')
  await click('[data-select-quote]')
  expect(host.querySelector('[data-repractice-quote]')?.textContent).toBe('甲😀乙')
  // 选段只是本地状态：没有任何 POST。
  expect(harness.submitted).toHaveLength(0)
  expect(harness.retried).toHaveLength(0)
})

it('相同原句出现在两条发言时仍绑定用户实际选中的消息', async () => {
  const harness: Harness = { versions: [], available: true, submitted: [], retried: [] }
  await mount(harness)
  await selectInUserMessage('204', '甲😀乙第 1 轮发言原文')
  await click('[data-select-quote]')
  expect(host.querySelector('[data-repractice-quote]')?.textContent).toBe('甲😀乙第 1 轮发言原文')
  await type('[data-repractice-rewrite]', '换一版')
  await click('[data-repractice-submit]')
  await vi.waitFor(() => expect(harness.submitted).toHaveLength(1))
  expect(harness.submitted[0]!.message_id).toBe('204')
})

it('未选中任何原文时给出可操作提示，而不是静默失败', async () => {
  const harness: Harness = { versions: [], available: true, submitted: [], retried: [] }
  await mount(harness)
  window.getSelection()?.removeAllRanges()
  await click('[data-select-quote]')
  expect(host.querySelector('[data-selection-notice]')?.textContent).toContain('请先在本人发言中选中一段原文')
  expect(host.querySelector('[data-repractice-quote]')).toBeNull()
})

it('复盘原文入口复用同一重练入口：带入原句与改进提示', async () => {
  const quote = { message_id: '202', quote: '甲😀乙', start_cp: 0, end_cp: 3, resolved: true }
  const review = {
    status: 'completed', attempt_version: 1, schema_version: 1,
    content: {
      dimensions: [
        { key: 'argument_structure', label: '论证结构', level: 3, level_label: '较稳', basis: '有理由', improvement: '补一句依据', insufficient_reason: null, quote },
        { key: 'responsiveness', label: '回应针对性', level: null, level_label: null, basis: '机会有限', improvement: '', insufficient_reason: '本场机会不足', quote: null },
        { key: 'evidence_use', label: '证据使用', level: 2, level_label: '发展中', basis: '有例子', improvement: '说明范围', insufficient_reason: null, quote },
        { key: 'concept_scope', label: '概念与范围', level: null, level_label: null, basis: '未展开', improvement: '', insufficient_reason: '未出现边界情形', quote: null },
      ],
      key_issues: [{ text: '争点', quote }],
      strengths: [{ text: '优点', quote }],
      improvements: [{ text: '改进', quote }],
    },
  }
  const harness: Harness = { versions: [], available: true, submitted: [], retried: [], review }
  await mount(harness)
  await vi.waitFor(() => expect(host.querySelector('[data-repractice="argument_structure"]')).not.toBeNull())
  await click('[data-repractice="argument_structure"]')
  expect(host.querySelector('[data-repractice-quote]')?.textContent).toBe('甲😀乙')
  const goal = inputInside('[data-repractice-goal]')
  expect(goal?.value).toBe('补一句依据')
  await click('[data-repractice-issue]')
  expect(host.querySelector('[data-repractice-quote]')?.textContent).toBe('甲😀乙')
  // 从复盘进入也只在本地填表，不触发生成。
  expect(harness.submitted).toHaveLength(0)
})

it('提交失败保留改写并显示失败版本，重试只推进同一版本', async () => {
  const failed = version({
    id: '7', status: 'failed', attempt_version: 1, rewrite: '未生成反馈的改写',
    feedback: null, error_code: 'model_timeout',
  } as Partial<RepracticeVersion>)
  const harness: Harness = { versions: [], available: true, submitted: [], retried: [], submitResult: failed }
  await mount(harness)
  await selectInUserMessage('202', '甲😀乙')
  await click('[data-select-quote]')
  await type('[data-repractice-rewrite]', '未生成反馈的改写')
  await click('[data-repractice-submit]')
  await vi.waitFor(() => expect(host.querySelector('[data-repractice-failure]')).not.toBeNull())
  expect(host.textContent).toContain('反馈未生成，改写已保留')
  expect(host.querySelector('[data-repractice-version="7"]')).not.toBeNull()
  // 失败版本保留改写文本，便于刷新后继续。
  expect(host.querySelector('[data-repractice-rewrite-text="7"]')?.textContent).toContain('未生成反馈的改写')

  await click('[data-repractice-retry="7"]')
  await vi.waitFor(() => expect(harness.retried).toHaveLength(1))
  expect(harness.retried[0]!.body).toEqual({ expected_attempt_version: 1 })
  // 重试成功后同一版本更新为 completed 反馈，不新增版本行。
  await vi.waitFor(() => expect(host.textContent).toContain('理由：'))
  expect(host.querySelector('[data-repractice-version="7"]')?.className).toContain('is-completed')
  expect(host.querySelectorAll('[data-repractice-version]')).toHaveLength(1)
})

it('历史版本可恢复到编辑区，且恢复本身不创建新版本', async () => {
  const existing = version({ id: '5', status: 'completed', rewrite: '历史版本的改写' })
  const harness: Harness = { versions: [existing], available: true, submitted: [], retried: [] }
  await mount(harness)
  await vi.waitFor(() => expect(host.querySelector('[data-repractice-restore="5"]')).not.toBeNull())
  await click('[data-repractice-restore="5"]')
  expect(inputInside('[data-repractice-rewrite]')?.value).toBe('历史版本的改写')
  expect(host.querySelector('[data-repractice-quote]')?.textContent).toBe('甲😀乙')
  expect(host.querySelector('[data-repractice-restored]')).not.toBeNull()
  // 恢复只是填回输入框：不产生提交请求。
  expect(harness.submitted).toHaveLength(0)

  await click('[data-repractice-submit]')
  await vi.waitFor(() => expect(harness.submitted).toHaveLength(1))
  expect(harness.submitted[0]!.restored_from).toBe('5')
})

it('能力关闭时整块重练区隐藏，不出现任何入口', async () => {
  const harness: Harness = { versions: [], available: false, submitted: [], retried: [] }
  await mount(harness)
  expect(host.querySelector('[data-repractice-panel]')).toBeNull()
  expect(host.querySelector('[data-repractice-entry]')).toBeNull()
  expect(host.querySelector('[data-select-quote]')).toBeNull()
  expect(host.textContent).not.toContain('局部重练')
})

