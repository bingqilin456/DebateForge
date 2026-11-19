import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import { authState } from '../auth/state'
import type {
  IssueCardAnalysisStatus,
  IssueCardAnchor,
  IssueCardCitation,
  IssueCardEditRequest,
  IssueCardItem,
  IssueCardList,
  IssueCardState,
} from '../types/issueCard'

/**
 * 争点卡 API：读取赛后整理结果、显式触整理、提交人工改名 / 合并 / 移除。
 *
 * 约束：
 * - 读取无模型调用，使用普通短超时；整理会等待一次分析，使用生成超时。
 * - 没有自动 POST、自动重试或轮询入口：整理与编辑都只能由用户显式触发。
 * - 所有响应按契约严格校验；引用坐标缺失或不可信时保留 resolved=false 交给展示层降级。
 */

const states: IssueCardState[] = ['not_ready', 'unsupported', 'ready']
const analysisStatuses: IssueCardAnalysisStatus[] = ['not_started', 'generating', 'completed', 'failed']
const actions = ['rename', 'merge', 'remove'] as const
// 与后端一致：争点卡可返回的受控错误码。
const errorCodes = ['model_error', 'model_timeout', 'model_unavailable', 'dependency_unavailable']

export function isEntityId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,19}$/.test(value) && BigInt(value) <= 18446744073709551615n
}

function isBoundedText(value: unknown, max: number): value is string {
  return typeof value === 'string' && value.length > 0 && [...value].length <= max && /[^\p{White_Space}\u{FEFF}]/u.test(value)
}

function isAnchor(value: unknown): value is IssueCardAnchor {
  if (!isRecord(value)) return false
  if (!isEntityId(value.message_id) || typeof value.quote !== 'string') return false
  // 引用保留原文空白：只校验非空、非全空白与长度，不做 trim。
  if ([...value.quote].length === 0 || !/[^\p{White_Space}\u{FEFF}]/u.test(value.quote)) return false
  if (typeof value.resolved !== 'boolean') return false
  for (const key of ['start_cp', 'end_cp'] as const) {
    const point = value[key]
    if (point === null) continue
    if (!Number.isInteger(point) || (point as number) < 0) return false
  }
  if (value.resolved) {
    // 已解析必须有坐标，且区间有序。
    if (value.start_cp === null || value.end_cp === null) return false
    if ((value.end_cp as number) < (value.start_cp as number)) return false
  } else if (value.start_cp !== null || value.end_cp !== null) {
    // 待核对不得携带坐标，避免前端按半截坐标高亮。
    return false
  }
  return true
}

function isCitation(value: unknown): value is IssueCardCitation {
  if (!isRecord(value)) return false
  if (!isAnchor(value.anchor)) return false
  if (value.speaker !== 'user' && value.speaker !== 'assistant') return false
  if (!Number.isInteger(value.turn_no) || (value.turn_no as number) < 1) return false
  if (!isBoundedText(value.note, 200)) return false
  return true
}

function isTextList(value: unknown, max: number, maxCount: number): value is string[] {
  return Array.isArray(value) && value.length <= maxCount && value.every(entry => isBoundedText(entry, max))
}

function isItem(value: unknown): value is IssueCardItem {
  if (!isRecord(value)) return false
  if (!isEntityId(value.id)) return false
  if (!isBoundedText(value.title, 80) || !isBoundedText(value.explanation, 600)) return false
  if (value.title_source !== 'ai' && value.title_source !== 'user') return false
  if (!isCitation(value.origin)) return false
  if (!Array.isArray(value.responses) || value.responses.length > 6 || !value.responses.every(isCitation)) return false
  if (!isTextList(value.pending, 300, 5)) return false
  if (!isTextList(value.notices, 300, 8)) return false
  if (!Number.isSafeInteger(value.version) || (value.version as number) < 1) return false
  return true
}

function isAnalysis(value: unknown): value is IssueCardList['analysis'] {
  if (!isRecord(value)) return false
  if (typeof value.status !== 'string' || !analysisStatuses.includes(value.status as IssueCardAnalysisStatus)) return false
  if (!Number.isSafeInteger(value.attempt_version) || (value.attempt_version as number) < 0) return false
  if (value.status === 'failed') return typeof value.error_code === 'string' && errorCodes.includes(value.error_code)
  // 非失败状态不得携带错误码，避免把半截状态渲染成“已完成”。
  return value.error_code === undefined
}

function isList(value: unknown): value is IssueCardList {
  if (!isRecord(value)) return false
  if (typeof value.state !== 'string' || !states.includes(value.state as IssueCardState)) return false
  if (!Number.isSafeInteger(value.display_cap) || (value.display_cap as number) < 1) return false
  if (!isAnalysis(value.analysis)) return false
  if (!Array.isArray(value.items) || value.items.length > (value.display_cap as number) || !value.items.every(isItem)) return false
  // 只对已完赛的四轮场返回 ready 与卡片；隔离状态不得携带卡片。
  if (value.state !== 'ready') return value.items.length === 0
  return true
}

function isItemPayload(value: unknown): value is IssueCardItem {
  return isItem(value)
}

export function createIssueCardApi(client: ApiClient = authState.client) {
  /** 读取当前会话的争点卡；无模型调用、不写库。 */
  async function list(debateId: string): Promise<IssueCardList> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    return client.request(`/api/v1/debates/${debateId}/issue-cards`, { expectedStatus: 200, validate: isList })
  }

  /**
   * 显式整理一次赛后争点卡。
   * 重复触发只会读回既有结果；分析槽忙返回 202 与当前状态，由调用方稍后再读。
   */
  async function analyze(debateId: string, request: { retry?: boolean; expected_attempt_version?: number } = {}): Promise<IssueCardList> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    const body: Record<string, unknown> = {}
    if (request.retry) body.retry = true
    if (request.expected_attempt_version !== undefined) {
      if (!Number.isSafeInteger(request.expected_attempt_version) || request.expected_attempt_version < 0) {
        throw new ApiError('format', '整理版本无效。')
      }
      body.expected_attempt_version = request.expected_attempt_version
    }
    return client.request(`/api/v1/debates/${debateId}/issue-cards/analyze`, {
      method: 'POST',
      expectedStatus: [200, 201, 202],
      timeoutMs: GENERATION_TIMEOUT_MS,
      body,
      validate: isList,
    })
  }

  /**
   * 提交一次人工编辑。版本过期返回 409，调用方应重新读取而不是自行换版本重发；
   * 已移除或已合并的卡再次编辑按 400 处理。
   */
  async function edit(debateId: string, cardId: string, request: IssueCardEditRequest): Promise<IssueCardItem> {
    if (!isEntityId(debateId) || !isEntityId(cardId)) throw new ApiError('format', '会话或卡片编号无效。')
    return client.request(`/api/v1/debates/${debateId}/issue-cards/${cardId}/edit`, {
      method: 'POST',
      expectedStatus: 200,
      body: request,
      validate: (value): value is IssueCardItem => isItemPayload(value) && value.id === cardId,
    })
  }

  return { list, analyze, edit }
}

export type IssueCardApi = ReturnType<typeof createIssueCardApi>

/** 供组件按函数名直接调用；默认复用共享 authState.client。 */
export async function listIssueCards(debateId: string, client?: ApiClient): Promise<IssueCardList> {
  return createIssueCardApi(client).list(debateId)
}

export async function analyzeIssueCards(
  debateId: string,
  request: { retry?: boolean; expected_attempt_version?: number } = {},
  client?: ApiClient,
): Promise<IssueCardList> {
  return createIssueCardApi(client).analyze(debateId, request)
}

export async function editIssueCard(
  debateId: string,
  cardId: string,
  request: IssueCardEditRequest,
  client?: ApiClient,
): Promise<IssueCardItem> {
  return createIssueCardApi(client).edit(debateId, cardId, request)
}

/** 与服务端一致的动作取值，避免测试与组件各写一份字面量。 */
export const issueCardActions = actions
