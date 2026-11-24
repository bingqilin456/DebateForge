import type { AnnotationAnchor, AnnotationItem, AnnotationBatch } from '../types/api'

/**
 * 批注展示的纯计算层。
 *
 * 关键约束：
 * - 坐标是 Unicode 码点半开区间，必须用 Array.from 拆分，不能用 UTF-16 下标切片。
 * - 坐标不可信：回读时用原文重新切片，与保存的引用逐字比对；不一致或越界都降级为待核对。
 * - 重叠区间按边界切分成无重叠文本段；同段多条批注共用一个入口，不叠底色。
 * - 逻辑问题用橙色波浪线、证据不足用淡蓝底虚线、范围不清用紫色下划线、优点用淡绿底实线，
 *   但颜色从不单独承载语义，必须同时给出线型与文字标签。
 */

export type AnnotationCategory = AnnotationItem['category']

/** 批注的颜色、线型和默认文字标签。 */
export interface AnnotationStyle {
  category: AnnotationCategory
  /** 线型只用于 CSS class 与文字说明，不替代文字标签。 */
  decoration: 'solid' | 'wavy' | 'dashed' | 'underline'
  /** 文字标签是语义的唯一可靠载体（色觉差异与灰度阅读可用）。 */
  label: string
  /** 屏幕阅读器与测试使用的类名后缀。 */
  className: string
}

export const ANNOTATION_STYLES: Record<AnnotationCategory, AnnotationStyle> = {
  // 论证优点：淡绿底色 + 实线。
  strength: { category: 'strength', decoration: 'solid', label: '论证有效', className: 'strength' },
  // 可能的逻辑问题：橙色波浪线。
  possible_logic_gap: { category: 'possible_logic_gap', decoration: 'wavy', label: '可能推理缺口', className: 'logic-gap' },
  // 证据或支持不足：淡蓝底色 + 虚线。
  insufficient_support: { category: 'insufficient_support', decoration: 'dashed', label: '缺少支持', className: 'support' },
  // 定义或范围不清：紫色下划线。
  unclear_scope: { category: 'unclear_scope', decoration: 'underline', label: '定义待明确', className: 'scope' },
  opponent_weakness: { category: 'opponent_weakness', decoration: 'wavy', label: '敌方破绽', className: 'opponent-weakness' },
  opponent_strength: { category: 'opponent_strength', decoration: 'solid', label: '敌方杀招', className: 'opponent-strength' },
}

const ownLabels: Partial<Record<AnnotationCategory, string>> = {
  strength: '我方亮点',
  possible_logic_gap: '我方漏洞',
  insufficient_support: '论据补强',
}

export function annotationDisplayLabel(item: AnnotationItem): string {
  const label = categoryLabel(item.category)
  const ownLabel = item.role === 'user' ? ownLabels[item.category] : undefined
  return ownLabel ? `${ownLabel} · ${label}` : label
}

export function isAnnotationCategory(value: unknown): value is AnnotationCategory {
  return typeof value === 'string' && value in ANNOTATION_STYLES
}

/** 一个可渲染的文本段：要么是普通原文，要么带一个或多个批注入口。 */
export interface AnnotatedSegment {
  /** 段落在原文中的码点区间。 */
  start: number
  end: number
  text: string
  /** 该段适用的批注条目；空数组表示普通文本。 */
  items: AnnotationItem[]
  /** 稳定的键：坐标已足以区分，因为分段互不重叠。 */
  key: string
}

/** 已通过回读校验、可安全高亮的条目。 */
export interface ResolvedAnnotation {
  item: AnnotationItem
  start: number
  end: number
}

/** 回读校验结果：可高亮集合 + 待核对条目。 */
export interface AnnotationProjection {
  resolved: ResolvedAnnotation[]
  /** 坐标缺失、越界或切片不一致的条目：只显示“原文待核对”，不高亮。 */
  pending: AnnotationItem[]
}

/**
 * 把内容拆成码点数组，所有下标都以它为准。
 *
 * 这里刻意不用 UTF-16 下标（`string.slice`）：一个 emoji 或补充平面字符
 * 在 UTF-16 里占两个单元，按它切片会把坐标整体错位。
 */
export function codepoints(content: string): string[] {
  return Array.from(content)
}

/**
 * 判断某个码点下标在字素簇上的状态。
 *
 * - `boundary`：确认落在字素簇边界（含 0 与末尾）。
 * - `interior`：确认切在字素簇内部，例如 ZWJ 家庭序列或组合符之间。
 * - `unknown`：环境未提供 `Intl.Segmenter`，无法判定内部分段。
 *
 * 坐标契约是码点，但显示单位是字素簇：组合字符（e + U+0301）与
 * 家庭/肤色等 ZWJ 序列被切开时会渲染成破碎字形。这里用浏览器原生的
 * `Intl.Segmenter` 取字素边界，不手写 Unicode 分段规则、也不引入依赖。
 *
 * 缺少该 API 时（旧引擎、部分测试环境）不做猜测：除首尾安全端点外一律返回
 * `unknown`，由调用方降级为"原文待核对"，避免靠猜把 ZWJ 序列切碎。
 */
export type GraphemeBoundaryState = 'boundary' | 'interior' | 'unknown'

export function graphemeBoundaryState(content: string, index: number): GraphemeBoundaryState {
  const points = codepoints(content)
  if (!Number.isInteger(index) || index < 0 || index > points.length) return 'unknown'
  // 首尾是安全端点，任何实现都可以确认。
  if (index === 0 || index === points.length) return 'boundary'
  const segmenter = graphemeSegmenter()
  if (!segmenter) return 'unknown'
  const boundaries = new Set<number>()
  let offset = 0
  for (const { segment } of segmenter.segment(content)) {
    boundaries.add(offset)
    offset += codepoints(segment).length
  }
  boundaries.add(offset)
  return boundaries.has(index) ? 'boundary' : 'interior'
}

/**
 * 旧式布尔入口：只有确认落在边界时返回 true。
 * `unknown` 不视为边界，避免在无法判定的环境里贸然切开字素簇。
 */
export function isGraphemeBoundary(content: string, index: number): boolean {
  return graphemeBoundaryState(content, index) === 'boundary'
}

type GraphemeSegmenter = { segment: (input: string) => Iterable<{ segment: string }> }

let cachedSegmenter: GraphemeSegmenter | null | undefined

/**
 * 惰性构造并按结果缓存：`Intl.Segmenter` 不存在或构造失败时记为 null，
 * 之后不再重复尝试，也不抛错。
 */
function graphemeSegmenter(): GraphemeSegmenter | null {
  if (cachedSegmenter !== undefined) return cachedSegmenter
  cachedSegmenter = null
  try {
    const ctor = (Intl as unknown as { Segmenter?: new (locales?: string, options?: { granularity?: string }) => GraphemeSegmenter }).Segmenter
    if (typeof ctor === 'function') {
      const candidate = new ctor(undefined, { granularity: 'grapheme' })
      if (typeof candidate.segment === 'function') cachedSegmenter = candidate
    }
  } catch {
    // 构造失败按“不支持”处理，保持与缺失 API 一致的安全降级。
    cachedSegmenter = null
  }
  return cachedSegmenter
}

/** 仅供测试重置惰性缓存。 */
export function resetGraphemeSegmenterCache(): void {
  cachedSegmenter = undefined
}

/**
 * 校验单条锚点：返回可用的码点区间，或在任何不一致时返回 null。
 *
 * 逐码点回读必须相等；此外两个端点都要落在字素簇边界上。
 * 缺少 `Intl.Segmenter` 时只有"整条原文"（0 到末尾）这类安全端点可标，
 * 其余内部区间一律降级为待核对，避免切开 ZWJ 或组合字符。
 */
export function verifiedRange(content: string, anchor: AnnotationAnchor): { start: number; end: number } | null {
  if (!anchor.resolved) return null
  const { start_cp: start, end_cp: end } = anchor
  if (typeof start !== 'number' || typeof end !== 'number') return null
  if (!Number.isInteger(start) || !Number.isInteger(end)) return null
  const points = codepoints(content)
  if (start < 0 || end < start || end > points.length) return null
  // 回读必须逐码点相等；不做 trim、归一化或大小写折叠。
  if (points.slice(start, end).join('') !== anchor.quote) return null
  // 端点必须可确认落在字素簇边界；无法判定时只有全文区间算安全。
  const wholeText = start === 0 && end === points.length
  for (const index of [start, end]) {
    const state = graphemeBoundaryState(content, index)
    if (state === 'boundary') continue
    if (state === 'interior') return null
    if (!wholeText) return null
  }
  return { start, end }
}

/**
 * 按原文回读一次批次条目的坐标，把可信条目分成可高亮与待核对两类。
 * 没有权限或内容非法的条目不会出现在这里：它们由服务端在写入前拒绝。
 */
export function projectAnnotations(content: string, items: AnnotationItem[]): AnnotationProjection {
  const resolved: ResolvedAnnotation[] = []
  const pending: AnnotationItem[] = []
  for (const item of items) {
    if (item.anchor.message_id === undefined || item.anchor.message_id === null) {
      pending.push(item)
      continue
    }
    const range = verifiedRange(content, item.anchor)
    if (!range) {
      pending.push(item)
      continue
    }
    resolved.push({ item, start: range.start, end: range.end })
  }
  return { resolved, pending }
}

/**
 * 把可高亮条目切成无重叠文本段。
 *
 * 所有边界先排序去重再逐段检查，因此部分重叠不会产生嵌套标记；
 * 完全相同的区间自然合并成同一段，并列出全部适用标签。
 */
export function buildSegments(content: string, resolved: ResolvedAnnotation[]): AnnotatedSegment[] {
  const points = codepoints(content)
  const bounds = new Set<number>([0, points.length])
  for (const entry of resolved) {
    bounds.add(entry.start)
    bounds.add(entry.end)
  }
  const ordered = [...bounds].filter(value => value >= 0 && value <= points.length).sort((a, b) => a - b)
  const segments: AnnotatedSegment[] = []
  for (let i = 0; i + 1 < ordered.length; i++) {
    const start = ordered[i]!
    const end = ordered[i + 1]!
    if (end <= start) continue
    const items = resolved
      .filter(entry => entry.start <= start && entry.end >= end)
      .map(entry => entry.item)
    segments.push({
      start,
      end,
      text: points.slice(start, end).join(''),
      items,
      key: `${start}-${end}`,
    })
  }
  return segments
}

/** 一次性把批次条目投影成分段，供组件直接渲染。 */
export interface ContentAnnotationView {
  segments: AnnotatedSegment[]
  pending: AnnotationItem[]
  /** 该内容上是否存在任何可高亮标记。 */
  hasMarkers: boolean
}

export function buildAnnotationView(content: string, items: AnnotationItem[]): ContentAnnotationView {
  const projection = projectAnnotations(content, items)
  const segments = buildSegments(content, projection.resolved)
  return {
    segments,
    pending: projection.pending,
    hasMarkers: projection.resolved.length > 0,
  }
}

/**
 * 稳定的条目键：用于列表渲染与"标记 ↔ 解释"双向定位。
 *
 * 只含服务端标识与类别，不含任何原文或用户文本；它只用于 Map/Set 比较，
 * 绝不拼接进 CSS 选择器，也不作为 HTML 属性以外的用途。
 */
export function annotationEntryKey(item: AnnotationItem): string {
  return `${item.anchor.message_id}|${item.anchor.start_cp ?? 'x'}|${item.anchor.end_cp ?? 'x'}|${item.category}|${item.label}`
}

/** 与 annotationEntryKey 等价的展示键，保留旧名字供既有调用方使用。 */
export function annotationKey(item: AnnotationItem): string { return annotationEntryKey(item) }

/** 类别默认标签兜底：服务端已给标签时优先使用服务端值。 */
export function categoryLabel(category: AnnotationCategory): string {
  return ANNOTATION_STYLES[category]?.label ?? '批注'
}

/** 批注批次的展示态：只用于界面文案，不改变服务端状态。 */
export type BatchDisplayStatus = 'not_started' | 'generating' | 'completed' | 'failed'

export function batchStatus(batch: AnnotationBatch): BatchDisplayStatus {
  return batch.status
}

/**
 * 场中提示只控制“是否显示标记”，不控制生成与保存。
 * 关闭后仍可读取赛后批注，这是 PRD §5.4 的明确要求。
 */
export function shouldShowMarkers(showInProgress: boolean): boolean {
  return showInProgress
}
