export interface AuthCredentials {
  username: string
  password: string
}

export interface User {
  readonly id: string
  readonly username: string
}

// 只包含 CSRF token；Session 原值只由浏览器处理 HttpOnly Cookie。
export interface AuthSession {
  readonly user: User
  readonly csrf_token: string
}

export type Stance = 'pro' | 'con'

/**
 * 预设辩题：id 必为十进制字符串，来自 GET /topics。
 * 会话快照中的 topic 可能是私有自定义题，那时 id 为 null，见 DebateTopic。
 */
export interface Topic {
  id: string
  title: string
  description: string
}

/** 会话快照中的辩题：预设题 id 为字符串，私有自定义题 id 为 null。 */
export interface DebateTopic {
  id: string | null
  title: string
  description: string
}

/** 私有自定义辩题原文：只存在于本场会话快照，不进入公共题库。 */
export interface CustomTopic {
  title: string
  background?: string
}

/**
 * 创建会话请求是严格二选一：topic_id 与 custom_topic 恰有一个出现。
 * 服务端按“字段是否出现”判定互斥，两者同时出现或都缺失都是 400。
 */
export type CreateDebateRequest =
  | { topic_id: string; user_stance: Stance; ai_style?: AIStyle }
  | { custom_topic: CustomTopic; user_stance: Stance; ai_style?: AIStyle }

/** LegacyCreateDebateRequest 是预设题的兼容形状，供已有调用方继续引用。 */
export interface LegacyCreateDebateRequest {
  topic_id: string
  user_stance: Stance
  ai_style?: AIStyle
}

export type DebateStatus = 'active' | 'completed'

export type DebateMode = 'quick_3' | 'standard_4' | 'training_v2'

export type AIStyle = 'logical' | 'aggressive' | 'empathic' | 'data'

/** 赛制环节：第 3 轮在 quick_3 为结辩、在 standard_4 为自由辩。 */
export type DebateStage = 'opening' | 'rebuttal' | 'free_debate' | 'closing'

/** 训练赛发言方：训练赛有 AI 先发言与跨阶段连续同方，不能从轮号推导。 */
export type TrainingSpeaker = 'user' | 'assistant'

/** 训练赛权威进度（服务端推进并持久化；客户端只展示，不自行推导）。 */
export interface TrainingProgress {
  stage: DebateStage
  stage_step: number
  free_debate_rounds: number
  free_debate_max_rounds: number
  committed_steps: number
  total_steps: number
  next_step_no: number | null
  next_speaker: TrainingSpeaker | null
  can_submit_step: boolean
  can_retry_ai: boolean
  can_end_free_debate: boolean
  end_blocked_reason: string | null
  pending_ai: TrainingPendingAI | null
  abandoned_ai: TrainingAbandonedAI | null
}

export interface TrainingPendingAI {
  claim_id: string
  step_no: number
  status: 'generating' | 'failed' | 'completed'
  attempt_version: number
  error_code: string | null
}

export interface TrainingAbandonedAI {
  claim_id: string
  step_no: number
  attempt_version: number
  error_code: string | null
}

/** 创建与列表响应里的训练赛轻量进度。 */
export interface TrainingSummary {
  stage: DebateStage
  stage_step: number
  free_debate_rounds: number
  free_debate_max_rounds: number
}

export interface TrainingStep {
  id: string
  step_no: number
  stage: DebateStage
  stage_step: number
  free_debate_round: number
  speaker: TrainingSpeaker
  client_request_id: string
  content: string
  created_at: string
}

export interface TrainingDetail {
  progress: TrainingProgress
  steps: TrainingStep[]
  query_url: string
}

/** 步骤提交/重试/结束自由辩的共享响应；step 仅在真正提交新正式步骤时非空。 */
export interface TrainingStepResult {
  step: TrainingStep | null
  progress: TrainingProgress
  debate_status: DebateStatus
  query_url: string
}

export interface Debate {
  id: string
  topic: DebateTopic
  user_stance: Stance
  ai_stance: Stance
  /** 赛制模式由服务端在创建时固定，客户端不可修改。 */
  mode: DebateMode
  ai_style: AIStyle
  /** 总轮数由服务端给出：V1 由赛制推导，训练赛为自由辩回合上限。 */
  total_rounds: number
  status: DebateStatus
  completed_rounds: number
  /**
   * 训练赛轻量进度；V1 会话为 null 或省略。
   * 可选是为了让未包含该字段的 V1 旧响应仍可读取；训练赛响应必须给出。
   */
  training?: TrainingSummary | null
  created_at: string
  updated_at: string
}

export type MessageRole = 'user' | 'assistant'

export interface Message {
  id: string
  role: MessageRole
  content: string
  created_at: string
}

export type TurnStatus = 'generating' | 'completed' | 'failed'

export type TurnErrorCode =
  | 'model_error'
  | 'model_timeout'
  | 'model_unavailable'
  | 'dependency_unavailable'
  | 'generation_cancelled'

export interface Turn {
  id: string
  round_no: number
  client_request_id: string
  status: TurnStatus
  attempt_version: number
  error_code: string | null
  messages: Message[]
}

export interface SummaryPoint {
  round_no: number
  point: string
  reason: string
}

export interface SummarySuggestion {
  round_no: number
  action: string
  example: string
}

export interface SummaryContent {
  strengths: SummaryPoint[]
  issues: SummaryPoint[]
  suggestions: SummarySuggestion[]
}

export type SummaryStatus =
  | { status: 'not_started'; attempt_version: null }
  | { status: 'generating'; attempt_version: number }
  | { status: 'failed'; attempt_version: number; error_code: string }
  | { status: 'completed'; attempt_version: number; schema_version: number; content: SummaryContent }

export interface DebateDetail {
  debate: Debate
  turns: Turn[]
  summary_status: SummaryStatus
  /** 训练赛权威进度与已提交步骤；V1 会话为 null 或省略。 */
  training?: TrainingDetail | null
}

export interface TurnResult {
  turn: Turn
  completed_rounds: number
  query_url: string
}

export interface DebateListResult {
  items: Debate[]
  total: number
  page: number
  page_size: number
}

export type TopLevelView = 'debate' | 'training' | 'topics' | 'history'

// 策略与轮次/总结分别保存状态及版本，不能复用 Turn 或 SummaryStatus。
export interface StrategyContent {
  thesis: string
  arguments: string[]
  risks: string[]
}
export type StrategyStatus =
  | { status: 'not_started'; attempt_version: null }
  | { status: 'generating'; attempt_version: number }
  | { status: 'failed'; attempt_version: number; error_code: TurnErrorCode }
  | { status: 'completed'; attempt_version: number; schema_version: 1; prompt_version: string; ai_style: AIStyle; content: StrategyContent }

export type StrategyRequest =
  | { retry?: false }
  | { retry: true; expected_attempt_version: number }

export interface StreamStart { turn_id: string; request_id: string }
export interface StreamDone { turn: Turn; completed_rounds: number; status: DebateStatus }
export type StreamUpdate = ({ type: 'start' } & StreamStart) | { type: 'delta'; text: string }
export type TurnStreamResult = { kind: 'done'; data: StreamDone } | { kind: 'pending'; data: TurnResult }

// V2-04 原文批注：与轮次、总结、策略各自独立的状态与版本，不能复用它们的 DTO。

/** 批注类别固定四种；颜色与线型只是展示派生。 */
export type AnnotationCategory = 'strength' | 'possible_logic_gap' | 'insufficient_support' | 'unclear_scope'

/**
 * 批注锚点：message_id 为服务端正式消息 ID；坐标是从 0 开始的 Unicode 码点半开区间。
 * resolved=false 表示引用存在歧义或坐标失配，前端只显示“原文待核对”，不按坐标高亮。
 */
export interface AnnotationAnchor {
  message_id: string
  quote: string
  start_cp: number | null
  end_cp: number | null
  resolved: boolean
}

export interface AnnotationItem {
  category: AnnotationCategory
  label: string
  reason: string
  improvement: string
  anchor: AnnotationAnchor
  /** 引用消息所在轮次（1 起）与角色，便于按轮展示；不参与坐标。 */
  turn_no: number
  role: MessageRole
}

/** 单轮批注批次状态；not_started 只出现在读取投影里，不落库。 */
export type AnnotationBatchStatus = 'not_started' | 'generating' | 'completed' | 'failed'

export interface AnnotationBatch {
  turn_id: string
  round_no: number
  status: AnnotationBatchStatus
  /** 未认领为 0。 */
  attempt_version: number
  items: AnnotationItem[]
  error_code?: string
}

export interface AnnotationList {
  items: AnnotationBatch[]
}

/** 手动补做是唯一允许的请求正文；0 表示未认领，其余为期望的当前版本。 */
export interface AnnotationRetryRequest {
  expected_attempt_version: number
}

// V2-13 赛后复盘 + V2-10 四维训练参考。
// 与旧总结（schema_version=1 三栏 / 2 的 0–100）完全分离：独立接口、独立版本、独立取值。

/** 复盘引用：resolved=false 只显示“原文待核对”，不按坐标高亮。 */
export interface ReviewQuote {
  message_id: string
  quote: string
  start_cp: number | null
  end_cp: number | null
  resolved: boolean
}

/** 四个固定维度键；顺序由服务端给定，前端不重排。 */
export type ReviewDimensionKey = 'argument_structure' | 'responsiveness' | 'evidence_use' | 'concept_scope'

/**
 * 单维结果。level 为 null 表示“观察不足”——合法结果，不是 0 分、也不是 1 档，
 * 此时必须带 insufficient_reason，且不得有档位标签与引用坐标。
 */
export interface ReviewDimension {
  key: ReviewDimensionKey
  label: string
  level: 1 | 2 | 3 | 4 | null
  level_label: string | null
  basis: string
  improvement: string
  insufficient_reason: string | null
  quote: ReviewQuote | null
}

/** 争点 / 优点 / 改进条目：文字始终可读，引用不可核对时不提供定位入口。 */
export interface ReviewTextItem {
  text: string
  quote: ReviewQuote | null
}

export interface ReviewContent {
  dimensions: ReviewDimension[]
  key_issues: ReviewTextItem[]
  strengths: ReviewTextItem[]
  improvements: ReviewTextItem[]
}

/**
 * 复盘状态与比赛完成状态是两个独立事实；not_started 只出现在读取投影里。
 *
 * available 仅在服务端关闭复盘切片时显式给出 false：此时 status 仍是 not_started，
 * 但客户端必须隐藏复盘界面并禁止发起生成。启用时省略该字段，旧响应形状保持可用。
 */
export type ReviewStatus =
  | { status: 'not_started'; attempt_version: null; available?: boolean }
  | { status: 'generating'; attempt_version: number }
  | { status: 'failed'; attempt_version: number; error_code: string }
  | { status: 'completed'; attempt_version: number; schema_version: 1; prompt_version?: string; content: ReviewContent }

/** 显式生成或重试的请求正文；首版没有任何自动触发。 */
export interface RepracticeRequest {
  client_request_id: string
  message_id: string
  start_cp: number
  end_cp: number
  quote: string
  goal: string
  rewrite: string
}

export interface RepracticeVersion {
  id: string
  attempt_version: number
  status: 'generating' | 'completed' | 'failed'
  rewrite: string
  feedback: { change: string; reason: string; new_problem: string }
}

export type ReviewRequest = { retry?: false } | { retry: true; expected_attempt_version: number }
// 训练赛流式：start 携带 claim_id，done 携带最终步骤与权威进度。
export interface TrainingStreamStart { claim_id: string; request_id: string }
export interface TrainingStreamDone { step: TrainingStep; progress: TrainingProgress; debate_status: DebateStatus }
export type TrainingStreamUpdate = ({ type: 'start' } & TrainingStreamStart) | { type: 'delta'; text: string }
export type TrainingStreamResult = { kind: 'done'; data: TrainingStreamDone } | { kind: 'pending'; data: TrainingStepResult }
