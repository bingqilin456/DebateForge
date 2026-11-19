import { describe, expect, it, vi } from 'vitest'
import {
  ANNOTATION_STYLES,
  annotationKey,
  buildAnnotationView,
  buildSegments,
  categoryLabel,
  codepoints,
  isAnnotationCategory,
  graphemeBoundaryState,
  isGraphemeBoundary,
  projectAnnotations,
  resetGraphemeSegmenterCache,
  verifiedRange,
} from './annotations'
import type { AnnotationAnchor, AnnotationItem } from '../types/api'

function anchor(quote: string, start: number | null, end: number | null, resolved = true): AnnotationAnchor {
  return { message_id: '21', quote, start_cp: start, end_cp: end, resolved }
}

function item(category: AnnotationItem['category'], label: string, value: AnnotationAnchor, reason = '理由'): AnnotationItem {
  return { category, label, reason, improvement: '改进方向', anchor: value, turn_no: 1, role: 'user' }
}

describe('批注锚点回读校验', () => {
  it('按 Unicode 码点而不是 UTF-16 单元计算坐标', () => {
    const content = '🌱理由：e\u0301 与 👍🏽 都保留原码点'
    // emoji 与肤色修饰都是独立码点；UTF-16 下标会整体错位。
    expect(codepoints(content)).toHaveLength(18)
    const range = verifiedRange(content, anchor('👍🏽', 9, 11))
    expect(range).toEqual({ start: 9, end: 11 })
    expect(codepoints(content).slice(9, 11).join('')).toBe('👍🏽')
  })

  it('坐标与引用不一致时不给高亮', () => {
    const content = '原文内容'
    expect(verifiedRange(content, anchor('原文', 0, 2))).toEqual({ start: 0, end: 2 })
    // 引用对但坐标错位：回读不一致，降级。
    expect(verifiedRange(content, anchor('原文', 1, 3))).toBeNull()
    // 大小写与归一化不被接受。
    expect(verifiedRange('ABC', anchor('abc', 0, 3))).toBeNull()
    expect(verifiedRange('e\u0301', anchor('é', 0, 1))).toBeNull()
  })

  it('拒绝越界、倒序、非整数与未解析锚点', () => {
    const content = '短文本'
    expect(verifiedRange(content, anchor('短', 0, 99))).toBeNull()
    expect(verifiedRange(content, anchor('短', 2, 1))).toBeNull()
    expect(verifiedRange(content, anchor('短', -1, 1))).toBeNull()
    expect(verifiedRange(content, anchor('短', 0.5, 1))).toBeNull()
    expect(verifiedRange(content, anchor('短', null, null))).toBeNull()
    expect(verifiedRange(content, anchor('短', 0, 1, false))).toBeNull()
  })

  it('切中可见字素时降级为待核对', () => {
    // e + U+0301 是一个字素簇，两个码点；切在中间会渲染破碎。
    const combining = 'e\u0301x'
    expect(isGraphemeBoundary(combining, 0)).toBe(true)
    expect(isGraphemeBoundary(combining, 2)).toBe(true)
    expect(isGraphemeBoundary(combining, 1)).toBe(false)
    expect(verifiedRange(combining, anchor('e', 0, 1))).toBeNull()
    expect(verifiedRange(combining, anchor('e\u0301', 0, 2))).toEqual({ start: 0, end: 2 })

    // ZWJ 家庭序列：四个码点构成一个字素簇。
    const zwj = '👨\u200d👩\u200d👧'
    expect(isGraphemeBoundary(zwj, 1)).toBe(false)
    expect(verifiedRange(zwj, anchor('👨', 0, 1))).toBeNull()
    expect(verifiedRange(zwj, anchor('👨\u200d👩\u200d👧', 0, 5))).toEqual({ start: 0, end: 5 })
  })

  it('缺少 Intl.Segmenter 时安全降级：只有整条原文可标，内部区间待核对', () => {
    const original = (Intl as unknown as { Segmenter?: unknown }).Segmenter
    try {
      // 模拟不支持该 API 的引擎：不能抛错，也不猜内部分段。
      ;(Intl as unknown as { Segmenter?: unknown }).Segmenter = undefined
      resetGraphemeSegmenterCache()
      // 无法判定内部端点：不视为边界，避免切开 ZWJ 或组合字符。
      expect(graphemeBoundaryState('e\u0301x', 1)).toBe('unknown')
      expect(isGraphemeBoundary('e\u0301x', 1)).toBe(false)
      // 首尾是安全端点，任何实现都能确认。
      expect(graphemeBoundaryState('e\u0301x', 0)).toBe('boundary')
      expect(graphemeBoundaryState('e\u0301x', 3)).toBe('boundary')
      // 内部区间降级为待核对。
      expect(verifiedRange('e\u0301x', anchor('e', 0, 1))).toBeNull()
      // 整条原文仍可标记。
      expect(verifiedRange('e\u0301x', anchor('e\u0301x', 0, 3))).toEqual({ start: 0, end: 3 })
      // 回读的逐码点比对仍然生效，降级不会放过内容不一致。
      expect(verifiedRange('e\u0301x', anchor('x', 0, 1))).toBeNull()
    } finally {
      ;(Intl as unknown as { Segmenter?: unknown }).Segmenter = original
      resetGraphemeSegmenterCache()
    }
  })
})

describe('批注分段落', () => {
  const content = '甲乙丙丁戊己庚辛'

  it('完全相同的区间合并成一个入口', () => {
    const view = buildAnnotationView(content, [
      item('strength', '论证有效', anchor('丙丁', 2, 4)),
      item('possible_logic_gap', '可能推理缺口', anchor('丙丁', 2, 4)),
    ])
    expect(view.segments).toHaveLength(3)
    const middle = view.segments[1]!
    expect(middle.text).toBe('丙丁')
    expect(middle.items).toHaveLength(2)
    expect(middle.items.map(entry => entry.category)).toEqual(['strength', 'possible_logic_gap'])
  })

  it('部分重叠按边界切分且不产生嵌套', () => {
    // [2,6) 与 [4,8) 重叠，切成 2-4 / 4-6 / 6-8 三段。
    const view = buildAnnotationView(content, [
      item('strength', '论证有效', anchor('丙丁戊己', 2, 6)),
      item('insufficient_support', '缺少支持', anchor('戊己庚辛', 4, 8)),
    ])
    const annotated = view.segments.filter(segment => segment.items.length > 0)
    expect(annotated.map(segment => [segment.start, segment.end])).toEqual([[2, 4], [4, 6], [6, 8]])
    // 重叠段同时列出两条批注，不叠底色。
    expect(annotated[1]!.items).toHaveLength(2)
    expect(annotated[0]!.items).toHaveLength(1)
    expect(annotated[2]!.items).toHaveLength(1)
    // 拼接后必须还原原文，不丢字也不重复。
    expect(view.segments.map(segment => segment.text).join('')).toBe(content)
  })

  it('返回的段落覆盖全文且键唯一', () => {
    const view = buildAnnotationView('短文本', [item('unclear_scope', '定义待明确', anchor('文', 1, 2))])
    expect(view.segments.map(segment => segment.text).join('')).toBe('短文本')
    const keys = view.segments.map(segment => segment.key)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('待核对条目单独归类，不进入高亮段', () => {
    const view = buildAnnotationView(content, [
      item('strength', '论证有效', anchor('丙丁', 2, 4)),
      item('unclear_scope', '定义待明确', anchor('不存在', 0, 3)),
      item('possible_logic_gap', '可能推理缺口', anchor('戊己', null, null, false)),
    ])
    expect(view.pending).toHaveLength(2)
    expect(view.hasMarkers).toBe(true)
    const highlighted = view.segments.filter(segment => segment.items.length > 0)
    expect(highlighted).toHaveLength(1)
    expect(highlighted[0]!.text).toBe('丙丁')
  })

  it('没有批注时返回整段普通文本', () => {
    const view = buildAnnotationView(content, [])
    expect(view.hasMarkers).toBe(false)
    expect(view.pending).toHaveLength(0)
    expect(view.segments).toHaveLength(1)
    expect(view.segments[0]!.text).toBe(content)
    expect(view.segments[0]!.items).toEqual([])
  })

  it('相邻但不重叠的区间各成一段', () => {
    const segments = buildSegments(content, [
      { item: item('strength', '论证有效', anchor('甲乙', 0, 2)), start: 0, end: 2 },
      { item: item('unclear_scope', '定义待明确', anchor('丙丁', 2, 4)), start: 2, end: 4 },
    ])
    expect(segments.filter(segment => segment.items.length > 0).map(segment => segment.text)).toEqual(['甲乙', '丙丁'])
  })
})

describe('批注投影', () => {
  it('把可信与不可信条目分开', () => {
    const content = '甲乙丙'
    const projection = projectAnnotations(content, [
      item('strength', '论证有效', anchor('甲乙', 0, 2)),
      item('unclear_scope', '定义待明确', anchor('甲乙', 2, 4)),
    ])
    expect(projection.resolved).toHaveLength(1)
    expect(projection.resolved[0]!.start).toBe(0)
    expect(projection.pending).toHaveLength(1)
  })

  it('条目键在相同区间与类别下稳定', () => {
    const first = item('strength', '论证有效', anchor('丙丁', 2, 4))
    const second = item('strength', '论证有效', anchor('丙丁', 2, 4))
    expect(annotationKey(first)).toBe(annotationKey(second))
    // 类别不同即为不同入口。
    expect(annotationKey(first)).not.toBe(annotationKey({ ...second, category: 'insufficient_support' }))
  })
})

describe('四类批注的展示派生', () => {
  it('线型与文字标签固定且互不相同', () => {
    const styles = Object.values(ANNOTATION_STYLES)
    expect(styles).toHaveLength(4)
    expect(styles.map(style => style.decoration).sort()).toEqual(['dashed', 'solid', 'underline', 'wavy'])
  })

  it('每个类别都有非空的文字标签，颜色不单独承载语义', () => {
    for (const style of Object.values(ANNOTATION_STYLES)) {
      expect(style.label.trim().length).toBeGreaterThan(0)
      expect(categoryLabel(style.category).length).toBeGreaterThan(0)
    }
    expect(categoryLabel('strength')).toBe('论证有效')
    expect(categoryLabel('insufficient_support')).toBe('缺少支持')
    expect(categoryLabel('unclear_scope')).toBe('定义待明确')
  })

  it('类别判定只接受固定四种', () => {
    for (const category of ['strength', 'possible_logic_gap', 'insufficient_support', 'unclear_scope']) {
      expect(isAnnotationCategory(category)).toBe(true)
    }
    for (const category of ['', 'fact_error', 'STRENGTH', null, 1]) {
      expect(isAnnotationCategory(category)).toBe(false)
    }
  })
})

describe('Intl.Segmenter 使用方式', () => {
  it('使用 grapheme 粒度构造一次并复用', () => {
    resetGraphemeSegmenterCache()
    const spy = vi.spyOn(Intl as unknown as { Segmenter: new (l?: string, o?: { granularity?: string }) => unknown }, 'Segmenter')
    try {
      isGraphemeBoundary('abc', 1)
      isGraphemeBoundary('abc', 2)
      expect(spy).toHaveBeenCalledTimes(1)
    } finally {
      spy.mockRestore()
      resetGraphemeSegmenterCache()
    }
  })
})
