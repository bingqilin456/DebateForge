import { computed, ref } from 'vue'
import { ApiError } from '../api/client'
import type { ReviewApi } from '../api/review'
import { isReviewStatus } from '../api/review'
import type { ReviewStatus } from '../types/api'
import { GENERATION_POLL_BUDGET_MS, GENERATION_POLL_INTERVAL_MS } from '../shared/limits'

/**
 * V2-13 复盘状态机（前端投影）。
 *
 * 关键约束：
 * - 只读 GET 与“用户显式触发”的 POST；没有自动生成、自动轮询（仅在生成中做有限查询）或自动重试。
 * - 比赛完成状态由会话详情给出，复盘状态单独维护；复盘失败不回写会话状态。
 * - 失败重试必须带当前版本；409 版本冲突时只重新读取，绝不自动换版本重发。
 * - 刷新恢复：每次挂载都先 GET，服务端才是唯一事实源。
 */

export function reviewFeedback(error: unknown): string {
  return error instanceof ApiError ? error.message : '操作失败，请稍后手动重试。'
}

export type ReviewState = ReturnType<typeof createReviewState>

export function createReviewState(api: ReviewApi) {
  const status = ref<ReviewStatus>({ status: 'not_started', attempt_version: null })
  const loading = ref(false)
  const busy = ref(false)
  const notice = ref('')
  const failedMessage = ref('')
  const loaded = ref(false)
  let active = true
  let currentId: string | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let pollingSince = 0

  const generating = computed(() => status.value.status === 'generating')
  const completed = computed(() => status.value.status === 'completed')
  const failed = computed(() => status.value.status === 'failed')
  // 服务端显式给出 available=false 表示本环境未开启复盘切片：
  // 此时必须隐藏复盘界面并禁止生成，不能把 not_started 当成可以点击的未开始。
  const unavailable = computed(() => status.value.status === 'not_started' && status.value.available === false)
  const canGenerate = computed(() => loaded.value && !unavailable.value && !busy.value && !loading.value
    && status.value.status === 'not_started')
  const canRetry = computed(() => loaded.value && !unavailable.value && !busy.value && !loading.value
    && status.value.status === 'failed' && typeof status.value.attempt_version === 'number')

  function stopPolling() { clearTimeout(timer); timer = undefined }

  /** 生成中只做有限次 GET 查询；超出预算后交给用户手动刷新。 */
  function schedulePolling() {
    if (!active || !currentId) return
    if (Date.now() - pollingSince >= GENERATION_POLL_BUDGET_MS) {
      notice.value = '复盘仍在生成，请稍后点击“刷新复盘状态”。'
      return
    }
    notice.value = '正在生成复盘，正在查询已保存状态…'
    timer = setTimeout(() => { void refresh() }, GENERATION_POLL_INTERVAL_MS)
  }

  function accept(value: ReviewStatus, preserved?: string) {
    status.value = value
    loaded.value = true
    failedMessage.value = value.status === 'failed' ? '复盘生成失败，比赛完成状态不受影响。' : ''
    if (value.status === 'generating') {
      schedulePolling()
      return
    }
    stopPolling()
    notice.value = preserved ?? ''
  }

  /** 读取当前复盘状态。刷新恢复的唯一入口。 */
  async function refresh(preserved?: string) {
    if (!active || !currentId || loading.value) return
    stopPolling()
    loading.value = true
    try {
      const result = await api.get(currentId)
      if (active) accept(result, preserved)
    } catch (error) {
      if (active) {
        loaded.value = false
        notice.value = `${preserved ? `${preserved} ` : ''}${reviewFeedback(error)} 请稍后刷新复盘状态。`
      }
    } finally {
      if (active) loading.value = false
    }
  }

  /** 绑定会话并读取状态；切换会话会清空上一场的状态与定时器。 */
  async function open(debateId: string) {
    if (!active) return
    if (currentId !== debateId) {
      stopPolling()
      currentId = debateId
      status.value = { status: 'not_started', attempt_version: null }
      loaded.value = false
      notice.value = ''
      failedMessage.value = ''
    }
    pollingSince = Date.now()
    await refresh()
  }

  async function start(body: Parameters<ReviewApi['request']>[1] = {}) {
    if (!active || !currentId || busy.value || unavailable.value) return
    stopPolling()
    busy.value = true
    notice.value = '正在生成复盘，请稍候…'
    try {
      const result = await api.request(currentId, body)
      if (active) {
        pollingSince = Date.now()
        accept(result)
      }
    } catch (error) {
      if (!active) return
      // 失败或版本冲突都只重新读取权威状态；绝不自动重发同一次生成。
      notice.value = reviewFeedback(error)
      await refresh(notice.value)
    } finally {
      if (active) busy.value = false
    }
  }

  /** 用户显式点击“生成复盘”。 */
  async function generate() {
    if (!canGenerate.value) return
    await start({ retry: false })
  }

  /** 用户显式点击“重试生成复盘”，必须带服务器当前版本。 */
  async function retry() {
    if (!canRetry.value || status.value.status !== 'failed') return
    await start({ retry: true, expected_attempt_version: status.value.attempt_version })
  }

  function dispose() {
    active = false
    stopPolling()
    currentId = null
    status.value = { status: 'not_started', attempt_version: null }
    loaded.value = false
    notice.value = ''
    failedMessage.value = ''
    busy.value = false
    loading.value = false
  }

  return {
    status, loading, busy, notice, failedMessage, loaded,
    generating, completed, failed, unavailable, canGenerate, canRetry,
    open, refresh, generate, retry, dispose,
  }
}

// isReviewStatus 从 API 模块再导出，便于组件与测试复用同一份校验口径。
export { isReviewStatus }
