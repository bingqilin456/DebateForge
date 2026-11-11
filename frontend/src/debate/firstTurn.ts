import { computed, ref } from 'vue'
import { ApiError } from '../api/client'
import { hasNonWhitespace, type DebateApi, type DebateDetail, type Stance } from '../api/debate'

export function feedback(error: unknown): string {
  return error instanceof ApiError ? error.message : '操作失败，请稍后手动重试。'
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
  let attempt: { content: string; key: string } | null = null
  let sendFailure = ''
  const canSend = computed(() => !busy.value && !uncertain.value && detail.value?.debate.status === 'active'
    && detail.value.debate.completed_rounds === 0 && detail.value.turns.length === 0)

  function stopPolling() { clearTimeout(timer); timer = undefined }
  function accept(value: DebateDetail) {
    detail.value = value
    uncertain.value = false
    const turn = value.turns[0]
    if (turn) { draft.value = ''; attempt = null }
    if (turn?.status === 'generating') {
      notice.value = '正在生成回复，正在查询已保存状态…'
      if (Date.now() - pollingSince < 120_000) timer = setTimeout(() => { void query() }, 2000)
      else notice.value = '回复仍在生成，请稍后点击查询状态。'
    } else if (turn?.status === 'failed') notice.value = '首轮生成失败，原文已保存。当前阶段暂不提供失败轮次重试。'
    else if (turn?.status === 'completed') notice.value = '首轮已完成。后续轮次将在下一阶段开放。'
    else notice.value = attempt ? `${sendFailure} 尚未查到轮次。可以明确重发相同请求；不会更换原文或请求标识。` : ''
  }
  async function query() {
    if (!active || busy.value || !id.value) return
    stopPolling()
    busy.value = true
    try {
      const result = await api.detail(id.value)
      if (active) accept(result)
    } catch (error) {
      if (active) { uncertain.value = true; notice.value = `${feedback(error)} 请稍后查询状态，避免重复发言。` }
    } finally { if (active) busy.value = false }
  }
  async function open(debateId: string) {
    if (!active || busy.value) return
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
  async function start(topicId: string, stance: Stance) {
    if (!active || busy.value || id.value) return
    busy.value = true
    notice.value = ''
    try {
      const debate = await api.create(topicId, stance)
      if (active) { id.value = debate.id; detail.value = { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } } }
    } catch (error) {
      // 创建没有幂等键；仅反馈结果不明，不静默重发。再次点击由 UI 明确标成另建。
      if (active) notice.value = `${feedback(error)} 创建可能已成功；再次创建会另建会话。`
    } finally { if (active) busy.value = false }
  }
  async function submit(content: string) {
    if (!active || !canSend.value || !id.value) return
    if (!hasNonWhitespace(content) || [...content].length > 2000) { notice.value = '请输入 1–2000 个 Unicode 字符，不能全为空白。'; return }
    if (!attempt) attempt = { content, key: crypto.randomUUID() }
    draft.value = attempt.content
    busy.value = true
    notice.value = '正在生成回复，请稍候…'
    sendFailure = ''
    let needsQuery = false
    try {
      const result = await api.send(id.value, attempt.content, attempt.key)
      if (active && detail.value) {
        pollingSince = Date.now()
        accept({ ...detail.value, debate: { ...detail.value.debate, completed_rounds: result.completed_rounds }, turns: [result.turn] })
      }
    } catch (error) {
      if (active) { uncertain.value = true; sendFailure = feedback(error); notice.value = sendFailure; needsQuery = true }
    } finally { if (active) busy.value = false }
    if (active && needsQuery) { pollingSince = Date.now(); await query() }
  }
  function dispose() {
    active = false
    stopPolling()
    detail.value = null
    draft.value = ''
    attempt = null
    busy.value = false
  }
  return { detail, busy, uncertain, draft, notice, id, canSend, start, open, refresh, submit, dispose }
}
