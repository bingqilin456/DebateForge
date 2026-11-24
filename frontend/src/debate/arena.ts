import { computed, onUnmounted, ref, watch, type Ref } from 'vue'
import { ApiError, GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from '../api/client'

type Source = 'local' | 'ai'
type Round = { round_no: number; pro_percent: number; source: Source; graph_source?: Source; user_message_id: string; assistant_message_id: string; user_point: string; assistant_point: string }
type Hint = { round_no: number; text: string; source: Source }
type Workspace = { rounds: Round[]; hints: Hint[]; practice?: boolean }
type Action = 'hints' | 'teammate' | 'framework'
const source = (value: unknown): value is Source => value === 'local' || value === 'ai'
const roundNo = (value: unknown) => typeof value === 'number' && Number.isInteger(value) && value > 0
function isHint(value: unknown): value is Hint {
  return isRecord(value) && roundNo(value.round_no) && source(value.source) && typeof value.text === 'string'
}
function isRound(value: unknown): value is Round {
  return isRecord(value) && roundNo(value.round_no) && source(value.source)
    && (value.graph_source === undefined || source(value.graph_source))
    && typeof value.pro_percent === 'number' && value.pro_percent >= 0 && value.pro_percent <= 100
    && typeof value.user_message_id === 'string' && typeof value.assistant_message_id === 'string'
    && typeof value.user_point === 'string' && typeof value.assistant_point === 'string'
}
export function isArenaWorkspace(value: unknown): value is Workspace {
  return isRecord(value) && Array.isArray(value.rounds) && value.rounds.every(isRound)
    && Array.isArray(value.hints) && value.hints.every(isHint)
    && (value.practice === undefined || typeof value.practice === 'boolean')
}

export const localFramework = '主张：[填写本轮观点]\n理由：[解释为什么]\n回应：[对方的观点是什么，你如何回应]'
export const sampleArguments = {
  strong: '我方主张先约定一套可检验的判断标准。请用同一标准检验两种方案，再比较它们达到目标的成本。',
  flawed: '我的一个朋友试过这个办法，结果很好，所以这个办法一定适合每个人。',
}

export function useArena(client: ApiClient, id: Ref<string | null>, completed: Ref<number>, nextRound: Ref<number | null>) {
  const arena = ref<Workspace>({ rounds: [], hints: [] })
  const updatedHints = ref<Hint[]>([])
  const pending = ref<Action | null>(null)
  const arenaLoading = ref(false)
  const error = ref('')
  let generation = 0
  let active = true
  const url = () => `/api/v1/debates/${id.value}/arena`
  const current = computed(() => arena.value.rounds.at(-1))
  const hint = computed(() => updatedHints.value.find(item => item.round_no === nextRound.value)
    ?? arena.value.hints.find(item => item.round_no === nextRound.value))

  async function refresh() {
    if (!id.value) return
    const version = ++generation
    arenaLoading.value = true
    try {
      const data = await client.request(url(), { expectedStatus: 200, validate: isArenaWorkspace })
      if (active && version === generation) arena.value = data
    } catch {
      // 历史服务未提供辅助接口时，仍可使用辩论和本地快捷辩词。
    } finally {
      if (active && version === generation) arenaLoading.value = false
    }
  }

  async function assist(action: Action) {
    if (!id.value || !nextRound.value || pending.value) return
    const round = nextRound.value
    pending.value = action
    error.value = ''
    try {
      const result = await client.request(url(), {
        method: 'POST', expectedStatus: 200, body: { action }, validate: isHint, timeoutMs: GENERATION_TIMEOUT_MS,
      })
      if (!active || nextRound.value !== round || result.round_no !== round) return
      if (action === 'hints') updatedHints.value = [...updatedHints.value.filter(item => item.round_no !== round), result]
      return result
    } catch (err) {
      if (active && nextRound.value === round) error.value = err instanceof ApiError ? err.message : '辅助内容生成失败，可以重试。'
    } finally {
      if (active) pending.value = null
    }
  }

  watch([id, completed], () => void refresh())
  watch(nextRound, () => { error.value = '' })
  onUnmounted(() => { active = false })
  return { arena, current, hint, pending, arenaLoading, error, refresh, assist }
}
