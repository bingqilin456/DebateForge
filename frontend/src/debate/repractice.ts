import { computed, ref } from 'vue'
import { ApiError } from '../api/client'
import type { RepracticeApi } from '../api/repractice'
import type { RepracticeList, RepracticeSource, RepracticeVersion } from '../types/api'
import { REPRACTICE_GOAL_MAX_CHARS, REPRACTICE_REWRITE_MAX_CHARS } from '../shared/limits'

/**
 * V2-08 局部重练状态机（前端投影）。
 *
 * 关键约束：
 * - 只读 GET 与“用户显式触发”的提交/重试；没有自动生成、自动重试或未知版本重发。
 * - 每次改写都是独立版本；版本历史由服务端保存，刷新后按服务端恢复。
 * - “恢复到编辑区”只把历史版本内容填回输入框，不创建新版本、不覆盖旧版本；
 *   用户再提交时才产生新版本，并带上 restored_from 说明来源。
 * - 提交失败（含保存失败）必须保留用户输入；生成失败保留失败版本，可显式重试。
 * - 能力关闭时 available=false：整个重练区隐藏，不显示可点击入口。
 */

export function repracticeFeedback(error: unknown): string {
  return error instanceof ApiError ? error.message : '操作失败，请稍后手动重试。'
}

/** 选段来源：正文与码点坐标一起携带，避免组件再自行按 UTF-16 换算。 */
export interface RepracticeSelection extends RepracticeSource {
  /** 选区实际文本由调用方按码点切片后传入，服务端会逐字复核。 */
  goal?: string
}

export type RepracticeState = ReturnType<typeof createRepracticeState>

/** 生成一次性请求编号：优先用浏览器 uuid，退化时用时间戳 + 随机串。 */
export function newRequestId(): string {
  const cryptoApi = globalThis.crypto
  if (cryptoApi && typeof cryptoApi.randomUUID === 'function') return cryptoApi.randomUUID()
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

export function createRepracticeState(api: RepracticeApi) {
  const list = ref<RepracticeVersion[]>([])
  const available = ref(true)
  const loading = ref(false)
  const busy = ref(false)
  const notice = ref('')
  const selection = ref<RepracticeSelection | null>(null)
  const goal = ref('')
  const rewrite = ref('')
  const restoredFrom = ref<string | null>(null)
  const failure = ref<string>('')
  let active = true
  let currentId: string | null = null
  // 同一版本重试前先记住当前 attempt，避免并发点击用过期版本重发。
  let requestSeq = 0
  let pendingSubmission: { key: string; requestId: string } | null = null

  const selectionQuote = computed(() => selection.value?.quote ?? '')
  const rewriteCount = computed(() => [...rewrite.value].length)
  const goalCount = computed(() => [...goal.value].length)
  // 空白改写不是有效输入：与发言正文同一口径（Unicode White_Space 与 U+FEFF）。
  const rewriteHasContent = computed(() => /[^\p{White_Space}\uFEFF]/u.test(rewrite.value))
  const canSubmit = computed(() => Boolean(selection.value) && rewriteHasContent.value
    && rewriteCount.value <= REPRACTICE_REWRITE_MAX_CHARS && goalCount.value <= REPRACTICE_GOAL_MAX_CHARS
    && !busy.value && !loading.value)
  const hasSelection = computed(() => selection.value !== null)

  function accept(result: RepracticeList, preserved?: string) {
    available.value = result.available
    // 历史按服务端顺序（写入顺序）展示；最新版本在末尾，界面可自行反向展示。
    list.value = result.items
    notice.value = preserved ?? ''
  }

  /** 读取历史版本；刷新恢复的唯一入口，不触发任何生成。 */
  async function refresh(preserved?: string) {
    if (!active || !currentId || loading.value) return
    loading.value = true
    try {
      const result = await api.list(currentId)
      if (active) accept(result, preserved)
    } catch (error) {
      if (active) notice.value = `${preserved ? `${preserved} ` : ''}${repracticeFeedback(error)} 请稍后刷新重练历史。`
    } finally {
      if (active) loading.value = false
    }
  }

  /** 绑定会话并读取历史；切换会话清空选段与草稿，避免串场。 */
  async function open(debateId: string) {
    if (!active) return
    if (currentId !== debateId) {
      currentId = debateId
      pendingSubmission = null
      list.value = []
      available.value = true
      selection.value = null
      goal.value = ''
      rewrite.value = ''
      restoredFrom.value = null
      failure.value = ''
      notice.value = ''
    }
    await refresh()
  }

  /**
   * 选中一段本人原文进入练习。
   * 只设置选段并清空草稿，不发起任何请求；goal 由调用方决定是否带入改进提示。
   */
  function selectSource(source: RepracticeSource, nextGoal?: string) {
    selection.value = { ...source }
    rewrite.value = ''
    restoredFrom.value = null
    failure.value = ''
    goal.value = nextGoal ?? ''
  }

  function clearSelection() {
    selection.value = null
    rewrite.value = ''
    goal.value = ''
    restoredFrom.value = null
    failure.value = ''
  }

  /** 编辑器回写：目标与改写由状态机统一持有，组件不直接改 ref。 */
  function setGoal(value: string) {
    goal.value = value
  }

  function setRewrite(value: string) {
    rewrite.value = value
  }

  /**
   * 只带入改进提示、不伪造原句：用于引用没有可核对坐标时的降级路径。
   */
  function applyGoalOnly(improvement: string) {
    goal.value = improvement
  }

  /**
   * 把历史版本恢复到编辑区继续改写。
   *
   * 只填回选段/目标/改写，并记录 restored_from；不创建版本、不覆盖历史。
   * 该操作完全本地，不产生网络请求。
   */
  function restore(version: RepracticeVersion) {
    selection.value = { ...version.source }
    goal.value = version.goal
    rewrite.value = version.rewrite
    restoredFrom.value = version.id
    failure.value = ''
    notice.value = `已把历史版本 v${version.id} 恢复到编辑区；再次提交会创建新版本，原版本保持不动。`
  }

  /** 显式提交改写；保存/生成失败都保留输入。 */
  async function submit() {
    if (!active || !currentId || !canSubmit.value || !selection.value) return
    const source = selection.value
    busy.value = true
    failure.value = ''
    notice.value = ''
    const seq = ++requestSeq
    try {
      const body = {
        message_id: source.message_id,
        start_cp: source.start_cp,
        end_cp: source.end_cp,
        quote: source.quote,
        goal: goal.value,
        rewrite: rewrite.value,
        ...(restoredFrom.value !== null ? { restored_from: restoredFrom.value } : {}),
      }
      const key = JSON.stringify(body)
      if (pendingSubmission?.key !== key) pendingSubmission = { key, requestId: newRequestId() }
      const created = await api.submit(currentId, { ...body, client_request_id: pendingSubmission.requestId })
      if (!active || seq !== requestSeq) return
      pendingSubmission = null
      // 成功或生成失败都返回已保存版本：立即并入历史，失败版本也保留改写。
      list.value = list.value.some(item => item.id === created.id)
        ? list.value.map(item => item.id === created.id ? created : item)
        : [...list.value, created]
      if (created.status === 'failed') {
        failure.value = '反馈未生成，改写已保留；可以按当前版本显式重试。'
        notice.value = ''
      } else {
        notice.value = '已保存新版本。'
        // 成功提交后清空草稿，但保留选段，便于继续写下一版。
        rewrite.value = ''
        restoredFrom.value = null
        goal.value = ''
      }
    } catch (error) {
      if (!active || seq !== requestSeq) return
      // 保存失败或请求失败：保留选段与改写，用户可原地重试。
      failure.value = repracticeFeedback(error)
      // 重新读取权威历史，避免把未保存的内容误当成版本。
      await refresh()
    } finally {
      if (active && seq === requestSeq) busy.value = false
    }
  }

  /** 显式重试失败版本；必须带服务器当前 attempt_version。 */
  async function retry(version: RepracticeVersion) {
    if (!active || !currentId || busy.value || version.status !== 'failed') return
    busy.value = true
    failure.value = ''
    const seq = ++requestSeq
    try {
      const updated = await api.retry(currentId, version.id, { expected_attempt_version: version.attempt_version })
      if (!active || seq !== requestSeq) return
      list.value = list.value.map(item => (item.id === updated.id ? updated : item))
      if (updated.status === 'failed') {
        failure.value = '反馈仍未生成，改写已保留；可以稍后再手动重试。'
      } else {
        notice.value = `版本 v${updated.id} 的反馈已生成。`
      }
    } catch (error) {
      if (!active || seq !== requestSeq) return
      // 版本冲突或失败只提示并重读，绝不自动换版本重发。
      failure.value = repracticeFeedback(error)
      await refresh()
    } finally {
      if (active && seq === requestSeq) busy.value = false
    }
  }

  function dispose() {
    active = false
    currentId = null
    list.value = []
    available.value = true
    selection.value = null
    goal.value = ''
    rewrite.value = ''
    restoredFrom.value = null
    failure.value = ''
    notice.value = ''
    busy.value = false
    loading.value = false
  }

  return {
    list, available, loading, busy, notice, selection, goal, rewrite, restoredFrom, failure,
    selectionQuote, rewriteCount, goalCount, canSubmit, hasSelection,
    open, refresh, selectSource, clearSelection, setGoal, setRewrite, applyGoalOnly,
    restore, submit, retry, dispose,
  }
}
