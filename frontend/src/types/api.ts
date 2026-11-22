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

export interface Topic {
  id: string
  title: string
  description: string
}

export interface CreateDebateRequest {
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
  topic: Topic
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

// 训练赛流式：start 携带 claim_id，done 携带最终步骤与权威进度。
export interface TrainingStreamStart { claim_id: string; request_id: string }
export interface TrainingStreamDone { step: TrainingStep; progress: TrainingProgress; debate_status: DebateStatus }
export type TrainingStreamUpdate = ({ type: 'start' } & TrainingStreamStart) | { type: 'delta'; text: string }
export type TrainingStreamResult = { kind: 'done'; data: TrainingStreamDone } | { kind: 'pending'; data: TrainingStepResult }
