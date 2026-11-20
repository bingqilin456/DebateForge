// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { locateQuoteText } from './locateQuote'

/**
 * 复盘与争点卡共用的原文定位：
 * - 坐标是 Unicode 码点半开区间，按码点切片，不按 UTF-16 下标；
 * - 命中后把引用文本写在整段原文节点上（视觉见 styles/theme.css），滚入视口并聚焦；
 * - 越界、非法区间或找不到原文时不猜位置、不改变已有落点。
 *
 * 行级视觉（整段一个外框）由浏览器用例核对，这里只验证落点语义。
 */

function mountMessage(id: string, content: string): HTMLParagraphElement {
  const article = document.createElement('article')
  article.dataset.messageId = id
  const paragraph = document.createElement('p')
  paragraph.dataset.messageContent = ''
  paragraph.tabIndex = -1
  paragraph.textContent = content
  article.append(paragraph)
  document.body.append(article)
  return paragraph
}

let scrollSpy: ReturnType<typeof vi.fn>

beforeEach(() => {
  document.body.innerHTML = ''
  scrollSpy = vi.fn()
  HTMLElement.prototype.scrollIntoView = scrollSpy as unknown as () => void
})

afterEach(() => {
  document.body.innerHTML = ''
  delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollIntoView
})

describe('共享原文定位', () => {
  it('按码点半开区间标注整段原文，并滚动聚焦该段', () => {
    const content = '第一轮用户发言：评价标准应当可检验，否则双方无法比较。'
    const paragraph = mountMessage('101', content)

    const hit = locateQuoteText(document.body, '101', 8, 14)

    expect(hit).not.toBeNull()
    expect(hit!.element).toBe(paragraph)
    expect(hit!.quote).toBe('评价标准应当')
    expect(paragraph.dataset.locatedQuote).toBe('评价标准应当')
    expect(scrollSpy).toHaveBeenCalledWith({ block: 'center' })
    expect(document.activeElement).toBe(paragraph)
  })

  it('代理对与组合字符按码点计数，不按 UTF-16 下标', () => {
    // “😀”占两个 UTF-16 单元但只算一个码点；“e”+组合重音是两个码点。
    const content = '甲😀e\u0301乙'
    expect(Array.from(content)).toHaveLength(5)
    const paragraph = mountMessage('102', content)

    expect(locateQuoteText(document.body, '102', 1, 2)?.quote).toBe('😀')
    expect(paragraph.dataset.locatedQuote).toBe('😀')
    expect(locateQuoteText(document.body, '102', 2, 5)?.quote).toBe('e\u0301乙')
    expect(paragraph.dataset.locatedQuote).toBe('e\u0301乙')
  })

  it('越界、空区间与非整数坐标都不标注，也不移动视口', () => {
    mountMessage('103', '甲乙丙丁')
    for (const [start, end] of [[-1, 2], [0, 0], [2, 1], [0, 5], [0.5, 2], [1, Number.NaN]]) {
      expect(locateQuoteText(document.body, '103', start as number, end as number)).toBeNull()
    }
    expect(document.querySelectorAll('[data-located-quote]')).toHaveLength(0)
    expect(scrollSpy).not.toHaveBeenCalled()
  })

  it('切换目标时只保留一处落点；找不到目标时不清除已有落点', () => {
    const first = mountMessage('104', '第一段原文')
    const second = mountMessage('105', '第二段原文')

    locateQuoteText(document.body, '104', 0, 3)
    expect(first.dataset.locatedQuote).toBe('第一段')

    locateQuoteText(document.body, '105', 0, 3)
    expect(first.dataset.locatedQuote).toBeUndefined()
    expect(second.dataset.locatedQuote).toBe('第二段')
    expect(document.querySelectorAll('[data-located-quote]')).toHaveLength(1)

    // 目标不存在时按“原文待核对”处理：不猜位置，也不清掉上一次可见落点。
    expect(locateQuoteText(document.body, '999', 0, 1)).toBeNull()
    expect(second.dataset.locatedQuote).toBe('第二段')
  })

  it('按 data-message-id 精确匹配，不拼接 CSS 选择器', () => {
    // ID 含引号/方括号时，字符串拼接选择器会失效或注入；这里必须仍精确命中。
    const tricky = mountMessage('10"1]', '特殊 ID 的原文')
    expect(locateQuoteText(document.body, '10"1]', 0, 2)?.element).toBe(tricky)
    expect(tricky.dataset.locatedQuote).toBe('特殊')

    // 缺根节点或空 ID 时直接失败，不抛异常。
    expect(locateQuoteText(null, '10"1]', 0, 2)).toBeNull()
    expect(locateQuoteText(document.body, '', 0, 2)).toBeNull()
  })
})
