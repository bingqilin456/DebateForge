<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { Alert, Button } from '@arco-design/web-vue'
import { ApiError } from '../../api/client'
import type { AnnotationApi } from '../../api/annotations'
import type { AnnotationBatch, AnnotationItem, DebateDetail } from '../../types/api'
import { ANNOTATION_STYLES, annotationEntryKey, categoryLabel } from '../../debate/annotations'
import { GENERATION_POLL_BUDGET_MS, GENERATION_POLL_INTERVAL_MS } from '../../shared/limits'

/**
 * 批注侧栏（桌面右栏 / 移动底部面板）。
 *
 * 职责边界：
 * - 原文与高亮由工作台的正式消息流渲染（见 AnnotatedText），侧栏只放解释与状态，
 *   绝不复制第二份原文。
 * - 工作台控制显示；隐藏面板时仍读取批注并追踪新批次。
 * - 有新一轮完成或仍有批次在生成时做有限 GET 轮询；切换会话或卸载立即清理定时器与版本，
 *   绝不自动 POST、不自动补做。
 * - 标记 ↔ 解释双向定位用元素 ref 精确比较，不把数据拼进 CSS 选择器。
 */

const props = defineProps<{
  detail: DebateDetail
  api: AnnotationApi
  disabled?: boolean
  trainingStep?: number
}>()

const emit = defineEmits<{
  locate: [key: string]
}>()

const batches = ref<AnnotationBatch[]>([])
const loading = ref(false)
const loadError = ref('')
const notice = ref('')
const retrying = ref<string | null>(null)
const expanded = ref<Set<string>>(new Set())
// 当前获得焦点的解释键：用状态类画方框，程序化（鼠标）焦点也可见，不依赖 :focus-visible。
const focusedKey = ref<string | null>(null)
// 面板里每条解释的 DOM 引用：双向定位靠它比较，不用选择器拼接。
const entryElements = new Map<string, HTMLElement>()
let active = true
let versionToken = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined
let pollSince = 0
const triggerPosition = computed(() => props.trainingStep ?? props.detail.debate.completed_rounds)
const positionUnit = computed(() => props.trainingStep === undefined ? '轮' : '步')
let observedRounds = triggerPosition.value
let awaitingFreshBatch = false

const completedBatches = computed(() => batches.value.filter(batch => batch.status === 'completed'))
const generatingBatches = computed(() => batches.value.filter(batch => batch.status === 'generating'))
const retryableBatches = computed(() => batches.value.filter(batch => batch.status === 'not_started' || batch.status === 'failed'))
const hasAnyBatch = computed(() => batches.value.length > 0)

/** 已完成的条目按引用消息聚合，供正式消息流取用。 */
const itemsByMessage = computed(() => {
  const map = new Map<string, AnnotationItem[]>()
  for (const batch of completedBatches.value) {
    for (const item of batch.items) {
      const list = map.get(item.anchor.message_id) ?? []
      list.push(item)
      map.set(item.anchor.message_id, list)
    }
  }
  return map
})

function styleOf(item: AnnotationItem) {
  return ANNOTATION_STYLES[item.category] ?? { label: categoryLabel(item.category), className: 'unknown' }
}

function keysOf(item: AnnotationItem) { return annotationEntryKey(item) }

function isExpanded(key: string) { return expanded.value.has(key) }

function toggle(item: AnnotationItem) {
  const key = keysOf(item)
  const next = new Set(expanded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expanded.value = next
}

/** 记录解释节点，供标记点击时精确查找。 */
function registerEntry(key: string, element: Element | null) {
  if (!element) { entryElements.delete(key); return }
  entryElements.set(key, element as HTMLElement)
}

/** 焦点进入/离开解释时同步方框状态；重叠条目里只有真正聚焦的那条高亮。 */
function onDetailFocus(key: string) { focusedKey.value = key }
function onDetailBlur(key: string) { if (focusedKey.value === key) focusedKey.value = null }

/**
 * 标记 → 解释：展开全部同段条目并滚动到第一个，随后把焦点交给它。
 * 只按条目键在 Map 里查找节点，绝不拼接 CSS 选择器。
 */
async function focusFromMarker(keys: string[]) {
  if (!keys.length) return
  const next = new Set(expanded.value)
  for (const key of keys) next.add(key)
  expanded.value = next
  await nextTick()
  for (const key of keys) {
    const entry = entryElements.get(key)
    if (!entry) continue
    entry.scrollIntoView?.({ block: 'nearest' })
    entry.focus?.()
    return
  }
}

function messageItems(messageId: string): AnnotationItem[] {
  return itemsByMessage.value.get(messageId) ?? []
}

function stopPolling() {
  clearTimeout(pollTimer)
  pollTimer = undefined
}

/**
 * 新轮次已提交但后台尚未认领时也短期查询，到达预算即停止。
 * 只发 GET，不触发任何写操作。
 */
function schedulePolling() {
  stopPolling()
  if (!active || (!awaitingFreshBatch && !generatingBatches.value.length)) return
  if (Date.now() - pollSince >= GENERATION_POLL_BUDGET_MS) return
  pollTimer = setTimeout(() => { void load() }, GENERATION_POLL_INTERVAL_MS)
}

async function load() {
  if (!active) return
  const token = ++versionToken
  loading.value = true
  loadError.value = ''
  if (!pollSince) pollSince = Date.now()
  try {
    const result = await props.api.list(props.detail.debate.id)
    if (!active || token !== versionToken) return
    batches.value = result.items
    if (awaitingFreshBatch && result.items.some(batch =>
      batch.round_no === observedRounds && batch.status !== 'not_started',
    )) awaitingFreshBatch = false
    schedulePolling()
  } catch (error) {
    if (!active || token !== versionToken) return
    loadError.value = error instanceof ApiError ? error.message : '批注读取失败，请手动重试。'
  } finally {
    if (active && token === versionToken) loading.value = false
  }
}

/**
 * 手动补做：唯一允许的写入口。
 * 服务端约束"最多一次"，成功后再次点击只读回已存结果。
 */
async function retry(batch: AnnotationBatch) {
  if (props.disabled || retrying.value) return
  retrying.value = batch.turn_id
  notice.value = ''
  let failureNotice = ''
  try {
    const updated = await props.api.retry(props.detail.debate.id, batch.turn_id, batch.attempt_version)
    if (!active) return
    batches.value = batches.value.map(current => current.turn_id === updated.turn_id ? updated : current)
    if (updated.status === 'generating') {
      notice.value = '批注正在生成，可以继续发言。'
      pollSince = Date.now()
      schedulePolling()
    } else if (updated.status === 'failed') {
      notice.value = '批注仍未完成，本场辩论不受影响。'
    }
  } catch (error) {
    if (!active) return
    // 版本冲突等失败只重新读取，不自行换版本重发。
    failureNotice = error instanceof ApiError ? error.message : '补做失败，请稍后手动重试。'
    notice.value = failureNotice
    await load()
    if (active && failureNotice) notice.value = failureNotice
  } finally {
    if (active) retrying.value = null
  }
}

// 会话切换：清空状态、定时器与版本，随后重读。
watch(() => props.detail.debate.id, () => {
  versionToken++
  stopPolling()
  expanded.value = new Set()
  focusedKey.value = null
  entryElements.clear()
  batches.value = []
  notice.value = ''
  pollSince = 0
  observedRounds = triggerPosition.value
  awaitingFreshBatch = false
  void load()
}, { immediate: true })

// 完成轮数增加（新一轮提交成功）时立即重读，让场中批注跟上来。
watch(triggerPosition, rounds => {
  if (rounds === observedRounds) return
  awaitingFreshBatch = rounds > observedRounds
  observedRounds = rounds
  pollSince = 0
  void load()
})

onUnmounted(() => {
  active = false
  versionToken++
  stopPolling()
  entryElements.clear()
})

defineExpose({ load, messageItems, focusFromMarker })
</script>

<template>
  <aside class="annotation-panel" aria-label="原文批注解释" data-annotation-panel :aria-busy="loading">
    <h3>原文批注</h3>

    <p v-if="loading && !hasAnyBatch" role="status">正在读取批注…</p>
    <Alert v-else-if="loadError" type="error" :title="loadError" role="alert">
      <Button size="small" data-annotation-reload :disabled="disabled" @click="load">重试读取</Button>
    </Alert>
    <p v-else-if="notice" class="notice" role="status">{{ notice }}</p>

    <template v-if="!loadError">
      <p v-if="generatingBatches.length" class="state" role="status">批注分析中，不阻塞发言。</p>

      <!-- not_started / failed 都给出可执行的手动补做入口。 -->
      <div v-for="batch in retryableBatches" :key="`pending-${batch.turn_id}`" class="pending-block" :data-annotation-pending="batch.round_no">
        <p v-if="batch.status === 'not_started'">第 {{ batch.round_no }} {{ positionUnit }}批注尚未开始，可手动补做。</p>
        <p v-else>第 {{ batch.round_no }} {{ positionUnit }}批注未完成（错误码：{{ batch.error_code ?? 'unknown' }}）。辩论与保存不受影响。</p>
        <Button
          size="small"
          data-annotation-retry
          :disabled="disabled || Boolean(retrying)"
          :loading="retrying === batch.turn_id"
          @click="retry(batch)"
        >
          手动补做批注
        </Button>
      </div>

      <div v-for="batch in completedBatches" :key="`batch-${batch.turn_id}`" class="round-block" :data-annotation-round="batch.round_no">
        <h4>第 {{ batch.round_no }} {{ positionUnit }}<span class="count">批注 {{ batch.items.length }} 条</span></h4>
        <p v-if="batch.items.length === 0" class="empty" role="status">本次没有批注，这不代表没有问题。</p>
        <ul v-else class="annotation-list">
          <li v-for="item in batch.items" :key="keysOf(item)">
            <button
              type="button"
              class="annotation-entry"
              :class="`marker-${styleOf(item).className}`"
              :data-annotation-entry="keysOf(item)"
              :aria-expanded="isExpanded(keysOf(item))"
              @click="toggle(item)"
            >
              <span class="tag">{{ styleOf(item).label }}</span>
              <span class="quote">{{ item.anchor.quote }}</span>
              <span class="chev">{{ isExpanded(keysOf(item)) ? '收起' : '展开' }}</span>
            </button>
            <div
              v-if="isExpanded(keysOf(item))"
              :ref="element => registerEntry(keysOf(item), element as Element | null)"
              class="annotation-detail"
              :class="{ 'is-focused': focusedKey === keysOf(item) }"
              :data-annotation-detail="keysOf(item)"
              tabindex="-1"
              @focus="onDetailFocus(keysOf(item))"
              @blur="onDetailBlur(keysOf(item))"
            >
              <p><span class="field">判断理由</span>{{ item.reason }}</p>
              <p><span class="field">改进方向</span>{{ item.improvement }}</p>
              <Button v-if="item.anchor.resolved" type="text" size="small" @click="emit('locate', keysOf(item))">定位原文</Button>
              <p v-if="!item.anchor.resolved" class="pending" role="status">
                原文待核对：该引用在原文中不唯一、位置与保存坐标不一致，或当前浏览器无法判定字素边界，因此不高亮。
              </p>
            </div>
          </li>
        </ul>
      </div>

      <p v-if="!loading && !hasAnyBatch" class="empty" role="status">本场暂无批注记录。</p>
    </template>
  </aside>
</template>

<style scoped>
.annotation-panel {
  padding: 18px;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-panel);
  background: var(--app-surface);
}
.annotation-panel h3 { margin: 0 0 12px; font-size: 16px; }
.state, .empty, .notice { color: var(--app-text-muted); font-size: 13px; }
.notice { color: var(--app-accent); }
.round-block { margin-top: 16px; }
.round-block h4 { margin: 0 0 8px; font-size: 14px; }
.count { margin-left: 8px; color: var(--app-text-muted); font-weight: 400; font-size: 12px; }
.annotation-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
.annotation-entry {
  display: flex; align-items: flex-start; gap: 8px; width: 100%;
  padding: 8px 10px; text-align: left;
  border: 1px solid var(--app-border); border-radius: var(--app-radius-control);
  background: var(--app-background); color: var(--app-text); font: inherit;
}
.tag { flex: none; font-size: 12px; font-weight: 600; }
.quote { flex: 1; overflow-wrap: anywhere; }
.chev { flex: none; font-size: 12px; color: var(--app-text-muted); }
.annotation-detail { margin-top: 8px; padding: 10px 12px; border-left: 3px solid var(--app-border); }
/* 状态类方框对程序化（鼠标）焦点同样可见；:focus-visible 兜底键盘直接聚焦。 */
.annotation-detail.is-focused,
.annotation-detail:focus-visible { outline: 3px solid var(--app-accent); outline-offset: 2px; }
.annotation-detail p { margin: 0 0 8px; font-size: 13px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
.field { display: block; font-weight: 600; margin-bottom: 2px; }
.pending { color: var(--app-warning-text, #a86b00); }
.pending-block { margin-top: 12px; padding: 12px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); background: var(--app-background); }
.pending-block p { margin: 0 0 10px; font-size: 13px; }
/* 四类解释入口同样保留颜色 + 线型，文字标签始终存在。 */
.marker-strength { background: #eef8ee; border-left: 4px solid #4a8f4a; }
.marker-logic-gap { border-bottom: 2px wavy #d97a1f; }
.marker-support { background: #edf4fb; border-left: 4px dashed #4a76a8; }
.marker-scope { border-bottom: 2px solid #6f4a9c; }
@media (max-width: 720px) {
  /* 移动端作为底部面板。 */
  .annotation-panel { border-radius: var(--app-radius-panel) var(--app-radius-panel) 0 0; }
}
</style>
