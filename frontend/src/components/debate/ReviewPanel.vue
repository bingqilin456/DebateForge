<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@arco-design/web-vue'
import type { ReviewContent, ReviewDimension, ReviewDimensionKey, ReviewQuote, ReviewStatus } from '../../types/api'

/**
 * P7 完赛复盘（V2-13 复盘 + V2-10 四维 + V2-11 雷达联动，可运行切片）。
 *
 * 边界：
 * - 复盘状态独立于比赛完成状态：失败只影响本面板，不改写会话状态。
 * - 只有用户显式点击才生成或重试；生成中禁用重复触发。
 * - 四维只展示 1–4 档或“观察不足”；观察不足不按 0 画、不补档，雷达对应轴留空。
 * - 雷达只是四维档级的展示形态：缺项时保留文字卡片，文字始终可比图形更完整。
 * - 原文定位、批注跳转与局部重练由上层可选接入；未接入时显示“待整合”，
 *   不伪造点击入口，也不隐藏可读文字。
 */

const props = defineProps<{
  review: ReviewStatus
  busy?: boolean
  canGenerate?: boolean
  canRetry?: boolean
  loading?: boolean
  /** 原文定位入口：未接入时维度卡只显示原文依据，不给可点击定位。 */
  locate?: (messageId: string, startCp: number, endCp: number) => void
  /** 局部重练入口：未接入时显示“待整合”而不是死链。 */
  repractice?: (improvement: string, quote: ReviewQuote | null) => void
}>()

const emit = defineEmits<{
  generate: []
  retry: []
  refresh: []
}>()

const completed = computed(() => props.review.status === 'completed')
// 用显式类型守卫取 content：只有 completed 分支才有内容，避免在模板里反复收窄联合类型。
const content = computed<ReviewContent | null>(() => (props.review.status === 'completed' ? props.review.content : null))
const dimensions = computed<ReviewDimension[]>(() => content.value?.dimensions ?? [])
const hasGaps = computed(() => dimensions.value.some(dimension => dimension.level === null))

// 雷达几何：四轴按服务端固定顺序。观察不足的轴不画点、不连边，更不当作 0。
const CENTER = 90
const RADIUS = 62
function pointAt(index: number, distance: number): { x: number; y: number } {
  const angle = (Math.PI * 2 * index) / 4 - Math.PI / 2
  return { x: CENTER + Math.cos(angle) * distance, y: CENTER + Math.sin(angle) * distance }
}
function levelPoint(index: number, level: 1 | 2 | 3 | 4): { x: number; y: number } {
  return pointAt(index, (RADIUS * level) / 4)
}
const rings = [1, 2, 3, 4].map(level =>
  [0, 1, 2, 3].map(index => levelPoint(index, level as 1 | 2 | 3 | 4)).map(point => `${point.x},${point.y}`).join(' '),
)
const axisLines = [0, 1, 2, 3].map(index => ({ end: levelPoint(index, 4) }))
// 只在相邻两个“已观察”维度之间连边；缺口处断开。
const edges = computed(() => {
  const segments: Array<{ key: string; from: { x: number; y: number }; to: { x: number; y: number } }> = []
  for (let index = 0; index < 4; index++) {
    const current = dimensions.value[index]
    const next = dimensions.value[(index + 1) % 4]
    if (!current || !next || current.level === null || next.level === null) continue
    segments.push({
      key: `${current.key}-${next.key}`,
      from: levelPoint(index, current.level),
      to: levelPoint((index + 1) % 4, next.level),
    })
  }
  return segments
})
const dots = computed(() => {
  const out: Array<{ key: ReviewDimensionKey; point: { x: number; y: number } }> = []
  dimensions.value.forEach((dimension, index) => {
    if (dimension.level === null) return
    out.push({ key: dimension.key, point: levelPoint(index, dimension.level) })
  })
  return out
})
const gaps = computed(() => {
  const out: Array<{ key: ReviewDimensionKey; point: { x: number; y: number } }> = []
  dimensions.value.forEach((dimension, index) => {
    if (dimension.level !== null) return
    out.push({ key: dimension.key, point: levelPoint(index, 4) })
  })
  return out
})

function statusText(): string {
  switch (props.review.status) {
    case 'not_started': return '复盘尚未生成'
    case 'generating': return '复盘生成中'
    case 'failed': return '复盘生成失败'
    default: return '复盘已生成'
  }
}
function levelText(dimension: ReviewDimension): string {
  return dimension.level === null ? '观察不足' : `${dimension.level_label ?? ''} · ${dimension.level} 档`
}
function onLocate(quote: ReviewQuote | null) {
  if (!quote || !quote.resolved || quote.start_cp === null || quote.end_cp === null || !props.locate) return
  props.locate(quote.message_id, quote.start_cp, quote.end_cp)
}
function onRepractice(improvement: string, quote: ReviewQuote | null) {
  props.repractice?.(improvement, quote)
}
</script>

<template>
  <section class="review-panel" aria-label="赛后复盘与四维训练参考" data-review-panel>
    <header class="review-header">
      <h3>赛后复盘 · 单场训练参考</h3>
      <span class="muted">训练参考，不是客观测量；不判胜负、不计总分、不跨场累计。</span>
    </header>

    <div class="review-state" :class="`is-${review.status}`" role="status" data-review-state>
      <div>
        <strong data-review-state-text>{{ statusText() }}</strong>
        <span v-if="review.attempt_version !== null" class="muted"> · 当前版本 v{{ review.attempt_version }}</span>
        <span v-if="review.status === 'failed'" class="state-error" data-review-error-code>（错误码：{{ review.error_code }}）</span>
      </div>
      <div class="state-actions">
        <Button v-if="review.status === 'not_started'" type="primary" data-generate-review
          :disabled="busy || !canGenerate" :loading="busy" @click="emit('generate')">
          生成复盘
        </Button>
        <Button v-if="review.status === 'failed'" type="primary" data-retry-review
          :disabled="busy || !canRetry" :loading="busy" @click="emit('retry')">
          重试生成复盘（v{{ review.attempt_version }}）
        </Button>
        <Button data-refresh-review :disabled="busy || loading" @click="emit('refresh')">刷新复盘状态</Button>
      </div>
      <p v-if="review.status === 'not_started'" class="state-hint">
        复盘单独生成，原文与批注始终可读；是否生成由你手动决定，系统不会自动触发。
      </p>
      <p v-else-if="review.status === 'generating'" class="state-hint">
        正在生成复盘，请稍候；你可以继续查看原文与批注，重复点击不会触发新的生成。
      </p>
      <p v-else-if="review.status === 'failed'" class="state-hint" data-review-failure>
        比赛仍为已完成；复盘失败不影响原文、批注与争点卡。可以手动重试，系统不会自动无限重试。
      </p>
    </div>

    <template v-if="completed">
      <div class="review-body" data-review-completed>
        <section class="radar-block" aria-label="四维雷达图">
          <h4>四维训练参考</h4>
          <svg class="radar" viewBox="0 0 180 180" role="img" data-review-radar
            :aria-label="`四维雷达图：${dimensions.map(dimension => `${dimension.label}${dimension.level === null ? '观察不足' : `第${dimension.level}档`}`).join('，')}`">
            <polygon v-for="(ring, index) in rings" :key="`ring-${index}`" :points="ring" class="radar-ring" />
            <line v-for="(line, index) in axisLines" :key="`axis-${index}`"
              :x1="CENTER" :y1="CENTER" :x2="line.end.x" :y2="line.end.y" class="radar-axis" />
            <line v-for="edge in edges" :key="edge.key"
              :x1="edge.from.x" :y1="edge.from.y" :x2="edge.to.x" :y2="edge.to.y" class="radar-edge" />
            <circle v-for="dot in dots" :key="`dot-${dot.key}`" :cx="dot.point.x" :cy="dot.point.y" r="3.5" class="radar-dot" />
            <circle v-for="gap in gaps" :key="`gap-${gap.key}`" :cx="gap.point.x" :cy="gap.point.y" r="3" class="radar-gap" />
          </svg>
          <ul class="radar-legend" data-review-legend>
            <li v-for="(dimension, index) in dimensions" :key="dimension.key">
              <!-- 图例即键盘入口：已定位的维度提供原生 button；观察不足维度保持空轴，不生成任何可点击入口。 -->
              <button v-if="locate && dimension.level !== null && dimension.quote?.resolved" type="button"
                class="legend-button" :data-legend-locate="dimension.key"
                @click="onLocate(dimension.quote)">
                <span class="legend-axis">轴 {{ index + 1 }}</span>
                <span>{{ dimension.label }}</span>
                <span class="legend-level">{{ levelText(dimension) }}</span>
              </button>
              <span v-else class="legend-static" :data-legend-static="dimension.key">
                <span class="legend-axis">轴 {{ index + 1 }}</span>
                <span>{{ dimension.label }}</span>
                <span :class="['legend-level', { insufficient: dimension.level === null }]">{{ levelText(dimension) }}</span>
              </span>
            </li>
          </ul>
          <p class="muted radar-note">四轴按 1–4 档由内向外；观察不足的维度留空，不按 0 分处理。</p>
        </section>

        <section class="dimension-list" aria-label="四维说明">
          <article v-for="dimension in dimensions" :key="dimension.key" class="dimension-card"
            :class="{ insufficient: dimension.level === null }" :data-dimension="dimension.key">
            <div class="dimension-head">
              <strong>{{ dimension.label }}</strong>
              <span v-if="dimension.level !== null" class="level-badge" :data-level="dimension.level">
                {{ levelText(dimension) }}
              </span>
              <span v-else class="level-badge insufficient" data-level="insufficient">观察不足</span>
            </div>
            <p class="dimension-basis"><span class="field-label">行为依据：</span>{{ dimension.basis }}</p>
            <template v-if="dimension.level === null">
              <p class="dimension-reason"><span class="field-label">为什么观察不足：</span>{{ dimension.insufficient_reason }}</p>
            </template>
            <template v-else>
              <p v-if="dimension.quote" class="dimension-quote">
                <span class="field-label">原文依据：</span>“{{ dimension.quote.quote }}”
              </p>
              <Button v-if="locate && dimension.quote && dimension.quote.resolved" size="mini" type="text"
                :data-locate-quote="dimension.key" @click="onLocate(dimension.quote)">
                定位原句
              </Button>
              <p v-else-if="dimension.quote && !dimension.quote.resolved" class="muted" :data-quote-unresolved="dimension.key">
                原文待核对：引用存在但跨消息有歧义，不按坐标高亮。
              </p>
              <p class="dimension-improve"><span class="field-label">改进提示：</span>{{ dimension.improvement }}</p>
              <Button v-if="repractice" size="mini" type="text" :data-repractice="dimension.key" @click="onRepractice(dimension.improvement, dimension.quote)">
                去重练
              </Button>
              <span v-else class="pending-entry" :data-pending-entry="`repractice-${dimension.key}`">局部重练待整合</span>
            </template>
          </article>
        </section>
      </div>

      <p v-if="hasGaps" class="muted gap-note" data-review-gap-note>
        有维度为“观察不足”：这是本场缺少足够观察依据的正常结果，不是 0 分，也不影响复盘与后续下载入口。
      </p>

      <section class="review-issues" aria-label="主要争点、优点与优先改进">
        <div class="issue-group" data-review-group="key_issues">
          <h4>主要争点</h4>
          <ul>
            <li v-for="(item, index) in content?.key_issues ?? []" :key="`issue-${index}`">
              <span>{{ item.text }}</span>
              <span v-if="item.quote" class="quote-ref">原文：“{{ item.quote.quote }}”</span>
              <span v-else class="pending-entry">原文定位待整合</span>
            </li>
          </ul>
        </div>
        <div class="issue-group" data-review-group="strengths">
          <h4>论证优点</h4>
          <ul>
            <li v-for="(item, index) in content?.strengths ?? []" :key="`strength-${index}`">
              <span>{{ item.text }}</span>
              <span v-if="item.quote" class="quote-ref">原文：“{{ item.quote.quote }}”</span>
            </li>
          </ul>
        </div>
        <div class="issue-group" data-review-group="improvements">
          <h4>优先改进</h4>
          <ul>
            <li v-for="(item, index) in content?.improvements ?? []" :key="`improvement-${index}`">
              <span>{{ item.text }}</span>
              <span v-if="item.quote" class="quote-ref">原文：“{{ item.quote.quote }}”</span>
            </li>
          </ul>
        </div>
      </section>

      <footer class="review-footer">
        <Button disabled data-pdf-pending>下载单场 PDF 战报（V2-14 待整合）</Button>
        <span class="muted">PDF 将复用已保存复盘、不新增模型调用；争点卡与重练接入后再替换对应入口。</span>
      </footer>
    </template>
  </section>
</template>

<style scoped>
.review-panel { margin-top: 28px; padding: 22px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-surface); }
.review-header { display: flex; justify-content: space-between; align-items: baseline; flex-wrap: wrap; gap: 8px; margin-bottom: 14px; }
.review-header h3 { margin: 0; font-size: 18px; }
.muted { color: var(--app-text-muted); font-size: 13px; }
.review-state { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.review-state.is-failed { border-color: var(--app-error-border); background: var(--app-error-bg); color: var(--app-error-text); }
.review-state.is-generating { border-color: var(--app-info-border); background: var(--app-info-bg); }
.state-error { margin-left: 8px; font-size: 13px; }
.state-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.state-hint { width: 100%; margin: 0; font-size: 13px; line-height: 1.7; overflow-wrap: anywhere; }
.review-body { display: grid; gap: 20px; margin-top: 18px; }
.radar-block h4, .review-issues h4 { margin: 0 0 10px; font-size: 15px; color: var(--app-accent); }
.radar { width: 100%; max-width: 260px; display: block; margin: 0 auto; }
.radar-ring { fill: none; stroke: var(--app-border); stroke-width: 1; }
.radar-axis { stroke: var(--app-border); stroke-width: 1; }
.radar-edge { stroke: var(--app-accent); stroke-width: 2; }
.radar-dot { fill: var(--app-accent); }
.radar-gap { fill: none; stroke: var(--app-text-muted); stroke-dasharray: 2 2; }
.radar-legend { list-style: none; margin: 8px 0 0; padding: 0; display: grid; gap: 4px; font-size: 13px; }
.radar-legend li { display: block; }
.legend-button, .legend-static { display: flex; flex-wrap: wrap; gap: 8px; align-items: baseline; width: 100%; text-align: left; }
.legend-button { padding: 4px 6px; border: 1px dashed var(--app-border); border-radius: var(--app-radius-control); background: transparent; color: inherit; }
.legend-button:hover { border-color: var(--app-accent); }
.legend-axis { color: var(--app-text-muted); font-size: 12px; }
.legend-level { margin-left: auto; font-weight: 600; color: var(--app-accent); }
.legend-level.insufficient { color: var(--app-text-muted); font-weight: 400; }
.radar-note { text-align: center; margin: 8px 0 0; }
.dimension-list { display: grid; gap: 12px; }
.dimension-card { padding: 14px 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.dimension-card.insufficient { border-style: dashed; }
.dimension-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
.level-badge { font-size: 12px; font-weight: 600; padding: 2px 8px; border-radius: 999px; background: var(--app-info-bg); color: var(--app-accent); }
.level-badge.insufficient { background: transparent; border: 1px dashed var(--app-border); color: var(--app-text-muted); }
.dimension-basis, .dimension-reason, .dimension-quote, .dimension-improve { margin: 8px 0 0; font-size: 14px; line-height: 1.7; overflow-wrap: anywhere; white-space: pre-wrap; }
.field-label { font-weight: 600; color: var(--app-text-secondary); }
.pending-entry { display: inline-block; margin-top: 6px; font-size: 12px; color: var(--app-text-muted); border-bottom: 1px dashed var(--app-border); }
.gap-note { margin: 12px 0 0; }
.review-issues { display: grid; gap: 14px; margin-top: 18px; }
.issue-group { padding: 14px 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.issue-group ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 10px; }
.issue-group li { display: grid; gap: 4px; font-size: 14px; line-height: 1.7; overflow-wrap: anywhere; }
.quote-ref { color: var(--app-text-secondary); font-size: 13px; }
.review-footer { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-top: 18px; }
@media (min-width: 900px) {
  .review-body { grid-template-columns: minmax(0, 280px) minmax(0, 1fr); align-items: start; }
  .review-issues { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
</style>
