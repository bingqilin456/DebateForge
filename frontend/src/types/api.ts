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

export type DebateMode = 'quick_3' | 'standard_4'

export type AIStyle = 'logical' | 'aggressive' | 'empathic' | 'data'

/** 赛制环节：第 3 轮在 quick_3 为结辩、在 standard_4 为自由辩。 */
export type DebateStage = 'opening' | 'rebuttal' | 'free_debate' | 'closing'

export interface Debate {
  id: string
  topic: DebateTopic
  user_stance: Stance
  ai_stance: Stance
  /** 赛制模式由服务端在创建时固定，客户端不可修改。 */
  mode: DebateMode
  ai_style: AIStyle
  /** 总轮数由赛制推导，前端以此为唯一上限来源，不再硬编码 3。 */
  total_rounds: number
  status: DebateStatus
  completed_rounds: number
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

export type TopLevelView = 'debate' | 'topics' | 'history'

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
