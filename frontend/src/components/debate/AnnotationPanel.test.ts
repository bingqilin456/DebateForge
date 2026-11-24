// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AnnotationPanel from './AnnotationPanel.vue'
import { resetGraphemeSegmenterCache } from '../../debate/annotations'
import AnnotatedText from './AnnotatedText.vue'
import type { AnnotationApi } from '../../api/annotations'
import { ApiError } from '../../api/client'
import type { AnnotationBatch, AnnotationItem, DebateDetail } from '../../types/api'

/**
 * 批注界面测试：开关只控展示、0 条措辞、轻失败与手动补做、Unicode 不错标、
 * 重叠共用入口、待核对降级、转义与键盘可达。全部使用桩 API，不发真实请求。
 */

const mounted: Array<{ app: App; host: HTMLDivElement }> = []

function anchor(overrides: Partial<AnnotationItem['anchor']> = {}): AnnotationItem['anchor'] {
  return { message_id: '101', quote: '标准应当可检验', start_cp: 0, end_cp: 7, resolved: true, ...overrides }
}

function item(overrides: Partial<AnnotationItem> = {}): AnnotationItem {
  return {
    category: 'strength', label: '论证有效', reason: '把主张落在可检验的标准上，因此论证有效。',
    improvement: '补一句该标准的适用范围。', anchor: anchor(), turn_no: 1, role: 'user', ...overrides,
  }
}

function batch(overrides: Partial<AnnotationBatch> = {}): AnnotationBatch {
  return { turn_id: '101', round_no: 1, status: 'completed', attempt_version: 1, items: [item()], ...overrides }
}

function detail(content = '标准应当可检验', id = '7'): DebateDetail {
  return {
    debate: {
      id, topic: { id: '1', title: '开放考核', description: '说明' }, user_stance: 'pro', ai_stance: 'con',
      mode: 'standard_4', total_rounds: 4, ai_style: 'logical', status: 'active', completed_rounds: 1,
      created_at: '2026-11-18T00:00:00Z', updated_at: '2026-11-18T00:00:00Z',
    },
    turns: [{
      id: '101', round_no: 1, client_request_id: '550e8400-e29b-41d4-a716-446655440000',
      status: 'completed', attempt_version: 1, error_code: null,
      messages: [
        { id: '101', role: 'user', content, created_at: '2026-11-18T00:00:00Z' },
        { id: '102', role: 'assistant', content: 'AI 回复原文', created_at: '2026-11-18T00:00:00Z' },
      ],
    }],
    summary_status: { status: 'not_started', attempt_version: null },
  }
}

function api(list: () => Promise<{ items: AnnotationBatch[] }>, retry?: AnnotationApi['retry']): AnnotationApi {
  return { list: vi.fn(list), retry: retry ?? vi.fn() }
}

async function mountPanel(options: {
  api: AnnotationApi
  detail?: DebateDetail
  disabled?: boolean
}) {
  const host = document.createElement('div')
  document.body.append(host)
  const located: string[] = []
  const panelRef = ref<InstanceType<typeof AnnotationPanel> | null>(null)
  const app = createApp(() => h(AnnotationPanel, {
    ref: panelRef,
    detail: options.detail ?? detail(),
    api: options.api,
    disabled: options.disabled ?? false,
    onLocate: (key: string) => located.push(key),
  }))
  app.mount(host)
  mounted.push({ app, host })
  await vi.waitFor(() => expect(host.textContent).not.toContain('正在读取批注'))
  return { host, app, located, panelRef }
}

async function mountText(content: string, items: AnnotationItem[], showMarkers = true) {
  const host = document.createElement('div')
  document.body.append(host)
  const selected: string[][] = []
  const app = createApp(() => h(AnnotatedText, {
    content, items, showMarkers,
    onSelect: (keys: string[]) => selected.push(keys),
  }))
  app.mount(host)
  mounted.push({ app, host })
  await nextTick()
  return { host, selected }
}

beforeEach(() => { vi.clearAllMocks() })
afterEach(() => {
  for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }
})

describe('批注面板状态', () => {
  it('已完成批次展示标签、原句与折叠的解释，理由与改进方向默认不显示', async () => {
    const { host } = await mountPanel({ api: api(async () => ({ items: [batch()] })) })
    expect(host.textContent).toContain('论证有效')
    expect(host.textContent).toContain('标准应当可检验')
    // 解释默认折叠。
    expect(host.textContent).not.toContain('把主张落在可检验的标准上')
    const entry = host.querySelector('[data-annotation-entry]') as HTMLButtonElement
    expect(entry.getAttribute('aria-expanded')).toBe('false')
    entry.click()
    await nextTick()
    expect(entry.getAttribute('aria-expanded')).toBe('true')
    expect(host.textContent).toContain('把主张落在可检验的标准上')
    expect(host.textContent).toContain('补一句该标准的适用范围')
  })

  it('0 条时明确措辞，不暗示没有问题', async () => {
    const { host } = await mountPanel({ api: api(async () => ({ items: [batch({ items: [] })] })) })
    expect(host.textContent).toContain('本次没有批注')
    expect(host.textContent).toContain('不代表没有问题')
  })

  it('旧会话没有批次时显示暂无记录而非错误', async () => {
    const { host } = await mountPanel({ api: api(async () => ({ items: [] })) })
    expect(host.textContent).toContain('本场暂无批注记录')
    expect(host.querySelector('[role="alert"]')).toBeNull()
  })

  it('生成中只做轻提示并说明不阻塞', async () => {
    const { host } = await mountPanel({ api: api(async () => ({ items: [batch({ status: 'generating', items: [] })] })) })
    expect(host.textContent).toContain('批注分析中')
    expect(host.textContent).toContain('不阻塞发言')
  })

  it('失败只显示轻提示与手动补做，且不自动发起补做', async () => {
    const retry = vi.fn()
    const { host } = await mountPanel({
      api: api(async () => ({ items: [batch({ status: 'failed', items: [], error_code: 'model_timeout' })] }), retry),
    })
    expect(host.textContent).toContain('批注未完成')
    expect(host.textContent).toContain('辩论与保存不受影响')
    expect(retry).not.toHaveBeenCalled()
  })

  it('读取失败只显示可重试提示，不渲染任何条目', async () => {
    const { host } = await mountPanel({ api: api(async () => { throw new ApiError('network', '无法连接服务。') }) })
    expect(host.textContent).toContain('无法连接服务')
    expect(host.querySelector('[data-annotation-entry]')).toBeNull()
  })
})

describe('标记渲染', () => {
  it('Unicode 补充平面与组合字符按码点不错标', async () => {
    // 原文含 emoji 与组合字符；引用与坐标都按码点给出。
    const content = '🌱标准应当可检验✅'
    const { host } = await mountText(content, [item({ anchor: anchor({ quote: '标准应当可检验', start_cp: 1, end_cp: 8 }) })])
    const entry = host.querySelector('[data-annotation-markers-entry]') as HTMLButtonElement
    expect(entry).not.toBeNull()
    // 标记文本必须与引用逐字相等，且不吞掉前后的 emoji。
    expect(entry.textContent).toBe('标准应当可检验')
    expect(host.textContent).toContain('🌱')
    expect(host.textContent).toContain('✅')
  })

  it('完全相同区间合并成一个入口并列出全部标签', async () => {
    const { host } = await mountText('甲乙丙丁', [
      item({ category: 'strength', label: '论证有效', anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) }),
      item({ category: 'insufficient_support', label: '缺少支持', anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) }),
    ])
    const entries = host.querySelectorAll('[data-annotation-markers-entry]')
    expect(entries).toHaveLength(1)
    const label = entries[0]!.getAttribute('aria-label') ?? ''
    expect(label).toContain('论证有效')
    expect(label).toContain('缺少支持')
  })

  it('部分重叠切成无重叠段，不产生嵌套标记', async () => {
    const { host } = await mountText('甲乙丙丁戊己庚辛', [
      item({ category: 'strength', label: '论证有效', anchor: anchor({ quote: '丙丁戊己', start_cp: 2, end_cp: 6 }) }),
      item({ category: 'unclear_scope', label: '定义待明确', anchor: anchor({ quote: '戊己庚辛', start_cp: 4, end_cp: 8 }) }),
    ])
    const entries = [...host.querySelectorAll('[data-annotation-markers-entry]')]
    expect(entries.map(node => node.textContent)).toEqual(['丙丁', '戊己', '庚辛'])
    for (const entry of entries) expect(entry.querySelector('[data-annotation-markers-entry]')).toBeNull()
    expect(entries[1]!.getAttribute('aria-label')).toContain('论证有效')
    expect(entries[1]!.getAttribute('aria-label')).toContain('定义待明确')
    expect(host.textContent?.replace(/\s/g, '')).toContain('甲乙丙丁戊己庚辛')
  })

  it('坐标与引用不一致时降级为待核对且不高亮', async () => {
    const { host } = await mountText('甲乙丙丁', [item({ anchor: anchor({ quote: '丙丁', start_cp: 0, end_cp: 2 }) })])
    expect(host.querySelector('[data-annotation-markers-entry]')).toBeNull()
    expect(host.textContent).toContain('原文待核对')
  })

  it('切中字素簇时不标记，改为待核对', async () => {
    const { host } = await mountText('e\u0301x', [item({ anchor: anchor({ quote: 'e', start_cp: 0, end_cp: 1 }) })])
    expect(host.querySelector('[data-annotation-markers-entry]')).toBeNull()
    expect(host.textContent).toContain('原文待核对')
  })

  it('缺少 Intl.Segmenter 时只有整条原文可标，内部区间降级待核对', async () => {
    const original = (Intl as unknown as { Segmenter?: unknown }).Segmenter
    try {
      ;(Intl as unknown as { Segmenter?: unknown }).Segmenter = undefined
      resetGraphemeSegmenterCache()
      const zwj = '👨\u200d👩\u200d👧家庭'
      // 内部区间：无法判定字素边界，必须降级，避免切开 ZWJ。
      const inner = await mountText(zwj, [item({ anchor: anchor({ quote: '👨', start_cp: 0, end_cp: 1 }) })])
      expect(inner.host.querySelector('[data-annotation-markers-entry]')).toBeNull()
      expect(inner.host.textContent).toContain('原文待核对')
      // 整条原文是安全端点，仍可标记。
      const whole = await mountText('短文本', [item({ anchor: anchor({ quote: '短文本', start_cp: 0, end_cp: 3 }) })])
      expect(whole.host.querySelector('[data-annotation-markers-entry]')).not.toBeNull()
    } finally {
      ;(Intl as unknown as { Segmenter?: unknown }).Segmenter = original
      resetGraphemeSegmenterCache()
    }
  })

  it('关闭展示时完全不产生标记入口，原文仍完整渲染', async () => {
    const { host } = await mountText('甲乙丙丁', [item({ anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) })], false)
    expect(host.querySelector('[data-annotation-markers-entry]')).toBeNull()
    expect(host.textContent).toContain('甲乙丙丁')
    expect(host.querySelector('[data-annotation-markers]')?.getAttribute('data-annotation-markers')).toBe('off')
  })

  it('用户原文按纯文本转义，不生成 HTML', async () => {
    const content = '<img src=x onerror=alert(1)>'
    const { host } = await mountText(content, [item({ anchor: anchor({ quote: '<img', start_cp: 0, end_cp: 4 }) })])
    expect(host.querySelector('img')).toBeNull()
    expect(host.textContent).toContain('<img src=x onerror=alert(1)>')
  })

  it('标记是按钮，可键盘聚焦并回传全部同段条目键', async () => {
    const { host, selected } = await mountText('甲乙丙丁', [
      item({ category: 'strength', label: '论证有效', anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) }),
      item({ category: 'unclear_scope', label: '定义待明确', anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) }),
    ])
    const entry = host.querySelector<HTMLElement>('[data-annotation-markers-entry]')!
    entry.focus()
    expect(document.activeElement).toBe(entry)
    entry.click()
    entry.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    const pressSpace = new KeyboardEvent('keydown', { key: ' ', cancelable: true })
    entry.dispatchEvent(pressSpace)
    expect(pressSpace.defaultPrevented).toBe(true)
    expect(selected).toHaveLength(2)
    entry.dispatchEvent(new KeyboardEvent('keyup', { key: ' ' }))
    await nextTick()
    expect(selected).toHaveLength(3)
    expect(selected[1]).toEqual(selected[0])
    expect(selected[2]).toEqual(selected[0])
    expect(selected[0]).toHaveLength(2)
    for (const key of selected[0]!) {
      expect(key).toContain('101')
      // 键只含标识与类别，不含原文文本，因此不可能是注入向量。
      expect(key).not.toContain('丙丁')
    }
  })
})

describe('手动补做', () => {
  it('点击补做只发一次请求，成功后替换该批次', async () => {
    let resolve!: (value: AnnotationBatch) => void
    const pending = new Promise<AnnotationBatch>(done => { resolve = done })
    const retry = vi.fn(() => pending)
    const failed = batch({ status: 'failed', items: [], error_code: 'model_timeout' })
    const { host } = await mountPanel({ api: api(async () => ({ items: [failed] }), retry) })
    const button = host.querySelector('[data-annotation-retry]') as HTMLButtonElement
    expect(button).not.toBeNull()
    button.click()
    button.click()
    await vi.waitFor(() => expect(retry).toHaveBeenCalledTimes(1))
    resolve(batch())
    await vi.waitFor(() => expect(host.textContent).toContain('论证有效'))
  })

  it('补做失败只提示并重读，不自行换版本重试', async () => {
    const retry = vi.fn(async () => { throw new ApiError('http', '重试版本已变化，请查询最新状态后再操作。', { status: 409, code: 'attempt_version_conflict' }) })
    const list = vi.fn(async () => ({ items: [batch({ status: 'failed', items: [], error_code: 'model_timeout' })] }))
    const { host } = await mountPanel({ api: { list, retry } as AnnotationApi })
    ;(host.querySelector('[data-annotation-retry]') as HTMLButtonElement).click()
    await vi.waitFor(() => expect(retry).toHaveBeenCalledTimes(1))
    // 只重新读取，不再补发第二次 POST。
    await vi.waitFor(() => expect(list.mock.calls.length).toBeGreaterThanOrEqual(2))
    expect(retry).toHaveBeenCalledTimes(1)
    // 重读结束后提示仍然保留，用户能看到版本冲突的原因。
    await vi.waitFor(() => expect(host.textContent).toContain('重试版本已变化'))
  })

  it('禁用时补做按钮不可点击', async () => {
    const retry = vi.fn()
    const { host } = await mountPanel({
      api: api(async () => ({ items: [batch({ status: 'failed', items: [], error_code: 'model_timeout' })] }), retry),
      disabled: true,
    })
    const button = host.querySelector('[data-annotation-retry]') as HTMLButtonElement
    expect(button.disabled).toBe(true)
    button.click()
    await nextTick()
    expect(retry).not.toHaveBeenCalled()
  })
})

describe('会话隔离', () => {
  it('切换会话时丢弃旧响应，不把上一场批注带过来', async () => {
    let resolveFirst!: (value: { items: AnnotationBatch[] }) => void
    const first = new Promise<{ items: AnnotationBatch[] }>(done => { resolveFirst = done })
    let call = 0
    const list = vi.fn(async () => {
      call++
      if (call === 1) return first
      return { items: [batch({ turn_id: '202', round_no: 2, items: [item({ label: '缺少支持', category: 'insufficient_support', anchor: anchor({ quote: 'AI 回复原文', start_cp: 0, end_cp: 5, message_id: '102' }) })] })] }
    })
    const host = document.createElement('div')
    document.body.append(host)
    const value = { current: detail() }
    const app = createApp(() => h(AnnotationPanel, { detail: value.current, api: { list, retry: vi.fn() } as AnnotationApi, showMarkers: true }))
    app.mount(host)
    mounted.push({ app, host })
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1))
    // 切到另一场会话。
    value.current = detail('AI 回复原文', '8')
    app.unmount()
    const host2 = document.createElement('div')
    document.body.append(host2)
    const app2 = createApp(() => h(AnnotationPanel, { detail: value.current, api: { list, retry: vi.fn() } as AnnotationApi, showMarkers: true }))
    app2.mount(host2)
    mounted.push({ app: app2, host: host2 })
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    resolveFirst({ items: [batch()] })
    await nextTick()
    // 新会话只显示自己的批注。
    await vi.waitFor(() => expect(host2.textContent).toContain('缺少支持'))
    expect(host2.textContent).not.toContain('论证有效')
  })
})

describe('完成轮数驱动刷新', () => {
  it('新一轮完成后无需切换会话即重新读取批注', async () => {
    const list = vi.fn(async () => ({ items: [batch()] }))
    const host = document.createElement('div')
    document.body.append(host)
    // 必须用响应式 ref：面板 watch 的是 props.detail 的嵌套属性。
    const current = ref<DebateDetail>(detail())
    const app = createApp(() => h(AnnotationPanel, { detail: current.value, api: { list, retry: vi.fn() } as AnnotationApi, showMarkers: true }))
    app.mount(host)
    mounted.push({ app, host })
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1))
    // 模拟首轮提交完成：completed_rounds 增加，面板必须重读。
    const next = detail()
    next.debate.completed_rounds = 2
    current.value = next
    await vi.waitFor(() => expect(list.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('轮数未变化时不额外请求', async () => {
    const list = vi.fn(async () => ({ items: [batch()] }))
    const host = document.createElement('div')
    document.body.append(host)
    const current = ref<DebateDetail>(detail())
    const app = createApp(() => h(AnnotationPanel, { detail: current.value, api: { list, retry: vi.fn() } as AnnotationApi, showMarkers: true }))
    app.mount(host)
    mounted.push({ app, host })
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1))
    // 只换引用、轮数不变：不应触发重读。
    current.value = detail()
    await nextTick()
    expect(list).toHaveBeenCalledTimes(1)
  })

  it('生成中只做有限 GET 轮询，绝不自动 POST', async () => {
    vi.useFakeTimers()
    try {
      let reads = 0
      const list = vi.fn(async () => {
        reads++
        return { items: [batch({ status: reads < 3 ? 'generating' : 'completed', items: reads < 3 ? [] : [item()] })] }
      })
      const retry = vi.fn()
      const host = document.createElement('div')
      document.body.append(host)
      const app = createApp(() => h(AnnotationPanel, { detail: detail(), api: { list, retry } as AnnotationApi, showMarkers: true }))
      app.mount(host)
      mounted.push({ app, host })
      await vi.waitFor(() => expect(reads).toBe(1))
      // 生成中：按间隔继续 GET，直到拿到完成结果。
      await vi.advanceTimersByTimeAsync(2_000)
      await vi.waitFor(() => expect(reads).toBe(2))
      await vi.advanceTimersByTimeAsync(2_000)
      await vi.waitFor(() => expect(reads).toBe(3))
      await vi.waitFor(() => expect(host.textContent).toContain('论证有效'))
      // 完成后不再轮询。
      await vi.advanceTimersByTimeAsync(10_000)
      expect(reads).toBe(3)
      expect(retry).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('卸载后清理定时器，不再发起请求', async () => {
    vi.useFakeTimers()
    try {
      const list = vi.fn(async () => ({ items: [batch({ status: 'generating', items: [] })] }))
      const host = document.createElement('div')
      document.body.append(host)
      const app = createApp(() => h(AnnotationPanel, { detail: detail(), api: { list, retry: vi.fn() } as AnnotationApi, showMarkers: true }))
      app.mount(host)
      await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1))
      app.unmount()
      host.remove()
      mounted.splice(mounted.findIndex(entry => entry.app === app), 1)
      await vi.advanceTimersByTimeAsync(10_000)
      expect(list).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('原句与解释双向定位', () => {
  it('解释入口默认折叠，展开后理由与改进方向可见且可聚焦', async () => {
    const { host } = await mountPanel({ api: api(async () => ({ items: [batch()] })) })
    // 面板自身不含原文副本：原文与高亮由工作台的正式消息流渲染。
    expect(host.querySelector('[data-annotation-markers-entry]')).toBeNull()
    expect(host.querySelector('[data-annotation-markers]')).toBeNull()
    const entry = host.querySelector('[data-annotation-entry]') as HTMLButtonElement
    expect(entry.getAttribute('aria-expanded')).toBe('false')
    entry.click()
    await nextTick()
    const detailNode = host.querySelector('[data-annotation-detail]') as HTMLElement
    expect(detailNode).not.toBeNull()
    // 解释节点可由标记点击后移交焦点。
    expect(detailNode.getAttribute('tabindex')).toBe('-1')
    expect(detailNode.textContent).toContain('把主张落在可检验的标准上')
    expect(detailNode.textContent).toContain('补一句该标准的适用范围')
  })

  it('标记点击通过条目键精确展开对应解释（不拼 CSS 选择器）', async () => {
    const { host } = await mountPanel({ api: api(async () => ({ items: [batch()] })) })
    const entry = host.querySelector('[data-annotation-entry]') as HTMLButtonElement
    const key = entry.getAttribute('data-annotation-entry') ?? ''
    expect(key).not.toBe('')
    // 键里只有服务端标识与类别，没有原文或用户文本。
    expect(key).not.toContain('标准应当可检验')
    // 键不能用作 CSS 选择器的假设由实现保证：定位走 Map 查找，这里验证键本身是纯数据。
    expect(key).not.toMatch(/[[\]()"'\\]/)
  })

  it('展开/收起只改卡片状态，绝不发出 locate；仅「定位原文」按钮跳转', async () => {
    const { host, located } = await mountPanel({ api: api(async () => ({ items: [batch()] })) })
    const entry = host.querySelector('[data-annotation-entry]') as HTMLButtonElement
    entry.click()
    await nextTick()
    expect(entry.getAttribute('aria-expanded')).toBe('true')
    expect(located).toHaveLength(0)
    entry.click()
    await nextTick()
    expect(entry.getAttribute('aria-expanded')).toBe('false')
    expect(located).toHaveLength(0)
    // 重新展开后点显式定位按钮才发出一次 locate。
    entry.click()
    await nextTick()
    const locate = [...host.querySelectorAll('button')].find(el => el.textContent?.includes('定位原文')) as HTMLButtonElement
    expect(locate).not.toBeUndefined()
    locate.click()
    await nextTick()
    expect(located).toEqual([entry.getAttribute('data-annotation-entry')])
  })

  it('focusFromMarker 展开、滚动并聚焦目标解释，鼠标触发也显示方框', async () => {
    const { host, panelRef } = await mountPanel({ api: api(async () => ({ items: [batch()] })) })
    const panel = panelRef.value!
    const entry = host.querySelector('[data-annotation-entry]') as HTMLButtonElement
    const key = entry.getAttribute('data-annotation-entry')!
    // jsdom 无 scrollIntoView，打桩以验证确实滚动到目标解释。
    const scrollSpy = vi.fn()
    const originalScroll = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollSpy
    try {
      await panel.focusFromMarker([key])
      await nextTick()
    } finally {
      if (originalScroll) HTMLElement.prototype.scrollIntoView = originalScroll
      else delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollIntoView
    }
    const detailNode = host.querySelector('[data-annotation-detail]') as HTMLElement
    expect(detailNode).not.toBeNull()
    expect(entry.getAttribute('aria-expanded')).toBe('true')
    expect(scrollSpy).toHaveBeenCalledWith({ block: 'nearest' })
    expect(document.activeElement).toBe(detailNode)
    // 程序化（鼠标路径）焦点也要有可见方框：用状态类而非 :focus-visible。
    expect(detailNode.classList.contains('is-focused')).toBe(true)
    // 焦点移走后收起方框，避免残留高亮。
    detailNode.blur()
    await nextTick()
    expect(detailNode.classList.contains('is-focused')).toBe(false)
  })

  it('重叠标记只突出实际获得焦点的那一条解释', async () => {
    const overlap = batch({
      items: [
        item({ category: 'strength', label: '论证有效', anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) }),
        item({ category: 'unclear_scope', label: '定义待明确', anchor: anchor({ quote: '丙丁', start_cp: 2, end_cp: 4 }) }),
      ],
    })
    const { host, panelRef } = await mountPanel({ api: api(async () => ({ items: [overlap] })) })
    const panel = panelRef.value!
    const entries = [...host.querySelectorAll<HTMLButtonElement>('[data-annotation-entry]')]
    expect(entries).toHaveLength(2)
    const keys = entries.map(el => el.getAttribute('data-annotation-entry')!)
    await panel.focusFromMarker(keys)
    await nextTick()
    const details = [...host.querySelectorAll<HTMLElement>('[data-annotation-detail]')]
    expect(details).toHaveLength(2)
    // 两条都展开，但只有第一条（实际聚焦目标）带方框。
    expect(document.activeElement).toBe(details[0])
    expect(details[0]!.classList.contains('is-focused')).toBe(true)
    expect(details[1]!.classList.contains('is-focused')).toBe(false)
  })
})

describe('未开始批次', () => {
  it('not_started 提供可执行的手动补做', async () => {
    const retry = vi.fn(async () => batch())
    const { host } = await mountPanel({
      api: api(async () => ({ items: [batch({ status: 'not_started', attempt_version: 0, items: [] })] }), retry),
    })
    expect(host.textContent).toContain('批注尚未开始')
    const button = host.querySelector('[data-annotation-retry]') as HTMLButtonElement
    expect(button).not.toBeNull()
    expect(button.disabled).toBe(false)
    button.click()
    await vi.waitFor(() => expect(retry).toHaveBeenCalledWith('7', '101', 0))
    await vi.waitFor(() => expect(host.textContent).toContain('论证有效'))
  })

  it('未开始批次不会被自动补做', async () => {
    const retry = vi.fn()
    await mountPanel({ api: api(async () => ({ items: [batch({ status: 'not_started', attempt_version: 0, items: [] })] }), retry) })
    await nextTick()
    expect(retry).not.toHaveBeenCalled()
  })
})
