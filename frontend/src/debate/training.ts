import { computed, ref } from 'vue'
import { ApiError } from '../api/client'
import { hasNonWhitespace } from '../api/debate'
import type { TrainingApi } from '../api/training'
import type {
  DebateDetail,
  DebateStage,
  DebateStatus,
  TrainingDetail,
  TrainingProgress,
  TrainingStep,
  TrainingStepResult,
} from '../types/api'
import { USER_CONTENT_MAX_CHARS, GENERATION_POLL_BUDGET_MS, GENERATION_POLL_INTERVAL_MS } from '../shared/limits'
import { feedback } from './firstTurn'

// 产品 V2 训练赛的前端状态机。
//
// 关键约束（PRD V2 §3.4、§3.7）：
//   - 阶段、回合与发言方一律以服务端 progress 为准；本文件只展示、不清算。
//   - 结束自由辩是服务端确认的业务操作：重放不重复切换、不产生模型调用。
//   - 生成中的回复不可被当成正式消息；只有服务端事务提交成功后才有新步骤。
//   - 失败步骤不占编号，失败记录来自 progress.pending_ai / abandoned_ai，保持可见。

/** 训练赛详情：沿用 DebateDetail 结构并强制携带 training，与 V1 状态分开。 */
export type TrainingView = DebateDetail & { training: TrainingDetail }

function mergeStep(steps: TrainingStep[], incoming: TrainingStep): TrainingStep[] {
  const next = steps.filter(step => step.step_no !== incoming.step_no)
  next.push(incoming)
  next.sort((a, b) => a.step_no - b.step_no)
  return next
}

// stageLabel 是阶段的中文名；与后端措辞保持一致，只用于展示。
export function stageLabel(stage: string): string {
  switch (stage) {
    case 'opening': return '立论'
    case 'rebuttal': return '驳立论'
    case 'free_debate': return '自由辩'
    case 'closing': return '结辩'
    default: return '未知阶段'
  }
}

export function speakerLabel(speaker: string): string {
  return speaker === 'user' ? '你' : 'AI'
}

// endBlockedText 把服务端的阻止原因翻成面向用户的中文说明。
export function endBlockedText(reason: string | null): string {
  switch (reason) {
    case 'no_completed_round': return '至少完成 1 个完整自由辩回合才能进入结辩。'
    case 'reply_in_progress': return '本回合完成后可进入结辩。'
    case 'round_incomplete': return '请先完成当前回合，再进入结辩。'
    case 'not_free_debate': return '当前不在自由辩阶段。'
    default: return '当前不能进入结辩。'
  }
}

/** 进度摘要：阶段 + 阶段内步数 + 自由辩回合数，供状态条渲染。 */
export function progressText(progress: TrainingProgress): string {
  const base = stageLabel(progress.stage)
  if (progress.stage === 'free_debate') {
    return `${base}（已完成 ${progress.free_debate_rounds} / ${progress.free_debate_max_rounds} 回合）`
  }
  if (progress.stage === 'closing' && progress.total_steps > 0) {
    return `${base}（第 ${progress.committed_steps} / ${progress.total_steps} 步）`
  }
  return `${base}（阶段第 ${progress.stage_step + 1} 步）`
}

// createTrainingState 绑定一个已认证用户与一个训练赛会话。
// 与 V1 状态机一样，卸载后只丢弃旧响应，绝不把旧用户/旧会话数据带入新面板。
export function createTrainingState(api: TrainingApi) {
  const detail = ref<TrainingView | null>(null)
  const busy = ref(false)
  const uncertain = ref(false)
  const draft = ref('')
  const notice = ref('')
  const id = ref<string | null>(null)
  const streamText = ref('')
  const streaming = ref(false)
  const streamSpeaker = ref<string | null>(null)
  let streamController: AbortController | null = null
  let active = true
  let timer: ReturnType<typeof setTimeout> | undefined
  let pollingSince = 0
  // attempt 记录一次「已发出但结果未确认」的请求，用于恢复时按业务键定位。
  let attempt: { stepNo: number; stage: DebateStage; key: string; content: string; stream: boolean; claimId?: string } | null = null
  let sendFailure = ''

  const progress = computed<TrainingProgress | null>(() => detail.value?.training.progress ?? null)
  const steps = computed<TrainingStep[]>(() => detail.value?.training.steps ?? [])
  const stage = computed(() => progress.value?.stage ?? null)
  const isFreeDebate = computed(() => stage.value === 'free_debate')
  const isClosed = computed(() => detail.value?.debate.status === 'completed')
  const failedAI = computed(() => {
    const pending = progress.value?.pending_ai
    return pending?.status === 'failed' ? pending : null
  })
  const generating = computed(() => progress.value?.pending_ai?.status === 'generating')
  const canSubmit = computed(() => !busy.value && !uncertain.value && !!progress.value?.can_submit_step
    && progress.value?.next_speaker === 'user' && !generating.value)
  const canGenerate = computed(() => !busy.value && !uncertain.value && !!progress.value?.can_submit_step
    && progress.value?.next_speaker === 'assistant' && !generating.value)
  const canRetryAI = computed(() => !busy.value && !uncertain.value && !!progress.value?.can_retry_ai && failedAI.value !== null)
  const canEndFreeDebate = computed(() => !busy.value && !uncertain.value && !!progress.value?.can_end_free_debate)
  const endBlocked = computed(() => endBlockedText(progress.value?.end_blocked_reason ?? null))

  function clearStream() { streamText.value = ''; streaming.value = false; streamSpeaker.value = null; streamController = null }
  function cancelStream() { streamController?.abort() }
  function stopPolling() { clearTimeout(timer); timer = undefined }
  function schedulePolling(generatingText: string, timeoutText: string, preservedNotice?: string) {
    if (Date.now() - pollingSince < GENERATION_POLL_BUDGET_MS) {
      notice.value = preservedNotice ? `${preservedNotice} ${generatingText}` : generatingText
      timer = setTimeout(() => { void query() }, GENERATION_POLL_INTERVAL_MS)
    } else {
      notice.value = preservedNotice ? `${preservedNotice} ${timeoutText}` : timeoutText
    }
  }

  function statusNotice(value: TrainingView): string {
    const p = value.training.progress
    if (p.abandoned_ai) {
      return `已放弃自由辩第 ${p.abandoned_ai.step_no} 步的失败回复，失败记录保留可见。`
    }
    if (p.pending_ai?.status === 'failed') {
      const error = p.pending_ai.error_code ? `（错误码：${p.pending_ai.error_code}）` : ''
      return `第 ${p.pending_ai.step_no} 步 AI 回复失败${error}，当前版本 v${p.pending_ai.attempt_version}。可重试本步，或在本回合未完成时按规则放弃。`
    }
    if (value.debate.status === 'completed') return '训练赛已完成。'
    if (attempt) return `${sendFailure} 尚未查到步骤。可以明确重发相同请求；不会更换原文或请求标识。`
    if (p.stage === 'free_debate' && p.free_debate_rounds >= p.free_debate_max_rounds && p.can_end_free_debate) {
      return `已完成 ${p.free_debate_max_rounds} 回合，请进入结辩。`
    }
    if (p.stage === 'closing') return `已进入结辩，请继续完成本阶段。`
    return ''
  }

  // accept 处理一次服务端权威状态的到达；返回是否需要在完成后自动请求总结。
  function accept(value: TrainingView, preservedNotice?: string) {
    detail.value = value
    uncertain.value = false
    const p = value.training.progress
    if (attempt) {
      const found = value.training.steps.some((step: TrainingStep) => step.client_request_id === attempt?.key)
        || value.training.progress.pending_ai?.claim_id === attempt?.claimId
      if (found) { draft.value = ''; attempt = null }
      else if (attempt.stage !== p.stage || attempt.stepNo !== p.next_step_no) {
        draft.value = ''; attempt = null
      } else if (attempt.stream && p.pending_ai?.status !== 'failed' && !p.can_submit_step) {
        uncertain.value = true
        schedulePolling('尚未匹配原请求标识，正在查询已保存状态…', '仍未确认原请求结果，请稍后手动查询；不会换键重发。', sendFailure)
        return
      }
    }
    if (p.pending_ai?.status === 'generating') {
      schedulePolling('正在生成 AI 回复，正在查询已保存状态…', '回复仍在生成，请稍后点击查询状态。', preservedNotice)
      return
    }
    const base = statusNotice(value)
    notice.value = preservedNotice ? (base ? `${preservedNotice} ${base}` : preservedNotice) : base
  }

  async function query(preservedNotice?: string) {
    if (!active || busy.value || !id.value) return
    stopPolling()
    busy.value = true
    try {
      const result = await api.detail(id.value)
      if (active) accept(result, preservedNotice)
    } catch (error) {
      if (active) {
        uncertain.value = true
        const prefix = preservedNotice ? `${preservedNotice} ` : ''
        notice.value = `${prefix}${feedback(error)} 请稍后查询状态，避免重复操作。`
      }
    } finally { if (active) busy.value = false }
  }

  async function open(debateId: string) {
    if (!active || busy.value) return
    cancelStream(); clearStream(); attempt = null; draft.value = ''
    id.value = debateId
    uncertain.value = true
    pollingSince = Date.now()
    await query()
  }

  async function refresh() {
    if (busy.value) return
    pollingSince = Date.now()
    await query()
  }

  // applyResult 把步骤提交/重试/结束的响应并入当前详情；step 为空表示本次没有新正式步骤。
  function applyResult(result: TrainingStepResult) {
    const current = detail.value
    if (!current) return
    const nextSteps = result.step ? mergeStep(current.training.steps, result.step) : current.training.steps
    const nextDebate: DebateStatus = result.debate_status
    detail.value = {
      ...current,
      debate: { ...current.debate, status: nextDebate, completed_rounds: result.progress.free_debate_rounds },
      training: { ...current.training, progress: result.progress, steps: nextSteps },
    }
    uncertain.value = false
  }

  function stepStatusExpected(result: TrainingStepResult, key: string): boolean {
    // 只有真正落库的步骤才能清空草稿；202/完成重放没有新步骤时保留原文供用户确认。
    if (result.step && result.step.client_request_id === key) { draft.value = ''; attempt = null; return true }
    return false
  }

  async function submit(content: string) {
    if (!active || !canSubmit.value || !id.value || !progress.value?.next_step_no) return
    const value = attempt?.content ?? content
    if (!hasNonWhitespace(value) || [...value].length > USER_CONTENT_MAX_CHARS) {
      notice.value = `请输入 1–${USER_CONTENT_MAX_CHARS} 个 Unicode 字符，不能全为空白。`
      return
    }
    if (!attempt) attempt = { stepNo: progress.value.next_step_no, stage: progress.value.stage, key: crypto.randomUUID(), content: value, stream: false }
    const request = attempt
    if (request.content !== value) request.content = value
    const debateId = id.value
    const current = () => active && id.value === debateId
    draft.value = request.content
    stopPolling()
    busy.value = true
    notice.value = '正在提交发言，请稍候…'
    sendFailure = ''
    let needsQuery = false
    try {
      const result = await api.submit(debateId, request.key, request.content, request.stage, request.stepNo)
      if (current()) {
        applyResult(result)
        if (!stepStatusExpected(result, request.key)) {
          // 202 或已完成重放：先查询权威状态，不把本地推测当结果。
          uncertain.value = true
          needsQuery = true
        }
      }
    } catch (error) {
      if (current()) {
        uncertain.value = true; sendFailure = feedback(error); notice.value = sendFailure
        needsQuery = !(error instanceof ApiError && (error.status === 401 || error.status === 404))
      }
    } finally {
      if (current()) busy.value = false
    }
    if (current() && needsQuery) { pollingSince = Date.now(); await query() }
  }

  // generate 请求服务端生成当前 AI 步骤。普通与流式都走同一认领内核，
  // 只有收到 done（最终事务提交成功）才采用正式步骤。
  async function generate(options: { stream: boolean } = { stream: true }) {
    if (!active || !canGenerate.value || !id.value || !progress.value?.next_step_no) return
    if (!attempt) attempt = { stepNo: progress.value.next_step_no, stage: progress.value.stage, key: crypto.randomUUID(), content: '', stream: options.stream }
    const request = attempt
    const debateId = id.value
    const current = () => active && id.value === debateId
    stopPolling()
    busy.value = true
    notice.value = '正在生成 AI 回复，请稍候…'
    sendFailure = ''
    let needsQuery = false
    try {
      if (options.stream) {
        const controller = new AbortController()
        streamController = controller
        streaming.value = true
        streamSpeaker.value = 'assistant'
        streamText.value = ''
        const streamed = await api.stream(debateId, request.key, request.stage, request.stepNo, event => {
          if (!current() || streamController !== controller || controller.signal.aborted || attempt?.key !== request.key) return
          if (event.type === 'start') request.claimId = event.claim_id
          else streamText.value += event.text
        }, controller.signal)
        if (current()) {
          if (streamed.kind === 'pending') {
            uncertain.value = true; needsQuery = true
          } else {
            applyResult({ step: streamed.data.step, progress: streamed.data.progress, debate_status: streamed.data.debate_status, query_url: '' })
            stepStatusExpected({ step: streamed.data.step, progress: streamed.data.progress, debate_status: streamed.data.debate_status, query_url: '' }, request.key)
          }
        }
      } else {
        const result = await api.generate(debateId, request.key, request.stage, request.stepNo)
        if (current()) {
          applyResult(result)
          if (!stepStatusExpected(result, request.key)) { uncertain.value = true; needsQuery = true }
        }
      }
    } catch (error) {
      if (current()) {
        uncertain.value = true; sendFailure = feedback(error); notice.value = sendFailure
        needsQuery = !(error instanceof ApiError && (error.status === 401 || error.status === 404))
      }
    } finally {
      if (current()) { busy.value = false; clearStream() }
    }
    if (current() && needsQuery) { pollingSince = Date.now(); await query() }
  }

  // retryAI 只在进度明确给出失败记录与版本时才允许；版本冲突时只查询，不自动换版本重发。
  async function retryAI() {
    const failed = failedAI.value
    if (!active || !canRetryAI.value || !id.value || !failed) return
    const expectedAttemptVersion = failed.attempt_version
    stopPolling()
    busy.value = true
    notice.value = `正在重试第 ${failed.step_no} 步（版本 v${expectedAttemptVersion}），请稍候…`
    let needsQuery = false
    try {
      const result = await api.retry(id.value, failed.step_no, expectedAttemptVersion)
      if (active) {
        applyResult(result)
        if (!result.step) { uncertain.value = true; needsQuery = true }
      }
    } catch (error) {
      // 收到 409 attempt_version_conflict 或其它失败后只重新查询，严禁自动替换版本号重发。
      if (active) {
        uncertain.value = true
        notice.value = feedback(error)
        needsQuery = true
      }
    } finally { if (active) busy.value = false }
    if (active && needsQuery) { pollingSince = Date.now(); await query() }
  }

  // endFreeDebate 是服务端确认的业务操作：不调用模型；重放不重复切换。
  async function endFreeDebate() {
    if (!active || !canEndFreeDebate.value || !id.value) return
    stopPolling()
    busy.value = true
    notice.value = '正在进入结辩…'
    let needsQuery = false
    try {
      const result = await api.endFreeDebate(id.value, failedAI.value?.attempt_version)
      if (active) applyResult(result)
    } catch (error) {
      if (active) {
        uncertain.value = true
        notice.value = feedback(error)
        needsQuery = true
      }
    } finally { if (active) busy.value = false }
    if (active && needsQuery) { pollingSince = Date.now(); await query() }
  }

  function dispose() {
    active = false
    cancelStream(); clearStream()
    stopPolling()
    detail.value = null
    draft.value = ''
    attempt = null
    busy.value = false
  }

  return {
    detail, busy, uncertain, draft, notice, id, streamText, streaming, streamSpeaker,
    progress, steps, stage, isFreeDebate, isClosed, failedAI, generating,
    canSubmit, canGenerate, canRetryAI, canEndFreeDebate, endBlocked,
    cancelStream, open, refresh, submit, generate, retryAI, endFreeDebate, dispose,
  }
}
