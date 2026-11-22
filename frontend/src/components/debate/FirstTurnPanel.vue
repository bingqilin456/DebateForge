<script setup lang="ts">
import { USER_CONTENT_MAX_CHARS } from '../../shared/limits'
import { locateQuoteText } from '../../shared/locateQuote'
import { readQuoteSelection, verifyQuoteInContent } from '../../shared/selectQuote'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Button, Switch } from '@arco-design/web-vue'
import { AI_STYLE_LABELS } from '../../debate/styles'
import type { AuthState } from '../../auth/state'
import { createDebateApi, hasNonWhitespace, isEntityId } from '../../api/debate'
import { createAnnotationApi } from '../../api/annotations'
import { createReviewApi } from '../../api/review'
import { createRepracticeApi } from '../../api/repractice'
import { createFirstTurnState } from '../../debate/firstTurn'
import { createReviewState } from '../../debate/review'
import { createRepracticeState } from '../../debate/repractice'
import type { AnnotationItem, RepracticeSource, RepracticeVersion, ReviewQuote } from '../../types/api'
import AnnotationPanel from './AnnotationPanel.vue'
import AnnotatedText from './AnnotatedText.vue'
import RepracticePanel from './RepracticePanel.vue'
import ReviewPanel from './ReviewPanel.vue'
import SummaryPanel from './SummaryPanel.vue'
import StrategyPanel from './StrategyPanel.vue'

const props = defineProps<{ auth: AuthState; route: string }>()
const emit = defineEmits<{
  'new-practice': []
  'open-history': []
}>()
const api = createDebateApi(props.auth.client)
// 批注与轮次共用同一认证客户端；它只在详情就绪后读取，不自动补做。
const annotationApi = createAnnotationApi(props.auth.client)
// 复盘同样是独立能力：读取已有状态，只有用户显式点击才生成或重试。
const reviewApi = createReviewApi(props.auth.client)
// 局部重练与复盘/批注互相独立：只读写自己的版本表，不改正式消息与比赛状态。
const repracticeApi = createRepracticeApi(props.auth.client)
const panel = createFirstTurnState(api)
const review = createReviewState(reviewApi)
const repractice = createRepracticeState(repracticeApi)
const {
  detail, busy, uncertain, draft, notice, id, streamText, streaming, streamRound,
  nextRound, canSend, canRetryTurn, canRetrySummary,
} = panel
const {
  status: reviewStatus, loading: reviewLoading, busy: reviewBusy, notice: reviewNotice,
  unavailable: reviewUnavailable, canGenerate: canGenerateReview, canRetry: canRetryReview,
} = review
const {
  available: repracticeAvailable, list: repracticeVersions, selection: repracticeSelection,
  goal: repracticeGoal, rewrite: repracticeRewrite, restoredFrom: repracticeRestoredFrom,
  rewriteCount: repracticeRewriteCount, goalCount: repracticeGoalCount, canSubmit: canSubmitRepractice,
  busy: repracticeBusy, loading: repracticeLoading, notice: repracticeNotice,
  failure: repracticeFailure, hasSelection: hasRepracticeSelection,
} = repractice
const input = ref('')
// 场中批注开关只控展示，不控生成与保存。
const showAnnotations = ref(true)
// 侧栏暴露 messageItems/focusFromMarker：标记点击后由它展开并聚焦对应解释。
const annotationPanel = ref<InstanceType<typeof AnnotationPanel> | null>(null)
const transcript = ref<HTMLElement | null>(null)
const repracticeSection = ref<HTMLElement | null>(null)
const repracticeEditor = ref<InstanceType<typeof RepracticePanel> | null>(null)
// 主动选段的即时提示：未选中任何原文时给出可操作说明，而不是静默失败。
const selectionNotice = ref('')

function messageAnnotations(messageId: string): AnnotationItem[] {
  return annotationPanel.value?.messageItems(messageId) ?? []
}

/** 标记点击：把该段全部条目键交给侧栏，由侧栏展开并聚焦第一条解释。 */
function onMarkerSelect(keys: string[]) {
  void annotationPanel.value?.focusFromMarker(keys)
}

function locateAnnotation(key: string) {
  const candidates = transcript.value?.querySelectorAll<HTMLButtonElement>('[data-annotation-keys]')
  const marker = Array.from(candidates ?? []).find(element =>
    element.dataset.annotationKeys?.split('\u0001').includes(key),
  )
  marker?.scrollIntoView?.({ block: 'center' })
  marker?.focus({ preventScroll: true })
}

/**
 * 把复盘引用定位到正式消息原文。
 *
 * 落点语义与视觉已收敛到 shared/locateQuote + styles/theme.css，和争点卡预览共用同一套：
 * 坐标是 Unicode 码点半开区间，命中后只给整段原文加外框，不改动原文、不复制第二份文本。
 */
function locateReviewQuote(messageId: string, startCp: number, endCp: number) {
  locateQuoteText(transcript.value, messageId, startCp, endCp)
}

/**
 * 主动选中本人发言的一段原文进入重练。
 *
 * 这是 V2-08 的主入口：不依赖批注或争点是否已生成。选段只在本地换算成
 * 码点区间并核对原文；服务端仍会再验证归属、完赛状态与逐字匹配。
 */
function selectRepracticeFromSelection() {
  selectionNotice.value = ''
  const picked = readQuoteSelection(transcript.value)
  if (!picked) {
    selectionNotice.value = '请先在本人发言中选中一段原文，再点“选句重练”。'
    return
  }
  // 只接受本人（user）消息：AI 原文不改写。
  const source = locateUserQuote(picked.messageId, picked.startCp, picked.endCp, picked.quote)
  if (!source) {
    selectionNotice.value = '只能选本人发言的原文；AI 回复不能作为重练对象。'
    return
  }
  repractice.selectSource(source)
  void nextTick(() => {
    repracticeSection.value?.scrollIntoView?.({ block: 'center' })
    repracticeEditor.value?.$el.querySelector('textarea')?.focus()
  })
}

/**
 * 复盘与争点的原文入口复用同一重练入口。
 *
 * 改进提示带入目标；引用若已由服务端核对且仍能在当前消息里逐字复核，
 * 就直接带入原句，否则只聚焦重练区并提示重新选段（不猜坐标）。
 */
function prepareRepractice(improvement: string, quote: ReviewQuote | null) {
  selectionNotice.value = ''
  if (quote && quote.resolved && quote.start_cp !== null && quote.end_cp !== null) {
    const source = locateUserQuote(quote.message_id, quote.start_cp, quote.end_cp, quote.quote)
    if (source) {
      repractice.selectSource(source, improvement)
      void nextTick(() => {
        repracticeSection.value?.scrollIntoView?.({ block: 'center' })
        repracticeEditor.value?.$el.querySelector('textarea')?.focus()
      })
      return
    }
    selectionNotice.value = '该引用与当前原文无法逐字核对，请在本人发言中重新选中要重练的原句。'
  } else if (improvement) {
    // 没有可核对的坐标时仍把改进提示带入目标，不伪造原句。
    repractice.applyGoalOnly(improvement)
    selectionNotice.value = '已带入改进提示；请先在本人发言中选中要重练的原句。'
  }
  void nextTick(() => { repracticeSection.value?.scrollIntoView?.({ block: 'center' }) })
}

/** 用消息号与码点区间在当前会话里复核引用，命中则返回选段。 */
function locateUserQuote(messageId: string, startCp: number, endCp: number, quote: string): RepracticeSource | null {
  for (const turn of detail.value?.turns ?? []) {
    for (const message of turn.messages) {
      if (message.id !== messageId || message.role !== 'user') continue
      if (!verifyQuoteInContent(message.content, startCp, endCp, quote)) return null
      return { message_id: messageId, start_cp: startCp, end_cp: endCp, quote }
    }
  }
  return null
}

const locked = computed(() => busy.value || props.auth.state.pending)
const count = computed(() => [...input.value].length)
const initialId = /^#debate=([1-9]\d{0,19})$/.exec(props.route)?.[1]
const invalidRoute = Boolean(props.route && (!initialId || !isEntityId(initialId)))
let active = true

watch(id, value => {
  if (value && active) history.replaceState(null, '', `${location.pathname}${location.search}#debate=${value}`)
})
// 会话就绪后读取复盘与重练状态；未完成会话也读取，但面板只在完赛后展示。
// 生成入口完全由用户点击触发，这里绝不自动请求生成。
watch(id, value => {
  if (!value) return
  void review.open(value)
  void repractice.open(value)
})
watch(draft, value => {
  if (value) input.value = value
  else if (detail.value?.turns.length) input.value = ''
})

async function submit() {
  if (locked.value) return
  await panel.submit(draft.value || input.value)
}

function newPractice() {
  emit('new-practice')
}

function localTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function retryRepractice(version: RepracticeVersion) {
  void repractice.retry(version)
}

onMounted(() => {
  if (initialId && !invalidRoute) void panel.open(initialId)
})
onUnmounted(() => { active = false; panel.dispose(); review.dispose(); repractice.dispose() })
</script>

<template>
  <section v-if="invalidRoute" class="card">
    <h2>会话地址无效</h2>
    <Button @click="newPractice">返回选题</Button>
  </section>
  <template v-else>
    <p v-if="notice" class="panel-notice" role="status" aria-live="polite">{{ notice }}</p>
    <section v-if="!id" class="card">
      <h2>开始一次辩论练习</h2>
      <p>请前往辩题列表选择辩题、立场和流派。</p>
      <div class="heading-actions">
        <Button type="primary" :disabled="locked" @click="newPractice">开始新的练习</Button>
        <Button data-open-history :disabled="locked" @click="emit('open-history')">查看历史会话</Button>
      </div>
    </section>

    <div v-else class="debate-workspace" :class="{ 'with-annotations': detail && showAnnotations }">
      <section class="card debate-main" :aria-busy="busy" aria-label="首轮辩论面板">
        <div class="debate-heading">
          <h2>{{ detail?.debate.topic.title ?? '正在读取会话' }}</h2>
          <span class="muted">{{ detail?.debate.status === 'completed' ? '已完成' : '辩论练习' }}</span>
        </div>
        <p v-if="detail" class="muted">
          你：{{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }} ·
          AI：{{ detail.debate.ai_stance === 'pro' ? '正方' : '反方' }} ·
          <span :data-ai-style="detail.debate.ai_style">流派：{{ AI_STYLE_LABELS[detail.debate.ai_style] }}（{{ detail.debate.ai_style }}）</span> ·
          赛制：{{ detail.debate.mode === 'standard_4' ? '四轮标准赛' : '三轮快速赛' }} ·
          已完成 {{ detail.debate.completed_rounds }} / {{ detail.debate.total_rounds }} 轮
        </p>
        <p class="muted">会话 {{ id }} · 刷新可恢复当前会话</p>

        <StrategyPanel v-if="detail" :auth="auth" :detail="detail" :disabled="locked || uncertain" />

        <div v-if="detail" class="annotation-controls">
          <span id="annotation-toggle-label">显示批注</span>
          <Switch v-model="showAnnotations" aria-labelledby="annotation-toggle-label" aria-describedby="annotation-toggle-hint" data-annotation-toggle size="small" />
          <span id="annotation-toggle-hint" class="muted">关闭后隐藏标记和侧栏，批注仍会保存。</span>
        </div>

        <div ref="transcript" class="debate-transcript">
          <div v-for="turn in detail?.turns" :key="turn.id" class="turn-block" :data-round-no="turn.round_no">
            <div class="round-title">第 {{ turn.round_no }} 轮（尝试 v{{ turn.attempt_version }}）</div>
            <article v-for="message in turn.messages" :key="message.id" :class="['message', message.role]" :data-message-id="message.id">
              <div class="message-label">{{ message.role === 'user' ? '你的发言' : 'AI 回复 · 仅供练习参考' }}</div>
              <!-- 批注标记渲染在正式消息原文上，侧栏只放解释，不复制第二份原文。
                   tabindex=-1 只允许程序化聚焦（复盘定位后把焦点交给该段原文），不进 Tab 顺序。 -->
              <p data-message-content tabindex="-1">
                <AnnotatedText
                  :content="message.content"
                  :items="messageAnnotations(message.id)"
                  :show-markers="showAnnotations"
                  @select="onMarkerSelect"
                />
              </p>
              <time :datetime="message.created_at">{{ localTime(message.created_at) }}</time>
            </article>            <p v-if="turn.status === 'generating'" class="turn-generating" role="status">
              第 {{ turn.round_no }} 轮正在生成 AI 回复（v{{ turn.attempt_version }}）…
            </p>
            <div v-else-if="turn.status === 'failed'" class="turn-failed" role="alert">
              <p>
                第 {{ turn.round_no }} 轮生成未完成（错误码：{{ turn.error_code }} · 当前版本 v{{ turn.attempt_version }}），你的发言原文已保存。
              </p>
              <Button
                type="primary"
                data-retry-turn
                :disabled="locked || !canRetryTurn"
                :loading="busy"
                @click="panel.retryTurn"
              >
                重试第 {{ turn.round_no }} 轮（v{{ turn.attempt_version }}）
              </Button>
            </div>
          </div>

          <section v-if="streaming" class="stream-preview" data-stream-preview aria-label="实时回复预览" :aria-busy="streaming">
            <h3>第 {{ streamRound }} 轮 · 正在接收回复</h3>
            <p class="muted">以下为临时片段，尚未确认保存；提交确认后才计入正式消息和完成轮数。</p>
            <p class="stream-text" data-stream-text aria-live="polite">{{ streamText || '正在等待首个回复片段…' }}</p>
            <Button data-stop-stream :disabled="props.auth.state.pending" @click="panel.cancelStream">停止等待并查询</Button>
            <p class="muted">断线或停止等待后先查询已保存状态，不自动重新生成。</p>
          </section>

          <form v-if="detail && nextRound !== null" data-first-turn @submit.prevent="submit">
            <label for="first-content">{{ nextRound === 1 ? '第一轮发言' : `第 ${nextRound} 轮发言` }}</label>
            <textarea
              id="first-content"
              v-model="input"
              name="content"
              rows="7"
              :readonly="Boolean(draft)"
              :disabled="locked || uncertain"
              aria-describedby="content-help"
            />
            <p id="content-help" class="muted">{{ count }} / {{ USER_CONTENT_MAX_CHARS }} 个 Unicode 字符；请围绕辩题表达你的论点。</p>
            <Button
              type="primary"
              html-type="submit"
              :disabled="!canSend || props.auth.state.pending || !hasNonWhitespace(input) || count > USER_CONTENT_MAX_CHARS"
              :loading="busy"
            >
              {{ busy ? '正在处理…' : draft ? '重新发送原请求' : nextRound === 1 ? '发送首轮' : `发送第 ${nextRound} 轮` }}
            </Button>
          </form>

          <SummaryPanel
            v-if="detail && detail.debate.status === 'completed'"
            :summary="detail.summary_status"
            :busy="locked"
            :can-retry="canRetrySummary"
            :can-start="!locked && !uncertain && detail.summary_status.status === 'not_started'"
            @retry="panel.retrySummary"
            @start="panel.requestSummary"
          />

          <!-- V2-08 局部重练的主入口：完赛后主动选中本人发言的一段原文即可进入，
               不依赖批注或争点是否已生成；打开面板与选段都不触发生成。 -->
          <section v-if="detail && detail.debate.status === 'completed' && repracticeAvailable" ref="repracticeSection" class="repractice-entry" data-repractice-entry aria-label="进入局部重练">
            <div class="heading-actions">
              <!-- 选段是纯本地读取动作：轮次提交的 transient uncertain 不影响它，
                   服务端仍会在提交时独立复核归属、完赛状态与原文匹配。 -->
              <Button data-select-quote :disabled="props.auth.state.pending || repracticeBusy" @mousedown.prevent @click="selectRepracticeFromSelection">选句重练</Button>
              <Button v-if="hasRepracticeSelection" data-clear-quote :disabled="repracticeBusy" @click="repractice.clearSelection()">清除选段</Button>
            </div>
            <p class="muted">先在本场上方本人发言中选中一段原文，再点“选句重练”。AI 原文不能作为重练对象。</p>
            <p v-if="selectionNotice" class="panel-notice" role="status" aria-live="polite" data-selection-notice>{{ selectionNotice }}</p>
          </section>

          <!-- V2-13 复盘独立于总结：只在完赛后出现，失败不影响原文与总结。
               服务端显式返回 available=false 表示本环境未开启切片：整个复盘区隐藏，
               不留可点击生成入口，也不显示“无复盘”误导文案。 -->
          <p v-if="reviewNotice && !reviewUnavailable" class="panel-notice" role="status" aria-live="polite" data-review-notice>{{ reviewNotice }}</p>
          <ReviewPanel
            v-if="detail && detail.debate.status === 'completed' && !reviewUnavailable"
            :review="reviewStatus"
            :busy="locked || reviewBusy"
            :loading="reviewLoading"
            :can-generate="canGenerateReview && !locked && !uncertain"
            :can-retry="canRetryReview && !locked && !uncertain"
            :locate="locateReviewQuote"
            :repractice="repracticeAvailable && !repracticeBusy ? prepareRepractice : undefined"
            :can-repractice-quote="quote => quote.start_cp !== null && quote.end_cp !== null && !!locateUserQuote(quote.message_id, quote.start_cp, quote.end_cp, quote.quote)"
            @generate="review.generate"
            @retry="review.retry"
            @refresh="review.refresh()"
          />

          <!-- 重练面板：与复盘并列、互不依赖；能力关闭时整块隐藏。 -->
          <RepracticePanel
            v-if="detail && detail.debate.status === 'completed' && repracticeAvailable"
            ref="repracticeEditor"
            :available="repracticeAvailable"
            :versions="repracticeVersions"
            :selection="repracticeSelection"
            :goal="repracticeGoal"
            :rewrite="repracticeRewrite"
            :rewrite-count="repracticeRewriteCount"
            :goal-count="repracticeGoalCount"
            :can-submit="canSubmitRepractice && !props.auth.state.pending"
            :busy="repracticeBusy"
            :loading="repracticeLoading"
            :notice="repracticeNotice"
            :failure="repracticeFailure"
            :restored-from="repracticeRestoredFrom"
            @submit="repractice.submit"
            @retry="retryRepractice"
            @restore="repractice.restore"
            @clear-selection="repractice.clearSelection()"
            @locate="locateReviewQuote"
            @update:goal="repractice.setGoal"
            @update:rewrite="repractice.setRewrite"
          />
        </div>

        <div class="panel-actions">
          <Button :disabled="locked" @click="panel.refresh">查询状态</Button>
          <Button :disabled="locked" @click="newPractice">开始新的练习</Button>
        </div>
      </section>
      <AnnotationPanel
        v-if="detail"
        v-show="showAnnotations"
        ref="annotationPanel"
        class="debate-annotations"
        :detail="detail"
        :api="annotationApi"
        :disabled="locked || uncertain"
        @locate="locateAnnotation"
      />
    </div>
  </template>
</template>

<style scoped>
label { display: block; font-weight: 600; margin: 20px 0 10px; }
select, textarea { display: block; width: 100%; border: 1px solid var(--app-input-border); border-radius: var(--app-radius-control); background: var(--app-surface); color: var(--app-text); padding: 10px 12px; }
textarea { resize: vertical; line-height: 1.8; }
.topic-description, .panel-notice { white-space: pre-wrap; overflow-wrap: anywhere; }
.panel-notice { color: var(--app-accent); background: var(--app-info-bg); padding: 14px 18px; border-radius: var(--app-radius-control); }
.muted { color: var(--app-text-muted); font-size: 13px; }
.debate-heading { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; }
.debate-heading h2 { margin-bottom: 0; }
.turn-block { margin-top: 20px; }
.round-title { font-size: 13px; font-weight: 600; color: var(--app-accent); margin-bottom: 8px; }
.message { margin: 14px 0; padding: 20px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.message.assistant { background: var(--app-info-bg); border-color: var(--app-info-border); }
.message-label { font-weight: 600; font-size: 14px; }
.message p { white-space: pre-wrap; overflow-wrap: anywhere; }
/* 复盘引用定位的落点外框已收敛到 styles/theme.css 的 [data-located-quote]，全站共用一处定义。 */
time { color: var(--app-text-muted); font-size: 12px; }
.turn-generating { color: var(--app-accent); font-size: 14px; }
.turn-failed { margin: 12px 0; padding: 16px; border: 1px solid var(--app-error-border); border-radius: var(--app-radius-panel); background: var(--app-error-bg); color: var(--app-error-text); }
.turn-failed p { margin: 0 0 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
.stream-preview { margin: 20px 0; padding: 18px; border: 1px dashed var(--app-info-border); border-radius: var(--app-radius-panel); background: var(--app-info-bg); }
.stream-preview h3 { margin-top: 0; }
.stream-text { white-space: pre-wrap; overflow-wrap: anywhere; min-height: 3em; }
.heading-actions, .panel-actions { display: flex; flex-wrap: wrap; gap: 12px; }
.panel-actions { margin-top: 24px; }
.debate-workspace { display: grid; gap: 20px; }
.debate-main, .debate-annotations, .debate-transcript { min-width: 0; }
.debate-workspace.with-annotations .debate-main { margin-bottom: 0; }
.annotation-controls { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-top: 16px; }
@media (min-width: 1000px) {
  .debate-workspace.with-annotations {
    --workspace-width: min(1120px, calc(100vw - 64px));
    width: var(--workspace-width);
    margin-inline-start: calc((100% - var(--workspace-width)) / 2);
    grid-template-columns: minmax(0, 1fr) minmax(280px, 30%);
    align-items: start;
  }
}
</style>
