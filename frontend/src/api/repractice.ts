import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import { authState } from '../auth/state'
import {
  REPRACTICE_GOAL_MAX_CHARS, REPRACTICE_REQUEST_ID_MAX_CHARS, REPRACTICE_REWRITE_MAX_CHARS,
} from '../shared/limits'
import type {
  RepracticeList,
  RepracticeRetryRequest,
  RepracticeSource,
  RepracticeSubmitRequest,
  RepracticeVersion,
} from '../types/api'

/**
 * V2-08 局部重练 API。
 *
 * 约束：
 * - 提交与重试都只能由用户显式触发；本模块不做自动 POST、自动轮询或自动重试。
 * - 读取没有模型调用，使用普通短超时；提交/重试等待一次反馈生成，使用生成超时。
 * - 坐标是 Unicode 码点半开区间，与服务端锚点一致，不做 UTF-16 换算。
 * - 重练只写独立版本表；本模块不调用任何会话/轮次/复盘写接口。
 */

const errorCodes = [
  'model_error', 'model_timeout', 'model_unavailable', 'dependency_unavailable',
  'generation_cancelled', 'rate_limited', 'generation_busy', 'daily_quota_exhausted',
  'invalid_request', 'round_conflict', 'not_found',
]

function isEntityId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,19}$/.test(value) && BigInt(value) <= 18446744073709551615n
}

function hasNonWhitespace(value: string): boolean {
  return /[^\p{White_Space}\u{FEFF}]/u.test(value)
}

function isBoundedText(value: unknown, max: number, required = false): value is string {
  return typeof value === 'string' && [...value].length <= max && (!required || hasNonWhitespace(value))
}

/** 选段：编号有效、坐标有序且落在非负码点范围内。 */
function isSource(value: unknown): value is RepracticeSource {
  if (!isRecord(value)) return false
  if (!isEntityId(value.message_id) || !isBoundedText(value.quote, REPRACTICE_REWRITE_MAX_CHARS, true)) return false
  const { start_cp: start, end_cp: end } = value
  if (!Number.isInteger(start) || !Number.isInteger(end)) return false
  return (start as number) >= 0 && (end as number) > (start as number)
}

function isFeedback(value: unknown): value is { improved: boolean; change: string; reason: string; new_problem: string } {
  if (!isRecord(value) || typeof value.improved !== 'boolean') return false
  return isBoundedText(value.change, REPRACTICE_REWRITE_MAX_CHARS, false)
    && isBoundedText(value.reason, REPRACTICE_REWRITE_MAX_CHARS, true)
    && isBoundedText(value.new_problem, REPRACTICE_REWRITE_MAX_CHARS, false)
}

/** 单条版本：状态与可选字段必须自洽，不能让失败版本伪装成已完成。 */
export function isRepracticeVersion(value: unknown): value is RepracticeVersion {
  if (!isRecord(value) || !isEntityId(value.id) || !isSource(value.source)) return false
  if (!Number.isSafeInteger(value.attempt_version) || (value.attempt_version as number) < 1) return false
  if (!isBoundedText(value.goal, REPRACTICE_GOAL_MAX_CHARS, false)) return false
  if (!isBoundedText(value.rewrite, REPRACTICE_REWRITE_MAX_CHARS, true)) return false
  if (value.restored_from !== null && !isEntityId(value.restored_from)) return false
  switch (value.status) {
    case 'generating':
      return value.feedback === null && value.error_code === null
    case 'failed':
      return value.feedback === null && typeof value.error_code === 'string'
        && errorCodes.includes(value.error_code)
    case 'completed':
      return value.error_code === null && isFeedback(value.feedback)
    default:
      return false
  }
}

export function isRepracticeList(value: unknown): value is RepracticeList {
  if (!isRecord(value) || typeof value.available !== 'boolean' || !Array.isArray(value.items)) return false
  return value.items.every(isRepracticeVersion)
}

export function createRepracticeApi(client: ApiClient = authState.client) {
  /** 读取本场全部历史版本；无模型调用、不写库。 */
  async function list(debateId: string): Promise<RepracticeList> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    return client.request(`/api/v1/debates/${debateId}/repractice`, { expectedStatus: 200, validate: isRepracticeList })
  }

  /** 显式提交一次改写；保存失败会被服务端拒绝，生成失败返回 failed 版本。 */
  async function submit(debateId: string, body: RepracticeSubmitRequest): Promise<RepracticeVersion> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    if (!isEntityId(body.message_id)) throw new ApiError('format', '原句编号无效。')
    // client_request_id 由前端生成（uuid 或等价字符串），只校验非空且不超长。
    if (!body.client_request_id.trim() || body.client_request_id.length > REPRACTICE_REQUEST_ID_MAX_CHARS) {
      throw new ApiError('format', '请求编号无效。')
    }
    if (!Number.isInteger(body.start_cp) || !Number.isInteger(body.end_cp) || body.end_cp <= body.start_cp) {
      throw new ApiError('format', '选段坐标无效。')
    }
    if (!hasNonWhitespace(body.rewrite) || [...body.rewrite].length > REPRACTICE_REWRITE_MAX_CHARS) {
      throw new ApiError('format', '改写不能为空白或超出长度上限。')
    }
    if ([...body.goal].length > REPRACTICE_GOAL_MAX_CHARS) throw new ApiError('format', '改进目标超出长度上限。')
    return client.request(`/api/v1/debates/${debateId}/repractice`, {
      method: 'POST',
      expectedStatus: 200,
      timeoutMs: GENERATION_TIMEOUT_MS,
      body,
      validate: isRepracticeVersion,
    })
  }

  /** 显式重试失败版本；必须带服务器当前版本，不做自动重发。 */
  async function retry(debateId: string, versionId: string, body: RepracticeRetryRequest): Promise<RepracticeVersion> {
    if (!isEntityId(debateId) || !isEntityId(versionId)) throw new ApiError('format', '版本编号无效。')
    if (!Number.isSafeInteger(body.expected_attempt_version) || body.expected_attempt_version < 1) {
      throw new ApiError('format', '重试版本无效。')
    }
    return client.request(`/api/v1/debates/${debateId}/repractice/${versionId}/retry`, {
      method: 'POST',
      expectedStatus: 200,
      timeoutMs: GENERATION_TIMEOUT_MS,
      body,
      validate: isRepracticeVersion,
    })
  }

  return { list, submit, retry }
}

export type RepracticeApi = ReturnType<typeof createRepracticeApi>
