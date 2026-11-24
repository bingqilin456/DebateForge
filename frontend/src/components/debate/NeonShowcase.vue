<script setup lang="ts">
import { ref } from 'vue'
import { createIssueCardPreviewMock } from '../../preview/issueCard/mockIssueCard'

const { messages } = createIssueCardPreviewMock()
const transcript = ref<HTMLElement | null>(null)
const dimensions = [
  { title: '逻辑严密性', score: 86 },
  { title: '论据充分度', score: 70 },
  { title: '临场反击力', score: 78 },
  { title: '价值升华', score: 72 },
  { title: '表达感染力', score: 82 },
]
const annotations: Record<string, string> = {
  '101': '我方亮点｜提出可检验的评价标准，双方可以围绕同一标准比较。',
  '103': '论据补强｜说明抽样范围后，再举例解释成本怎么估算。',
  '106': '敌方杀招｜区分口径与权重，指出沿用同一口径仍可能得出不同结论。',
}
function showExampleQuote() {
  const quote = transcript.value?.querySelector<HTMLElement>('[data-message-id="101"] details')
  if (quote instanceof HTMLDetailsElement) quote.open = true
  quote?.scrollIntoView?.({ block: 'center' })
  quote?.querySelector('summary')?.focus({ preventScroll: true })
}
</script>

<template>
  <section class="showcase" aria-label="完整复盘示例">
    <header>
      <p>示例复盘</p>
      <h1>如何判断一项评价标准公平？</h1>
    </header>
    <div class="showcase-review">
      <section class="review-card" aria-label="五维示例数据">
        <h2>五维评分 <small>示例数据</small></h2>
        <dl class="dimension-scores">
          <div v-for="dimension in dimensions" :key="dimension.title">
            <dt>{{ dimension.title }}</dt>
            <dd><meter :aria-label="dimension.title" :value="dimension.score" min="0" max="100" /> {{ dimension.score }}</dd>
          </div>
        </dl>
      </section>
      <section class="review-card" aria-label="主要争点和改进建议">
        <h2>这场争在哪</h2>
        <p>双方都希望标准可检验。正方强调先约定标准，反方追问验证成本和权重差异。</p>
        <h3>下一次可以怎么改</h3>
        <p>把「标准应当可检验」写成双方都能核对的条件，并给一个能验证的例子。</p>
        <button class="quote-action" type="button" @click="showExampleQuote">定位示例原句</button>
      </section>
    </div>
    <section ref="transcript" class="showcase-transcript" aria-label="示例原文">
      <h3>四轮发言</h3>
      <article v-for="message in messages" :key="message.id" :data-message-id="message.id" :class="message.role">
        <strong>{{ message.role === 'user' ? '正方' : '反方' }}</strong>
        <p data-message-content tabindex="-1">{{ message.content }}</p>
        <details v-if="annotations[message.id]">
          <summary>查看示例批注</summary>
          <p>{{ annotations[message.id] }}</p>
        </details>
      </article>
    </section>
  </section>
</template>

<style scoped>
.showcase { display: grid; gap: 16px; }
header, .showcase-transcript, .review-card {
  padding: clamp(16px, 3vw, 32px);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-panel);
  background: var(--app-surface);
}
header p { margin: 0; color: var(--app-accent); }
header h1 { margin-bottom: 0; }
.showcase-review { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.review-card h2 { font-family: var(--app-serif); }
.review-card small { color: var(--app-text-muted); font: normal 12px system-ui, sans-serif; }
.dimension-scores > div { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 12px; margin-top: 10px; }
.dimension-scores dd { display: grid; grid-template-columns: minmax(0, 1fr) 3ch; gap: 6px; align-items: center; margin: 0; }
.dimension-scores meter { width: 100%; }
.quote-action { color: var(--app-pro); background: var(--app-pro-pale); border: 1px solid currentColor; border-radius: 8px; padding: 7px 12px; min-height: 44px; }
.showcase-transcript article { border-inline-start: 3px solid #b43b30; padding-inline-start: 12px; }
.showcase-transcript article.assistant { border-color: #2554a1; }
.showcase-transcript p { overflow-wrap: anywhere; }
@media (max-width: 680px) { .showcase-review { grid-template-columns: 1fr; } }
</style>
