import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import { authState } from '../auth/state'
import {
  REVIEW_CONTENT_MAX_BYTES, REVIEW_KEY_ISSUES_MAX_ITEMS, REVIEW_LIST_MAX_ITEMS, REVIEW_TEXT_MAX_CHARS,
} from '../shared/limits'
import type {
  ReviewContent,
  ReviewDimension,
  ReviewDimensionKey,
  ReviewQuote,
  ReviewRequest,
  ReviewStatus,
  ReviewTextItem,
} from '../types/api'

/**
 * V2-13 复盘 API。
 *
 * 约束：
 * - 生成与重试都只能由用户显式触发，本模块不做自动 POST、自动轮询或自动重试。
 * - 读取没有模型调用，使用普通短超时；生成/重试等待一次分析，使用生成超时。
 * - 复盘与总结是两套契约：这里不解析也不猜测 schema_version=2 的 0–100 形状。
 */

const dimensionOrder: ReviewDimensionKey[] = ['argument_structure', 'responsiveness', 'evidence_use', 'concept_scope']
const dimensionLabels: Record<ReviewDimensionKey, string> = {
  argument_structure: '论证结构',
  responsiveness: '回应针对性',
  evidence_use: '证据使用',
  concept_scope: '概念与范围',
}
const levelLabels: Record<number, string> = { 1: '起步', 2: '发展中', 3: '较稳', 4: '充分' }
const errorCodes = ['model_error', 'model_timeout', 'model_unavailable', 'dependency_unavailable', 'generation_cancelled', 'content_invalid']

function isEntityId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,19}$/.test(value) && BigInt(value) <= 18446744073709551615n
}

function hasNonWhitespace(value: string): boolean {
  return /[^\p{White_Space}\u{FEFF}]/u.test(value)
}

function isBoundedText(value: unknown, max = REVIEW_TEXT_MAX_CHARS): value is string {
  return typeof value === 'string' && hasNonWhitespace(value) && [...value].length <= max
}

/** 引用：已定位必须有有序坐标；待核对必须没有坐标，两者不能同时成立。 */
function isQuote(value: unknown): value is ReviewQuote {
  if (!isRecord(value)) return false
  if (!isEntityId(value.message_id) || !isBoundedText(value.quote)) return false
  if (typeof value.resolved !== 'boolean') return false
  for (const key of ['start_cp', 'end_cp'] as const) {
    const point = value[key]
    if (point === null) continue
    if (!Number.isInteger(point) || (point as number) < 0) return false
  }
  if (value.resolved) {
    if (value.start_cp === null || value.end_cp === null) return false
    return (value.end_cp as number) > (value.start_cp as number)
  }
  return value.start_cp === null && value.end_cp === null
}

/** 四维：键与顺序固定、档级只能是 1–4 或 null、观察不足不占档位、有档级必须有引用。 */
function isDimension(value: unknown, expectedKey: ReviewDimensionKey): value is ReviewDimension {
  if (!isRecord(value)) return false
  if (value.key !== expectedKey || value.label !== dimensionLabels[expectedKey]) return false
  if (!isBoundedText(value.basis)) return false
  const level = value.level
  if (level === null) {
    // 观察不足：合法结果，不是 0 分；必须说明原因，且不得带档位标签或坐标。
    if (typeof value.insufficient_reason !== 'string' || !isBoundedText(value.insufficient_reason)) return false
    if (value.level_label !== null || value.quote !== null) return false
    return value.improvement === '' || isBoundedText(value.improvement)
  }
  if (level !== 1 && level !== 2 && level !== 3 && level !== 4) return false
  if (value.level_label !== levelLabels[level]) return false
  if (!isBoundedText(value.improvement)) return false
  if (value.insufficient_reason !== null) return false
  return isQuote(value.quote)
}

function isTextItem(value: unknown): value is ReviewTextItem {
  if (!isRecord(value) || !isBoundedText(value.text)) return false
  return value.quote === null || isQuote(value.quote)
}

function isContent(value: unknown): value is ReviewContent {
  if (!isRecord(value)) return false
  if (!Array.isArray(value.dimensions) || value.dimensions.length !== dimensionOrder.length) return false
  if (!value.dimensions.every((dimension, index) => isDimension(dimension, dimensionOrder[index]!))) return false
  const list = (items: unknown, max: number): items is ReviewTextItem[] =>
    Array.isArray(items) && items.length >= 1 && items.length <= max && items.every(isTextItem)
  if (!list(value.key_issues, REVIEW_KEY_ISSUES_MAX_ITEMS)) return false
  if (!list(value.strengths, REVIEW_LIST_MAX_ITEMS)) return false
  if (!list(value.improvements, REVIEW_LIST_MAX_ITEMS)) return false
  return new TextEncoder().encode(JSON.stringify(value)).length <= REVIEW_CONTENT_MAX_BYTES
}

export function isReviewStatus(value: unknown): value is ReviewStatus {
  if (!isRecord(value)) return false
  if (value.status === 'not_started') {
    if (value.attempt_version !== null || value.content !== undefined || value.error_code !== undefined) return false
    // available 是关闭切片的显式信号：省略等价于可用，旧 Mock 与旧响应不必改。
    return value.available === undefined || typeof value.available === 'boolean'
  }
  if (!Number.isSafeInteger(value.attempt_version) || (value.attempt_version as number) < 1) return false
  if (value.status === 'generating') return value.content === undefined && value.error_code === undefined
  if (value.status === 'failed') {
    return typeof value.error_code === 'string' && errorCodes.includes(value.error_code) && value.content === undefined
  }
  if (value.status !== 'completed' || value.schema_version !== 1 || !isContent(value.content)) return false
  return value.error_code === undefined && (value.prompt_version === undefined || typeof value.prompt_version === 'string')
}

export function createReviewApi(client: ApiClient = authState.client) {
  /** 读取复盘状态；无模型调用，不写库。 */
  async function get(debateId: string): Promise<ReviewStatus> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    return client.request(`/api/v1/debates/${debateId}/review`, { expectedStatus: 200, validate: isReviewStatus })
  }

  /**
   * 显式生成（retry=false）或显式重试（retry=true 且带期望版本）。
   * 已完成是终态：重复 POST 幂等返回 200 与已保存内容，不再生成；生成中返回 202，由调用方查询。
   */
  async function request(debateId: string, body: ReviewRequest = {}): Promise<ReviewStatus> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    if (body.retry === true) {
      if (!Number.isSafeInteger(body.expected_attempt_version) || body.expected_attempt_version < 1) {
        throw new ApiError('format', '复盘重试版本无效。')
      }
    } else if ('expected_attempt_version' in body) {
      throw new ApiError('format', '首次生成不应携带重试版本。')
    }
    return client.request(`/api/v1/debates/${debateId}/review`, {
      method: 'POST',
      expectedStatus: [200, 201, 202],
      timeoutMs: GENERATION_TIMEOUT_MS,
      body,
      validate: (value): value is ReviewStatus => isReviewStatus(value) && value.status !== 'not_started',
    })
  }

  return { get, request }
}

export type ReviewApi = ReturnType<typeof createReviewApi>

export const reviewDimensionLabels = dimensionLabels
export const reviewLevelLabels = levelLabels
export const reviewDimensionOrder = dimensionOrder
