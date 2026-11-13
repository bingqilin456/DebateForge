<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Button } from '@arco-design/web-vue'
import type { AuthState } from '../../auth/state'
import { createDebateApi, hasNonWhitespace, isEntityId, type Topic, type Stance } from '../../api/debate'
import { createFirstTurnState, feedback } from '../../debate/firstTurn'
import SummaryPanel from './SummaryPanel.vue'

const props = defineProps<{ auth: AuthState; route: string }>()
const emit = defineEmits<{
  'new-practice': []
  'open-topics': []
  'open-history': []
}>()
const api = createDebateApi(props.auth.client)
const panel = createFirstTurnState(api)
const {
  detail, busy, uncertain, draft, notice, id,
  nextRound, canSend, canRetryTurn, canRetrySummary,
} = panel
const topics = ref<Topic[]>([])
const topicId = ref('')
const stance = ref<Stance>('pro')
const loadingTopics = ref(false)
const topicError = ref('')
const createAttempted = ref(false)
const input = ref('')

const locked = computed(() => busy.value || props.auth.state.pending)
const count = computed(() => [...input.value].length)
const selectedTopic = computed(() => topics.value.find(topic => topic.id === topicId.value))
const initialId = /^#debate=([1-9]\d{0,19})$/.exec(props.route)?.[1]
const invalidRoute = Boolean(props.route && (!initialId || !isEntityId(initialId)))
let active = true

watch(id, value => {
  if (value && active) history.replaceState(null, '', `${location.pathname}${location.search}#debate=${value}`)
})
watch(draft, value => {
  if (value) input.value = value
  else if (detail.value?.turns.length) input.value = ''
})

async function loadTopics() {
  // 登录接受 Session 后组件可能先于认证写操作的 finally 挂载；只读初始化不能被 pending 丢弃。
  if (loadingTopics.value) return
  loadingTopics.value = true; topicError.value = ''
  try {
    const result = await api.topics()
    if (active) { topics.value = result; topicId.value = result[0]?.id ?? '' }
  } catch (error) { if (active) topicError.value = feedback(error) }
  finally { if (active) loadingTopics.value = false }
}

async function create() {
  if (locked.value || !selectedTopic.value) return
  createAttempted.value = true
  await panel.start(topicId.value, stance.value)
}

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

onMounted(() => {
  if (initialId && !invalidRoute) void panel.open(initialId)
  else if (!invalidRoute) void loadTopics()
})
onUnmounted(() => { active = false; panel.dispose() })
</script>

<template>
  <section v-if="invalidRoute" class="card">
    <h2>会话地址无效</h2>
    <Button @click="newPractice">返回选题</Button>
  </section>
  <template v-else>
    <p v-if="notice" class="panel-notice" role="status" aria-live="polite">{{ notice }}</p>
    <section v-if="!id" class="card" :aria-busy="loadingTopics || busy">
      <div class="debate-heading">
        <h2>选题与立场</h2>
        <div class="heading-actions">
          <Button type="outline" data-open-topics :disabled="locked" @click="emit('open-topics')">
            辩题列表视图
          </Button>
          <Button type="outline" data-open-history :disabled="locked" @click="emit('open-history')">
            查看历史会话
          </Button>
        </div>
      </div>
      <p v-if="loadingTopics" role="status">正在读取预设辩题…</p>
      <p v-else-if="topicError" role="alert">{{ topicError }}</p>
      <p v-else-if="!topics.length">暂无可用辩题，请稍后重新读取。</p>
      <Button v-if="!loadingTopics && (topicError || !topics.length)" :disabled="locked" @click="loadTopics">重新读取辩题</Button>
      <form v-if="topics.length" @submit.prevent="create">
        <label for="topic">预设辩题</label>
        <select id="topic" v-model="topicId" name="topic" :disabled="locked">
          <option v-for="topic in topics" :key="topic.id" :value="topic.id">{{ topic.title }}</option>
        </select>
        <p class="topic-description">{{ selectedTopic?.description }}</p>
        <label for="stance">你的立场</label>
        <select id="stance" v-model="stance" name="stance" :disabled="locked">
          <option value="pro">正方</option>
          <option value="con">反方</option>
        </select>
        <p class="muted">AI 将持相反立场，由你先发言，共进行 4 轮标准赛（立论/驳立论/自由辩/结辩）与复盘总结。</p>
        <Button type="primary" html-type="submit" :loading="busy" :disabled="locked || !selectedTopic">
          {{ createAttempted && !busy ? '另建新会话' : '创建会话' }}
        </Button>
      </form>
    </section>

    <section v-else class="card" :aria-busy="busy" aria-label="首轮辩论面板">
      <div class="debate-heading">
        <h2>{{ detail?.debate.topic.title ?? '正在读取会话' }}</h2>
        <span class="muted">{{ detail?.debate.status === 'completed' ? '已完成' : '辩论练习' }}</span>
      </div>
      <p v-if="detail" class="muted">
        你：{{ detail.debate.user_stance === 'pro' ? '正方' : '反方' }} ·
        AI：{{ detail.debate.ai_stance === 'pro' ? '正方' : '反方' }} ·
        赛制：{{ detail.debate.mode === 'standard_4' ? '四轮标准赛' : '三轮快速赛' }} ·
        已完成 {{ detail.debate.completed_rounds }} / {{ detail.debate.total_rounds }} 轮
      </p>
      <p class="muted">会话 {{ id }} · 刷新可恢复当前会话</p>

      <div v-for="turn in detail?.turns" :key="turn.id" class="turn-block" :data-round-no="turn.round_no">
        <div class="round-title">第 {{ turn.round_no }} 轮（尝试 v{{ turn.attempt_version }}）</div>
        <article v-for="message in turn.messages" :key="message.id" :class="['message', message.role]">
          <div class="message-label">{{ message.role === 'user' ? '你的发言' : 'AI 回复 · 仅供练习参考' }}</div>
          <p>{{ message.content }}</p>
          <time :datetime="message.created_at">{{ localTime(message.created_at) }}</time>
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
        <p id="content-help" class="muted">{{ count }} / 2000 个 Unicode 字符；请围绕辩题表达你的论点。</p>
        <Button
          type="primary"
          html-type="submit"
          :disabled="!canSend || props.auth.state.pending || !hasNonWhitespace(input) || count > 2000"
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

      <div class="panel-actions">
        <Button :disabled="locked" @click="panel.refresh">查询状态</Button>
        <Button :disabled="locked" @click="newPractice">开始新的练习</Button>
      </div>
    </section>
  </template>
</template>

<style scoped>
label { display: block; font-weight: 600; margin: 20px 0 10px; }
select, textarea { display: block; width: 100%; border: 1px solid #879aab; border-radius: 8px; background: #fff; color: #172b3a; padding: 10px 12px; }
textarea { resize: vertical; line-height: 1.8; }
.topic-description, .panel-notice { white-space: pre-wrap; overflow-wrap: anywhere; }
.panel-notice { color: #234f72; background: #eaf2f8; padding: 14px 18px; border-radius: 8px; }
.muted { color: #526578; font-size: 13px; }
.debate-heading { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; }
.debate-heading h2 { margin-bottom: 0; }
.turn-block { margin-top: 20px; }
.round-title { font-size: 13px; font-weight: 600; color: #235b81; margin-bottom: 8px; }
.message { margin: 14px 0; padding: 20px; border: 1px solid #d8e0e8; border-radius: 12px; background: #f6f8fa; }
.message.assistant { background: #edf5fa; border-color: #c3d8e6; }
.message-label { font-weight: 600; font-size: 14px; }
.message p { white-space: pre-wrap; overflow-wrap: anywhere; }
time { color: #526578; font-size: 12px; }
.turn-generating { color: #234f72; font-size: 14px; }
.turn-failed { margin: 12px 0; padding: 16px; border: 1px solid #e6b8be; border-radius: 10px; background: #fff5f6; color: #8a2330; }
.turn-failed p { margin: 0 0 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
.heading-actions, .panel-actions { display: flex; flex-wrap: wrap; gap: 12px; }
.panel-actions { margin-top: 24px; }
</style>
