<script setup lang="ts">
import { ref } from 'vue'
import { createIssueCardPreviewMock } from '../../preview/issueCard/mockIssueCard'
import NeonRadarChart from './NeonRadarChart.vue'

const { messages } = createIssueCardPreviewMock()
const transcript = ref<HTMLElement | null>(null)
const dimensions = [
  { title: '逻辑严密性', score: 88 },
  { title: '论据充分度', score: 82 },
  { title: '临场反击力', score: 91 },
  { title: '价值升华', score: 89 },
  { title: '表达感染力', score: 86 },
]
const annotations: Record<string, string> = {
  '101': '我方亮点｜提出可检验的评价标准，双方可以围绕同一标准比较。',
  '103': '论据补强｜说明抽样范围后，再举例解释成本怎么估算。',
  '106': '敌方杀招｜区分口径与权重，指出沿用同一口径仍可能得出不同结论。',
}
const selectedQuote = ref<string | null>(null)
function showExampleQuote() {
  selectedQuote.value = '101'
  const quote = transcript.value?.querySelector<HTMLElement>('[data-message-id="101"] details')
  if (quote instanceof HTMLDetailsElement) quote.open = true
  quote?.scrollIntoView?.({ block: 'center' })
  quote?.querySelector('summary')?.focus({ preventScroll: true })
}
</script>

<template>
  <section class="showcase" aria-label="完整复盘示例">
    <header class="showcase-heading">
      <div>
        <div class="title-row"><h1>赛后深度复盘报告</h1><span>示例复盘</span></div>
        <p>辩题：如何判断一项评价标准公平？</p>
      </div>
      <button class="quote-action" type="button" @click="showExampleQuote">定位示例原句</button>
    </header>
    <div class="showcase-review">
      <section class="review-card overview" aria-label="主要争点和改进建议">
        <h2>这场争在哪</h2>
        <p>双方都希望标准可检验。正方强调先约定标准，反方追问验证成本和权重差异。</p>
        <h3>下一次可以怎么改</h3>
        <p>把「标准应当可检验」写成双方都能核对的条件，并给一个能验证的例子。</p>
      </section>
      <section class="review-card" aria-label="五维示例数据">
        <h2>五维思辨能力雷达图 <small>示例数据</small></h2>
        <NeonRadarChart :dimensions="dimensions" :opponent-scores="[89, 86, 82, 80, 78]" />
        <dl class="dimension-scores">
          <div v-for="dimension in dimensions" :key="dimension.title">
            <dt>{{ dimension.title }}</dt>
            <dd><meter :aria-label="dimension.title" :value="dimension.score" min="0" max="100" /> {{ dimension.score }}</dd>
          </div>
        </dl>
      </section>
    </div>
    <div class="showcase-replay">
    <section ref="transcript" class="showcase-transcript" aria-label="示例原文">
      <h3>四轮全文行内批注复盘</h3>
      <article v-for="message in messages" :key="message.id" :data-message-id="message.id" :class="message.role">
        <strong>{{ message.role === 'user' ? '正方' : '反方' }}</strong>
        <p data-message-content tabindex="-1">{{ message.content }}</p>
        <details v-if="annotations[message.id]" @toggle="($event.target as HTMLDetailsElement).open && (selectedQuote = message.id)">
          <summary>查看示例批注</summary>
          <p>{{ annotations[message.id] }}</p>
        </details>
      </article>
    </section>
    <aside class="review-card critique" aria-label="示例批注诊断">
      <h3>逐句诊断与改写</h3>
      <p v-if="selectedQuote" class="critique-note">{{ annotations[selectedQuote] }}</p>
      <p v-else>展开一条发言下的示例批注，即可在这里查看诊断。</p>
    </aside>
    </div>
  </section>
</template>

<style scoped>
.showcase { display: grid; gap: 20px; }
.showcase-transcript, .review-card {
  min-width: 0;
  padding: clamp(16px, 3vw, 20px);
  border: 1px solid var(--app-border);
  border-radius: 16px;
  background: var(--app-surface);
  box-shadow: 0 5px 18px rgba(55,45,33,.05);
}
.showcase-heading, .title-row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.showcase-heading { justify-content: space-between; }
.showcase-heading p { margin: 4px 0 0; color: var(--app-text-muted); font-size: 14px; }
.showcase-heading h1 { margin: 0; font: 700 24px/1.4 var(--app-serif); }
.title-row span { padding: 3px 9px; border: 1px solid #ead6a5; border-radius: 999px; background: #fff8e8; color: #795a1e; font-size: 12px; font-weight: 600; }
.showcase-review, .showcase-replay { display: grid; gap: 16px; }
.showcase-review { grid-template-columns: minmax(0, 5fr) minmax(0, 7fr); }
.showcase-replay { grid-template-columns: minmax(0, 7fr) minmax(0, 5fr); align-items: start; }
.critique-note { padding: 12px; border-radius: 10px; background: #fff8e8; }
.review-card h2 { font-family: var(--app-serif); }
.review-card small { color: var(--app-text-muted); font: normal 12px system-ui, sans-serif; }
.dimension-scores > div { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 12px; margin-top: 10px; }
.dimension-scores dd { display: grid; grid-template-columns: minmax(0, 1fr) 3ch; gap: 6px; align-items: center; margin: 0; }
.dimension-scores meter { width: 100%; }
.dimension-scores meter::-webkit-meter-bar { height: 7px; border: 0; border-radius: 999px; background: #f0eeeb; }
.dimension-scores meter::-webkit-meter-optimum-value { border-radius: 999px; background: #c9362b; }
.dimension-scores meter::-moz-meter-bar { border-radius: 999px; background: #c9362b; }
.quote-action { color: var(--app-pro); background: var(--app-pro-pale); border: 1px solid currentColor; border-radius: 8px; padding: 7px 12px; min-height: 44px; }
.showcase-transcript article { border-inline-start: 3px solid #b43b30; padding-inline-start: 12px; }
.showcase-transcript article + article { margin-top: 12px; }
.showcase-transcript article.assistant { border-color: #2554a1; }
.showcase-transcript p { overflow-wrap: anywhere; }
@media (max-width: 850px) { .showcase-review, .showcase-replay { grid-template-columns: 1fr; } }
</style>
