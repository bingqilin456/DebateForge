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
    <header class="dojo-heading">
      <h1>思辨道场 · 逻辑排雷与技法库</h1>
      <p>识别辩论中的高频逻辑谬误，练习攻防句式。</p>
    </header>
    <div class="dojo-layout">
      <section class="exercise" aria-labelledby="dojo-quiz-heading">
        <header class="exercise-heading">
          <div class="exercise-title">
            <span class="exercise-icon" aria-hidden="true">ϟ</span>
            <div>
              <h2 id="dojo-quiz-heading">30 秒逻辑排雷</h2>
              <p>内置题首次答对 +5 ELO</p>
            </div>
          </div>
          <div class="dojo-stats">
            <span v-if="question" class="timer"><span aria-hidden="true">◷</span> {{ seconds ? seconds + ' 秒' : '仍可作答' }}</span>
            <span v-if="questions.length" class="solved">已答对 {{ solved }}/{{ questions.length }}</span>
          </div>
        </header>
        <div class="dojo-actions">
          <button type="button" :disabled="historyBusy || busy || loading" @click="loadHistoryQuiz">
            {{ historyBusy ? '正在读取历史…' : 'AI 按历史对局出题' }}
          </button>
          <button v-if="history" type="button" :disabled="busy || historyBusy" @click="showBuiltin(index)">回到内置题</button>
        </div>
        <p v-if="historyNotice" class="notice" role="status">{{ historyNotice }}</p>
        <p v-if="loading" class="notice" role="status">正在加载内置题…</p>
        <p v-if="error" class="notice error" role="alert">{{ error }}</p>
        <button v-if="error && !question" type="button" @click="load">重新加载</button>
        <template v-if="question">
          <div class="question-excerpt">
            <p class="question-source">
              {{ history ? (question.source === 'ai' ? 'AI 历史题' : '历史批注题') : '内置题 ' + (index + 1) + '/' + questions.length }}
            </p>
            <blockquote>{{ question.quote }}</blockquote>
          </div>
          <p class="question-prompt">{{ question.prompt || '找出这段发言中的主要逻辑漏洞。' }}</p>
          <div class="options">
            <button v-for="(option, choice) in question.options" :key="option" type="button"
              :class="{ correct: result?.correct === choice, incorrect: result?.choice === choice && result.correct !== choice }"
              :disabled="busy || historyBusy" @click="answer(choice)">
              {{ 'ABCD'[choice] }}. {{ option }}
            </button>
          </div>
          <div v-if="result" class="answer" :class="result.correct === result.choice ? 'answer-correct' : 'answer-incorrect'" role="status">
            <strong>{{ result.correct === result.choice ? '答对了' : '再想一想' }}</strong>
            <span v-if="result.elo_delta"> · +{{ result.elo_delta }} ELO</span>
            <span v-else-if="result.rewarded"> · 本题奖励已领取</span>
            <p>正确选项为 {{ question.options[result.correct] }}。{{ result.explanation }}</p>
          </div>
          <div v-if="!history" class="dojo-actions question-navigation">
            <button type="button" :disabled="index === 0 || busy || historyBusy" @click="showBuiltin(index - 1)">上一题</button>
            <button type="button" :disabled="index === questions.length - 1 || busy || historyBusy" @click="showBuiltin(index + 1)">下一题</button>
          </div>
        </template>
      </section>
      <section class="techniques" aria-labelledby="dojo-techniques-heading">
        <header class="techniques-heading">
          <div>
            <h2 id="dojo-techniques-heading"><span class="technique-icon" aria-hidden="true">▤</span> 核心技法库</h2>
            <p>结合你的历史辩词讲解攻防技法。</p>
          </div>
          <button type="button" class="techniques-action" :disabled="techniquesBusy" @click="loadTechnique">
            {{ techniquesBusy ? '正在读取历史…' : 'AI 总结我的薄弱技法' }}
          </button>
        </header>
        <p v-if="techniqueNotice" class="notice" role="status">{{ techniqueNotice }}</p>
        <article v-if="technique?.quote" class="answer">
          <h3>{{ technique.title }}</h3>
          <p>{{ technique.source === 'ai' ? 'AI 生成 · 本人辩词' : '历史批注 · 本人辩词' }}</p>
          <blockquote>{{ technique.quote }}</blockquote>
          <p>{{ technique.reason }}</p>
          <p>{{ technique.guide }}</p>
        </article>
        <div class="technique-list">
          <details v-for="item in playbook" :key="item.tag">
            <summary><span class="technique-tag">{{ item.tag }}</span>{{ item.title }}</summary>
            <div class="technique-content">
              <p>{{ item.desc }}</p>
              <blockquote>{{ item.template }}</blockquote>
            </div>
          </details>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.dojo { color: var(--app-text); min-width: 0; }
.dojo-heading { margin-bottom: 24px; }
.dojo-heading h1 { font: 700 24px/1.35 var(--app-serif); }
.dojo h1, .dojo h2, .dojo h3, .dojo p, .dojo blockquote { margin: 0; }
.dojo .dojo-heading p { margin-top: 4px; color: #57534e; font-size: 14px; }
.dojo-layout { display: grid; gap: 20px; align-items: start; }
.exercise, .techniques {
  display: grid;
  min-width: 0;
  gap: 16px;
  padding: 20px;
  border: 1px solid #e7e5e4;
  border-radius: var(--app-radius-panel);
  background: #fff;
  box-shadow: 0 1px 2px rgb(0 0 0 / 6%);
}
.exercise-heading, .techniques-heading, .exercise-title, .dojo-stats, .dojo-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.exercise-heading, .techniques-heading { justify-content: space-between; flex-wrap: wrap; }
.exercise-heading { padding-bottom: 12px; border-bottom: 1px solid #f5f5f4; }
.exercise-icon {
  display: grid;
  flex: 0 0 32px;
  height: 32px;
  place-items: center;
  border-radius: var(--app-radius-control);
  background: var(--app-pro-pale);
  color: var(--app-pro);
  font-weight: 700;
}
.exercise-heading h2, .techniques h2 { font-size: 14px; line-height: 1.4; }
.exercise-heading p, .techniques-heading p { color: #57534e; font-size: 12px; }
.dojo-stats, .dojo-actions { flex-wrap: wrap; }
.timer, .solved { padding: 4px 10px; border-radius: var(--app-radius-xs); font-size: 12px; font-weight: 600; white-space: nowrap; }
.timer { background: #f5f5f4; color: #44403c; }
.solved { background: #f0fdfa; color: #0f766e; }
.question-excerpt {
  padding: 16px;
  border: 1px solid #e7e5e4;
  border-radius: var(--app-radius-control);
  background: var(--app-background);
  overflow-wrap: anywhere;
}
.question-excerpt .question-source { margin-bottom: 6px; color: #57534e; font-size: 11px; font-weight: 600; }
.question-excerpt blockquote { font: 400 16px/1.65 var(--app-serif); }
.question-prompt { color: #57534e; font-size: 13px; }
.options, .technique-list { display: grid; gap: 10px; }
.dojo button {
  min-width: 0;
  border: 1px solid #e7e5e4;
  border-radius: var(--app-radius-control);
  padding: 8px 10px;
  background: #fff;
  color: var(--app-text);
  font-size: 12px;
  text-align: left;
  overflow-wrap: anywhere;
}
.dojo button:hover:not(:disabled) { border-color: currentColor; }
.dojo button:disabled { opacity: .58; }
.options button { padding: 12px 14px; font-size: 14px; }
.options .correct, .answer-correct { border-color: #99f6e4; background: #f0fdfa; color: #115e59; }
.options .incorrect, .answer-incorrect { border-color: #fecaca; background: var(--app-pro-pale); color: var(--app-pro); }
.dojo-actions button:first-child { border-color: #fecaca; background: var(--app-pro-pale); color: var(--app-pro); }
.question-navigation button:first-child { border-color: #e7e5e4; background: #fff; color: var(--app-text); }
.question-navigation button:last-child { background: #18181b; color: #fff; }
.answer, .notice {
  padding: 12px 14px;
  border: 1px solid #e7e5e4;
  border-radius: var(--app-radius-control);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.answer p, .answer blockquote { margin-top: 6px; }
.answer.answer-correct { border-color: #99f6e4; }
.answer.answer-incorrect { border-color: #fecaca; }
.technique-icon, .technique-tag { color: var(--app-con); }
.techniques .techniques-action {
  border-color: #bfdbfe;
  border-radius: var(--app-radius-xs);
  background: var(--app-con-pale);
  color: var(--app-con);
  font-weight: 600;
}
.techniques article.answer { background: #f0fdfa; border-color: #99f6e4; color: #115e59; }
.techniques details { border: 1px solid #e7e5e4; border-radius: var(--app-radius-control); background: #fafaf9; }
.techniques details[open] { background: #fff; }
.techniques summary {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  list-style: none;
}
.techniques summary::-webkit-details-marker { display: none; }
.techniques summary::after {
  content: '';
  width: 6px;
  height: 6px;
  margin-inline-start: auto;
  border-inline-end: 1px solid #57534e;
  border-block-end: 1px solid #57534e;
  transform: rotate(45deg);
}
.techniques details[open] summary::after { transform: rotate(225deg); }
.technique-tag { flex: none; padding: 2px 8px; border-radius: 4px; background: var(--app-con-pale); font-size: 10px; }
.technique-content { padding: 8px 14px 14px; border-top: 1px solid #f5f5f4; color: #57534e; font-size: 13px; }
.techniques blockquote { padding: 10px; border-radius: var(--app-radius-xs); background: #f5f5f4; font: 400 14px/1.6 var(--app-serif); }
.technique-content blockquote { margin-top: 8px; }
.notice { background: #fafaf9; color: #57534e; }
.notice.error { border-color: var(--app-error-border); background: var(--app-error-bg); color: var(--app-error-text); }
.dojo summary:focus-visible { outline: 2px solid var(--app-accent); outline-offset: 2px; }
@media (min-width: 640px) { .options { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (min-width: 1024px) { .dojo-layout { grid-template-columns: minmax(0, 7fr) minmax(0, 5fr); } }
</style>
