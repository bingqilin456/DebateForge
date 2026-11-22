import { USER_CONTENT_MAX_CHARS, AI_CONTENT_MAX_CHARS } from '../shared/limits'
import { readSSE } from './sse'
import { authState } from '../auth/state'
import type {
  AIStyle,
  Debate,
  DebateDetail,
  DebateStage,
  Stance,
  TrainingAbandonedAI,
  TrainingDetail,
  TrainingPendingAI,
  TrainingProgress,
  TrainingSpeaker,
  TrainingStep,
  TrainingStepResult,
  TrainingStreamDone,
  TrainingStreamUpdate,
} from '../types/api'
import { ApiError, GENERATION_TIMEOUT_MS, isRecord, streamError, type ApiClient } from './client'
import { isEntityId } from './debate'

// 产品 V2 训练赛的前端 API 封装。
//
// 与 V1 的关键差异：训练赛的进度由服务端权威推进并持久化，前端只按
// `progress` 渲染与决定下一步，不自行推导阶段、回合或发言方（PRD V2 §3.7）。
// 因此这里的所有校验都只做「形状与自洽」，不做业务推演。

export const TRAINING_FREE_DEBATE_MAX_ROUNDS = 10

const stages: readonly DebateStage[] = ['opening', 'rebuttal', 'free_debate', 'closing']
const speakers: readonly TrainingSpeaker[] = ['user', 'assistant']
const claimStatuses = ['generating', 'failed', 'completed'] as const
const endBlockedReasons = ['not_free_debate', 'reply_in_progress', 'no_completed_round', 'round_incomplete'] as const

export function isDebateStage(value: unknown): value is DebateStage {
  return typeof value === 'string' && (stages as readonly string[]).includes(value)
}
function isSpeaker(value: unknown): value is TrainingSpeaker {
  return typeof value === 'string' && (speakers as readonly string[]).includes(value)
}
function isClaimId(value: unknown): value is string {
  return isEntityId(value)
}
function isCountInRange(value: unknown, min: number, max: number): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max
}

function isPendingAI(value: unknown): value is TrainingPendingAI {
  if (!isRecord(value)) return false
  const keys = Object.keys(value).length
  if (keys !== 5) return false
  return isClaimId(value.claim_id) && isCountInRange(value.step_no, 1, 26)
    && typeof value.status === 'string' && (claimStatuses as readonly string[]).includes(value.status)
    && Number.isSafeInteger(value.attempt_version) && Number(value.attempt_version) >= 1
    && (value.error_code === null || typeof value.error_code === 'string')
    && (value.status === 'failed') === (value.error_code !== null)
}

function isAbandonedAI(value: unknown): value is TrainingAbandonedAI {
  if (!isRecord(value)) return false
  if (Object.keys(value).length !== 4) return false
  return isClaimId(value.claim_id) && isCountInRange(value.step_no, 1, 26)
    && Number.isSafeInteger(value.attempt_version) && Number(value.attempt_version) >= 1
    && (value.error_code === null || typeof value.error_code === 'string')
}

// isTrainingProgress 只校验形状与字段间自洽，不重算阶段（阶段是服务端事实）。
export function isTrainingProgress(value: unknown): value is TrainingProgress {
  if (!isRecord(value) || !isDebateStage(value.stage)) return false
  if (!isCountInRange(value.stage_step, 0, 2)) return false
  if (!isCountInRange(value.free_debate_rounds, 0, TRAINING_FREE_DEBATE_MAX_ROUNDS)) return false
  if (value.free_debate_max_rounds !== TRAINING_FREE_DEBATE_MAX_ROUNDS) return false
  if (!isCountInRange(value.committed_steps, 0, 26)) return false
  if (!isCountInRange(value.total_steps, 0, 26)) return false
  if (value.next_step_no !== null && !isCountInRange(value.next_step_no, 1, 26)) return false
  if (value.next_speaker !== null && !isSpeaker(value.next_speaker)) return false
  if (typeof value.can_submit_step !== 'boolean' || typeof value.can_retry_ai !== 'boolean' || typeof value.can_end_free_debate !== 'boolean') return false
  if (value.end_blocked_reason !== null
    && (typeof value.end_blocked_reason !== 'string' || !(endBlockedReasons as readonly string[]).includes(value.end_blocked_reason))) return false
  if (value.pending_ai !== null && !isPendingAI(value.pending_ai)) return false
  if (value.abandoned_ai !== null && !isAbandonedAI(value.abandoned_ai)) return false
  // 发言方与步骤号必须成对出现或成对为空。
  if ((value.next_step_no === null) !== (value.next_speaker === null)) return false
  // 可提交必须同时具备下一步与发言方。
  if (value.can_submit_step && value.next_step_no === null) return false
  // 结辩阶段的 total_steps 必须已知（总步数由自由辩实际回合数决定）。
  if (value.stage === 'closing' && value.total_steps < value.committed_steps) return false
  return true
}

function isTrainingStep(value: unknown): value is TrainingStep {
  if (!isRecord(value)) return false
  if (!isEntityId(value.id) || !isCountInRange(value.step_no, 1, 26)) return false
  if (!isDebateStage(value.stage) || !isCountInRange(value.stage_step, 1, 2)) return false
  if (!isCountInRange(value.free_debate_round, 0, TRAINING_FREE_DEBATE_MAX_ROUNDS)) return false
  if (!isSpeaker(value.speaker)) return false
  if (typeof value.client_request_id !== 'string' || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value.client_request_id)) return false
  const max = value.speaker === 'user' ? USER_CONTENT_MAX_CHARS : AI_CONTENT_MAX_CHARS
  if (typeof value.content !== 'string' || value.content.trim() === '' || [...value.content].length > max) return false
  if (typeof value.created_at !== 'string' || !/^\d{4}-\d\d-\d\dT.*Z$/.test(value.created_at) || !Number.isFinite(Date.parse(value.created_at))) return false
  // 自由辩步骤必须记录回合号，其他阶段必须为 0。
  if ((value.stage === 'free_debate') !== (value.free_debate_round > 0)) return false
  return true
}

export function isTrainingDetail(value: unknown): value is TrainingDetail {
  if (!isRecord(value) || !isTrainingProgress(value.progress) || !Array.isArray(value.steps)) return false
  if (typeof value.query_url !== 'string') return false
  if (!value.steps.every(isTrainingStep)) return false
  const steps: TrainingStep[] = value.steps
  // 步骤编号连续、发言方与阶段进度一致：只做形状校验，仍以服务端 progress 为准。
  for (let i = 0; i < steps.length; i += 1) {
    if (steps[i]!.step_no !== i + 1) return false
  }
  if (steps.length !== value.progress.committed_steps) return false
  return true
}

function isTrainingStepResult(value: unknown): value is TrainingStepResult {
  if (!isRecord(value) || !isTrainingProgress(value.progress)) return false
  if (value.step !== null && !isTrainingStep(value.step)) return false
  if (value.debate_status !== 'active' && value.debate_status !== 'completed') return false
  return typeof value.query_url === 'string'
}

export interface TrainingCreateRequest {
  topic_id: string
  user_stance: Stance
  ai_style?: AIStyle
}

export interface TrainingApi {
  create(request: TrainingCreateRequest): Promise<Debate>
  detail(id: string): Promise<DebateDetail & { training: TrainingDetail }>
  submit(id: string, key: string, content: string, stage: DebateStage, step: number): Promise<TrainingStepResult>
  generate(id: string, key: string, stage: DebateStage, step: number): Promise<TrainingStepResult>
  retry(id: string, stepNo: number, expectedAttemptVersion: number): Promise<TrainingStepResult>
  endFreeDebate(id: string, failedAttemptVersion?: number): Promise<TrainingStepResult>
  stream(id: string, key: string, stage: DebateStage, step: number, update: (event: TrainingStreamUpdate) => void, signal?: AbortSignal): Promise<{ kind: 'done'; data: TrainingStreamDone } | { kind: 'pending'; data: TrainingStepResult }>
}

// createTrainingApi 绑定一个 ApiClient；所有请求仍走共享的路径、Session、CSRF 与超时封装。
export function createTrainingApi(client: ApiClient = authState.client): TrainingApi {
  // assertTrainingDebate 是类型谓词：创建只接受训练赛会话，且必须带阶段进度。
  function assertTrainingDebate(value: unknown): value is Debate {
    if (!isRecord(value) || !isEntityId(value.id)) return false
    if (value.mode !== 'training_v2') return false
    if (!isRecord(value.training) || !isDebateStage(value.training.stage)) return false
    return typeof value.total_rounds === 'number' && value.training.free_debate_max_rounds === TRAINING_FREE_DEBATE_MAX_ROUNDS
  }

  return {
    async create(request: TrainingCreateRequest): Promise<Debate> {
      if (!isRecord(request) || !isEntityId(request.topic_id)) throw new ApiError('format', '辩题编号无效。')
      if (request.user_stance !== 'pro' && request.user_stance !== 'con') throw new ApiError('format', '立场参数无效。')
      if (request.ai_style !== undefined && !['logical', 'aggressive', 'empathic', 'data'].includes(request.ai_style)) {
        throw new ApiError('format', 'AI 流派参数无效。')
      }
      return client.request('/api/v1/training-debates', {
        method: 'POST',
        expectedStatus: 201,
        body: { topic_id: request.topic_id, user_stance: request.user_stance, ...(request.ai_style === undefined ? {} : { ai_style: request.ai_style }) },
        validate: assertTrainingDebate,
      })
    },

    async detail(id: string): Promise<DebateDetail & { training: TrainingDetail }> {
      const debateId = isEntityId(id) ? id : (() => { throw new ApiError('format', '会话编号无效。') })()
      const result = await client.request(`/api/v1/debates/${debateId}`, {
        expectedStatus: 200,
        validate: (value): value is DebateDetail & { training: TrainingDetail } => {
          if (!isRecord(value)) return false
          if (!isRecord(value.debate) || value.debate.mode !== 'training_v2') return false
          if (value.debate.id !== debateId) return false
          if (!isTrainingDetail(value.training)) return false
          // 训练赛的正式发言只来自 training.steps，V1 turns 必须为空。
          return Array.isArray(value.turns) && value.turns.length === 0
        },
      })
      return result
    },

    async submit(id: string, key: string, content: string, stage: DebateStage, step: number): Promise<TrainingStepResult> {
      const debateId = isEntityId(id) ? id : (() => { throw new ApiError('format', '会话编号无效。') })()
      if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(key)) throw new ApiError('format', '请求标识无效。')
      if (typeof content !== 'string' || content.trim() === '' || [...content].length > USER_CONTENT_MAX_CHARS) throw new ApiError('format', '发言内容无效。')
      return client.request(`/api/v1/debates/${debateId}/steps`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { client_request_id: key, content, expected_stage: stage, expected_step: step },
        validate: isTrainingStepResult,
      })
    },

    async generate(id: string, key: string, stage: DebateStage, step: number): Promise<TrainingStepResult> {
      const debateId = isEntityId(id) ? id : (() => { throw new ApiError('format', '会话编号无效。') })()
      if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(key)) throw new ApiError('format', '请求标识无效。')
      return client.request(`/api/v1/debates/${debateId}/steps`, {
        method: 'POST', expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { client_request_id: key, expected_stage: stage, expected_step: step },
        validate: isTrainingStepResult,
      })
    },

    async retry(id: string, stepNo: number, expectedAttemptVersion: number): Promise<TrainingStepResult> {
      const debateId = isEntityId(id) ? id : (() => { throw new ApiError('format', '会话编号无效。') })()
      if (!Number.isInteger(stepNo) || stepNo < 1 || stepNo > 26) throw new ApiError('format', '步骤编号无效。')
      if (!Number.isSafeInteger(expectedAttemptVersion) || expectedAttemptVersion < 1) throw new ApiError('format', '重试版本号无效。')
      return client.request(`/api/v1/debates/${debateId}/steps/${stepNo}/retry`, {
        method: 'POST', expectedStatus: [200, 202], timeoutMs: GENERATION_TIMEOUT_MS,
        body: { expected_attempt_version: expectedAttemptVersion },
        validate: isTrainingStepResult,
      })
    },

    async endFreeDebate(id: string, failedAttemptVersion?: number): Promise<TrainingStepResult> {
      const debateId = isEntityId(id) ? id : (() => { throw new ApiError('format', '会话编号无效。') })()
      return client.request(`/api/v1/debates/${debateId}/free-debate/end`, {
        method: 'POST', expectedStatus: 200,
        body: { expected_failed_attempt_version: failedAttemptVersion },
        validate: (value): value is TrainingStepResult => isTrainingStepResult(value) && value.step === null,
      })
    },

    async stream(id: string, key: string, stage: DebateStage, step: number, update: (event: TrainingStreamUpdate) => void, signal?: AbortSignal) {
      const debateId = isEntityId(id) ? id : (() => { throw new ApiError('format', '会话编号无效。') })()
      if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(key)) throw new ApiError('format', '请求标识无效。')
      return client.requestStream<TrainingStreamDone, TrainingStepResult>('/api/v1/debates/' + debateId + '/steps/stream', {
        body: { client_request_id: key, expected_stage: stage, expected_step: step }, signal,
        validatePending: (value): value is TrainingStepResult => isTrainingStepResult(value),
        consume: async (response, requestId, abort) => {
          let started: string | null = null
          let done: TrainingStreamDone | null = null
          let characters = 0
          const invalid = () => new ApiError('format', '流式事件不符合训练赛约定，请查询状态。')
          if (!response.body) throw invalid()
          await readSSE(response.body, (name, value) => {
            if (!isRecord(value)) throw invalid()
            if (name === 'start') {
              if (started || Object.keys(value).length !== 2 || !isClaimId(value.claim_id) || value.request_id !== requestId) throw invalid()
              started = value.claim_id
              update({ type: 'start', claim_id: started, request_id: requestId })
            } else if (name === 'delta') {
              if (!started || Object.keys(value).length !== 1 || typeof value.text !== 'string'
                || [...value.text].some(char => { const point = char.codePointAt(0)!; return point >= 0xd800 && point <= 0xdfff })) throw invalid()
              characters += [...value.text].length
              if (characters > AI_CONTENT_MAX_CHARS) throw invalid()
              update({ type: 'delta', text: value.text })
            } else if (name === 'done') {
              if (!started || Object.keys(value).length !== 3 || !isTrainingStep(value.step) || value.step.client_request_id !== key
                || value.step.step_no < 1 || !isTrainingProgress(value.progress)
                || (value.debate_status !== 'active' && value.debate_status !== 'completed')) throw invalid()
              done = { step: value.step as TrainingStep, progress: value.progress as TrainingProgress, debate_status: value.debate_status as 'active' | 'completed' }
              return true
            } else if (name === 'error') {
              if (value.request_id !== requestId || !isRecord(value.error) || typeof value.error.code !== 'string'
                || !value.error.code.trim() || typeof value.error.message !== 'string' || !value.error.message.trim()) throw invalid()
              throw streamError(value.error.code, requestId)
            } else throw invalid()
          }, abort)
          if (!done) throw new ApiError('network', '连接结束但未收到提交确认，请查询已保存状态。')
          return done
        },
      })
    },
  }
}
