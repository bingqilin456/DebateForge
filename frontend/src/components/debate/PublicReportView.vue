<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ApiError } from '../../api/client'
import { getPublicReport, publicReportMarkdownURL, type PublicReport } from '../../api/publicReports'
import NeonRadarChart from './NeonRadarChart.vue'

const props = defineProps<{ shareId: string }>()
const emit = defineEmits<{ close: [] }>()
const report = ref<PublicReport | null>(null)
const loading = ref(false)
const error = ref('')
const stance = { pro: '正方', con: '反方' }
const side = { user: '我方', assistant: 'AI 对手' }
const outcome = { win: '我方获胜', loss: 'AI 对手获胜', draw: '平局' }
const metrics = ['逻辑严密性', '论据充分度', '临场反击力', '价值升华', '表达感染力']
const radarDimensions = computed(() => {
  const score = report.value?.score
  return score ? metrics.map((title, index) => ({ title, score: score.user[index] ?? 0 })) : []
})
const radarOpponentScores = computed(() => report.value?.score?.assistant ?? [])

watch(() => props.shareId, async (id, _, onCleanup) => {
  let active = true
  onCleanup(() => { active = false })
  report.value = null
  error.value = ''
  loading.value = true
  try {
    const value = await getPublicReport(id)
    if (active) report.value = value
  } catch (cause) {
    if (active) error.value = cause instanceof ApiError && cause.status === 404
      ? '这场复盘未公开或已撤回。' : '复盘暂时无法读取，请稍后重新打开。'
  } finally {
    if (active) loading.value = false
  }
}, { immediate: true })
</script>

<template>
  <section class="public-report" aria-label="公开复盘">
    <button class="back" type="button" @click="emit('close')">返回思弈阁</button>
    <p v-if="loading" role="status">正在读取公开复盘…</p>
    <p v-else-if="error" role="alert">{{ error }}</p>
    <template v-else-if="report">
      <header class="public-header">
        <div>
          <small>公开复盘 · 只读</small>
          <h1>{{ report.title }}</h1>
          <p>我方 · {{ stance[report.user_stance] }}　AI 对手 · {{ stance[report.ai_stance] }}</p>
        </div>
        <a :href="publicReportMarkdownURL(shareId)" download="siyige-review.md">导出 Markdown</a>
      </header>
      <section class="report-block" aria-label="裁决与评语">
        <h2>裁决与评语</h2>
        <template v-if="report.score">
          <div class="report-summary-grid">
            <div>
              <p><strong>{{ outcome[report.score.outcome] }}</strong> · {{ report.score.source === 'ai' ? '模型裁决' : '演示裁决' }} · 我方 {{ report.score.user_total }} / 对手 {{ report.score.assistant_total }}</p>
              <dl class="report-scores">
                <template v-for="(label, index) in metrics" :key="label">
                  <dt>{{ label }}</dt><dd>{{ report.score.user[index] }} / {{ report.score.assistant[index] }}</dd>
                </template>
              </dl>
            </div>
            <NeonRadarChart :dimensions="radarDimensions" :opponent-scores="radarOpponentScores" :min="0" />
          </div>
        </template>
        <p v-else class="muted">暂无已保存的裁决。</p>
        <ul v-if="report.judge_comments.length || report.feedback.length">
          <li v-for="(comment, index) in [...report.judge_comments, ...report.feedback]" :key="index">{{ comment }}</li>
        </ul>
        <p v-else class="muted">暂无已保存的评语。</p>
      </section>
      <div class="report-replay">
      <section class="report-block" aria-label="本场辩词">
        <h2>本场辩词</h2>
        <article v-for="round in report.rounds" :key="round.number" class="report-round">
          <h3>第 {{ round.number }} 轮</h3>
          <div v-for="(speech, index) in round.speeches" :key="index" class="report-speech">
            <strong>{{ side[speech.side] }}</strong>
            <p>{{ speech.content }}</p>
          </div>
        </article>
      </section>
      <section class="report-block" aria-label="本场批注">
        <h2>本场批注</h2>
        <p v-if="!report.annotations.length" class="muted">暂无已保存的批注。</p>
        <article v-for="(note, index) in report.annotations" :key="index" class="report-round">
          <h3>第 {{ note.round }} 轮 · {{ side[note.side] }} · {{ note.label }}</h3>
          <blockquote v-if="note.quote">{{ note.quote }}</blockquote>
          <p>{{ note.reason }}</p>
          <p v-if="note.suggestion">{{ note.suggestion }}</p>
        </article>
      </section>
      </div>
    </template>
  </section>
</template>

<style scoped>
.public-report { margin: 20px auto 42px; }
.report-replay { display: grid; grid-template-columns: minmax(0, 7fr) minmax(0, 5fr); gap: 16px; align-items: start; }
.public-header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 20px; margin: 22px 0; }
.public-header h1 { margin: 5px 0; overflow-wrap: anywhere; }
.public-header p { margin: 5px 0; }
.public-header small, .report-speech strong { color: var(--app-accent); }
.report-block { margin-bottom: 18px; padding: 18px 22px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-surface); overflow-wrap: anywhere; }
.report-block h2 { margin: 0 0 14px; font-size: 19px; }
.report-round + .report-round { margin-top: 16px; padding-top: 12px; border-top: 1px solid var(--app-border); }
.report-round h3 { font-size: 15px; }
.report-speech p { white-space: pre-wrap; margin: 4px 0 12px; }
.report-scores { display: grid; grid-template-columns: minmax(0, max-content) 1fr; gap: 4px 18px; }
.report-scores dd { margin: 0; }
.report-summary-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 1.2fr); gap: 18px; align-items: center; }
blockquote { margin: 6px 0; padding-left: 12px; border-left: 3px solid var(--app-border); }
.back { background: none; border: 0; padding: 0; color: var(--app-accent); cursor: pointer; }
.public-report { font-family: Inter, -apple-system, BlinkMacSystemFont, "Microsoft YaHei", sans-serif; }
.report-block { border-color: #e5e0da; background: #fff; box-shadow: 0 5px 18px rgba(55,45,33,.05); }
.public-header a { padding: 8px 12px; border-radius: 9px; background: #18181b; color: #fff; text-decoration: none; }
@media (max-width: 850px) {
  .report-replay { grid-template-columns: minmax(0, 1fr); gap: 0; }
  .report-summary-grid { grid-template-columns: minmax(0, 1fr); }
}
</style>
