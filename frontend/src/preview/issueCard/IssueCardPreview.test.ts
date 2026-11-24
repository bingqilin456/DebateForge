// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IssueCardPreview from './IssueCardPreview.vue'
import { createIssueCardPreviewMock, memoryPreviewStorage, type IssueCardPreviewMock } from './mockIssueCard'

/**
 * 争点卡 / 复盘共用的原文定位渲染回归：
 * - 定位语义仍复用正式工作台的 `locateQuoteText`：`data-located-quote` 写在整段原文上，负责滚动与聚焦；
 * - 预览只给精确片段 `.located-fragment` 一个 inline 外框，整段原文不显示可见外框（CSS 由浏览器用例核对）；
 * - 片段内部逐字节点保留码点坐标，但不描边、不加定位 class；
 * - 片段文本严格等于已复核码点区间，不包整条发言、不新增空格；
 * - 待核对与非法/非规范定位键都不改变上一次成功落点，只提示待核对；
 * - 逐字节点重构后选句重练的码点换算不变。
 *
 * 这里只验证 DOM 结构与语义；行级视觉（同一行一个框）由浏览器用例核对。
 */

const mounted: Array<{ app: App; host: HTMLDivElement }> = []

type Selected = { messageId: string; start: number; end: number; quote: string }

async function mount(options: { integrated?: boolean } = {}) {
  const mock: IssueCardPreviewMock = createIssueCardPreviewMock(memoryPreviewStorage())
  const selected: Selected[] = []
  const host = document.createElement('div')
  document.body.append(host)
  const preview = ref<InstanceType<typeof IssueCardPreview> | null>(null)
  const app = createApp(() => h(IssueCardPreview, {
    mock,
    integrated: options.integrated ?? false,
    ref: preview,
    onSelectSource: (value: Selected) => { selected.push(value) },
  }))
  app.mount(host)
  mounted.push({ app, host })
  await vi.waitFor(() => expect(host.querySelectorAll('[data-issue-card]').length).toBe(5))
  return { host, mock, preview, selected }
}

function citationMarkers(host: HTMLElement): HTMLButtonElement[] {
  return [...host.querySelectorAll<HTMLButtonElement>('[data-issue-card-citation]')]
}

function locateButton(host: HTMLElement): HTMLButtonElement {
  const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(element => element.textContent?.includes('选句重练'))
  if (!button) throw new Error('缺少“选句重练”按钮')
  return button
}

beforeEach(() => { document.body.innerHTML = '' })
afterEach(() => {
  for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }
  document.body.innerHTML = ''
  window.getSelection()?.removeAllRanges()
})

describe('争点卡原文定位复用共享定位语义', () => {
  it('整段原文只保留定位语义，精确片段外层才有一个外框', async () => {
    const { host, mock } = await mount()
    // 未定位时既没有定位 attribute，也没有片段外框。
    expect(host.querySelectorAll('[data-located-quote]').length).toBe(0)
    expect(host.querySelectorAll('[data-located-run]').length).toBe(0)

    const marker = citationMarkers(host)[0]!
    const quote = marker.textContent ?? ''
    expect(Array.from(quote).length).toBeGreaterThan(1)
    marker.click()
    // 定位语义与片段外框在同一次定位里就位。
    await vi.waitFor(() => {
      expect(host.querySelectorAll('[data-located-quote]').length).toBe(1)
      expect(host.querySelectorAll('[data-located-run]').length).toBe(1)
    })

    // 整段原文保留 data-located-quote 语义：共享定位用它滚动/聚焦；预览的可见外框由 scoped CSS 取消。
    const paragraph = host.querySelector<HTMLElement>('.message.user .message-text')!
    expect(paragraph.dataset.locatedQuote).toBe(quote)
    expect(paragraph.textContent).toBe(mock.contents['101'])

    // 精确片段承载唯一的外框 class，不重复承载整段定位 attribute。
    const run = host.querySelector<HTMLElement>('[data-located-run]')!
    expect(run.classList.contains('located-fragment')).toBe(true)
    expect(run.classList.contains('is-located')).toBe(false)
    expect(run.hasAttribute('data-located-quote')).toBe(false)
    expect(run.textContent).toBe(quote)
    expect(run.textContent).not.toBe(mock.contents['101'])
    expect(host.querySelectorAll('.located-fragment').length).toBe(1)

    // 内部逐字坐标节点仍在，但不描边、不加片段外框 class。
    const points = [...run.querySelectorAll<HTMLElement>('[data-preview-index]')]
    expect(points.length).toBe(Array.from(quote).length)
    expect(points.every(point => !point.classList.contains('located-fragment'))).toBe(true)
    expect(points.every(point => !point.classList.contains('is-located'))).toBe(true)
    expect(points.every(point => point.dataset.previewMessage === '101')).toBe(true)

    // 精确码点范围：下标连续；正文没有新增空格（所有文字都在逐字节点里）。
    const indices = points.map(point => Number(point.dataset.previewIndex))
    expect(indices).toEqual(indices.map((_, offset) => offset + indices[0]!))
    expect([...paragraph.querySelectorAll<HTMLElement>('[data-preview-index]')]
      .map(point => point.textContent).join('')).toBe(paragraph.textContent)

    // 其他发言、其他卡片都不受影响。
    expect(host.querySelector('.message.assistant [data-located-quote]')).toBeNull()
    expect(paragraph.querySelectorAll('[data-located-run]').length).toBe(1)
  })

  it('切换定位时只保留一处片段外框，并滚动聚焦到该段', async () => {
    const { host } = await mount()
    // jsdom 无 scrollIntoView，打桩以验证确实滚动到定位段落。
    const scrollSpy = vi.fn()
    const originalScroll = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollSpy
    try {
      const markers = citationMarkers(host)
      markers[0]!.click()
      await vi.waitFor(() => expect(host.querySelectorAll('[data-located-quote]').length).toBe(1))
      const userParagraph = host.querySelector<HTMLElement>('.message.user .message-text')!
      expect(userParagraph.dataset.locatedQuote).toBeTruthy()

      markers[1]!.click()
      // 旧落点要等新定位真正落位才消失，所以直接等目标段落拿到 attribute。
      const assistantParagraph = host.querySelector<HTMLElement>('.message.assistant .message-text')!
      await vi.waitFor(() => expect(assistantParagraph.dataset.locatedQuote).toBeTruthy())
      expect(userParagraph.hasAttribute('data-located-quote')).toBe(false)
      expect(host.querySelectorAll('[data-located-quote]').length).toBe(1)
      expect(scrollSpy).toHaveBeenCalledWith({ block: 'center' })
      expect(document.activeElement).toBe(assistantParagraph)
    } finally {
      if (originalScroll) HTMLElement.prototype.scrollIntoView = originalScroll
      else delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollIntoView
    }
  })

  it('待核对引用与非法定位键都不产生落点', async () => {
    const { host, preview } = await mount()
    // 第 5 张卡的提出原文“抽样”重复歧义：只显示待核对，不给定位入口。
    const ambiguous = host.querySelectorAll<HTMLElement>('[data-issue-card]')[4]!
    expect(ambiguous.textContent).toContain('原文待核对')
    expect(ambiguous.querySelectorAll('[data-issue-card-citation]').length).toBe(1)
    expect(host.querySelectorAll('[data-located-quote]').length).toBe(0)
    expect(host.querySelectorAll('[data-located-run]').length).toBe(0)

    // 规范之外的键一律拒绝：空值不能被 Number('') 当成 0，缺段/多段/带符号/前导零也都不猜位置。
    const invalidKeys = ['', '||', '101|999|1000', '101|', '101||3', '101|+1|3', '101|01|3', '101| 1|3', '101|1', '101|1|3|4']
    for (const key of invalidKeys) {
      await preview.value!.locate(key)
      await nextTick()
      expect(host.querySelectorAll('[data-located-quote]').length).toBe(0)
      expect(host.querySelectorAll('[data-located-run]').length).toBe(0)
      expect(host.querySelector('.notice')?.textContent).toContain('无法唯一定位')
    }
  })

  it('定位失败保留上一次成功的高亮，只更新待核对提示', async () => {
    const { host, preview } = await mount()
    const marker = citationMarkers(host)[0]!
    const quote = marker.textContent ?? ''
    marker.click()
    await vi.waitFor(() => expect(host.querySelectorAll('[data-located-run]').length).toBe(1))
    const paragraph = host.querySelector<HTMLElement>('.message.user .message-text')!
    const run = host.querySelector<HTMLElement>('[data-located-run]')!
    expect(paragraph.dataset.locatedQuote).toBe(quote)
    expect(run.textContent).toBe(quote)

    // 失败不先清空 located：整段 attribute 与片段外框同进同退，不会只剩一半。
    for (const key of ['101|999|1000', '101||3', '101|+1|3', '101|01|3', '', 'missing']) {
      await preview.value!.locate(key)
      await nextTick()
      expect(paragraph.dataset.locatedQuote).toBe(quote)
      expect(host.querySelectorAll('[data-located-quote]').length).toBe(1)
      expect(host.querySelectorAll('[data-located-run]').length).toBe(1)
      expect(run.textContent).toBe(quote)
      expect(host.querySelector('.notice')?.textContent).toContain('无法唯一定位')
    }
  })

  it('逐字节点与外层片段不影响选句重练的码点换算', async () => {
    const { host, mock, selected } = await mount({ integrated: true })
    const article = host.querySelector<HTMLElement>('article.user')!
    const paragraph = article.querySelector<HTMLElement>('.message-text')!
    const points = [...paragraph.querySelectorAll<HTMLElement>('[data-preview-index]')]
    // 选中夹具原文的第 9–13 个码点（“评价标准应当”）。
    const range = document.createRange()
    range.setStartBefore(points[8]!)
    range.setEndAfter(points[12]!)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)

    locateButton(host).click()
    await nextTick()
    expect(selected).toHaveLength(1)
    expect(selected[0]!.start).toBe(8)
    expect(selected[0]!.end).toBe(13)
    expect(selected[0]!.quote).toBe([...mock.contents['101']!].slice(8, 13).join(''))
  })
})
