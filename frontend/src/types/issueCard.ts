/**
 * 争点卡（V2-06 / V2-07）的前端契约类型。
 *
 * 边界：
 * - 卡片身份由服务端给出的稳定 `id` 决定；编辑一律携带 `version` 做 CAS。
 * - 引用坐标是 Unicode 码点半开区间，且只是提示：展示前必须用原文回读校验，
 *   不一致或缺失时降级为“待核对”，不猜测位置。
 * - 双方引用用纯文本渲染，不拼接 HTML。
 */

/** 一处双方原句引用；`resolved` 为 false 时只显示“待核对”，不带坐标。 */
export interface IssueCardAnchor {
  message_id: string
  quote: string
  start_cp: number | null
  end_cp: number | null
  resolved: boolean
}

export interface IssueCardCitation {
  anchor: IssueCardAnchor
  speaker: 'user' | 'assistant'
  turn_no: number
  note: string
}

export interface IssueCardItem {
  id: string
  title: string
  /** 人工改名后永远是 user；AI 更新不得覆盖。 */
  title_source: 'ai' | 'user'
  explanation: string
  origin: IssueCardCitation
  responses: IssueCardCitation[]
  pending: string[]
  /** 人工编辑与晚到分析的可见冲突提示。 */
  notices: string[]
  version: number
}

export type IssueCardState = 'not_ready' | 'unsupported' | 'ready'

export type IssueCardAnalysisStatus = 'not_started' | 'generating' | 'completed' | 'failed'

export interface IssueCardAnalysis {
  status: IssueCardAnalysisStatus
  attempt_version: number
  error_code?: string
}

export interface IssueCardList {
  state: IssueCardState
  display_cap: number
  pending?: boolean
  analysis: IssueCardAnalysis
  items: IssueCardItem[]
}

export type IssueCardEditAction = 'rename' | 'merge' | 'remove'

export interface IssueCardAnalyzeRequest {
  retry?: boolean
  expected_attempt_version?: number
}

export type IssueCardEditRequest =
  | { expected_version: number; action: 'rename'; title: string }
  | { expected_version: number; action: 'merge'; target_card_id: string }
  | { expected_version: number; action: 'remove' }
