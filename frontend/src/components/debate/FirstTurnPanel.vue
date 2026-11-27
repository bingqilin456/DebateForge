<script setup lang="ts">
import { USER_CONTENT_MAX_CHARS } from '../../shared/limits'
import { locateAnnotationMarker, locateQuoteText } from '../../shared/locateQuote'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Button, Switch } from '@arco-design/web-vue'
import { AI_STYLE_LABELS } from '../../debate/styles'
import type { AuthState } from '../../auth/state'
import { createDebateApi, hasNonWhitespace, isEntityId } from '../../api/debate'
import { createAnnotationApi } from '../../api/annotations'
import { createReviewApi } from '../../api/review'
import { createFirstTurnState } from '../../debate/firstTurn'
import { createReviewState } from '../../debate/review'
import type { AnnotationItem, ReviewQuote } from '../../types/api'
import AnnotationPanel from './AnnotationPanel.vue'
import AnnotatedText from './AnnotatedText.vue'
import ReviewPanel from './ReviewPanel.vue'
import ReviewSharing from './ReviewSharing.vue'
import RepracticePanel from './RepracticePanel.vue'
import JuryPanel from './JuryPanel.vue'
import SummaryPanel from './SummaryPanel.vue'
import StrategyPanel from './StrategyPanel.vue'
import { localFramework, sampleArguments, useArena } from '../../debate/arena'
import { useArenaVoice } from '../../debate/arenaVoice'
import { playArenaSound } from '../../debate/arenaSound'

const props = defineProps<{ auth: AuthState; route: string; focusReport?: boolean }>()
const emit = defineEmits<{
  'new-practice': []
  'open-history': []
  'cup-changed': []
}>()
const api = createDebateApi(props.auth.client)
// 批注与轮次共用同一认证客户端；它只在详情就绪后读取，不自动补做。
const annotationApi = createAnnotationApi(props.auth.client)
// 复盘同样是独立能力：读取已有状态，只有用户显式点击才生成或重试。
const reviewApi = createReviewApi(props.auth.client)
const panel = createFirstTurnState(api)
const review = createReviewState(reviewApi)
const {
  detail, busy, uncertain, draft, notice, id, streamText, streaming, streamRound,
  nextRound, canSend, canRetryTurn, canRetrySummary,
} = panel
const {
  status: reviewStatus, loading: reviewLoading, busy: reviewBusy, notice: reviewNotice,
  unavailable: reviewUnavailable, canGenerate: canGenerateReview, canRetry: canRetryReview,
} = review
const input = ref('')
const juryOpen = ref(false)
const focusedReportId = ref<string | null>(null)
const completedRounds = computed(() => detail.value?.debate.completed_rounds ?? 0)
const { arena, current: momentum, hint, pending: arenaPending, arenaLoading, error: arenaError, assist } =
  useArena(props.auth.client, id, completedRounds, nextRound)
const draftNotice = ref('')
const frameworkDialog = ref(false)
const focusView = ref<'coach' | 'graph' | 'progress'>('coach')
const speech = useArenaVoice(text => {
  if (canAdjustDraft.value) setDraft(input.value ? `${input.value}\n${text}` : text)
})
const { listening, voiceNotice, toggleVoice, stopVoice } = speech
const timing = ref<{ start: number; first: number; elapsed: number; chars: number } | null>(null)
const cupStageNames: Record<string, string> = { prelim: '初赛', quarter: '复赛', semi: '半决赛', final: '决赛' }
const cupStage = computed(() => cupStageNames[detail.value?.debate.cup_stage ?? ''])
const streamStatus = computed(() => {
  if (streaming.value) return '正在接收 AI 回复'
  if (busy.value) return '正在处理请求'
  if (uncertain.value) return '等待确认会话状态'
  if (detail.value?.debate.status === 'completed') return '本场已结束'
  if (detail.value?.turns.at(-1)?.status === 'failed') return '等待重试'
  return nextRound.value === null ? '等待会话状态' : `第 ${nextRound.value} 轮 · 等待发言`
})

function setDraft(value: string) {
  draftNotice.value = ''
  if ([...value].length > USER_CONTENT_MAX_CHARS) {
    draftNotice.value = '辅助内容已超出发言字数上限。原草稿仍保留。'
    return
  }
  input.value = value
}

async function requestDraft(action: 'teammate' | 'framework') {
  frameworkDialog.value = false
  if (!canAdjustDraft.value) return
  const initial = input.value
  const response = await assist(action)
  if (response && canAdjustDraft.value && input.value === initial) {
    setDraft(response.text)
    if (!draftNotice.value) draftNotice.value = response.source === 'ai' ? 'AI 草稿已填入，可修改后发送。' : '本地草稿已填入，可修改后发送。'
  }
}

function locateArenaMessage(messageId: string) {
  const message = Array.from(transcript.value?.querySelectorAll<HTMLElement>('[data-message-id]') ?? [])
    .find(element => element.dataset.messageId === messageId)
  if (!message) return
  const text = message.querySelector('[data-message-content]')?.textContent
  if (text) locateQuoteText(transcript.value, messageId, 0, [...text].length)
}
// 场中批注开关只控展示，不控生成与保存。
const showAnnotations = ref(true)
const showCoach = computed(() => focusView.value === 'coach')
// 侧栏暴露 messageItems/focusFromMarker：标记点击后由它展开并聚焦对应解释。
const annotationPanel = ref<InstanceType<typeof AnnotationPanel> | null>(null)
const transcript = ref<HTMLElement | null>(null)
const following = ref(true)
const secondsLeft = ref(120)
const timerRunning = ref(false)
const timerNotice = ref('')
let timer: ReturnType<typeof setInterval> | undefined

function followLatest() {
  if (following.value && transcript.value) transcript.value.scrollTop = transcript.value.scrollHeight
}

function onTranscriptScroll() {
  const element = transcript.value
  if (element) following.value = element.scrollHeight - element.scrollTop - element.clientHeight <= 24
}

function jumpToLatest() {
  following.value = true
  followLatest()
}

function pauseTimer() {
  clearInterval(timer)
  timerRunning.value = false
}

function resetTimer() {
  pauseTimer()
  secondsLeft.value = 120
  timerNotice.value = ''
}

function toggleTimer() {
  if (timerRunning.value) {
    pauseTimer()
    return
  }
  if (!secondsLeft.value) return
  timerRunning.value = true
  timer = setInterval(() => {
    secondsLeft.value--
    if (!secondsLeft.value) {
      pauseTimer()
      timerNotice.value = '本轮计时结束，可以继续完成发言。'
    }
  }, 1000)
}

const timerText = computed(() => `${String(Math.floor(secondsLeft.value / 60)).padStart(2, '0')}:${String(secondsLeft.value % 60).padStart(2, '0')}`)
const roundLabels = computed(() => {
  const debate = detail.value?.debate
  if (!debate) return []
  const labels = debate.mode === 'quick_3' ? ['立论', '攻辩', '结辩'] : ['立论', '攻辩', '交锋', '结辩']
  return Array.from({ length: debate.total_rounds }, (_, index) => labels[index] ?? `第 ${index + 1} 轮`)
})

function jumpToReview() {
  if (detail.value?.debate.status === 'completed') focusedReportId.value = detail.value.debate.id
  const workbench = transcript.value?.closest('.debate-main')
  const target = workbench?.querySelector<HTMLElement>('[data-match-review]')
    ?? workbench?.querySelector<HTMLElement>('[data-completed-summary]')
  target?.scrollIntoView?.({ block: 'start' })
}

watch(nextRound, resetTimer)
watch([detail, streamText], followLatest, { flush: 'post' })
watch(() => props.focusReport && detail.value?.debate.status === 'completed', ready => {
  if (ready) jumpToReview()
}, { flush: 'post' })
const repracticePanel = ref<InstanceType<typeof RepracticePanel> | null>(null)

function canRepractice(quote: ReviewQuote | null) {
  return !!quote?.resolved && !!detail.value?.turns.some(turn =>
    turn.messages.some(message => message.id === quote.message_id && message.role === 'user'))
}

function prepareRepractice(improvement: string, quote: ReviewQuote | null) {
  if (!quote || !canRepractice(quote) || quote.start_cp === null || quote.end_cp === null) return
  const located = locateQuoteText(transcript.value, quote.message_id, quote.start_cp, quote.end_cp)
  if (located?.quote !== quote.quote) return
  void repracticePanel.value?.prepare({
    message_id: quote.message_id, start_cp: quote.start_cp, end_cp: quote.end_cp, quote: quote.quote, goal: improvement,
  })
}

function reviseMessage(messageId: string, content: string) {
  const annotation = messageAnnotations(messageId).find(item => item.anchor.resolved && item.improvement)
  const anchor = annotation?.anchor
  const goal = annotation?.improvement ?? '让这段发言的论点和理由更清晰。'
  const fullText: ReviewQuote = {
    message_id: messageId, quote: content, start_cp: 0, end_cp: [...content].length, resolved: true,
  }
  prepareRepractice(goal, anchor ?? fullText)
}

function messageAnnotations(messageId: string): AnnotationItem[] {
  return annotationPanel.value?.messageItems(messageId) ?? []
}

/** 标记点击：把该段全部条目键交给侧栏，由侧栏展开并聚焦第一条解释。 */
function onMarkerSelect(keys: string[]) {
  focusView.value = 'coach'
  void nextTick(() => annotationPanel.value?.focusFromMarker(keys))
}

function locateAnnotation(key: string) {
  locateAnnotationMarker(transcript.value, key)
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

const locked = computed(() => busy.value || props.auth.state.pending)
const canAdjustDraft = computed(() => !locked.value && !uncertain.value && !draft.value && nextRound.value !== null)
const count = computed(() => [...input.value].length)
const sendable = computed(() => canSend.value && !locked.value && hasNonWhitespace(input.value) && count.value <= USER_CONTENT_MAX_CHARS)
const initialId = /^#debate=([1-9]\d{0,19})$/.exec(props.route)?.[1]
const invalidRoute = Boolean(props.route && (!initialId || !isEntityId(initialId)))
let active = true

watch(id, value => {
  if (value && active) history.replaceState(null, '', `${location.pathname}${location.search}#debate=${value}`)
})
// 会话就绪后读取复盘状态；未完成会话也读取，但面板只在完赛后展示。
// 生成入口完全由用户点击触发，这里绝不自动请求生成。
watch(id, value => {
  if (!value) return
  void review.open(value)
})
watch(draft, value => {
  if (value) input.value = value
  else if (detail.value?.turns.length) input.value = ''
})

async function submit() {
  if (!sendable.value) return
  stopVoice()
  playArenaSound()
  timing.value = { start: performance.now(), first: 0, elapsed: 0, chars: 0 }
  await panel.submit(draft.value || input.value)
}

watch(streamText, value => {
  if (!timing.value || !value) return
  const elapsed = performance.now() - timing.value.start
  timing.value.first ||= elapsed
  timing.value.elapsed = elapsed
  timing.value.chars = [...value].length
}, { flush: 'sync' })

watch(nextRound, () => { frameworkDialog.value = false; draftNotice.value = ''; stopVoice() })

function submitShortcut(event: KeyboardEvent) {
  if (event.isComposing || !event.ctrlKey) return
  event.preventDefault()
  void submit()
}

function newPractice() {
  emit('new-practice')
}

function localTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => {
  if (initialId && !invalidRoute) void panel.open(initialId)
})
onUnmounted(() => { active = false; pauseTimer(); panel.dispose(); review.dispose() })
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

    <div v-else class="neon-arena">
      <section class="neon-match-card" :aria-busy="busy">
        <div class="neon-match-top">
          <div class="neon-match-title">
            <div v-if="detail" class="neon-tags">
              <span v-if="cupStage" class="gold">思弈杯 · {{ cupStage }}</span>
              <span v-if="arena.practice || detail.debate.practice" data-practice-label>本地练习 · 不计正式积分</span>
              <span v-if="detail.debate.status === 'completed'">已完成</span>
              <span :class="detail.debate.user_stance">我方：{{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }}</span>
              <span :class="detail.debate.ai_stance">
                AI {{ detail.debate.ai_stance === 'pro' ? '正方' : '反方' }}：{{ detail.debate.opponent_name || AI_STYLE_LABELS[detail.debate.ai_style] }}
              </span>
            </div>
            <h1>{{ detail?.debate.topic.title ?? '正在读取会话' }}</h1>
          </div>
          <div class="neon-match-actions">
            <div v-if="detail?.debate.status === 'active'" class="neon-timer-control">
              <span aria-hidden="true">◷</span>
              <strong>{{ timerText }}</strong>
              <Button size="small" :aria-label="timerRunning ? '暂停计时' : '开始计时'" :disabled="!secondsLeft" @click="toggleTimer">
                {{ timerRunning ? 'Ⅱ' : '▷' }}
              </Button>
              <Button size="small" aria-label="重置计时" @click="resetTimer">↻</Button>
            </div>
            <Button size="small" :disabled="locked" @click="newPractice">开始新的练习</Button>
            <Button size="small" :disabled="locked" @click="panel.refresh">查询状态</Button>
          </div>
        </div>
        <p v-if="timerNotice" class="muted" role="status">{{ timerNotice }}</p>
        <div v-if="detail" class="neon-step-row">
          <ol class="neon-stepper" aria-label="单场轮次">
            <li
              v-for="(label, index) in roundLabels"
              :key="index"
              :class="{ current: index + 1 === nextRound, done: index < completedRounds }"
              :aria-current="index + 1 === nextRound ? 'step' : undefined"
            ><b>{{ index + 1 }}</b>{{ label }}</li>
          </ol>
          <div class="neon-momentum">
            <div>
              <strong>正方 {{ momentum?.pro_percent ?? 50 }}%</strong>
              <span>实时说服力博弈</span>
              <strong>{{ 100 - (momentum?.pro_percent ?? 50) }}% 反方气势</strong>
            </div>
            <div class="neon-momentum-bar" aria-hidden="true">
              <i :style="{ width: (momentum?.pro_percent ?? 50) + '%' }"></i>
              <i :style="{ width: (100 - (momentum?.pro_percent ?? 50)) + '%' }"></i>
            </div>
          </div>
        </div>
      </section>

      <section v-if="detail" class="neon-workspace">
        <article class="neon-stage debate-main" role="region" :aria-busy="busy" aria-label="首轮辩论面板">
          <div class="neon-stage-head">
            <div><span class="live-dot" aria-hidden="true"></span>{{ streamStatus }}</div>
            <div class="neon-legend">
              <span><i class="jade"></i>我方亮点</span>
              <span><i class="red"></i>我方漏洞</span>
              <span><i class="gold-line"></i>论据补强</span>
              <span><i class="violet"></i>敌方破绽</span>
              <span><i class="blue"></i>敌方杀招</span>
            </div>
          </div>
          <p class="muted neon-session-meta">
            <span :data-ai-style="detail.debate.ai_style">流派：{{ AI_STYLE_LABELS[detail.debate.ai_style] }}（{{ detail.debate.ai_style }}）</span>
            · 已完成 {{ completedRounds }} / {{ detail.debate.total_rounds }} 轮 · 会话 {{ id }}
          </p>
          <div ref="transcript" class="neon-feed debate-transcript" tabindex="0" aria-label="交锋记录" @scroll="onTranscriptScroll">
            <div class="neon-chair">
              <b>⚑ 主席台 · 赛制引导</b>
              本场共 {{ detail.debate.total_rounds }} 轮。你代表「{{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }}」。
              <p v-if="detail.debate.topic.description">{{ detail.debate.topic.description }}</p>
            </div>
            <div v-if="!detail.turns.length && !streaming" class="neon-empty">
              <div><strong>准备好了吗？</strong><p>从界定辩题中的关键概念开始，写下你的第一轮立论。</p></div>
            </div>
            <div v-for="turn in detail?.turns" :key="turn.id" class="turn-block" :data-round-no="turn.round_no">
              <div class="round-title">第 {{ turn.round_no }} 轮（尝试 v{{ turn.attempt_version }}）</div>
              <article
                v-for="message in turn.messages"
                :key="message.id"
                :class="['message', message.role]"
                :data-stance="message.role === 'user' ? detail?.debate.user_stance : detail?.debate.ai_stance"
                :data-message-id="message.id"
              >
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
                <Button v-if="detail?.debate.status === 'completed' && message.role === 'user' && !reviewUnavailable"
                  type="text" size="mini" @click="reviseMessage(message.id, message.content)">
                  修订并重评
                </Button>
              </article>
              <p v-if="turn.status === 'generating'" class="turn-generating" role="status">
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
          </div>
          <Button v-if="!following" class="jump-latest" @click="jumpToLatest">↓ 回到底部</Button>

          <div class="neon-composer">
            <form v-if="nextRound !== null" data-first-turn @submit.prevent="submit">
              <div class="neon-quick-row" aria-label="辅助发言">
                <span>队友/辅助：</span>
                <Button class="quick-teammate" size="small" aria-label="AI 队友代打本轮" :disabled="!canAdjustDraft || !!arenaPending" :loading="arenaPending === 'teammate'" @click="requestDraft('teammate')">
                  AI 队友代打本轮 <em aria-hidden="true">AI</em>
                </Button>
                <Button size="small" :disabled="!canAdjustDraft || !!arenaPending" @click="frameworkDialog = true">三段论框架（AI）</Button>
                <span class="separator" aria-hidden="true"></span>
                <Button class="quick-strong" size="small" :disabled="!canAdjustDraft" @click="setDraft(sampleArguments.strong)">预设高分辩词</Button>
                <Button class="quick-flawed" size="small" :disabled="!canAdjustDraft" @click="setDraft(sampleArguments.flawed)">预设漏洞辩词</Button>
                <Button size="small" :disabled="!canAdjustDraft && !listening" :aria-pressed="listening" @click="toggleVoice">
                  {{ listening ? '停止语音输入' : '语音输入' }}
                </Button>
              </div>
              <fieldset v-if="frameworkDialog" class="framework-options">
                <legend>生成当前轮的三段论框架？</legend>
                <p>确认后可能调用已配置的模型。本地框架无需调用模型。</p>
                <Button size="small" @click="frameworkDialog = false">取消</Button>
                <Button size="small" @click="frameworkDialog = false; setDraft(localFramework)">用本地框架</Button>
                <Button size="small" :disabled="!!arenaPending || !canAdjustDraft" @click="requestDraft('framework')">确认生成</Button>
              </fieldset>
              <p v-if="draftNotice || voiceNotice || arenaError" role="status">{{ draftNotice || voiceNotice || arenaError }}</p>
              <label for="first-content">{{ nextRound === 1 ? '第一轮发言' : '第 ' + nextRound + ' 轮发言' }}</label>
              <div class="neon-input-row">
                <div class="neon-textarea-wrap">
                  <textarea
                    id="first-content"
                    v-model="input"
                    name="content"
                    rows="3"
                    placeholder="输入本轮辩词，或使用上方的辅助工具"
                    :readonly="Boolean(draft)"
                    :disabled="locked || uncertain"
                    aria-describedby="content-help"
                    @keydown.enter="submitShortcut"
                  />
                  <div id="content-help" class="neon-input-meta"><span>Ctrl+Enter 发送</span><span>{{ count }} / {{ USER_CONTENT_MAX_CHARS }} 字</span></div>
                </div>
                <Button type="primary" html-type="submit" class="neon-send" :disabled="!sendable" :loading="busy">
                  {{ busy ? '正在处理…' : draft ? '重新发送原请求' : detail.debate.mode === 'standard_4' ? '发送交锋' : nextRound === 1 ? '发送首轮' : '发送第 ' + nextRound + ' 轮' }}
                </Button>
              </div>
            </form>

            <div v-if="detail?.debate.status === 'completed'" class="match-complete">
              <strong>本场交锋已结束</strong>
              <Button type="primary" @click="jumpToReview">查看本场复盘</Button>
            </div>
            <SummaryPanel
              v-if="detail && detail.debate.status === 'completed'"
              :summary="detail.summary_status"
              :busy="locked"
              :can-retry="canRetrySummary"
              :can-start="!locked && !uncertain && detail.summary_status.status === 'not_started'"
              @retry="panel.retrySummary"
              @start="panel.requestSummary"
              data-completed-summary
            />

            <!-- V2-13 复盘独立于总结：只在完赛后出现，失败不影响原文与总结。
                 服务端显式返回 available=false 表示本环境未开启切片：整个复盘区隐藏，
                 不留可点击生成入口，也不显示“无复盘”误导文案。 -->
            <p v-if="reviewNotice && !reviewUnavailable" class="panel-notice" role="status" aria-live="polite" data-review-notice>{{ reviewNotice }}</p>
            <details v-if="id && detail?.debate.mode === 'standard_4' && detail.debate.status === 'completed'" @toggle="juryOpen ||= ($event.target as HTMLDetailsElement).open">
              <summary>五维评分与裁判团</summary>
              <JuryPanel v-if="juryOpen" :client="auth.client" :debate-id="id" :practice="detail.debate.practice" @settled="emit('cup-changed')" />
            </details>
            <ReviewPanel
              v-if="detail && detail.debate.status === 'completed' && !reviewUnavailable"
              data-match-review
              :feedback-only="detail.debate.mode === 'standard_4'"
              :review="reviewStatus"
              :busy="locked || reviewBusy"
              :loading="reviewLoading"
              :can-generate="canGenerateReview && !locked && !uncertain"
              :can-retry="canRetryReview && !locked && !uncertain"
              :locate="locateReviewQuote"
              :repractice="prepareRepractice"
              :can-repractice="canRepractice"
              @generate="review.generate"
              @retry="review.retry"
              @refresh="review.refresh()"
            />
            <ReviewSharing
              v-if="id && detail?.debate.status === 'completed' && detail.debate.mode === 'standard_4' && (focusReport || focusedReportId === id)"
              :key="id"
              :client="auth.client"
              :debate-id="id"
            />
            <RepracticePanel
              v-if="id && detail?.debate.status === 'completed' && !reviewUnavailable"
              :key="id"
              ref="repracticePanel"
              :client="auth.client"
              :debate-id="id"
            />
          </div>
        </article>

        <aside class="neon-rail" aria-label="竞技场辅助">
          <div class="neon-rail-tabs">
            <Button size="small" :class="{ active: showCoach }" :aria-pressed="showCoach" @click="focusView = 'coach'">AI 教练</Button>
            <Button size="small" :class="{ active: focusView === 'graph' }" :aria-pressed="focusView === 'graph'" @click="focusView = 'graph'">交锋图谱</Button>
            <Button size="small" :class="{ active: focusView === 'progress' }" :aria-pressed="focusView === 'progress'" @click="focusView = 'progress'">AI 进度</Button>
          </div>
          <div v-show="showCoach" class="neon-rail-body">
            <div class="neon-annotation-toggle">
              <span id="annotation-toggle-label">显示批注</span>
              <Switch v-model="showAnnotations" aria-labelledby="annotation-toggle-label" data-annotation-toggle size="small" />
            </div>
            <AnnotationPanel
              v-show="showAnnotations"
              ref="annotationPanel"
              data-annotation-panel
              :detail="detail"
              :api="annotationApi"
              :disabled="locked || uncertain"
              @locate="locateAnnotation"
            />
            <details v-if="nextRound !== null" :key="nextRound" class="neon-tactical">
              <summary><span class="hint-title">第 {{ nextRound }} 轮攻防锦囊</span><span>点击展开（防剧透）</span></summary>
              <p v-if="hint">{{ hint.text }} <small>{{ hint.source === 'ai' ? 'AI 提炼' : '本地建议' }}</small></p>
              <p v-else>{{ arenaLoading ? '正在读取锦囊…' : '可先发言，或生成当前轮的提示。' }}</p>
              <Button size="small" :loading="arenaPending === 'hints'" :disabled="locked || uncertain || !!arenaPending" @click="assist('hints')">AI 重新生成</Button>
            </details>
            <StrategyPanel :auth="auth" :detail="detail" :disabled="locked || uncertain" />
          </div>
          <div v-if="focusView === 'graph'" class="neon-rail-body">
            <p v-if="!arena.rounds.length">首轮双方发言完成后，交锋图谱会在这里显示。</p>
            <article v-for="round in arena.rounds" :key="round.round_no" class="graph-round">
              <h4><button type="button" @click="locateArenaMessage(round.user_message_id)">第 {{ round.round_no }} 轮争点</button></h4>
              <button type="button" @click="locateArenaMessage(round.user_message_id)">我方：{{ round.user_point }}</button>
              <button type="button" @click="locateArenaMessage(round.assistant_message_id)">对方：{{ round.assistant_point }}</button>
            </article>
          </div>
          <div v-else-if="focusView === 'progress'" class="neon-rail-body">
            <p>{{ streamStatus }}</p>
            <details>
              <summary>开发者模式</summary>
              <p>浏览器 → 服务端 SSE → 陪练模型</p>
              <p v-if="timing?.first">首段延迟 {{ Math.round(timing.first) }} ms · {{ Math.round(timing.chars * 1000 / Math.max(1, timing.elapsed)) }} 字/秒</p>
              <p v-else>本次尚未收到回复片段。</p>
            </details>
          </div>
        </aside>
      </section>
    </div>
  </template>
</template>

<style scoped>
.panel-notice { color: var(--app-accent); background: var(--app-info-bg); padding: 14px 18px; border-radius: var(--app-radius-control); white-space: pre-wrap; overflow-wrap: anywhere; }
.muted { color: var(--app-text-muted); font-size: 13px; }
.match-complete { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; margin-top: 16px; padding: 12px; color: var(--app-success-text); background: var(--app-success-bg); border-radius: var(--app-radius-panel); }
.turn-block { margin-top: 20px; }
.round-title { font-size: 13px; font-weight: 600; color: var(--app-accent); margin-bottom: 8px; }
time { color: var(--app-text-muted); font-size: 12px; }
.turn-generating { color: var(--app-accent); font-size: 14px; }
.turn-failed { margin: 12px 0; padding: 16px; border: 1px solid var(--app-error-border); border-radius: var(--app-radius-panel); background: var(--app-error-bg); color: var(--app-error-text); }
.turn-failed p { margin: 0 0 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
.stream-preview { margin: 20px 0; padding: 18px; border: 1px dashed var(--app-info-border); border-radius: var(--app-radius-panel); background: var(--app-info-bg); }
.stream-preview h3 { margin-top: 0; }
.stream-text { white-space: pre-wrap; overflow-wrap: anywhere; min-height: 3em; }
.heading-actions { display: flex; flex-wrap: wrap; gap: 12px; }

/* 竞技场首屏沿用 Neon view-arena 的信息层级，业务组件仍由上方模板负责。 */
.neon-arena {
  --neon-ink: #18181b;
  --neon-muted: #766d66;
  --neon-line: #e5e0da;
  --neon-soft-line: #eeeae5;
  --neon-paper: #fff;
  color: var(--neon-ink);
  font-family: Inter, -apple-system, BlinkMacSystemFont, "Microsoft YaHei", sans-serif;
}
.neon-match-card, .neon-stage, .neon-rail {
  border: 1px solid var(--neon-line);
  border-radius: 16px;
  background: var(--neon-paper);
  box-shadow: 0 5px 18px rgba(55, 45, 33, .05);
}
.neon-match-card { padding: 16px; }
.neon-match-top { display: flex; justify-content: space-between; align-items: flex-start; gap: 18px; }
.neon-match-title { min-width: 0; }
.neon-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.neon-tags span { padding: 4px 8px; border: 1px solid var(--neon-line); border-radius: 7px; color: #6a625b; background: #f7f5f2; font-size: 11px; font-weight: 600; }
.neon-tags .gold { color: #82601f; border-color: #ead6a5; background: #fff8e8; }
.neon-tags .pro { color: #a13a30; border-color: #efc8c2; background: #fff3f1; }
.neon-tags .con { color: #275fa5; border-color: #c7d9f1; background: #f0f6ff; }
.neon-match-title h1 { margin: 0; font: 700 21px/1.35 Georgia, serif; overflow-wrap: anywhere; }
.neon-session-meta { margin: 0; }
.neon-session-meta { padding: 5px 14px; }
.neon-match-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 7px; }
.neon-timer-control { display: flex; align-items: center; gap: 6px; padding: 6px 9px; border: 1px solid var(--neon-line); border-radius: 10px; background: #faf9f7; }
.neon-timer-control strong { min-width: 42px; text-align: center; font: 700 13px ui-monospace, monospace; }
.neon-timer-control :deep(.arco-btn) { min-height: 28px; padding: 3px 5px; border: 0; border-radius: 6px; color: #756b64; background: transparent; }
.neon-step-row { display: grid; grid-template-columns: minmax(0, 7fr) minmax(280px, 5fr); gap: 14px; align-items: center; margin-top: 15px; padding-top: 13px; border-top: 1px solid var(--neon-soft-line); }
.neon-stepper { display: grid; grid-auto-flow: column; grid-auto-columns: minmax(0, 1fr); gap: 5px; margin: 0; padding: 0; list-style: none; }
.neon-stepper li { display: flex; align-items: center; justify-content: center; gap: 6px; min-height: 34px; padding: 6px; border: 1px solid var(--neon-line); border-radius: 8px; color: #81776f; background: #faf8f5; font-size: 12px; white-space: nowrap; }
.neon-stepper b { display: grid; place-items: center; width: 17px; height: 17px; border-radius: 50%; background: #eeeae5; font-size: 10px; }
.neon-stepper li.current { color: #fff; border-color: var(--neon-ink); background: var(--neon-ink); font-weight: 700; }
.neon-stepper li.current b { color: var(--neon-ink); background: #fff; }
.neon-stepper li.done { color: #287456; border-color: #c7e6d3; background: #eff9f3; }
.neon-momentum > div:first-child { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 6px; font-size: 11px; }
.neon-momentum strong:first-child { color: #a13a30; }
.neon-momentum strong:last-child { color: #275fa5; }
.neon-momentum span { color: #9a9088; }
.neon-momentum-bar { display: flex; height: 9px; overflow: hidden; border-radius: 999px; background: var(--neon-line); box-shadow: inset 0 1px 2px #18181b20; }
.neon-momentum-bar i:first-child { background: #d24c43; }
.neon-momentum-bar i:last-child { background: #5188cc; }
.neon-workspace { display: grid; grid-template-columns: minmax(0, 8fr) minmax(300px, 4fr); gap: 16px; margin-top: 16px; align-items: start; }
.neon-stage { min-width: 0; min-height: 560px; overflow: hidden; display: flex; flex-direction: column; }
.neon-stage-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; border-bottom: 1px solid var(--neon-soft-line); background: #faf9f7; color: var(--neon-muted); font-size: 11px; }
.live-dot { display: inline-block; width: 7px; height: 7px; margin-right: 6px; border-radius: 50%; background: #18a66f; }
.neon-legend { display: flex; flex-wrap: wrap; gap: 8px; }
.neon-legend span { white-space: nowrap; }
.neon-legend i { display: inline-block; width: 10px; height: 5px; margin-right: 3px; border-radius: 2px; }
.neon-legend i.jade { background: #2d9e78; }
.neon-legend i.red { background: #c9362b; }
.neon-legend i.gold-line { background: #d5a32d; }
.neon-legend i.violet { background: #7c4db2; }
.neon-legend i.blue { background: #316eb8; }
.neon-feed { flex: 1; min-height: 390px; max-height: max(420px, 62vh); overflow: auto; overscroll-behavior: contain; padding: 16px; }
.neon-feed:focus-visible { outline: 2px solid var(--app-accent); outline-offset: -2px; }
.neon-chair { padding: 13px 14px; border: 1px solid #eee5db; border-radius: 10px; color: #55483e; background: #faf7f3; line-height: 1.75; }
.neon-chair b { color: #8d3e2c; }
.neon-empty { display: grid; place-items: center; min-height: 240px; color: #9a9088; text-align: center; }
.neon-empty strong { color: #655b53; font: 700 18px Georgia, serif; }
.neon-empty span { margin-top: 8px; font-size: 13px; }
.neon-arena .message { max-width: 82%; margin-top: 16px; padding: 13px 15px; border: 1px solid #e3d9cf; border-radius: 10px; line-height: 1.8; }
.neon-arena .message[data-stance="pro"] { border-inline-start: 3px solid #c9362b; background: #fff4f2; }
.neon-arena .message[data-stance="con"] { border-inline-start: 3px solid #5188cc; background: #f3f7fd; }
.neon-arena .message.assistant { margin-inline-start: auto; }
.neon-arena .message-label { font-size: 12px; font-weight: 700; }
.neon-arena [data-stance="pro"] .message-label { color: #a13a30; }
.neon-arena [data-stance="con"] .message-label { color: #275fa5; }
.neon-arena .message p { margin: 4px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; }
.neon-arena .message time { display: block; margin-top: 6px; }
.neon-composer { padding: 13px; border-top: 1px solid var(--neon-line); background: #faf9f7; }
.neon-quick-row { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; margin-bottom: 10px; color: #847a72; font-size: 11px; }
.neon-quick-row :deep(.arco-btn) { min-height: 30px; padding: 5px 8px; border: 1px solid var(--neon-line); border-radius: 7px; color: #655b53; background: #fff; font-size: 11px; }
.neon-quick-row .quick-teammate { color: #275fa5; background: #eff6ff; border-color: #c7d9f1; }
.neon-quick-row .quick-strong { color: #287456; background: #eff9f3; border-color: #c7e6d3; }
.neon-quick-row em { padding: 1px 3px; border-radius: 3px; background: #eef5ff; color: #316eb8; font-size: 9px; font-style: normal; }
.separator { width: 1px; height: 18px; margin: 0 2px; background: #d9d2cb; }
.neon-input-row { display: flex; align-items: stretch; gap: 10px; }
.neon-textarea-wrap { position: relative; flex: 1; min-width: 0; }
.neon-textarea-wrap textarea { min-height: 86px; padding: 10px 12px 28px; border: 1px solid #d2cbc4; border-radius: 10px; resize: none; background: #fff; color: #3e3732; font-size: 13px; line-height: 1.7; }
.neon-input-meta { position: absolute; inset: auto 11px 7px; display: flex; justify-content: space-between; color: #9a9088; font-size: 10px; pointer-events: none; }
.neon-input-row .neon-send { align-self: stretch; height: auto; min-width: 122px; border-color: #c9362b; border-radius: 10px; color: #fff; background: #c9362b; font-weight: 700; box-shadow: 0 6px 14px #c9362b2e; }
.neon-rail { min-width: 0; overflow: hidden; }
.neon-rail-tabs { display: grid; grid-template-columns: repeat(3, 1fr); gap: 4px; padding: 5px; border-bottom: 1px solid var(--neon-line); background: #faf9f7; }
.neon-rail-tabs :deep(.arco-btn) { min-height: 35px; border: 0; border-radius: 8px; color: #746a62; background: transparent; font-size: 11px; font-weight: 600; }
.neon-rail-tabs :deep(.arco-btn.active) { color: #4f3b2d; background: #fff; box-shadow: 0 1px 3px #18181b18; }
.neon-rail-body { display: grid; gap: 14px; padding: 15px; }
.neon-tactical { border: 1px solid #ead6a5; border-radius: 10px; overflow: hidden; background: #fff8e8; }
.neon-tactical summary { padding: 11px 12px; color: #514034; cursor: pointer; font-size: 12px; font-weight: 700; }
.neon-tactical summary span { float: right; color: #917448; font-size: 10px; font-weight: 400; }
.neon-tactical p { margin: 0; padding: 0 12px 12px; color: #806646; font-size: 12px; line-height: 1.7; }
.graph-round { padding-top: 8px; border-top: 1px solid var(--neon-soft-line); }
.graph-round button { display: block; width: 100%; text-align: left; overflow-wrap: anywhere; border: 0; background: transparent; padding: 8px 0; }
.framework-options { margin: 12px 0; padding: 10px 12px; border: 1px solid var(--neon-line); border-radius: 10px; }
.framework-options :deep(.arco-btn) { margin-inline-end: 6px; }
@media (max-width: 980px) {
  .neon-workspace { grid-template-columns: 1fr; }
  .neon-rail { position: static; }
  .neon-stage-head { align-items: flex-start; flex-direction: column; }
}
@media (max-width: 720px) {
  .neon-match-top { flex-direction: column; }
  .neon-match-actions { justify-content: flex-start; }
  .neon-step-row { grid-template-columns: 1fr; }
  .neon-stepper { grid-auto-flow: row; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .neon-input-row { flex-direction: column; }
  .neon-send { min-height: 44px; }
  .neon-arena .message { max-width: 100%; }
}

.neon-chair, .neon-rail-body { overflow-wrap: anywhere; }
.neon-chair p { margin: 4px 0 0; white-space: pre-wrap; }
.neon-empty p { margin: 8px 0 0; font-size: 13px; }
.neon-match-actions > .arco-btn { min-height: 34px; }
.neon-annotation-toggle { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 12px; }
.neon-rail-body :deep(.strategy-card) { margin: 0; }
.neon-tactical .hint-title { float: none; color: inherit; font-size: inherit; font-weight: inherit; }
.neon-tactical > .arco-btn { margin: 0 12px 10px; }
.neon-quick-row .quick-flawed { color: #a13a30; background: #fff3f1; border-color: #efc8c2; }
.neon-composer label { display: block; margin: 6px 0; font-size: 13px; font-weight: 600; }
.neon-textarea-wrap textarea { display: block; width: 100%; }
.neon-input-row .neon-send:disabled { opacity: .62; }
.jump-latest { align-self: center; margin: 8px; }
@media (max-width: 720px) {
  .neon-feed { min-height: 265px; max-height: 52vh; }
  .neon-stage { min-height: 0; }
}
</style>
