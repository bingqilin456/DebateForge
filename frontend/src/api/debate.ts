import { authState } from '../auth/state'
import type {
  CreateDebateRequest,
  Debate,
  DebateDetail,
  DebateListResult,
  DebateStatus,
  Message,
  MessageRole,
  Stance,
  SummaryContent,
  SummaryPoint,
  SummaryStatus,
  SummarySuggestion,
  Topic,
  Turn,
  TurnErrorCode,
  TurnResult,
  TurnStatus,
} from '../types/api'
import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'

export type {
  CreateDebateRequest,
  Debate,
  DebateDetail,
  DebateListResult,
  DebateStatus,
  Message,
  MessageRole,
  Stance,
  SummaryContent,
  SummaryPoint,
  SummaryStatus,
  SummarySuggestion,
  Topic,
  Turn,
  TurnErrorCode,
  TurnResult,
  TurnStatus,
}

const errorCodes = ['model_error', 'model_timeout', 'model_unavailable', 'dependency_unavailable', 'generation_cancelled']

export function isStance(value: unknown): value is Stance {
  return value === 'pro' || value === 'con'
}
function isApiClient(value: unknown): value is ApiClient {
  return isRecord(value) && typeof value.request === 'function'
}
export function isEntityId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,19}$/.test(value) && BigInt(value) <= 18446744073709551615n
}
function entityId(value: string): string {
  if (!isEntityId(value)) throw new ApiError('format', '会话或辩题编号无效。')
  return value
}
// 与后端 unicode.IsSpace + BOM 一致；JS trim() 不包含 U+0085，不能用于发言判空。
export function hasNonWhitespace(value: string): boolean {
  return /[^\p{White_Space}\u{FEFF}]/u.test(value)
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
    && errorCodes.includes(value.error_code)
}
function isSummaryPoint(value: unknown): value is SummaryPoint {
  return isRecord(value) && Number.isInteger(value.round_no) && Number(value.round_no) >= 1 && Number(value.round_no) <= 3
    && text(value.point, 300) && text(value.reason, 300)
}
function isSummarySuggestion(value: unknown): value is SummarySuggestion {
  return isRecord(value) && Number.isInteger(value.round_no) && Number(value.round_no) >= 1 && Number(value.round_no) <= 3
    && text(value.action, 300) && text(value.example, 300)
}
export function isSummaryContent(value: unknown): value is SummaryContent {
  return isRecord(value)
    && Array.isArray(value.strengths) && value.strengths.length >= 1 && value.strengths.length <= 3 && value.strengths.every(isSummaryPoint)
    && Array.isArray(value.issues) && value.issues.length >= 1 && value.issues.length <= 3 && value.issues.every(isSummaryPoint)
    && Array.isArray(value.suggestions) && value.suggestions.length >= 1 && value.suggestions.length <= 3 && value.suggestions.every(isSummarySuggestion)
}
export function isSummaryStatus(value: unknown): value is SummaryStatus {
  if (!isRecord(value)) return false
  if (value.status === 'not_started') {
    return value.attempt_version === null && value.error_code === undefined && value.content === undefined
  }
  if (!Number.isSafeInteger(value.attempt_version) || Number(value.attempt_version) < 1) return false
  if (value.status === 'generating') {
    return value.error_code === undefined && value.content === undefined
  }
  if (value.status === 'failed') {
    return typeof value.error_code === 'string' && errorCodes.includes(value.error_code) && value.content === undefined
  }
  return value.status === 'completed' && value.schema_version === 1 && isSummaryContent(value.content) && value.error_code === undefined
}
function isDetail(value: unknown): value is DebateDetail {
  return isRecord(value) && isDebate(value.debate) && Array.isArray(value.turns)
    && value.turns.length <= 3 && value.turns.every(isTurn)
    && value.turns.every((turn, index) => turn.round_no === index + 1)
    && value.turns.filter(turn => turn.status === 'completed').length === value.debate.completed_rounds
    && isSummaryStatus(value.summary_status)
    && (value.debate.status === 'active' ? value.summary_status.status === 'not_started' : true)
}

export function createDebateApi(client: ApiClient = authState.client) {
  async function listTopics(): Promise<Topic[]> {
    const data = await client.request('/api/v1/topics', {
      expectedStatus: 200, validate: (v): v is { items: Topic[] } => isRecord(v) && Array.isArray(v.items) && v.items.every(isTopic),
    })
    return data.items
  }

  async function listDebates(page = 1, pageSize = 20): Promise<DebateListResult> {
    if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 50) {
      throw new ApiError('format', '历史分页参数无效。')
    }
    return client.request(`/api/v1/debates?page=${page}&page_size=${pageSize}`, {
      expectedStatus: 200,
      validate: (v): v is DebateListResult => isRecord(v) && Array.isArray(v.items) && v.items.every(isDebate)
        && Number.isSafeInteger(v.total) && Number(v.total) >= 0 && v.page === page && v.page_size === pageSize,
    })
  }

  function createDebate(topicId: string, stance: Stance): Promise<Debate>
  function createDebate(request: CreateDebateRequest): Promise<Debate>
  async function createDebate(topicOrRequest: string | CreateDebateRequest, stance?: Stance): Promise<Debate> {
    const topicId = typeof topicOrRequest === 'string'
      ? topicOrRequest
      : isRecord(topicOrRequest) && typeof topicOrRequest.topic_id === 'string'
        ? topicOrRequest.topic_id
        : ''
    const resolvedStance = typeof topicOrRequest === 'string'
      ? stance
      : isRecord(topicOrRequest)
        ? topicOrRequest.user_stance
        : undefined
    const validTopicId = entityId(topicId)
    if (!isStance(resolvedStance)) {
      throw new ApiError('format', '立场参数无效。')
    }
    return client.request('/api/v1/debates', {
      method: 'POST',
      expectedStatus: 201,
      body: { topic_id: validTopicId, user_stance: resolvedStance },
      validate: isDebate,
    })
  }

  async function getDebateDetail(id: string): Promise<DebateDetail> {
    const result = await client.request(`/api/v1/debates/${entityId(id)}`, { expectedStatus: 200, validate: isDetail })
    if (result.debate.id !== id) throw new ApiError('format', '会话响应与请求不一致。')
    return result
  }

  return {
    topics: listTopics,
    listTopics,
    list: listDebates,
    listDebates,
    create(topicId: string, stance: Stance): Promise<Debate> {
      return createDebate(topicId, stance)
    },
    createDebate,
    detail: getDebateDetail,
    getDebateDetail,
    async send(id: string, content: string, requestId: string, roundNo = 1): Promise<TurnResult> {
      if (!Number.isInteger(roundNo) || roundNo < 1 || roundNo > 3) {
        throw new ApiError('format', '轮次编号无效。')
      }
      return client.request(`/api/v1/debates/${entityId(id)}/turns`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { round_no: roundNo, content, client_request_id: requestId },
        validate: (v): v is TurnResult => isRecord(v) && isTurn(v.turn) && v.turn.round_no === roundNo
          && v.turn.client_request_id === requestId && v.turn.messages[0]?.content === content
          && v.completed_rounds === (v.turn.status === 'completed' ? roundNo : roundNo - 1) && v.query_url === `/api/v1/debates/${id}`,
      })
    },
    async retryTurn(id: string, turnId: string, expectedAttemptVersion: number): Promise<TurnResult> {
      if (!Number.isSafeInteger(expectedAttemptVersion) || expectedAttemptVersion < 1) {
        throw new ApiError('format', '重试版本号无效。')
      }
      return client.request(`/api/v1/debates/${entityId(id)}/turns/${entityId(turnId)}/retry`, {
        method: 'POST', expectedStatus: [200, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { expected_attempt_version: expectedAttemptVersion },
        validate: (v): v is TurnResult => isRecord(v) && isTurn(v.turn) && v.turn.id === turnId
          && v.turn.attempt_version >= expectedAttemptVersion
          && v.completed_rounds === (v.turn.status === 'completed' ? v.turn.round_no : v.turn.round_no - 1) && v.query_url === `/api/v1/debates/${id}`,
      })
    },
    async summary(id: string): Promise<SummaryStatus> {
      return client.request(`/api/v1/debates/${entityId(id)}/summary`, {
        expectedStatus: 200,
        validate: isSummaryStatus,
      })
    },
    async requestSummary(id: string, options: { retry?: boolean; expectedAttemptVersion?: number } = {}): Promise<SummaryStatus> {
      const retry = Boolean(options.retry)
      if (retry && (!Number.isSafeInteger(options.expectedAttemptVersion) || Number(options.expectedAttemptVersion) < 1)) {
        throw new ApiError('format', '总结重试版本号无效。')
      }
      const body = retry
        ? { retry: true, expected_attempt_version: options.expectedAttemptVersion }
        : { retry: false }
      return client.request(`/api/v1/debates/${entityId(id)}/summary`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body,
        validate: (v): v is SummaryStatus => isSummaryStatus(v) && v.status !== 'not_started',
      })
    },
  }
}

// 供成员 B 在 TopicsView（T08）与 HistoryView（T11）中直接按函数名调用；默认复用共享 authState.client。
export async function listTopics(client: ApiClient = authState.client): Promise<Topic[]> {
  return createDebateApi(client).listTopics()
}

export function createDebate(topicId: string, userStance: Stance, client?: ApiClient): Promise<Debate>
export function createDebate(request: CreateDebateRequest, client?: ApiClient): Promise<Debate>
export async function createDebate(
  topicOrRequest: string | CreateDebateRequest,
  stanceOrClient?: Stance | ApiClient,
  maybeClient?: ApiClient,
): Promise<Debate> {
  if (typeof topicOrRequest === 'string') {
    const client = maybeClient ?? authState.client
    return createDebateApi(client).createDebate(topicOrRequest, stanceOrClient as Stance)
  }
  const client = isApiClient(stanceOrClient) ? stanceOrClient : authState.client
  return createDebateApi(client).createDebate(topicOrRequest)
}

export async function listDebates(
  page = 1,
  pageSize = 20,
  client: ApiClient = authState.client,
): Promise<DebateListResult> {
  return createDebateApi(client).listDebates(page, pageSize)
}

export async function getDebateDetail(
  id: string,
  client: ApiClient = authState.client,
): Promise<DebateDetail> {
  return createDebateApi(client).getDebateDetail(id)
}

export type DebateApi = Pick<
  ReturnType<typeof createDebateApi>,
  'topics' | 'list' | 'create' | 'detail' | 'send' | 'retryTurn' | 'summary' | 'requestSummary'
> & Partial<Pick<
  ReturnType<typeof createDebateApi>,
  'listTopics' | 'listDebates' | 'createDebate' | 'getDebateDetail'
>>
