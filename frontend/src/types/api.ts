// 存活检查不等于数据库、模型或完整应用就绪。
export interface LiveHealth {
  status: 'alive'
}

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
}

export type DebateStatus = 'active' | 'completed'

export interface Debate {
  id: string
  topic: Topic
  user_stance: Stance
  ai_stance: Stance
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
