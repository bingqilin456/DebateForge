import type { IssueCardAnalysis, IssueCardCitation, IssueCardItem, IssueCardList, IssueCardState } from '../types/issueCard'
import { verifiedRange } from './annotations'

/**
 * 争点卡展示的纯计算层。
 *
 * 关键约束：
 * - 坐标是 Unicode 码点半开区间，必须用 Array.from 拆分；回读校验复用 V2-04 的 verifiedRange。
 * - 引用不可信：坐标缺失、越界、切片不一致或切中可见字素时只显示“待核对”，不高亮、不猜位置。
 * - 双方引用各自独立校验：提出位置与每条相关回应都可以单独降级。
 * - 默认最多展示 5 张；服务端已截断，这里只做防御性上限，不重排、不改身份。
 * - 所有用户与模型文本按纯文本渲染，不拼接 HTML。
 */

export const ISSUE_CARD_DISPLAY_CAP = 5

/** 一条引用的展示投影：`verified` 才给坐标，其余一律 `pending`。 */
export interface IssueCardCitationView {
  citation: IssueCardCitation
  status: 'verified' | 'pending'
  /** 仅在 verified 时有值；码点下标。 */
  start: number | null
  end: number | null
  /** 展示用定位键：消息 + 坐标 + 原文；只用于比较，不拼进 CSS 选择器。 */
  key: string
}

export interface IssueCardView {
  item: IssueCardItem
  origin: IssueCardCitationView
  responses: IssueCardCitationView[]
  pending: string[]
  notices: string[]
  /** 提出位置是否还能在原文中定位。 */
  canLocateOrigin: boolean
  /** 该卡是否至少有一处引用可定位。 */
  canLocate: boolean
}

/** 会话详情里的原文索引：message_id → content。只用于回读校验，不做权限判断。 */
export type IssueCardContents = Record<string, string>

function citationKey(view: IssueCardCitation, start: number | null, end: number | null): string {
  return `${view.anchor.message_id}|${start ?? 'x'}|${end ?? 'x'}`
}

/**
 * 回读校验一条引用。
 *
 * `verifiedRange` 已覆盖：未解析、坐标缺失、越界、倒序、回读不一致、字素边界不确定。
 * 这里只把 `resolved=false` 与原文缺失也归入待核对，绝不按保存坐标直接高亮。
 */
export function projectIssueCardCitation(citation: IssueCardCitation, contents: IssueCardContents): IssueCardCitationView {
  const content = contents[citation.anchor.message_id]
  if (content === undefined) {
    return { citation, status: 'pending', start: null, end: null, key: citationKey(citation, null, null) }
  }
  const range = verifiedRange(content, citation.anchor)
  if (!range) {
    return { citation, status: 'pending', start: null, end: null, key: citationKey(citation, null, null) }
  }
  return { citation, status: 'verified', start: range.start, end: range.end, key: citationKey(citation, range.start, range.end) }
}

/** 把读取结果投影成可渲染的卡片列表；保持服务端顺序与身份，只做上限截断。 */
export function buildIssueCardViews(list: IssueCardList, contents: IssueCardContents): IssueCardView[] {
  return list.items.slice(0, ISSUE_CARD_DISPLAY_CAP).map(item => {
    const origin = projectIssueCardCitation(item.origin, contents)
    const responses = item.responses.map(citation => projectIssueCardCitation(citation, contents))
    const canLocateOrigin = origin.status === 'verified'
    return {
      item,
      origin,
      responses,
      pending: [...item.pending],
      notices: [...item.notices],
      canLocateOrigin,
      canLocate: canLocateOrigin || responses.some(response => response.status === 'verified'),
    }
  })
}

/** 场次状态文案；隔离状态必须明确说明为什么没有卡片，而不是显示“0 条＝无争点”。 */
export function issueCardStateMessage(state: IssueCardState): string {
  switch (state) {
    case 'not_ready':
      return '本场尚未完成四轮辩论，赛后争点卡在完赛后集中整理。'
    case 'unsupported':
      return '本场为历史三轮快速赛，不参与争点卡整理。'
    default:
      return ''
  }
}

/** 分析状态的界面文案；不把失败写成“没有争点”。 */
export function issueCardAnalysisMessage(analysis: IssueCardAnalysis): string {
  switch (analysis.status) {
    case 'not_started':
      return '尚未整理争点卡，可手动整理一次。'
    case 'generating':
      return '争点卡整理中，不阻塞查看与编辑。'
    case 'failed':
      return `争点卡整理未完成（错误码：${analysis.error_code ?? 'unknown'}）。已保存的卡片与人工编辑不受影响。`
    default:
      return ''
  }
}

/** 卡片标题展示：人工标题不标记“原名”，只标注来源供无障碍朗读使用。 */
export function issueCardTitleAccessibleName(item: IssueCardItem): string {
  return item.title_source === 'user' ? `争点卡：${item.title}（人工标题）` : `争点卡：${item.title}`
}

/**
 * 合并预览文案：说明哪些内容会进入目标卡。
 * 只用于确认对话框，不写回服务端。
 */
export function issueCardMergePreview(source: IssueCardItem, target: IssueCardItem): string {
  const moved = 1 + source.responses.length
  return `将「${source.title}」并入「${target.title}」：保留双方 ${moved} 处原句链接，原卡不再单独显示。`
}

/** 移除确认文案：只说明本场视图的后果，不承诺能否恢复。 */
export function issueCardRemovePreview(item: IssueCardItem): string {
  return `移除「${item.title}」后，该卡将从本场视图移除，且不会因重新整理而复活。`
}

/**
 * 按引用查找正文标记元素：只用 dataset 比较，不把数据拼进选择器。
 * 返回 null 表示当前视图没有对应标记，调用方应只提示“待核对”而不滚动。
 */
export function findIssueCardMarker(root: ParentNode | null, key: string): HTMLElement | null {
  if (!root) return null
  const candidates = root.querySelectorAll<HTMLElement>('[data-issue-card-citation]')
  for (const element of Array.from(candidates)) {
    if (element.dataset.issueCardCitation === key) return element
  }
  return null
}

/**
 * 把一条引用切成可渲染片段：可定位时带标记，否则整段纯文本。
 * 与批注分段不同，争点卡按引用独立渲染，不合并重叠区间。
 */
export interface IssueCardCitationSegments {
  before: string
  quote: string
  after: string
  verified: boolean
}

export function splitCitation(content: string, view: IssueCardCitationView): IssueCardCitationSegments {
  const points = Array.from(content)
  if (view.status !== 'verified' || view.start === null || view.end === null) {
    return { before: '', quote: view.citation.anchor.quote, after: '', verified: false }
  }
  return {
    before: points.slice(0, view.start).join(''),
    quote: points.slice(view.start, view.end).join(''),
    after: points.slice(view.end).join(''),
    verified: true,
  }
}
