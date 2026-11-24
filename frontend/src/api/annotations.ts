import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import { authState } from '../auth/state'
import { USER_CONTENT_MAX_CHARS } from '../shared/limits'
import type {
  AnnotationAnchor,
  AnnotationBatch,
  AnnotationBatchStatus,
  AnnotationCategory,
  AnnotationItem,
  AnnotationList,
  AnnotationRetryRequest,
} from '../types/api'

/**
 * 批注 API：读取已有批次，或显式补做一次失败/未启动的批次。
 *
 * 约束：
 * - 没有自动 POST、自动重试或轮询入口：补做只能由用户显式触发。
 * - 读取无模型调用，因此使用普通短超时；补做会等待一次分析，使用生成超时。
 * - 所有条目按契约严格校验，坐标不可信时保留 resolved=false 交给展示层降级。
 */

const batchStatuses: AnnotationBatchStatus[] = ['not_started', 'generating', 'completed', 'failed']
// 与后端一致：这些是批注可返回的受控错误码。
const errorCodes = ['model_error', 'model_timeout', 'model_unavailable', 'dependency_unavailable', 'generation_cancelled', 'content_invalid']

function isEntityId(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d{0,19}$/.test(value) && BigInt(value) <= 18446744073709551615n
}

function isBoundedText(value: unknown, max: number): value is string {
  return typeof value === 'string' && value.length > 0 && [...value].length <= max && /[^\p{White_Space}\u{FEFF}]/u.test(value)
}

function isAnchor(value: unknown): value is AnnotationAnchor {
  if (!isRecord(value)) return false
  if (!isEntityId(value.message_id) || typeof value.quote !== 'string' || value.quote.length === 0) return false
  // quote 必须保留原文空白，因此只校验长度与是否全空白，不做 trim。
  if ([...value.quote].length > USER_CONTENT_MAX_CHARS || !/[^\p{White_Space}\u{FEFF}]/u.test(value.quote)) return false
  if (typeof value.resolved !== 'boolean') return false
  for (const key of ['start_cp', 'end_cp'] as const) {
    const point = value[key]
    if (point === null) continue
    if (!Number.isInteger(point) || (point as number) < 0) return false
  }
  if (value.resolved) {
    // 已解析必须有坐标，且必须是非负且有序的区间。
    if (value.start_cp === null || value.end_cp === null) return false
    if ((value.end_cp as number) < (value.start_cp as number)) return false
  } else {
    // 待核对不得携带坐标：前端据此只显示“原文待核对”，避免按半截坐标高亮。
    if (value.start_cp !== null || value.end_cp !== null) return false
  }
  return true
}

function isItem(value: unknown): value is AnnotationItem {
  if (!isRecord(value)) return false
  if (typeof value.category !== 'string' || !Object.hasOwn(annotationLabels, value.category)) return false
  if (!isBoundedText(value.label, 40) || !isBoundedText(value.reason, 300) || !isBoundedText(value.improvement, 300)) return false
  if (!isAnchor(value.anchor)) return false
  if (!Number.isInteger(value.turn_no) || (value.turn_no as number) < 1) return false
  if (value.role !== 'user' && value.role !== 'assistant') return false
  return true
}

function isBatch(value: unknown): value is AnnotationBatch {
  if (!isRecord(value)) return false
  if (!isEntityId(value.turn_id) || !Number.isInteger(value.round_no) || (value.round_no as number) < 1) return false
  if (typeof value.status !== 'string' || !batchStatuses.includes(value.status as AnnotationBatchStatus)) return false
  const version = value.attempt_version
  if (!Number.isSafeInteger(version) || (version as number) < 0) return false
  if (!Array.isArray(value.items) || !value.items.every(isItem)) return false
  const status = value.status as AnnotationBatchStatus
  // 状态与条数/版本/错误码必须自洽，避免把半截状态渲染成“已完成”。
  if (status === 'not_started') return version === 0 && value.items.length === 0 && value.error_code === undefined
  if ((version as number) < 1) return false
  if (status === 'completed') return value.error_code === undefined
  if (status === 'generating') return value.items.length === 0 && value.error_code === undefined
  return value.error_code !== undefined && typeof value.error_code === 'string' && errorCodes.includes(value.error_code) && value.items.length === 0
}

function isList(value: unknown): value is AnnotationList {
  return isRecord(value) && Array.isArray(value.items) && value.items.every(isBatch)
}

export function createAnnotationApi(client: ApiClient = authState.client) {
  /** 读取当前会话的批注批次。无模型调用，不写库。 */
  async function list(debateId: string): Promise<AnnotationList> {
    if (!isEntityId(debateId)) throw new ApiError('format', '会话编号无效。')
    return client.request(`/api/v1/debates/${debateId}/annotations`, { expectedStatus: 200, validate: isList })
  }

  /**
   * 显式补做一个未启动或已失败的批次。
   * 成功批次、生成中批次只返回当前状态；版本不符返回 409，由调用方重新读取。
   */
  async function retry(debateId: string, turnId: string, expectedAttemptVersion: number): Promise<AnnotationBatch> {
    if (!isEntityId(debateId) || !isEntityId(turnId)) throw new ApiError('format', '会话或轮次编号无效。')
    if (!Number.isSafeInteger(expectedAttemptVersion) || expectedAttemptVersion < 0) {
      throw new ApiError('format', '批注重试版本无效。')
    }
    const body: AnnotationRetryRequest = { expected_attempt_version: expectedAttemptVersion }
    return client.request(`/api/v1/debates/${debateId}/turns/${turnId}/annotations/retry`, {
      method: 'POST',
      expectedStatus: [200, 201, 202],
      timeoutMs: GENERATION_TIMEOUT_MS,
      body,
      validate: (value): value is AnnotationBatch => isBatch(value) && value.turn_id === turnId,
    })
  }

  return { list, retry }
}

export type AnnotationApi = ReturnType<typeof createAnnotationApi>

// 供组件按函数名直接调用；默认复用共享 authState.client。
export async function listAnnotations(debateId: string, client?: ApiClient): Promise<AnnotationList> {
  return createAnnotationApi(client).list(debateId)
}

export async function retryAnnotation(
  debateId: string,
  turnId: string,
  expectedAttemptVersion: number,
  client?: ApiClient,
): Promise<AnnotationBatch> {
  return createAnnotationApi(client).retry(debateId, turnId, expectedAttemptVersion)
}

// 导出给测试与组件复用，避免在两处重复同一份契约常量。
export const annotationLabels: Record<AnnotationCategory, string> = {
  strength: '论证有效',
  possible_logic_gap: '可能推理缺口',
  insufficient_support: '缺少支持',
  unclear_scope: '定义待明确',
  opponent_weakness: '敌方破绽',
  opponent_strength: '敌方杀招',
}
