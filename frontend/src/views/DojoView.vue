<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { createDojoApi, type DojoFeedback, type DojoQuestion } from '../api/dojo'
import type { ApiClient } from '../api/client'

const props = defineProps<{ client: ApiClient }>()
const emit = defineEmits<{ 'rating-changed': [] }>()
const api = createDojoApi(props.client)
const questions = ref<DojoQuestion[]>([])
const feedback = ref<Record<string, DojoFeedback>>({})
const history = ref<DojoQuestion>()
const technique = ref<Awaited<ReturnType<typeof api.techniques>>>()
const index = ref(0)
const seconds = ref(30)
const busy = ref(false)
const loading = ref(false)
const historyBusy = ref(false)
const techniquesBusy = ref(false)
const error = ref('')
const historyNotice = ref('')
const techniqueNotice = ref('')
const question = computed(() => history.value ?? questions.value[index.value])
const result = computed(() => question.value && feedback.value[question.value.id])
const solved = computed(() => questions.value.filter(item => feedback.value[item.id]?.rewarded).length)
let timer: ReturnType<typeof setInterval> | undefined
let disposed = false

const playbook = [
  { tag: '立论', title: '定义破局法', desc: '先给出可以实际用于判断的定义，再说明它怎样影响本题。', template: '对方将 X 理解为 A。我方建议用 B 衡量 X，因为……' },
  { tag: '攻辩', title: '归谬反问三步法', desc: '接受对方前提，推到一个不合理的结论，再请对方给出区分标准。', template: '如果有风险就必须禁止，那么互联网也该禁止吗？两者的区分标准是什么？' },
  { tag: '交锋', title: '让步反转', desc: '承认对方事实中成立的一部分，然后回到决定输赢的比较。', template: '我方承认有短期成本，但它能够通过制度调整减少。相比之下，对方方案的长期成本是……' },
  { tag: '结辩', title: '战场收敛与价值升华', desc: '收束全场最重要的一至两个分歧，说明为什么己方的取舍更合理。', template: '全场最重要的分歧在于……在这项取舍上，我方的方案更能……' },
]

function startTimer() {
  clearInterval(timer)
  seconds.value = 30
  if (disposed) return
  const deadline = Date.now() + 30_000
  timer = setInterval(() => {
    seconds.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
    if (seconds.value === 0) clearInterval(timer)
  }, 250)
}

function showBuiltin(next: number) {
  history.value = undefined
  index.value = next
  error.value = ''
  startTimer()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const state = await api.state()
    questions.value = state.questions
    feedback.value = Object.fromEntries(state.feedback.map(item => [item.question_id, item]))
    const firstUnsolved = questions.value.findIndex(item => !feedback.value[item.id]?.rewarded)
    showBuiltin(Math.max(0, firstUnsolved))
  } catch {
    error.value = '道场题目加载失败，请重试。'
  } finally {
    loading.value = false
  }
}

async function answer(choice: number) {
  const current = question.value
  if (!current || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const saved = await api.answer(current.id, choice)
    feedback.value[current.id] = saved
    clearInterval(timer)
    if (saved.elo_delta) emit('rating-changed')
  } catch {
    error.value = '答题提交失败，请再试一次。'
  } finally {
    busy.value = false
  }
}

async function loadHistoryQuiz() {
  historyBusy.value = true
  historyNotice.value = ''
  try {
    const item = await api.history()
    historyNotice.value = item.notice ?? ''
    if (item.question) {
      if (item.feedback) feedback.value[item.question.id] = item.feedback
      history.value = item.question
      startTimer()
    } else {
      showBuiltin(index.value)
    }
  } catch {
    historyNotice.value = '暂时无法按历史出题，可以继续练习内置题。'
  } finally {
    historyBusy.value = false
  }
}

async function loadTechnique() {
  techniquesBusy.value = true
  techniqueNotice.value = ''
  try {
    technique.value = await api.techniques()
    techniqueNotice.value = technique.value.notice ?? ''
  } catch {
    techniqueNotice.value = '暂时无法总结个人技法，可以继续查看基础技法。'
  } finally {
    techniquesBusy.value = false
  }
}

onMounted(load)
onUnmounted(() => {
  disposed = true
  clearInterval(timer)
})
</script>

<template>
  <section class="dojo" aria-label="思辨道场">
    <header>
      <h1>思辨道场</h1>
      <p>识别辩论中的逻辑漏洞，练习每轮用得上的攻防技巧。</p>
    </header>
    <div class="dojo-layout">
      <section class="card exercise" aria-labelledby="dojo-quiz-heading">
        <header>
          <div>
            <h2 id="dojo-quiz-heading">30 秒逻辑排雷</h2>
            <p v-if="questions.length">内置题已答对 {{ solved }}/{{ questions.length }} · 每题首次答对 +5 ELO</p>
          </div>
          <span v-if="question" class="timer">{{ seconds ? `${seconds} 秒` : '仍可作答' }}</span>
        </header>
        <div class="dojo-actions">
          <button type="button" :disabled="historyBusy || busy || loading" @click="loadHistoryQuiz">
            {{ historyBusy ? '正在读取历史…' : 'AI 按历史对局出题' }}
          </button>
          <button v-if="history" type="button" :disabled="busy || historyBusy" @click="showBuiltin(index)">回到内置题</button>
        </div>
        <p v-if="historyNotice" role="status">{{ historyNotice }}</p>
        <p v-if="loading" role="status">正在加载内置题…</p>
        <p v-if="error" role="alert">{{ error }}</p>
        <button v-if="error && !question" type="button" @click="load">重新加载</button>
        <template v-if="question">
          <p class="question-source">
            {{ history ? (question.source === 'ai' ? 'AI 历史题' : '历史批注题') : `内置题 ${index + 1}/${questions.length}` }}
          </p>
          <blockquote>{{ question.quote }}</blockquote>
          <p>{{ question.prompt || '找出这段发言中的主要逻辑漏洞。' }}</p>
          <div class="options">
            <button v-for="(option, choice) in question.options" :key="option" type="button"
              :class="{ correct: result?.correct === choice, incorrect: result?.choice === choice && result.correct !== choice }"
              :disabled="busy || historyBusy" @click="answer(choice)">
              {{ 'ABCD'[choice] }}. {{ option }}
            </button>
          </div>
          <div v-if="result" class="answer" role="status">
            <strong>{{ result.correct === result.choice ? '答对了' : '再想一想' }}</strong>
            <span v-if="result.elo_delta"> · +{{ result.elo_delta }} ELO</span>
            <span v-else-if="result.rewarded"> · 本题奖励已领取</span>
            <p>正确选项为 {{ question.options[result.correct] }}。{{ result.explanation }}</p>
          </div>
          <div v-if="!history" class="dojo-actions">
            <button type="button" :disabled="index === 0 || busy || historyBusy" @click="showBuiltin(index - 1)">上一题</button>
            <button type="button" :disabled="index === questions.length - 1 || busy || historyBusy" @click="showBuiltin(index + 1)">下一题</button>
          </div>
        </template>
      </section>
      <section class="card techniques" aria-labelledby="dojo-techniques-heading">
        <h2 id="dojo-techniques-heading">辩论核心技法</h2>
        <details v-for="item in playbook" :key="item.tag">
          <summary><span>{{ item.tag }}</span> {{ item.title }}</summary>
          <p>{{ item.desc }}</p>
          <blockquote>{{ item.template }}</blockquote>
        </details>
        <button type="button" :disabled="techniquesBusy" @click="loadTechnique">
          {{ techniquesBusy ? '正在读取历史…' : 'AI 总结我的薄弱技法' }}
        </button>
        <p v-if="techniqueNotice" role="status">{{ techniqueNotice }}</p>
        <article v-if="technique?.quote" class="answer">
          <h3>{{ technique.title }}</h3>
          <p>{{ technique.source === 'ai' ? 'AI 生成 · 本人辩词' : '历史批注 · 本人辩词' }}</p>
          <blockquote>{{ technique.quote }}</blockquote>
          <p>{{ technique.reason }}</p>
          <p>{{ technique.guide }}</p>
        </article>
      </section>
    </div>
  </section>
</template>

<style scoped>
.dojo { width: 100%; min-width: 0; }
.dojo > header { margin-bottom: 1.25rem; }
.dojo h1, .dojo h2, .dojo h3 { margin: 0; }
.dojo p { margin: .45rem 0; }
.dojo-layout { display: grid; grid-template-columns: minmax(0, 3fr) minmax(0, 2fr); gap: 1rem; align-items: start; }
.exercise, .techniques { display: grid; min-width: 0; gap: .8rem; align-content: start; }
.exercise > header { display: flex; align-items: start; justify-content: space-between; gap: .6rem; }
.question-source, .timer { color: #704324; font-size: .875rem; }
.timer { border: 1px solid #ded1bf; border-radius: 1rem; padding: .2rem .6rem; white-space: nowrap; }
.dojo blockquote { margin: 0; padding: .8rem 1rem; background: #f6f1ea; border-left: 3px solid #ad8055; line-height: 1.65; overflow-wrap: anywhere; }
.options { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .5rem; }
.dojo button { border: 1px solid #d9cabb; border-radius: .45rem; background: #fffdf9; color: #312219; padding: .55rem .8rem; text-align: left; cursor: pointer; }
.dojo button:disabled { opacity: .58; cursor: default; }
.dojo button:focus-visible, .dojo summary:focus-visible { outline: 2px solid #9b382f; outline-offset: 3px; }
.dojo button.correct { border-color: #467754; background: #edf4eb; }
.dojo button.incorrect { border-color: #b04c43; background: #faedeb; }
.dojo-actions { display: flex; flex-wrap: wrap; gap: .5rem; }
.answer, .techniques details { border: 1px solid #e1d7c9; border-radius: .55rem; padding: .65rem .8rem; }
.techniques summary { cursor: pointer; padding-block: .2rem; }
.techniques summary span { color: #8d3e2c; }
.techniques > button { justify-self: start; }
@media (max-width: 740px) { .dojo-layout { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 420px) { .options { grid-template-columns: minmax(0, 1fr); } }
</style>
