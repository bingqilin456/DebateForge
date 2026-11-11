import { computed, ref } from 'vue'
import { ApiError } from '../api/client'
import { hasNonWhitespace, type DebateApi, type DebateDetail, type Stance, type Turn } from '../api/debate'

export function feedback(error: unknown): string {
  return error instanceof ApiError ? error.message : '操作失败，请稍后手动重试。'
}

function mergeTurn(turns: Turn[], incoming: Turn): Turn[] {
  const next = turns.filter(turn => turn.round_no !== incoming.round_no)
  next.push(incoming)
  next.sort((a, b) => a.round_no - b.round_no)
  return next
}

// 每次挂载绑定一个已认证用户；卸载后只丢弃旧响应，绝不把旧用户数据带入新面板。
export function createFirstTurnState(api: DebateApi) {
  const detail = ref<DebateDetail | null>(null)
  const busy = ref(false)
  const uncertain = ref(false)
  const draft = ref('')
  const notice = ref('')
  const id = ref<string | null>(null)
  let active = true
  let timer: ReturnType<typeof setTimeout> | undefined
  let pollingSince = 0
  let attempt: { roundNo: number; content: string; key: string } | null = null
  let sendFailure = ''
  let autoSummaryForId: string | null = null

  const failedTurn = computed<Turn | null>(() => {
    const last = detail.value?.turns.at(-1)
    return last?.status === 'failed' ? last : null
  })
  const nextRound = computed<number | null>(() => {
    const current = detail.value
    if (!current || current.debate.status !== 'active') return null
    if (current.debate.completed_rounds < 0 || current.debate.completed_rounds >= 3) return null
    if (current.turns.length !== current.debate.completed_rounds) return null
    return current.debate.completed_rounds + 1
  })
  const canSend = computed(() => !busy.value && !uncertain.value && nextRound.value !== null)
  const canRetryTurn = computed(() => !busy.value && !uncertain.value
    && detail.value?.debate.status === 'active' && failedTurn.value !== null)
  const canRetrySummary = computed(() => !busy.value && !uncertain.value
    && detail.value?.debate.status === 'completed'
    && detail.value.summary_status.status === 'failed'
    && typeof detail.value.summary_status.attempt_version === 'number')

  function stopPolling() { clearTimeout(timer); timer = undefined }
  function schedulePolling(generatingText: string, timeoutText: string, preservedNotice?: string) {
    if (Date.now() - pollingSince < 120_000) {
      notice.value = preservedNotice ? `${preservedNotice} ${generatingText}` : generatingText
      timer = setTimeout(() => { void query() }, 2000)
    } else {
      notice.value = preservedNotice ? `${preservedNotice} ${timeoutText}` : timeoutText
    }
  }
  function statusNotice(value: DebateDetail): string {
    const lastTurn = value.turns.at(-1)
    if (lastTurn?.status === 'failed') {
      return `第 ${lastTurn.round_no} 轮生成失败，原文已保存（当前版本 v${lastTurn.attempt_version}）。可点击重试本轮。`
    }
    if (value.debate.status === 'completed') {
      if (value.summary_status.status === 'completed') return '三轮辩论与结构化总结均已完成。'
      if (value.summary_status.status === 'failed') {
        return `总结生成失败（当前版本 v${value.summary_status.attempt_version}），辩论内容已完整保存，可手动重试总结。`
      }
      return '三轮辩论已完成，正在准备结构化总结…'
    }
    if (attempt) {
      return `${sendFailure} 尚未查到轮次。可以明确重发相同请求；不会更换原文或请求标识。`
    }
    if (value.debate.completed_rounds === 1) return '首轮已完成，请继续发送第 2 轮发言。'
    if (value.debate.completed_rounds === 2) return '第 2 轮已完成，请继续发送第 3 轮发言。'
    return ''
  }

  function accept(value: DebateDetail, preservedNotice?: string): boolean {
    detail.value = value
    uncertain.value = false
    if (attempt && value.turns.some(turn => turn.round_no === attempt?.roundNo)) {
      draft.value = ''
      attempt = null
    }
    const lastTurn = value.turns.at(-1)
    if (lastTurn?.status === 'generating') {
      schedulePolling('正在生成回复，正在查询已保存状态…', '回复仍在生成，请稍后点击查询状态。', preservedNotice)
      return false
    }
    if (value.summary_status.status === 'generating') {
      schedulePolling('正在生成总结，正在查询已保存状态…', '总结仍在生成，请稍后点击查询状态。', preservedNotice)
      return false
    }
    const base = statusNotice(value)
    notice.value = preservedNotice ? (base ? `${preservedNotice} ${base}` : preservedNotice) : base
    if (value.debate.status === 'completed' && value.summary_status.status === 'not_started'
      && autoSummaryForId !== value.debate.id && typeof api.requestSummary === 'function') {
      autoSummaryForId = value.debate.id
      return true
    }
    return false
  }

  async function runSummary(options: { retry: boolean; expectedAttemptVersion?: number }) {
    if (!active || busy.value || !id.value || typeof api.requestSummary !== 'function') return
    stopPolling()
    busy.value = true
    notice.value = options.retry ? '正在重试生成总结，请稍候…' : '三轮辩论已完成，正在生成结构化总结…'
    let failureNotice = ''
    let needsQuery = false
    try {
      const summaryStatus = await api.requestSummary(id.value, options)
      if (active && detail.value) {
        pollingSince = Date.now()
        accept({ ...detail.value, summary_status: summaryStatus })
      }
    } catch (error) {
      if (active) {
        uncertain.value = true
        failureNotice = feedback(error)
        notice.value = failureNotice
        needsQuery = true
      }
    } finally { if (active) busy.value = false }
    if (active && needsQuery) { pollingSince = Date.now(); await query(failureNotice) }
  }

  async function query(preservedNotice?: string) {
    if (!active || busy.value || !id.value) return
    stopPolling()
    busy.value = true
    let shouldAutoSummary = false
    try {
      const result = await api.detail(id.value)
      if (active) shouldAutoSummary = accept(result, preservedNotice)
    } catch (error) {
      if (active) {
        uncertain.value = true
        const prefix = preservedNotice ? `${preservedNotice} ` : ''
        notice.value = `${prefix}${feedback(error)} 请稍后查询状态，避免重复操作。`
      }
    } finally { if (active) busy.value = false }
    if (active && shouldAutoSummary) await runSummary({ retry: false })
  }

  async function open(debateId: string) {
    if (!active || busy.value) return
    id.value = debateId
    autoSummaryForId = null
    uncertain.value = true
    pollingSince = Date.now()
    await query()
  }

  async function refresh() {
    if (busy.value) return
    pollingSince = Date.now()
    await query()
  }

  async function start(topicId: string, stance: Stance) {
    if (!active || busy.value || id.value) return
    busy.value = true
    notice.value = ''
    try {
      const debate = await api.create(topicId, stance)
      if (active) {
        id.value = debate.id
        autoSummaryForId = null
        detail.value = { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
      }
    } catch (error) {
      // 创建没有幂等键；仅反馈结果不明，不静默重发。再次点击由 UI 明确标成另建。
      if (active) notice.value = `${feedback(error)} 创建可能已成功；再次创建会另建会话。`
    } finally { if (active) busy.value = false }
  }

  async function submit(content: string) {
    if (!active || !canSend.value || !id.value || nextRound.value === null) return
    const roundNo = nextRound.value
    const effective = attempt?.roundNo === roundNo ? attempt.content : content
    if (!hasNonWhitespace(effective) || [...effective].length > 2000) {
      notice.value = '请输入 1–2000 个 Unicode 字符，不能全为空白。'
      return
    }
    if (!attempt || attempt.roundNo !== roundNo) {
      attempt = { roundNo, content: effective, key: crypto.randomUUID() }
    }
    draft.value = attempt.content
    stopPolling()
    busy.value = true
    notice.value = '正在生成回复，请稍候…'
    sendFailure = ''
    let needsQuery = false
    let shouldAutoSummary = false
    try {
      const result = roundNo === 1
        ? await api.send(id.value, attempt.content, attempt.key)
        : await api.send(id.value, attempt.content, attempt.key, roundNo)
      if (active && detail.value) {
        pollingSince = Date.now()
        const completedRounds = result.completed_rounds
        const status = completedRounds >= 3 ? 'completed' : detail.value.debate.status
        shouldAutoSummary = accept({
          ...detail.value,
          debate: { ...detail.value.debate, completed_rounds: completedRounds, status },
          turns: mergeTurn(detail.value.turns, result.turn),
        })
      }
    } catch (error) {
      if (active) { uncertain.value = true; sendFailure = feedback(error); notice.value = sendFailure; needsQuery = true }
    } finally { if (active) busy.value = false }
    if (active && needsQuery) { pollingSince = Date.now(); await query() }
    else if (active && shouldAutoSummary) await runSummary({ retry: false })
  }

  async function retryTurn() {
    const target = failedTurn.value
    if (!active || !canRetryTurn.value || !id.value || !target || typeof api.retryTurn !== 'function') return
    const expectedAttemptVersion = target.attempt_version
    stopPolling()
    busy.value = true
    notice.value = `正在重试第 ${target.round_no} 轮（版本 v${expectedAttemptVersion}），请稍候…`
    let failureNotice = ''
    let needsQuery = false
    let shouldAutoSummary = false
    try {
      const result = await api.retryTurn(id.value, target.id, expectedAttemptVersion)
      if (active && detail.value) {
        pollingSince = Date.now()
        const completedRounds = result.completed_rounds
        const status = completedRounds >= 3 ? 'completed' : detail.value.debate.status
        shouldAutoSummary = accept({
          ...detail.value,
          debate: { ...detail.value.debate, completed_rounds: completedRounds, status },
          turns: mergeTurn(detail.value.turns, result.turn),
        })
      }
    } catch (error) {
      // 收到 409 attempt_version_conflict 或其它失败后，只重新查询并展示最新状态，严禁自动替换版本号重发。
      if (active) {
        uncertain.value = true
        failureNotice = feedback(error)
        notice.value = failureNotice
        needsQuery = true
      }
    } finally { if (active) busy.value = false }
    if (active && needsQuery) { pollingSince = Date.now(); await query(failureNotice) }
    else if (active && shouldAutoSummary) await runSummary({ retry: false })
  }

  async function retrySummary() {
    const summaryStatus = detail.value?.summary_status
    if (!active || !canRetrySummary.value || !id.value || summaryStatus?.status !== 'failed') return
    await runSummary({ retry: true, expectedAttemptVersion: summaryStatus.attempt_version })
  }

  async function requestSummary() {
    if (!active || busy.value || uncertain.value || !id.value) return
    if (detail.value?.debate.status !== 'completed' || detail.value.summary_status.status !== 'not_started') return
    autoSummaryForId = detail.value.debate.id
    await runSummary({ retry: false })
  }

  function dispose() {
    active = false
    stopPolling()
    detail.value = null
    draft.value = ''
    attempt = null
    busy.value = false
  }

  return {
    detail, busy, uncertain, draft, notice, id,
    nextRound, failedTurn, canSend, canRetryTurn, canRetrySummary,
    start, open, refresh, submit, retryTurn, retrySummary, requestSummary, dispose,
  }
}
