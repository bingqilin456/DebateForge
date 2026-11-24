<script setup lang="ts">
import { USER_CONTENT_MAX_CHARS } from '../../shared/limits'
import { locateAnnotationMarker, locateQuoteText } from '../../shared/locateQuote'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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
const streamStatus = computed(() => streaming.value ? '正在接收 AI 回复' : busy.value ? '正在处理请求' : notice.value || '等待发言')

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
  void annotationPanel.value?.focusFromMarker(keys)
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

    <div v-else class="debate-workspace" :class="{ 'with-annotations': detail && showAnnotations }">
      <section class="card debate-main" :aria-busy="busy" aria-label="首轮辩论面板">
        <div class="debate-heading">
          <h1>{{ detail?.debate.topic.title ?? '正在读取会话' }}</h1>
          <span class="muted">{{ detail?.debate.status === 'completed' ? '已完成' : '辩论练习' }}</span>
        </div>
        <p v-if="arena.practice === true || detail?.debate.practice === true" class="practice-label" data-practice-label>本地练习 · 不计正式积分</p>
        <p v-if="detail" class="muted">
          <span class="stance" :data-stance="detail.debate.user_stance">你 · {{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }}</span>
          <span class="stance" :data-stance="detail.debate.ai_stance">AI · {{ detail.debate.ai_stance === 'pro' ? '正方' : '反方' }}</span>
          <span :data-ai-style="detail.debate.ai_style">流派：{{ AI_STYLE_LABELS[detail.debate.ai_style] }}（{{ detail.debate.ai_style }}）</span> ·
          赛制：{{ detail.debate.mode === 'standard_4' ? '四轮标准赛' : '三轮快速赛' }} ·
          已完成 {{ detail.debate.completed_rounds }} / {{ detail.debate.total_rounds }} 轮
        </p>
        <p class="muted">会话 {{ id }} · 刷新可恢复当前会话</p>
        <p v-if="detail?.debate.topic.description" class="topic-description">{{ detail.debate.topic.description }}</p>

        <ol v-if="roundLabels.length" class="round-track" aria-label="单场轮次">
          <li
            v-for="(label, index) in roundLabels"
            :key="label"
            :class="{ 'round-done': index < (detail?.debate.completed_rounds ?? 0) }"
            :aria-current="index + 1 === nextRound ? 'step' : undefined"
          ><span>{{ index + 1 }}</span>{{ label }}</li>
        </ol>
        <div v-if="detail?.debate.status === 'active'" class="match-clock">
          <strong>{{ timerText }}</strong>
          <Button size="small" :disabled="!secondsLeft" @click="toggleTimer">{{ timerRunning ? '暂停计时' : '开始计时' }}</Button>
          <Button size="small" @click="resetTimer">重置计时</Button>
          <span v-if="timerNotice" class="muted" role="status">{{ timerNotice }}</span>
        </div>

        <details v-if="detail && nextRound !== null" class="arena-hints" :key="nextRound">
          <summary>第 {{ nextRound }} 轮攻防锦囊</summary>
          <p v-if="hint">{{ hint.text }} <small>{{ hint.source === 'ai' ? 'AI 提炼' : '本地建议' }}</small></p>
          <p v-else>{{ arenaLoading ? '正在读取锦囊…' : '可先发言，或生成当前轮的提示。' }}</p>
          <Button size="small" :loading="arenaPending === 'hints'" :disabled="locked || !!arenaPending" @click="assist('hints')">AI 重新生成</Button>
        </details>
        <StrategyPanel v-if="detail" :auth="auth" :detail="detail" :disabled="locked || uncertain" />

        <div v-if="detail" class="annotation-controls">
          <span id="annotation-toggle-label">显示批注</span>
          <Switch v-model="showAnnotations" aria-labelledby="annotation-toggle-label" aria-describedby="annotation-toggle-hint" data-annotation-toggle size="small" />
          <span id="annotation-toggle-hint" class="muted">关闭后隐藏标记和侧栏，批注仍会保存。</span>
        </div>

        <div ref="transcript" class="debate-transcript" tabindex="0" aria-label="交锋记录" @scroll="onTranscriptScroll">
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

          <form v-if="detail && nextRound !== null" data-first-turn @submit.prevent="submit">
            <div class="arena-tools" aria-label="辅助发言">
              <Button size="small" :disabled="!canAdjustDraft || !!arenaPending" :loading="arenaPending === 'teammate'" @click="requestDraft('teammate')">AI 队友代打本轮</Button>
              <Button size="small" :disabled="!canAdjustDraft || !!arenaPending" @click="frameworkDialog = true">三段论框架（AI）</Button>
              <Button size="small" :disabled="!canAdjustDraft" @click="setDraft(sampleArguments.strong)">预设高分辩词</Button>
              <Button size="small" :disabled="!canAdjustDraft" @click="setDraft(sampleArguments.flawed)">预设漏洞辩词</Button>
              <Button size="small" :disabled="!canAdjustDraft && !listening" :aria-pressed="listening" @click="toggleVoice">{{ listening ? '停止语音输入' : '语音输入' }}</Button>
            </div>
            <fieldset v-if="frameworkDialog" class="framework-options">
              <legend>生成当前轮的三段论框架？</legend>
              <p>确认后可能调用已配置的模型。本地框架无需调用模型。</p>
              <Button size="small" @click="frameworkDialog = false">取消</Button>
              <Button size="small" @click="frameworkDialog = false; setDraft(localFramework)">用本地框架</Button>
              <Button size="small" :disabled="!!arenaPending || !canAdjustDraft" @click="requestDraft('framework')">确认生成</Button>
            </fieldset>
            <p v-if="draftNotice || voiceNotice || arenaError" role="status">{{ draftNotice || voiceNotice || arenaError }}</p>
            <label for="first-content">{{ nextRound === 1 ? '第一轮发言' : `第 ${nextRound} 轮发言` }}</label>
            <textarea
              id="first-content"
              v-model="input"
              name="content"
              rows="7"
              :readonly="Boolean(draft)"
              :disabled="locked || uncertain"
              aria-describedby="content-help"
              @keydown.enter="submitShortcut"
            />
            <p id="content-help" class="muted">{{ count }} / {{ USER_CONTENT_MAX_CHARS }} 字 · Ctrl+Enter 发送</p>
            <Button
              type="primary"
              html-type="submit"
              :disabled="!sendable"
              :loading="busy"
            >
              {{ busy ? '正在处理…' : draft ? '重新发送原请求' : detail.debate.mode === 'standard_4' ? '发送交锋' : nextRound === 1 ? '发送首轮' : `发送第 ${nextRound} 轮` }}
            </Button>
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
        <div class="panel-actions">
          <Button :disabled="locked" @click="panel.refresh">查询状态</Button>
          <Button :disabled="locked" @click="newPractice">开始新的练习</Button>
        </div>
      </section>
      <aside v-if="detail" class="arena-assist">
        <section class="card">
          <h3>攻防气势</h3>
          <div class="arena-momentum" aria-label="正反方气势">
            <strong>正方 {{ momentum?.pro_percent ?? 50 }}%</strong>
            <strong>反方 {{ 100 - (momentum?.pro_percent ?? 50) }}%</strong>
            <meter :value="momentum?.pro_percent ?? 50" min="0" max="100" aria-label="正方气势" />
          </div>
          <small>{{ !momentum ? '等待首轮交锋' : momentum.source === 'ai' ? 'AI 提炼 · 仅供参考' : '本地计算 · 仅供练习参考' }}</small>
          <div class="arena-tools" aria-label="竞技场辅助面板">
            <Button size="small" :aria-pressed="focusView === 'coach'" @click="focusView = 'coach'">AI 教练</Button>
            <Button size="small" :aria-pressed="focusView === 'graph'" @click="focusView = 'graph'">交锋图谱</Button>
            <Button size="small" :aria-pressed="focusView === 'progress'" @click="focusView = 'progress'">AI 进度</Button>
          </div>
          <div v-if="focusView === 'graph'">
            <p v-if="!arena.rounds.length">预设背景：{{ detail.debate.topic.description || detail.debate.topic.title }}</p>
            <article v-for="round in arena.rounds" :key="round.round_no" class="graph-round">
              <h4><button type="button" @click="locateArenaMessage(round.user_message_id)">第 {{ round.round_no }} 轮争点</button> <small>{{ (round.graph_source ?? round.source) === 'ai' ? 'AI 提炼' : '本地摘录' }}</small></h4>
              <button type="button" @click="locateArenaMessage(round.user_message_id)">我方：{{ round.user_point }}</button>
              <button type="button" @click="locateArenaMessage(round.assistant_message_id)">对方：{{ round.assistant_point }}</button>
            </article>
          </div>
          <div v-if="focusView === 'progress'">
            <p>{{ streamStatus }}</p>
            <details>
              <summary>开发者模式</summary>
              <p>浏览器 → 服务端 SSE → 陪练模型</p>
              <p v-if="timing?.first">首段延迟 {{ Math.round(timing.first) }} ms · {{ Math.round(timing.chars * 1000 / Math.max(1, timing.elapsed)) }} 字/秒 · 浏览器实测</p>
              <p v-else>本次尚未收到回复片段。</p>
              <p>采样反映本次页面收到的数据，不代表模型服务端耗时。</p>
            </details>
          </div>
        </section>
        <AnnotationPanel
        v-if="detail"
        v-show="showAnnotations && focusView === 'coach'"
        ref="annotationPanel"
        class="debate-annotations"
        :detail="detail"
        :api="annotationApi"
        :disabled="locked || uncertain"
        @locate="locateAnnotation"
        />
      </aside>
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
.debate-heading h1 { margin-block: 0; font-size: clamp(22px, 3vw, 32px); }
.practice-label { color: var(--app-accent); font-weight: 600; }
.stance { display: inline-block; margin-inline-end: 8px; padding: 3px 8px; border-radius: 7px; }
[data-stance="pro"] { --side-accent: var(--app-pro); --side-bg: var(--app-pro-pale); }
[data-stance="con"] { --side-accent: var(--app-con); --side-bg: var(--app-con-pale); }
.stance { color: var(--side-accent); background: var(--side-bg); }
.round-track { display: grid; grid-auto-flow: column; grid-auto-columns: 1fr; gap: 5px; padding: 0; list-style: none; }
.round-track li { display: flex; align-items: center; justify-content: center; gap: 6px; padding: 9px 4px; border: 1px solid var(--app-border); border-radius: 8px; font-size: 13px; color: var(--app-text-muted); background: var(--app-background); }
.round-track li[aria-current="step"] { border-color: var(--app-pro); color: var(--app-pro); font-weight: 700; background: var(--app-pro-pale); }
.round-track li.round-done { color: var(--app-text); background: var(--app-success-bg); }
.match-clock, .match-complete { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.match-clock strong { min-width: 5ch; font: 700 20px ui-monospace, monospace; }
.match-complete { justify-content: space-between; margin-top: 16px; padding: 12px; color: var(--app-success-text); background: var(--app-success-bg); border-radius: var(--app-radius-panel); }
.turn-block { margin-top: 20px; }
.round-title { font-size: 13px; font-weight: 600; color: var(--app-accent); margin-bottom: 8px; }
.message { margin: 14px 0; padding: 20px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.message[data-stance] { background: var(--side-bg); border-inline-start: 3px solid var(--side-accent); }
.message-label { font-weight: 600; font-size: 14px; color: var(--side-accent); }
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
.debate-transcript { max-height: max(240px, 54vh); overflow: auto; overscroll-behavior: contain; padding-inline: 4px; }
.debate-transcript:focus-visible { outline: 2px solid var(--app-accent); outline-offset: 2px; }
.jump-latest { margin: 8px 0; }
.arena-assist { min-width: 0; }
.arena-hints { margin-block: 12px; padding: 12px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); }
.arena-hints summary { cursor: pointer; font-weight: 600; }
.arena-hints p { white-space: pre-wrap; }
.arena-tools { display: flex; gap: 6px; flex-wrap: wrap; margin-block: 8px; }
.arena-momentum { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.arena-momentum meter { width: 100%; }
.framework-options { border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); }
.graph-round button { display: block; text-align: left; max-width: 100%; overflow-wrap: anywhere; border: 0; background: transparent; padding: 8px 0; }
.debate-workspace.with-annotations .debate-main { margin-bottom: 0; }
.annotation-controls { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-top: 16px; }
@media (min-width: 1000px) {
  .debate-workspace.with-annotations {
    grid-template-columns: minmax(0, 1fr) minmax(280px, 30%);
    align-items: start;
  }
}
</style>
