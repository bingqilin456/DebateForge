import { authState } from '../auth/state'
import type {
  AIStyle,
  CreateDebateRequest,
  Debate,
  DebateDetail,
  DebateListResult,
  DebateMode,
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
// 赛制上限由后端 DTO 的 total_rounds 提供，前端不保留第二份轮数常量。
// 未知赛制或越界轮数一律判为格式错误，避免用错误上限推进界面状态机。
const MAX_KNOWN_ROUNDS = 4

function isMode(value: unknown): value is DebateMode {
  return value === 'quick_3' || value === 'standard_4'
}

function isAIStyle(value: unknown): value is AIStyle {
  return value === 'logical' || value === 'aggressive' || value === 'empathic' || value === 'data'
}

function isDebate(value: unknown): value is Debate {
  if (!(isRecord(value) && isEntityId(value.id) && isTopic(value.topic)
    && (value.user_stance === 'pro' || value.user_stance === 'con')
    && value.ai_stance === (value.user_stance === 'pro' ? 'con' : 'pro')
    && isMode(value.mode) && isAIStyle(value.ai_style)
    && (value.status === 'active' || value.status === 'completed')
    && Number.isInteger(value.total_rounds) && Number(value.total_rounds) >= 1 && Number(value.total_rounds) <= MAX_KNOWN_ROUNDS
    && Number.isInteger(value.completed_rounds) && Number(value.completed_rounds) >= 0
    && Number(value.completed_rounds) <= Number(value.total_rounds)
    && date(value.created_at) && date(value.updated_at))) return false
  // 赛制与总轮数必须自洽：quick_3 恒为 3，standard_4 恒为 4。
  return (value.mode === 'quick_3' && Number(value.total_rounds) === 3)
    || (value.mode === 'standard_4' && Number(value.total_rounds) === 4)
}
function isMessage(value: unknown): value is Message {
  return isRecord(value) && isEntityId(value.id) && (value.role === 'user' || value.role === 'assistant') && text(value.content, 2000) && date(value.created_at)
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
function isTurn(value: unknown): value is Turn {
  if (!isRecord(value) || !isEntityId(value.id) || !Number.isInteger(value.round_no) || Number(value.round_no) < 1 || Number(value.round_no) > MAX_KNOWN_ROUNDS
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
export function isSummaryContent(value: unknown, totalRounds = MAX_KNOWN_ROUNDS): value is SummaryContent {
  const inRange = (v: unknown): boolean =>
    isRecord(v) && Number.isInteger(v.round_no) && Number(v.round_no) >= 1 && Number(v.round_no) <= totalRounds
  const isPoint = (v: unknown): v is SummaryPoint =>
    inRange(v) && text((v as Record<string, unknown>).point, 300) && text((v as Record<string, unknown>).reason, 300)
  const isSuggestion = (v: unknown): v is SummarySuggestion =>
    inRange(v) && text((v as Record<string, unknown>).action, 300) && text((v as Record<string, unknown>).example, 300)
  return isRecord(value)
    && Array.isArray(value.strengths) && value.strengths.length >= 1 && value.strengths.length <= 3 && value.strengths.every(isPoint)
    && Array.isArray(value.issues) && value.issues.length >= 1 && value.issues.length <= 3 && value.issues.every(isPoint)
    && Array.isArray(value.suggestions) && value.suggestions.length >= 1 && value.suggestions.length <= 3 && value.suggestions.every(isSuggestion)
}
export function isSummaryStatus(value: unknown, totalRounds = MAX_KNOWN_ROUNDS): value is SummaryStatus {
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
  return value.status === 'completed' && value.schema_version === 1
    && isSummaryContent(value.content, totalRounds) && value.error_code === undefined
}
function isDetail(value: unknown): value is DebateDetail {
  // 轮次数量与总结轮号上限都取自该会话自身的 total_rounds，而不是全局常量。
  if (!(isRecord(value) && isDebate(value.debate) && Array.isArray(value.turns))) return false
  const total = Number(value.debate.total_rounds)
  return value.turns.length <= total && value.turns.every(isTurn)
    && value.turns.every((turn, index) => turn.round_no === index + 1)
    && value.turns.filter(turn => turn.status === 'completed').length === value.debate.completed_rounds
    && isSummaryStatus(value.summary_status, total)
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
    async send(id: string, content: string, requestId: string, roundNo = 1, totalRounds = MAX_KNOWN_ROUNDS): Promise<TurnResult> {
      // 轮次上限由调用方按会话的 total_rounds 传入，不再固定为 3。
      if (!Number.isInteger(roundNo) || roundNo < 1 || roundNo > totalRounds) {
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
    // 总结的合法轮号范围同样由该会话赛制决定，因此必须由调用方传入 total_rounds；
    // 缺省时退回“所有已知赛制的最大轮数”，只作为格式防御，不作为业务上限。
    async summary(id: string, totalRounds = MAX_KNOWN_ROUNDS): Promise<SummaryStatus> {
      return client.request(`/api/v1/debates/${entityId(id)}/summary`, {
        expectedStatus: 200,
        validate: (v): v is SummaryStatus => isSummaryStatus(v, totalRounds),
      })
    },
    async requestSummary(id: string, options: { retry?: boolean; expectedAttemptVersion?: number; totalRounds?: number } = {}): Promise<SummaryStatus> {
      const retry = Boolean(options.retry)
      if (retry && (!Number.isSafeInteger(options.expectedAttemptVersion) || Number(options.expectedAttemptVersion) < 1)) {
        throw new ApiError('format', '总结重试版本号无效。')
      }
      const totalRounds = options.totalRounds ?? MAX_KNOWN_ROUNDS
      const body = retry
        ? { retry: true, expected_attempt_version: options.expectedAttemptVersion }
        : { retry: false }
      return client.request(`/api/v1/debates/${entityId(id)}/summary`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body,
        validate: (v): v is SummaryStatus => isSummaryStatus(v, totalRounds) && v.status !== 'not_started',
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
