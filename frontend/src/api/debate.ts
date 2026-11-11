import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'

export type Stance = 'pro' | 'con'
export interface Topic { id: string; title: string; description: string }
export interface Debate {
  id: string; topic: Topic; user_stance: Stance; ai_stance: Stance
  status: 'active' | 'completed'; completed_rounds: number; created_at: string; updated_at: string
}
export interface Message { id: string; role: 'user' | 'assistant'; content: string; created_at: string }
export interface Turn {
  id: string; round_no: number; client_request_id: string; status: 'generating' | 'completed' | 'failed'
  attempt_version: number; error_code: string | null; messages: Message[]
}
export interface DebateDetail { debate: Debate; turns: Turn[]; summary_status: { status: 'not_started'; attempt_version: null } }
export interface TurnResult { turn: Turn; completed_rounds: number; query_url: string }

export function isEntityId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,19}$/.test(value) && BigInt(value) <= 18446744073709551615n
}
function entityId(value: string): string {
  if (!isEntityId(value)) throw new ApiError('format', '会话或辩题编号无效。')
  return value
}
// 与后端 unicode.IsSpace + BOM 一致；JS trim() 不包含 U+0085，不能用于发言判空。
export function hasNonWhitespace(value: string): boolean {
  return /[^\p{White_Space}\uFEFF]/u.test(value)
}
function text(value: unknown, max: number): value is string {
  return typeof value === 'string' && hasNonWhitespace(value) && [...value].length <= max
}
function date(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d\d-\d\dT.*Z$/.test(value) && Number.isFinite(Date.parse(value))
}
function isTopic(value: unknown): value is Topic {
  return isRecord(value) && isEntityId(value.id) && text(value.title, 200) && typeof value.description === 'string' && [...value.description].length <= 2000
}
function isDebate(value: unknown): value is Debate {
  return isRecord(value) && isEntityId(value.id) && isTopic(value.topic)
    && (value.user_stance === 'pro' || value.user_stance === 'con')
    && value.ai_stance === (value.user_stance === 'pro' ? 'con' : 'pro')
    && (value.status === 'active' || value.status === 'completed')
    && Number.isInteger(value.completed_rounds) && Number(value.completed_rounds) >= 0 && Number(value.completed_rounds) <= 3
    && date(value.created_at) && date(value.updated_at)
}
function isMessage(value: unknown): value is Message {
  return isRecord(value) && isEntityId(value.id) && (value.role === 'user' || value.role === 'assistant') && text(value.content, 2000) && date(value.created_at)
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
function isTurn(value: unknown): value is Turn {
  if (!isRecord(value) || !isEntityId(value.id) || !Number.isInteger(value.round_no) || Number(value.round_no) < 1 || Number(value.round_no) > 3
    || typeof value.client_request_id !== 'string' || !uuid.test(value.client_request_id)
    || !Number.isSafeInteger(value.attempt_version) || Number(value.attempt_version) < 1
    || !Array.isArray(value.messages) || !value.messages.every(isMessage)) return false
  const messages: Message[] = value.messages
  if (messages[0]?.role !== 'user') return false
  if (value.status === 'completed') return value.error_code === null && messages.length === 2 && messages[1]?.role === 'assistant'
  if (value.status === 'generating') return value.error_code === null && messages.length === 1
  return value.status === 'failed' && messages.length === 1 && typeof value.error_code === 'string'
    && ['model_error', 'model_timeout', 'model_unavailable', 'dependency_unavailable', 'generation_cancelled'].includes(value.error_code)
}
function isDetail(value: unknown): value is DebateDetail {
  return isRecord(value) && isDebate(value.debate) && Array.isArray(value.turns) && value.turns.every(isTurn)
    && value.turns.every((turn, index) => turn.round_no === index + 1)
    && value.turns.filter(turn => turn.status === 'completed').length === value.debate.completed_rounds
    && isRecord(value.summary_status) && value.summary_status.status === 'not_started' && value.summary_status.attempt_version === null
}

// 使用 B 的同源 client 与内存 CSRF；本模块不读取 Cookie，也不实现后续轮次/重试。
export function createDebateApi(client: ApiClient) {
  return {
    async topics(): Promise<Topic[]> {
      const data = await client.request('/api/v1/topics', {
        expectedStatus: 200, validate: (v): v is { items: Topic[] } => isRecord(v) && Array.isArray(v.items) && v.items.every(isTopic),
      })
      return data.items
    },
    async create(topicId: string, stance: Stance): Promise<Debate> {
      return client.request('/api/v1/debates', { method: 'POST', expectedStatus: 201, body: { topic_id: entityId(topicId), user_stance: stance }, validate: isDebate })
    },
    async detail(id: string): Promise<DebateDetail> {
      const result = await client.request(`/api/v1/debates/${entityId(id)}`, { expectedStatus: 200, validate: isDetail })
      if (result.debate.id !== id) throw new ApiError('format', '会话响应与请求不一致。')
      return result
    },
    async send(id: string, content: string, requestId: string): Promise<TurnResult> {
      return client.request(`/api/v1/debates/${entityId(id)}/turns`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { round_no: 1, content, client_request_id: requestId },
        validate: (v): v is TurnResult => isRecord(v) && isTurn(v.turn) && v.turn.round_no === 1
          && v.turn.client_request_id === requestId && v.turn.messages[0]?.content === content
          && v.completed_rounds === (v.turn.status === 'completed' ? 1 : 0) && v.query_url === `/api/v1/debates/${id}`,
      })
    },
  }
}
export type DebateApi = ReturnType<typeof createDebateApi>
