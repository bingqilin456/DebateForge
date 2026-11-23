<script setup lang="ts">
import { USER_CONTENT_MAX_CHARS } from '../../shared/limits'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Button, Switch } from '@arco-design/web-vue'
import { AI_STYLE_LABELS } from '../../debate/styles'
import { hasNonWhitespace, isEntityId } from '../../api/debate'
import { createTrainingApi } from '../../api/training'
import { createAnnotationApi } from '../../api/annotations'
import { createIssueCardApi } from '../../api/issueCard'
import { createReviewApi } from '../../api/review'
import { createReviewState } from '../../debate/review'
import { locateAnnotationMarker, locateQuoteText } from '../../shared/locateQuote'
import type { ReviewQuote } from '../../types/api'
import AnnotatedText from './AnnotatedText.vue'
import AnnotationPanel from './AnnotationPanel.vue'
import IssueCardPanel from './IssueCardPanel.vue'
import ReviewPanel from './ReviewPanel.vue'
import RepracticePanel from './RepracticePanel.vue'
import { createTrainingState, progressText, speakerLabel, stageLabel } from '../../debate/training'
import type { AuthState } from '../../auth/state'

// 产品 V2 训练赛工作台：阶段轨、权威进度、按发言步骤推进、自由辩结束与失败恢复。
// 阶段与回合一律来自服务端 progress，组件本身不推导。

const props = defineProps<{ auth: AuthState; route: string }>()
const emit = defineEmits<{
  'new-practice': []
  'open-history': []
}>()
const api = createTrainingApi(props.auth.client)
const panel = createTrainingState(api)
const annotationApi = createAnnotationApi(props.auth.client)
const issueApi = createIssueCardApi(props.auth.client)
const review = createReviewState(createReviewApi(props.auth.client))
const { status: reviewStatus, loading: reviewLoading, busy: reviewBusy, notice: reviewNotice,
  unavailable: reviewUnavailable, canGenerate: canGenerateReview, canRetry: canRetryReview,
} = review
const {
  detail, busy, uncertain, draft, notice, id, streamText, streaming, streamSpeaker,
  progress, steps, isFreeDebate, isClosed, failedAI, generating,
  canSubmit, canGenerate, canRetryAI, canEndFreeDebate, endBlocked,
} = panel
const input = ref('')
const transcript = ref<HTMLElement | null>(null)
const annotationPanel = ref<InstanceType<typeof AnnotationPanel> | null>(null)
const repracticePanel = ref<InstanceType<typeof RepracticePanel> | null>(null)
const showAnnotations = ref(true)
const issueContents = computed(() => Object.fromEntries(steps.value.map(step => [step.id, step.content])))
const lastAIPosition = computed(() => steps.value.reduce((last, step) => step.speaker === 'assistant' ? step.step_no : last, 0))
const annotationDetail = computed(() => detail.value && { debate: detail.value.debate, turns: [], summary_status: detail.value.summary_status })

function locate(messageId: string, start: number, end: number) {
  return locateQuoteText(transcript.value, messageId, start, end)
}

function locateIssue(key: string) {
  const [messageId, start, end] = key.split('|')
  locate(messageId, Number(start), Number(end))
}

function canRepractice(quote: ReviewQuote | null) {
  return Boolean(quote?.resolved && steps.value.some(step => step.id === quote.message_id && step.speaker === 'user'))
}

function prepareRepractice(improvement: string, quote: ReviewQuote | null) {
  if (!quote || !canRepractice(quote) || quote.start_cp === null || quote.end_cp === null) return
  const located = locate(quote.message_id, quote.start_cp, quote.end_cp)
  if (located?.quote !== quote.quote) return
  repracticePanel.value?.prepare({
    message_id: quote.message_id, start_cp: quote.start_cp, end_cp: quote.end_cp, quote: quote.quote, goal: improvement,
  })
}

watch([id, isClosed], ([debateId, completed]) => {
  if (debateId && completed) void review.open(debateId)
})

const locked = computed(() => busy.value || props.auth.state.pending)
const count = computed(() => [...input.value].length)
const initialId = /^#training=([1-9]\d{0,19})$/.exec(props.route)?.[1]
const invalidRoute = Boolean(props.route && (!initialId || !isEntityId(initialId)))
let active = true

const stages = ['opening', 'rebuttal', 'free_debate', 'closing'] as const
const currentStageIndex = computed(() => (progress.value ? stages.indexOf(progress.value.stage as typeof stages[number]) : -1))

watch(id, value => {
  if (value && active) history.replaceState(null, '', `${location.pathname}${location.search}#training=${value}`)
})
watch(draft, value => {
  if (value) input.value = value
})

async function submit() {
  if (locked.value) return
  await panel.submit(draft.value || input.value)
  if (!draft.value && !uncertain.value) input.value = ''
}
function newPractice() { emit('new-practice') }
function localTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}
function stepTitle(step: { stage: string; speaker: string; free_debate_round: number; step_no: number }) {
  if (step.stage === 'free_debate' && step.free_debate_round > 0) {
    return `${stageLabel(step.stage)}第 ${step.free_debate_round} 回合 · ${speakerLabel(step.speaker)}`
  }
  return `${stageLabel(step.stage)} · ${speakerLabel(step.speaker)}（第 ${step.step_no} 步）`
}

onMounted(() => {
  if (initialId && !invalidRoute) void panel.open(initialId)
})
onUnmounted(() => { active = false; panel.dispose(); review.dispose() })
</script>

<template>
  <section v-if="invalidRoute" class="card">
    <h2>会话地址无效</h2>
    <Button @click="newPractice">返回选题</Button>
  </section>
  <template v-else>
    <p v-if="notice" class="panel-notice" role="status" aria-live="polite">{{ notice }}</p>
    <section v-if="!id" class="card">
      <h2>开始一场完整训练赛</h2>
      <p>请前往辩题列表选择辩题、立场与流派，再选择「完整训练赛」创建。</p>
      <div class="heading-actions">
        <Button type="primary" :disabled="locked" @click="newPractice">开始新的训练赛</Button>
        <Button data-open-history :disabled="locked" @click="emit('open-history')">查看历史会话</Button>
      </div>
    </section>

    <section v-else class="card" :aria-busy="busy" aria-label="训练赛面板">
      <div class="debate-heading">
        <h2>{{ detail?.debate.topic.title ?? '正在读取会话' }}</h2>
        <span class="muted">{{ isClosed ? '已完成' : '完整训练赛' }}</span>
      </div>
      <p v-if="detail" class="muted">
        你：{{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }} ·
        AI：{{ detail.debate.ai_stance === 'pro' ? '正方' : '反方' }} ·
        <span :data-ai-style="detail.debate.ai_style">流派：{{ AI_STYLE_LABELS[detail.debate.ai_style] }}</span>
      </p>
      <p class="muted">会话 {{ id }} · 刷新可恢复当前会话</p>

      <!-- 阶段轨：只显示已完成 / 当前 / 未开始，不用轮号反推 -->
      <ol v-if="progress" class="stage-track" data-stage-track aria-label="训练赛阶段轨">
        <li
          v-for="(stageName, index) in stages"
          :key="stageName"
          :class="['stage-item', { done: index < currentStageIndex, current: index === currentStageIndex }]"
          :data-stage="stageName"
        >
          <span class="stage-name">{{ stageLabel(stageName) }}</span>
          <span class="stage-state">{{ index < currentStageIndex ? '✓ 已完成' : index === currentStageIndex ? (isClosed ? '✓ 已完成' : '进行中') : '未开始' }}</span>
        </li>
      </ol>
      <p v-if="progress" class="progress-status" data-progress-status role="status">
        {{ progressText(progress) }}
        <span v-if="progress.next_speaker">· 轮到{{ speakerLabel(progress.next_speaker) }}发言</span>
      </p>

      <div v-if="detail" class="heading-actions">
        <Switch v-model="showAnnotations" aria-label="显示原文批注" data-training-annotation-toggle />
        <span>显示原文批注</span>
      </div>
      <div ref="transcript" class="step-list">
        <article v-for="step in steps" :key="step.id" class="step-block" :data-step-no="step.step_no" :data-speaker="step.speaker" :data-message-id="step.id">
          <div class="step-title">{{ stepTitle(step) }}</div>
          <p class="step-content" :class="step.speaker" data-message-content tabindex="-1"><AnnotatedText
            :content="step.content" :items="annotationPanel?.messageItems(step.id) ?? []"
            :show-markers="showAnnotations" @select="keys => annotationPanel?.focusFromMarker(keys)"
          /></p>
          <time :datetime="step.created_at">{{ localTime(step.created_at) }}</time>
        </article>
        <p v-if="!steps.length" class="muted">尚未产生正式发言。AI 先发言时由服务端生成第一条。</p>
      </div>

      <section v-if="streaming" class="stream-preview" data-stream-preview aria-label="实时回复预览" :aria-busy="streaming">
        <h3>{{ streamSpeaker === 'assistant' ? 'AI 正在回复' : '正在接收回复' }}</h3>
        <p class="muted">以下为临时片段，尚未确认保存；提交确认后才计入正式步骤与回合数。</p>
        <p class="stream-text" data-stream-text aria-live="polite">{{ streamText || '正在等待首个回复片段…' }}</p>
        <Button data-stop-stream :disabled="props.auth.state.pending" @click="panel.cancelStream">停止等待并查询</Button>
        <p class="muted">断线或停止等待后先查询已保存状态，不自动重新生成。</p>
      </section>

      <!-- AI 步骤：生成或失败恢复 -->
      <div v-if="canGenerate || generating" class="ai-action" data-ai-action>
        <Button type="primary" :disabled="locked || uncertain || !canGenerate" :loading="busy" data-generate-ai @click="panel.generate()">
          {{ generating ? 'AI 正在生成…' : '让 AI 回复本步' }}
        </Button>
        <p class="muted">AI 步骤由服务端生成；流式输出确认提交成功后才会写入正式发言。</p>
      </div>

      <div v-if="failedAI" class="ai-failed" role="alert" data-ai-failed>
        <p>
          第 {{ failedAI.step_no }} 步 AI 回复失败（错误码：{{ failedAI.error_code ?? '未知' }} · 当前版本 v{{ failedAI.attempt_version }}）。
          失败步骤不占编号、不计入正式发言。
        </p>
        <Button type="primary" data-retry-ai :disabled="locked || uncertain || !canRetryAI" :loading="busy" @click="panel.retryAI">
          重试第 {{ failedAI.step_no }} 步（v{{ failedAI.attempt_version }}）
        </Button>
      </div>

      <!-- 用户步骤输入 -->
      <form v-if="detail && canSubmit" data-training-step @submit.prevent="submit">
        <label for="training-content">{{ progress?.stage === 'free_debate' ? `自由辩第 ${(progress?.free_debate_rounds ?? 0) + 1} 回合你的发言` : `${stageLabel(progress?.stage ?? 'opening')}你的发言` }}</label>
        <textarea
          id="training-content"
          v-model="input"
          name="content"
          rows="6"
          :readonly="Boolean(draft)"
          :disabled="locked || uncertain"
          aria-describedby="training-content-help"
        />
        <p id="training-content-help" class="muted">{{ count }} / {{ USER_CONTENT_MAX_CHARS }} 个 Unicode 字符。</p>
        <Button
          type="primary"
          html-type="submit"
          :disabled="locked || uncertain || !canSubmit || !hasNonWhitespace(input) || count > USER_CONTENT_MAX_CHARS"
          :loading="busy"
        >
          {{ busy ? '正在处理…' : draft ? '重新发送原请求' : '发送本步发言' }}
        </Button>
      </form>

      <!-- 自由辩结束：服务端确认的业务操作 -->
      <section v-if="detail && isFreeDebate" class="end-free-debate" data-end-free-debate>
        <Button
          type="primary"
          data-end-free
          :disabled="locked || uncertain || !canEndFreeDebate"
          :loading="busy"
          @click="panel.endFreeDebate"
        >
          {{ failedAI ? '放弃本次 AI 回复并进入结辩' : '进入结辩' }}
        </Button>
        <p v-if="!canEndFreeDebate" class="muted" data-end-blocked>{{ endBlocked }}</p>
        <p v-else class="muted">{{ failedAI ? '本次失败会保留在记录里。' : '你可以继续自由辩，也可以现在进入结辩。' }}</p>
      </section>

      <AnnotationPanel v-if="annotationDetail" v-show="showAnnotations" ref="annotationPanel"
        :detail="annotationDetail" :api="annotationApi" :training-step="lastAIPosition" :disabled="locked"
        @locate="key => locateAnnotationMarker(transcript, key)" />
      <IssueCardPanel v-if="detail" :key="id" training :debate-id="id" :api="issueApi"
        :contents="issueContents" :disabled="locked" @locate="locateIssue" />
      <template v-if="detail && isClosed">
        <p v-if="reviewNotice" role="status">{{ reviewNotice }}</p>
        <ReviewPanel v-if="!reviewUnavailable" :review="reviewStatus" :busy="reviewBusy" :loading="reviewLoading"
          :can-generate="canGenerateReview && !locked" :can-retry="canRetryReview && !locked"
          :locate="locate" :repractice="prepareRepractice" :can-repractice="canRepractice"
          @generate="review.generate" @retry="review.retry" @refresh="review.refresh()" />
        <RepracticePanel ref="repracticePanel" :client="props.auth.client" :debate-id="id" />
      </template>
      <div class="panel-actions">
        <Button :disabled="locked" @click="panel.refresh">查询状态</Button>
        <Button data-open-history :disabled="locked" @click="emit('open-history')">{{ isClosed ? '返回历史会话' : '保存并退出' }}</Button>
        <Button :disabled="locked" @click="newPractice">开始新的训练赛</Button>
      </div>
    </section>
  </template>
</template>

<style scoped>
label { display: block; font-weight: 600; margin: 20px 0 10px; }
textarea { display: block; width: 100%; border: 1px solid var(--app-input-border); border-radius: var(--app-radius-control); background: var(--app-surface); color: var(--app-text); padding: 10px 12px; resize: vertical; line-height: 1.8; }
.panel-notice { color: var(--app-accent); background: var(--app-info-bg); padding: 14px 18px; border-radius: var(--app-radius-control); white-space: pre-wrap; overflow-wrap: anywhere; }
.muted { color: var(--app-text-muted); font-size: 13px; }
.debate-heading { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; }
.debate-heading h2 { margin-bottom: 0; }
.stage-track { display: flex; flex-wrap: wrap; gap: 10px; padding: 0; margin: 20px 0 8px; list-style: none; }
.stage-item { flex: 1 1 140px; display: flex; flex-direction: column; gap: 4px; padding: 12px 14px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); }
.stage-item.done { background: var(--app-success-bg); border-color: var(--app-success-text); }
.stage-item.current { background: var(--app-info-bg); border-color: var(--app-info-border); }
.stage-name { font-weight: 600; }
.stage-state { font-size: 12px; color: var(--app-text-muted); }
.progress-status { font-weight: 600; color: var(--app-accent); }
.step-list { margin-top: 8px; }
.step-block { margin-top: 18px; padding: 18px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.step-title { font-size: 13px; font-weight: 600; color: var(--app-accent); margin-bottom: 8px; }
.step-content { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0 0 8px; }
.step-content.assistant { color: var(--app-text); }
.step-content.user { color: var(--app-text); }
time { color: var(--app-text-muted); font-size: 12px; }
.stream-preview { margin: 20px 0; padding: 18px; border: 1px dashed var(--app-info-border); border-radius: var(--app-radius-panel); background: var(--app-info-bg); }
.stream-preview h3 { margin-top: 0; }
.stream-text { white-space: pre-wrap; overflow-wrap: anywhere; min-height: 3em; }
.ai-action, .end-free-debate { margin: 20px 0; display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
.ai-failed { margin: 20px 0; padding: 16px; border: 1px solid var(--app-error-border); border-radius: var(--app-radius-panel); background: var(--app-error-bg); color: var(--app-error-text); }
.ai-failed p { margin: 0 0 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
.heading-actions, .panel-actions { display: flex; flex-wrap: wrap; gap: 12px; }
.panel-actions { margin-top: 24px; }
</style>
