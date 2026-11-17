<script setup lang="ts">
import { GENERATION_POLL_INTERVAL_MS, GENERATION_POLL_BUDGET_MS } from '../../shared/limits'
import { computed, onUnmounted, ref, watch } from 'vue'
import { Button } from '@arco-design/web-vue'
import type { AuthState } from '../../auth/state'
import type { DebateDetail, StrategyStatus } from '../../types/api'
import { createDebateApi } from '../../api/debate'
import { ApiError } from '../../api/client'
import { AI_STYLE_LABELS } from '../../debate/styles'

const props = withDefaults(defineProps<{ auth: AuthState; detail: DebateDetail; disabled?: boolean }>(), { disabled: false })
const api = computed(() => createDebateApi(props.auth.client))
const strategy = ref<StrategyStatus | null>(null)
const querying = ref(false)
const posting = ref(false)
const error = ref('')
const notice = ref('')
const retryVersion = ref<number | null>(null)
const latestHasTurn = ref(false)
const pollExpired = ref(false)
const authenticated = computed(() => props.auth.state.status === 'authenticated' && !!props.auth.state.user)
const eligible = computed(() => !latestHasTurn.value && props.detail.turns.length === 0 && props.detail.debate.status === 'active')
const locked = computed(() => props.disabled || props.auth.state.pending || querying.value || posting.value || !authenticated.value)
let epoch = 0
let disposed = false
let timer: ReturnType<typeof setTimeout> | undefined
let pollSince: number | null = null
function current(token: number) { return !disposed && token === epoch && authenticated.value }
function stopPolling() { clearTimeout(timer); timer = undefined }
function message(value: unknown) { return value instanceof ApiError ? value.message : '策略请求失败，请稍后手动查询。' }

function accept(value: StrategyStatus, token: number) {
  if (!current(token)) return
  if (value.status === 'completed' && value.ai_style !== props.detail.debate.ai_style) {
    throw new ApiError('format', '策略流派与该会话保存值不一致，请重新查询。')
  }
  strategy.value = value
  stopPolling()
  if (value.status !== 'generating') { pollSince = null; return }
  pollSince ??= Date.now()
  if (Date.now() - pollSince < GENERATION_POLL_BUDGET_MS) {
    // 202 只触发只读轮询；到达预算后等待用户显式查询，绝不补发 POST。
    timer = setTimeout(() => { void query(false, false) }, GENERATION_POLL_INTERVAL_MS)
  } else { pollExpired.value = true }
}
async function readState(token: number) {
  const value = await api.value.getStrategy(props.detail.debate.id)
  if (!current(token)) return null
  accept(value, token)
  return value
}
async function checkNoTurns(token: number) {
  // 页面可能已落后于另一标签页；发送前重读详情。最终并发条件仍由后端会话锁保证。
  const latest = await api.value.detail(props.detail.debate.id)
  if (!current(token)) return false
  latestHasTurn.value = latest.turns.length > 0 || latest.debate.status !== 'active'
  if (!eligible.value) {
    retryVersion.value = null
    notice.value = '已存在发言或会话已结束，不能生成或重试赛前策略。请查询辩论状态。'
    return false
  }
  return true
}

async function query(prepareRetry = false, manual = true) {
  if (!authenticated.value || disposed) return
  if (querying.value || posting.value) {
    // 恢复流程可能仍在重读详情；只读定时器顺延，不能悄悄消失，也不补发 POST。
    if (!manual && strategy.value?.status === 'generating') accept(strategy.value, epoch)
    return
  }
  stopPolling()
  if (manual) { pollSince = null; pollExpired.value = false }
  querying.value = true
  retryVersion.value = null
  error.value = ''; notice.value = ''
  const token = epoch
  try {
    const value = await readState(token)
    if (prepareRetry && value?.status === 'failed' && await checkNoTurns(token) && current(token)) {
      // 查询不是重试；显示刚查询到的版本，必须再点击一次才发送 retry:true。
      retryVersion.value = value.attempt_version
      notice.value = `已查询最新失败版本 v${value.attempt_version}，请确认后再重试。`
    }
  } catch (failure) {
    if (current(token)) { strategy.value = null; error.value = message(failure) }
  } finally { if (current(token)) querying.value = false }
}

async function generate(retry = false) {
  if (locked.value || !eligible.value || !strategy.value) return
  if (retry ? strategy.value.status !== 'failed' || retryVersion.value === null : strategy.value.status !== 'not_started') return
  const expected = retryVersion.value
  retryVersion.value = null
  posting.value = true
  error.value = ''; notice.value = ''
  pollSince = null; pollExpired.value = false
  stopPolling()
  const token = epoch
  try {
    if (!await checkNoTurns(token) || !current(token)) return
    const value = await api.value.generateStrategy(props.detail.debate.id,
      retry ? { retry: true, expected_attempt_version: expected! } : {})
    if (current(token)) accept(value, token)
  } catch (failure) {
    if (!current(token)) return
    error.value = message(failure)
    // 响应丢失、受控失败和 409 都只能 GET 恢复，不能换版本自动重发。
    try {
      const recovered = await readState(token)
      if (current(token) && failure instanceof ApiError && failure.status === 409) {
        await checkNoTurns(token)
        if (current(token)) notice.value = '会话或策略版本已变化，已重新查询；不会自动重试。'
      }
      if (current(token) && recovered?.status === 'completed') {
        error.value = ''
        notice.value = '已从服务端恢复保存的策略，没有重新生成。'
      }
    } catch (queryFailure) {
      if (current(token)) { strategy.value = null; error.value = `${message(failure)} 状态查询也未完成：${message(queryFailure)}` }
    }
  } finally { if (current(token)) posting.value = false }
}

watch([() => props.auth, () => props.auth.state.status, () => props.auth.state.user?.id, () => props.detail.debate.id, () => props.detail.debate.ai_style], () => {
  epoch++; stopPolling(); pollSince = null
  strategy.value = null; querying.value = false; posting.value = false; retryVersion.value = null
  error.value = ''; notice.value = ''; latestHasTurn.value = false; pollExpired.value = false
  void query()
}, { immediate: true, flush: 'sync' })
watch(() => props.detail.turns.length, count => { if (count > 0) retryVersion.value = null }, { flush: 'sync' })
onUnmounted(() => { disposed = true; epoch++; stopPolling() })
</script>

<template>
  <section class="strategy-card" aria-label="赛前策略卡" :data-strategy-id="detail.debate.id" :aria-busy="querying || posting">
    <div class="strategy-heading">
      <h3>赛前策略（可跳过）</h3>
      <Button data-query-strategy :disabled="locked" @click="query()">查询策略状态</Button>
    </div>
    <p class="muted">为你（{{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }}）破题，应对{{ AI_STYLE_LABELS[detail.debate.ai_style] }}对手。可直接在下方发送首轮，无需先生成。</p>
    <p v-if="querying" role="status">正在查询已保存策略…</p>
    <p v-if="posting" role="status">正在生成赛前策略，请稍候…</p>
    <p v-if="error" class="strategy-error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="!eligible" class="muted">已存在发言或会话已结束，不再提供首次生成和失败重试；已有策略仍可查询。</p>
    <p v-if="strategy?.status === 'not_started'" role="status">尚未生成赛前策略。</p>
    <p v-else-if="strategy?.status === 'generating'" role="status">策略生成中（v{{ strategy.attempt_version }}），仅查询已有状态，不重复生成。</p>
    <p v-else-if="strategy?.status === 'failed'" role="status">策略生成失败（{{ strategy.error_code }}），当前版本 v{{ strategy.attempt_version }}。不会自动重试。</p>
    <p v-if="pollExpired" role="status">查询已达等待预算，请稍后手动查询策略状态。</p>
    <div v-if="strategy?.status === 'completed'" data-strategy-completed>
      <p class="muted">策略 v{{ strategy.attempt_version }} · schema {{ strategy.schema_version }} · Prompt {{ strategy.prompt_version }} · 保存流派：{{ AI_STYLE_LABELS[strategy.ai_style] }}（{{ strategy.ai_style }}）</p>
      <h4>核心主张</h4><p class="strategy-text">{{ strategy.content.thesis }}</p>
      <h4>论据</h4><ul><li v-for="(argument, index) in strategy.content.arguments" :key="index" class="strategy-text">{{ argument }}</li></ul>
      <h4>应对风险</h4><ul><li v-for="(risk, index) in strategy.content.risks" :key="index" class="strategy-text">{{ risk }}</li></ul>
    </div>
    <Button v-if="eligible && strategy?.status === 'not_started'" data-generate-strategy type="primary" :disabled="locked" @click="generate()">生成赛前策略</Button>
    <template v-if="eligible && strategy?.status === 'failed'">
      <Button v-if="retryVersion === null" data-prepare-strategy-retry :disabled="locked" @click="query(true)">查询最新重试版本</Button>
      <Button v-else data-retry-strategy type="primary" :disabled="locked" @click="generate(true)">确认重试策略（v{{ retryVersion }}）</Button>
    </template>
  </section>
</template>

<style scoped>
.strategy-card { margin: 20px 0; padding: 18px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-info-bg); }
.strategy-heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
h3 { margin: 0; } h4 { margin: 18px 0 8px; }
.muted { color: var(--app-text-muted); font-size: 13px; line-height: 1.8; }
.strategy-error { padding: 12px; color: var(--app-error-text); background: var(--app-error-bg); border-radius: var(--app-radius-control); }
.strategy-text, [role="alert"], [role="status"] { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; }
ul { padding-left: 22px; }
</style>
