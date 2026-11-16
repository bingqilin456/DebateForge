import {
  USER_CONTENT_MAX_CHARS, AI_CONTENT_MAX_CHARS, TOPIC_TITLE_MAX_CHARS, TOPIC_DESCRIPTION_MAX_CHARS,
  SUMMARY_TEXT_MAX_CHARS, SUMMARY_LIST_MAX_ITEMS, STRATEGY_TEXT_MAX_CHARS, STRATEGY_LIST_MAX_ITEMS,
  STRATEGY_CONTENT_MAX_BYTES, HISTORY_PAGE_SIZE, HISTORY_MAX_PAGE_SIZE,
} from '../shared/limits'
import { readSSE } from './sse'
import { authState } from '../auth/state'
import type {
  AIStyle,
  StreamDone,
  StreamUpdate,
  TurnStreamResult,
  StrategyStatus,
  StrategyRequest,
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
import { ApiError, GENERATION_TIMEOUT_MS, isRecord, streamError, type ApiClient } from './client'

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
  return isRecord(value) && isEntityId(value.id) && text(value.title, TOPIC_TITLE_MAX_CHARS) && typeof value.description === 'string' && [...value.description].length <= TOPIC_DESCRIPTION_MAX_CHARS
}
// 仅检查协议支持的总轮数；业务推进与响应校验始终使用当前会话传入值。
function isTotalRounds(value: number): boolean {
  return value === 3 || value === 4
}

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
    && typeof value.total_rounds === 'number' && isTotalRounds(value.total_rounds)
    && Number.isInteger(value.completed_rounds) && Number(value.completed_rounds) >= 0
    && Number(value.completed_rounds) <= Number(value.total_rounds)
    && date(value.created_at) && date(value.updated_at))) return false
  // 赛制与总轮数必须自洽：quick_3 恒为 3，standard_4 恒为 4。
  return (value.mode === 'quick_3' && Number(value.total_rounds) === 3)
    || (value.mode === 'standard_4' && Number(value.total_rounds) === 4)
}
function isMessage(value: unknown): value is Message {
  return isRecord(value) && isEntityId(value.id) && (value.role === 'user' || value.role === 'assistant') && text(value.content, value.role === 'user' ? USER_CONTENT_MAX_CHARS : AI_CONTENT_MAX_CHARS) && date(value.created_at)
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
function isTurn(value: unknown, totalRounds: number): value is Turn {
  if (!isRecord(value) || !isEntityId(value.id) || !Number.isInteger(value.round_no) || Number(value.round_no) < 1 || Number(value.round_no) > totalRounds
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
export function isSummaryContent(value: unknown, totalRounds: number): value is SummaryContent {
  const inRange = (v: unknown): boolean =>
    isRecord(v) && Number.isInteger(v.round_no) && Number(v.round_no) >= 1 && Number(v.round_no) <= totalRounds
  const isPoint = (v: unknown): v is SummaryPoint =>
    inRange(v) && text((v as Record<string, unknown>).point, SUMMARY_TEXT_MAX_CHARS) && text((v as Record<string, unknown>).reason, SUMMARY_TEXT_MAX_CHARS)
  const isSuggestion = (v: unknown): v is SummarySuggestion =>
    inRange(v) && text((v as Record<string, unknown>).action, SUMMARY_TEXT_MAX_CHARS) && text((v as Record<string, unknown>).example, SUMMARY_TEXT_MAX_CHARS)
  return isRecord(value)
    && Array.isArray(value.strengths) && value.strengths.length >= 1 && value.strengths.length <= SUMMARY_LIST_MAX_ITEMS && value.strengths.every(isPoint)
    && Array.isArray(value.issues) && value.issues.length >= 1 && value.issues.length <= SUMMARY_LIST_MAX_ITEMS && value.issues.every(isPoint)
    && Array.isArray(value.suggestions) && value.suggestions.length >= 1 && value.suggestions.length <= SUMMARY_LIST_MAX_ITEMS && value.suggestions.every(isSuggestion)
}
export function isSummaryStatus(value: unknown, totalRounds: number): value is SummaryStatus {
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
  return value.turns.length <= total && value.turns.every(turn => isTurn(turn, total))
    && value.turns.every((turn, index) => turn.round_no === index + 1)
    && value.turns.filter(turn => turn.status === 'completed').length === value.debate.completed_rounds
    && isSummaryStatus(value.summary_status, total)
    && (value.debate.status === 'active' ? value.summary_status.status === 'not_started' : true)
}

function isStrategyStatus(value: unknown): value is StrategyStatus {
  if (!isRecord(value)) return false
  const keys = Object.keys(value).length
  if (value.status === 'not_started') return keys === 2 && value.attempt_version === null
  if (!Number.isSafeInteger(value.attempt_version) || Number(value.attempt_version) < 1) return false
  if (value.status === 'generating') return keys === 2
  if (value.status === 'failed') return keys === 3 && typeof value.error_code === 'string' && errorCodes.includes(value.error_code)
  if (value.status !== 'completed' || keys !== 6 || value.schema_version !== 1 || typeof value.prompt_version !== 'string' || value.prompt_version.length === 0
    || !isAIStyle(value.ai_style) || !isRecord(value.content) || Object.keys(value.content).length !== 3) return false
  const validText = (v: unknown): v is string => text(v, STRATEGY_TEXT_MAX_CHARS)
    && [...v].every(char => { const point = char.codePointAt(0)!; return point < 0xd800 || point > 0xdfff })
  const validList = (v: unknown): v is string[] => Array.isArray(v) && v.length >= 1 && v.length <= STRATEGY_LIST_MAX_ITEMS && v.every(validText)
  return validText(value.content.thesis) && validList(value.content.arguments) && validList(value.content.risks)
    && new TextEncoder().encode(JSON.stringify(value.content)).length <= STRATEGY_CONTENT_MAX_BYTES
}
export function createDebateApi(client: ApiClient = authState.client) {
  async function listTopics(): Promise<Topic[]> {
    const data = await client.request('/api/v1/topics', {
      expectedStatus: 200, validate: (v): v is { items: Topic[] } => isRecord(v) && Array.isArray(v.items) && v.items.every(isTopic),
    })
    return data.items
  }

  async function listDebates(page = 1, pageSize = HISTORY_PAGE_SIZE): Promise<DebateListResult> {
    if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > HISTORY_MAX_PAGE_SIZE) {
      throw new ApiError('format', '历史分页参数无效。')
    }
    return client.request(`/api/v1/debates?page=${page}&page_size=${pageSize}`, {
      expectedStatus: 200,
      validate: (v): v is DebateListResult => isRecord(v) && Array.isArray(v.items) && v.items.every(isDebate)
        && Number.isSafeInteger(v.total) && Number(v.total) >= 0 && v.page === page && v.page_size === pageSize,
    })
  }

  function createDebate(topicId: string, stance: Stance, aiStyle?: AIStyle): Promise<Debate>
  function createDebate(request: CreateDebateRequest): Promise<Debate>
  async function createDebate(topicOrRequest: string | CreateDebateRequest, stance?: Stance, aiStyle?: AIStyle): Promise<Debate> {
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
    const resolvedStyle = typeof topicOrRequest === 'string' ? aiStyle : topicOrRequest?.ai_style
    if (resolvedStyle !== undefined && !isAIStyle(resolvedStyle)) throw new ApiError('format', 'AI 流派参数无效。')
    const validTopicId = entityId(topicId)
    if (!isStance(resolvedStance)) {
      throw new ApiError('format', '立场参数无效。')
    }
    return client.request('/api/v1/debates', {
      method: 'POST',
      expectedStatus: 201,
      body: { topic_id: validTopicId, user_stance: resolvedStance, ...(resolvedStyle === undefined ? {} : { ai_style: resolvedStyle }) },
      validate: isDebate,
    })
  }

  async function getDebateDetail(id: string): Promise<DebateDetail> {
    const result = await client.request(`/api/v1/debates/${entityId(id)}`, { expectedStatus: 200, validate: isDetail })
    if (result.debate.id !== id) throw new ApiError('format', '会话响应与请求不一致。')
    return result
  }

  return {
    async stream(id: string, content: string, key: string, roundNo: number, totalRounds: number,
      update: (event: StreamUpdate) => void, signal?: AbortSignal): Promise<TurnStreamResult> {
      const debateId = entityId(id)
      if (!isTotalRounds(totalRounds) || !Number.isInteger(roundNo) || roundNo < 1 || roundNo > totalRounds || !uuid.test(key)
        || !text(content, USER_CONTENT_MAX_CHARS)) throw new ApiError('format', '轮次输入或请求标识无效。')
      const belongs = (value: unknown): value is Turn => isTurn(value, totalRounds) && value.round_no === roundNo
        && value.client_request_id === key && value.messages[0]?.content === content
      return client.requestStream<StreamDone, TurnResult>('/api/v1/debates/' + debateId + '/turns/stream', {
        body: { round_no: roundNo, content, client_request_id: key }, signal,
        validatePending: (value): value is TurnResult => isRecord(value) && belongs(value.turn)
          && value.turn.status === 'generating' && value.completed_rounds === roundNo - 1
          && value.query_url === '/api/v1/debates/' + debateId,
        consume: async (response, requestId, abort) => {
          let started: string | null = null
          let result: StreamDone | null = null
          let characters = 0
          const invalid = () => new ApiError('format', '流式事件不符合当前会话约定，请查询状态。')
          if (!response.body) throw invalid()
          await readSSE(response.body, (name, value) => {
            if (!isRecord(value)) throw invalid()
            if (name === 'start') {
              if (started || Object.keys(value).length !== 2 || !isEntityId(value.turn_id) || value.request_id !== requestId) throw invalid()
              started = value.turn_id
              update({ type: 'start', turn_id: started, request_id: requestId })
            } else if (name === 'delta') {
              if (!started || Object.keys(value).length !== 1 || typeof value.text !== 'string'
                || [...value.text].some(char => { const point = char.codePointAt(0)!; return point >= 0xd800 && point <= 0xdfff })) throw invalid()
              characters += [...value.text].length
              if (characters > AI_CONTENT_MAX_CHARS) throw invalid()
              update({ type: 'delta', text: value.text })
            } else if (name === 'done') {
              if (!started || Object.keys(value).length !== 3 || !belongs(value.turn) || value.turn.id !== started
                || value.turn.status !== 'completed' || !Number.isInteger(value.completed_rounds)
                || Number(value.completed_rounds) < roundNo || Number(value.completed_rounds) > totalRounds
                || value.status !== (value.completed_rounds === totalRounds ? 'completed' : 'active')) throw invalid()
              result = { turn: value.turn, completed_rounds: Number(value.completed_rounds), status: value.completed_rounds === totalRounds ? 'completed' : 'active' }
              return true
            } else if (name === 'error') {
              if (value.request_id !== requestId || !isRecord(value.error) || typeof value.error.code !== 'string'
                || !value.error.code.trim() || typeof value.error.message !== 'string' || !value.error.message.trim()) throw invalid()
              throw streamError(value.error.code, requestId)
            } else throw invalid()
          }, abort)
          if (!result) throw new ApiError('network', '连接结束但未收到提交确认，请查询已保存状态。')
          return result
        },
      })
    },
    async getStrategy(id: string): Promise<StrategyStatus> {
      return client.request(`/api/v1/debates/${entityId(id)}/strategy`, { expectedStatus: 200, validate: isStrategyStatus })
    },
    async generateStrategy(id: string, body: StrategyRequest = {}): Promise<StrategyStatus> {
      // 精确遵守新接口的严格输入；不复用总结的 Boolean() 强制转换。
      if (!isRecord(body) || Object.keys(body).some(key => key !== 'retry' && key !== 'expected_attempt_version')
        || (body.retry !== undefined && typeof body.retry !== 'boolean')
        || (body.retry === true
          ? !Number.isSafeInteger(body.expected_attempt_version) || body.expected_attempt_version < 1
          : 'expected_attempt_version' in body)) throw new ApiError('format', '策略重试参数无效。')
      return client.request(`/api/v1/debates/${entityId(id)}/strategy`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS, body,
        validate: (value): value is StrategyStatus => isStrategyStatus(value) && value.status !== 'not_started',
      })
    },
    topics: listTopics,
    listTopics,
    list: listDebates,
    listDebates,
    create: createDebate,
    createDebate,
    detail: getDebateDetail,
    getDebateDetail,
    async send(id: string, content: string, requestId: string, roundNo: number, totalRounds: number): Promise<TurnResult> {
      // 轮次上限由调用方按会话的 total_rounds 传入，不再固定为 3。
      if (!isTotalRounds(totalRounds) || !Number.isInteger(roundNo) || roundNo < 1 || roundNo > totalRounds) {
        throw new ApiError('format', '轮次编号无效。')
      }
      if (!text(content, USER_CONTENT_MAX_CHARS) || !uuid.test(requestId)) throw new ApiError('format', '发言或请求标识无效。')
      return client.request(`/api/v1/debates/${entityId(id)}/turns`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { round_no: roundNo, content, client_request_id: requestId },
        validate: (v): v is TurnResult => isRecord(v) && isTurn(v.turn, totalRounds) && v.turn.round_no === roundNo
          && v.turn.client_request_id === requestId && v.turn.messages[0]?.content === content
          && v.completed_rounds === (v.turn.status === 'completed' ? roundNo : roundNo - 1) && v.query_url === `/api/v1/debates/${id}`,
      })
    },
    async retryTurn(id: string, turnId: string, expectedAttemptVersion: number, totalRounds: number): Promise<TurnResult> {
      if (!isTotalRounds(totalRounds) || !Number.isSafeInteger(expectedAttemptVersion) || expectedAttemptVersion < 1) {
        throw new ApiError('format', '重试版本号无效。')
      }
      return client.request(`/api/v1/debates/${entityId(id)}/turns/${entityId(turnId)}/retry`, {
        method: 'POST', expectedStatus: [200, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { expected_attempt_version: expectedAttemptVersion },
        validate: (v): v is TurnResult => isRecord(v) && isTurn(v.turn, totalRounds) && v.turn.id === turnId
          && v.turn.attempt_version >= expectedAttemptVersion
          && v.completed_rounds === (v.turn.status === 'completed' ? v.turn.round_no : v.turn.round_no - 1) && v.query_url === `/api/v1/debates/${id}`,
      })
    },
    // 总结的合法轮号范围同样由该会话赛制决定，因此必须由调用方传入 total_rounds；
    async summary(id: string, totalRounds: number): Promise<SummaryStatus> {
      if (!isTotalRounds(totalRounds)) throw new ApiError('format', '会话总轮数无效。')
      return client.request(`/api/v1/debates/${entityId(id)}/summary`, {
        expectedStatus: 200,
        validate: (v): v is SummaryStatus => isSummaryStatus(v, totalRounds),
      })
    },
    async requestSummary(id: string, options: { retry?: boolean; expectedAttemptVersion?: number; totalRounds: number }): Promise<SummaryStatus> {
      const retry = Boolean(options.retry)
      if (retry && (!Number.isSafeInteger(options.expectedAttemptVersion) || Number(options.expectedAttemptVersion) < 1)) {
        throw new ApiError('format', '总结重试版本号无效。')
      }
      const totalRounds = options.totalRounds
      const body = retry
        ? { retry: true, expected_attempt_version: options.expectedAttemptVersion }
        : { retry: false }
      if (!isTotalRounds(totalRounds)) throw new ApiError('format', '会话总轮数无效。')
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
  pageSize = HISTORY_PAGE_SIZE,
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
  'listTopics' | 'listDebates' | 'createDebate' | 'getDebateDetail' | 'stream'
>>
