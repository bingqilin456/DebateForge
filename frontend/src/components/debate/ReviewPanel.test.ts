// @vitest-environment jsdom
import { createApp, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ReviewPanel from './ReviewPanel.vue'
import type { ReviewContent, ReviewDimension, ReviewStatus } from '../../types/api'

/**
 * P7 复盘面板渲染测试：
 * - 失败态独立展示、比赛完成状态不受影响；
 * - 四维 1–4 与观察不足并列可读，观察不足不按 0 画；
 * - 雷达缺项留空、文字卡片始终可读；
 * - 原文定位未接入/已接入两种降级，局部重练与 PDF 明确标为待整合；
 * - 纯文本渲染，不执行模型 HTML。
 */

const mounted: Array<{ app: App; host: HTMLDivElement }> = []

function quote(text = '我方认为开放式考核更能检验真实能力', overrides: Partial<ReviewContent['dimensions'][number]['quote']> = {}) {
  return { message_id: '101', quote: text, start_cp: 0, end_cp: [...text].length, resolved: true, ...overrides } as never
}

function dimension(key: ReviewDimension['key'], label: string, level: 1 | 2 | 3 | 4 | null, text = '依据'): ReviewDimension {
  return level === null
    ? { key, label, level: null, level_label: null, basis: text, improvement: '', insufficient_reason: '本场缺少足够观察机会', quote: null }
    : { key, label, level, level_label: { 1: '起步', 2: '发展中', 3: '较稳', 4: '充分' }[level]!, basis: text, improvement: '改进提示', insufficient_reason: null, quote: quote() }
}

function content(overrides: Partial<ReviewContent> = {}): ReviewContent {
  return {
    dimensions: [
      dimension('argument_structure', '论证结构', 3),
      dimension('responsiveness', '回应针对性', null),
      dimension('evidence_use', '证据使用', 2),
      dimension('concept_scope', '概念与范围', null),
    ],
    key_issues: [{ text: '主要争点在于标准是否可检验', quote: quote() }],
    strengths: [{ text: '把主张落在可检验标准上', quote: quote() }],
    improvements: [{ text: '先回应标准质疑再收束主线', quote: quote() }],
    ...overrides,
  }
}

const completed: ReviewStatus = { status: 'completed', attempt_version: 1, schema_version: 1, content: content() }

async function mount(review: ReviewStatus = completed, options: {
  locate?: (messageId: string, startCp: number, endCp: number) => void
  repractice?: (text: string) => void
  busy?: boolean
  canGenerate?: boolean
  canRetry?: boolean
} = {}) {
  const host = document.createElement('div')
  document.body.append(host)
  const events = { generate: 0, retry: 0, refresh: 0 }
  const app = createApp(() => h(ReviewPanel, {
    review,
    busy: options.busy ?? false,
    canGenerate: options.canGenerate ?? true,
    canRetry: options.canRetry ?? true,
    locate: options.locate,
    repractice: options.repractice,
    onGenerate: () => { events.generate++ },
    onRetry: () => { events.retry++ },
    onRefresh: () => { events.refresh++ },
  }))
  app.mount(host)
  mounted.push({ app, host })
  await nextTick()
  return { host, events }
}

function button(host: HTMLElement, selector: string): HTMLButtonElement {
  const element = host.querySelector<HTMLButtonElement>(selector)
  if (!element) throw new Error(`缺少按钮 ${selector}`)
  return element
}

beforeEach(() => { document.body.innerHTML = '' })
afterEach(() => {
  for (const { app } of mounted.splice(0)) app.unmount()
  document.body.innerHTML = ''
})

describe('ReviewPanel', () => {
  it('未生成时只给手动入口，并说明原文仍可读', async () => {
    const { host, events } = await mount({ status: 'not_started', attempt_version: null })
    expect(host.textContent).toContain('复盘尚未生成')
    expect(host.textContent).toContain('系统不会自动触发')
    button(host, '[data-generate-review]').click()
    await nextTick()
    expect(events.generate).toBe(1)
    expect(host.querySelector('[data-review-completed]')).toBeNull()
  })

  it('失败态独立展示、可重试，且不出现“比赛未完成”的说法', async () => {
    const { host, events } = await mount({ status: 'failed', attempt_version: 2, error_code: 'model_timeout' })
    expect(host.textContent).toContain('复盘生成失败')
    expect(host.textContent).toContain('model_timeout')
    expect(host.textContent).toContain('比赛仍为已完成')
    expect(host.textContent).not.toContain('比赛未完成')
    button(host, '[data-retry-review]').click()
    await nextTick()
    expect(events.retry).toBe(1)
  })

  it('生成中禁用重复点击并说明不会重复生成', async () => {
    const { host } = await mount({ status: 'generating', attempt_version: 1 })
    expect(host.textContent).toContain('正在生成复盘')
    expect(host.textContent).toContain('重复点击不会触发新的生成')
    expect(host.querySelector('[data-generate-review]')).toBeNull()
    expect(host.querySelector('[data-retry-review]')).toBeNull()
  })

  it('四维卡片同时展示档级与观察不足，观察不足写原因且不带档级', async () => {
    const { host } = await mount()
    const cards = host.querySelectorAll('[data-dimension]')
    expect(cards.length).toBe(4)
    expect(host.querySelector('[data-dimension="argument_structure"] [data-level="3"]')?.textContent).toContain('较稳')
    const insufficient = host.querySelector('[data-dimension="responsiveness"] [data-level="insufficient"]')
    expect(insufficient?.textContent).toContain('观察不足')
    expect(host.querySelector('[data-dimension="responsiveness"]')?.textContent).toContain('为什么观察不足')
    // 观察不足不得被渲染成 0 档或 1 档。
    expect(host.querySelector('[data-dimension="responsiveness"] [data-level="0"]')).toBeNull()
    expect(host.querySelector('[data-dimension="responsiveness"] [data-level="1"]')).toBeNull()
    expect(host.querySelector('[data-review-gap-note]')?.textContent).toContain('不是 0 分')
  })

  it('雷达保留四轴图例，观察不足轴不画点', async () => {
    const { host } = await mount()
    expect(host.querySelector('[data-review-radar]')).not.toBeNull()
    const legend = host.querySelector('[data-review-legend]')!.textContent ?? ''
    expect(legend).toContain('论证结构')
    expect(legend).toContain('观察不足')
    // 只有两个已观察维度 → 两个实心点；观察不足维度用虚线缺口标记。
    expect(host.querySelectorAll('[data-review-radar] .radar-dot').length).toBe(2)
    expect(host.querySelectorAll('[data-review-radar] .radar-gap').length).toBe(2)
  })

  it('全部观察不足时仍给出四张可读卡片，不补档', async () => {
    const allGaps = content({
      dimensions: [
        dimension('argument_structure', '论证结构', null),
        dimension('responsiveness', '回应针对性', null),
        dimension('evidence_use', '证据使用', null),
        dimension('concept_scope', '概念与范围', null),
      ],
    })
    const { host } = await mount({ status: 'completed', attempt_version: 1, schema_version: 1, content: allGaps })
    expect(host.querySelectorAll('[data-dimension]').length).toBe(4)
    expect(host.querySelectorAll('[data-level="insufficient"]').length).toBe(4)
    expect(host.querySelectorAll('[data-review-radar] .radar-dot').length).toBe(0)
    expect(host.querySelector('[data-review-gap-note]')).not.toBeNull()
  })

  it('争议点与优点保留文字；原文定位未接入时显示待整合而不是死链', async () => {
    const { host } = await mount()
    expect(host.querySelector('[data-review-group="key_issues"]')?.textContent).toContain('标准是否可检验')
    expect(host.querySelector('[data-review-group="strengths"]')?.textContent).toContain('可检验标准')
    expect(host.querySelector('[data-pending-entry="repractice-argument_structure"]')?.textContent).toContain('局部重练待整合')
    expect(host.querySelector('[data-locate-quote="argument_structure"]')).toBeNull()
    expect(button(host, '[data-pdf-pending]').disabled).toBe(true)
  })

  it('雷达轴图例提供键盘可达的原文定位，观察不足保持空轴', async () => {
    const locate = vi.fn()
    const { host } = await mount(completed, { locate })
    // 已观察且已定位的维度是原生 button，可用键盘聚焦与触发。
    const legendButton = host.querySelector<HTMLButtonElement>('[data-legend-locate="argument_structure"]')
    expect(legendButton).not.toBeNull()
    expect(legendButton!.tagName).toBe('BUTTON')
    legendButton!.click()
    expect(locate).toHaveBeenCalledWith('101', 0, [...'我方认为开放式考核更能检验真实能力'].length)
    // 观察不足维度没有定位入口，不伪造成 0 档按钮。
    expect(host.querySelector('[data-legend-locate="responsiveness"]')).toBeNull()
    expect(host.querySelector('[data-legend-static="responsiveness"]')?.textContent).toContain('观察不足')
  })

  it('接入定位与重练时调用对应回调，并把码点坐标原样传出', async () => {
    const locate = vi.fn()
    const repractice = vi.fn()
    const { host } = await mount(completed, { locate, repractice })
    button(host, '[data-locate-quote="argument_structure"]').click()
    expect(locate).toHaveBeenCalledWith('101', 0, [...'我方认为开放式考核更能检验真实能力'].length)
    button(host, '[data-repractice="argument_structure"]').click()
    expect(repractice).toHaveBeenCalledWith('改进提示', expect.objectContaining({ resolved: true }))
  })

  it('未定位的引用只显示待核对，不提供定位按钮', async () => {
    const unresolved = content({
      dimensions: [
        { ...dimension('argument_structure', '论证结构', 3), quote: quote('我方认为开放式考核更能检验真实能力', { resolved: false, start_cp: null, end_cp: null }) },
        dimension('responsiveness', '回应针对性', null),
        dimension('evidence_use', '证据使用', 2),
        dimension('concept_scope', '概念与范围', null),
      ],
    })
    const { host } = await mount({ status: 'completed', attempt_version: 1, schema_version: 1, content: unresolved })
    expect(host.querySelector('[data-quote-unresolved="argument_structure"]')?.textContent).toContain('原文待核对')
    expect(host.querySelector('[data-locate-quote="argument_structure"]')).toBeNull()
  })

  it('模型文本按纯文本渲染，不执行 HTML', async () => {
    const escaped = content({
      dimensions: [
        { ...dimension('argument_structure', '论证结构', 3), basis: '<img src=x onerror=alert(1)>' },
        dimension('responsiveness', '回应针对性', null),
        dimension('evidence_use', '证据使用', 2),
        dimension('concept_scope', '概念与范围', null),
      ],
    })
    const { host } = await mount({ status: 'completed', attempt_version: 1, schema_version: 1, content: escaped })
    expect(host.querySelector('img')).toBeNull()
    expect(host.textContent).toContain('<img src=x onerror=alert(1)>')
  })

  it('刷新按钮始终可用，生成/重试按 canX 禁用', async () => {
    const { host, events } = await mount({ status: 'failed', attempt_version: 1, error_code: 'model_error' }, { canRetry: false })
    expect(button(host, '[data-retry-review]').disabled).toBe(true)
    button(host, '[data-refresh-review]').click()
    await nextTick()
    expect(events.refresh).toBe(1)
  })
})
