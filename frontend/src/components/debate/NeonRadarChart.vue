<script setup lang="ts">
import { computed } from 'vue'

type Dimension = { title: string; score: number }

const props = withDefaults(defineProps<{
  dimensions: Dimension[]
  opponentScores: number[]
  min?: number
}>(), { min: 50 })

const center = { x: 260, y: 154 }
const radius = 112
const angles = props.dimensions.map((_, index) => index * (2 * Math.PI / props.dimensions.length) - Math.PI / 2)

function point(index: number, score: number) {
  const ratio = Math.max(0, Math.min(1, (score - props.min) / (100 - props.min)))
  return { x: center.x + Math.cos(angles[index]) * radius * ratio, y: center.y + Math.sin(angles[index]) * radius * ratio }
}

function polygon(scores: number[]) {
  return scores.map((score, index) => {
    const p = point(index, score)
    return `${p.x},${p.y}`
  }).join(' ')
}

const rings = computed(() => props.min === 0 ? [20, 40, 60, 80, 100] : [60, 70, 80, 90, 100])
const axes = props.dimensions.map((dimension, index) => {
  const x = center.x + Math.cos(angles[index]) * (radius + 28)
  const y = center.y + Math.sin(angles[index]) * (radius + 28)
  const anchor = x < center.x - 8 ? 'end' : x > center.x + 8 ? 'start' : 'middle'
  return { ...dimension, x, y, anchor }
})
</script>

<template>
  <svg class="neon-radar" viewBox="0 0 520 330" role="img" aria-label="五维思辨能力雷达图">
    <g class="radar-grid">
      <polygon v-for="value in rings" :key="value" :points="polygon(props.dimensions.map(() => value))" class="radar-ring" />
      <line
        v-for="(angle, index) in angles"
        :key="index"
        :x1="center.x"
        :y1="center.y"
        :x2="center.x + Math.cos(angle) * radius"
        :y2="center.y + Math.sin(angle) * radius"
        class="radar-axis"
      />
      <text v-for="value in rings" :key="`scale-${value}`" :x="center.x + 5" :y="center.y - radius * (value - props.min) / (100 - props.min) + 4" class="radar-scale">{{ value }}</text>
    </g>
    <polygon :points="polygon(props.opponentScores)" class="radar-opponent" />
    <polygon :points="polygon(props.dimensions.map(item => item.score))" class="radar-scores" />
    <g class="radar-points">
      <template v-for="(_, index) in props.dimensions" :key="index">
        <circle :cx="point(index, props.dimensions[index].score).x" :cy="point(index, props.dimensions[index].score).y" r="4" class="radar-user-point" />
        <circle :cx="point(index, props.opponentScores[index]).x" :cy="point(index, props.opponentScores[index]).y" r="4" class="radar-ai-point" />
      </template>
    </g>
    <g v-for="axis in axes" :key="axis.title">
      <text :x="axis.x" :y="axis.y" :text-anchor="axis.anchor" class="radar-label">{{ axis.title }}</text>
      <text :x="axis.x" :y="axis.y + 15" :text-anchor="axis.anchor" class="radar-value">{{ axis.score }}</text>
    </g>
    <g class="radar-legend" transform="translate(174 305)">
      <rect width="12" height="12" class="legend-user" />
      <text x="18" y="11">我方表现</text>
      <rect x="92" width="12" height="12" class="legend-ai" />
      <text x="110" y="11">AI 对手</text>
    </g>
  </svg>
</template>
<style scoped>
.neon-radar {
  display: block;
  width: min(100%, 520px);
  height: auto;
  margin-inline: auto;
  overflow: visible;
}

.radar-ring,
.radar-axis {
  fill: none;
  stroke: #e7e5e4;
  stroke-width: 1;
}

.radar-scale,
.radar-value {
  fill: #78716c;
  font: 10px system-ui, sans-serif;
}

.radar-label {
  fill: #18181b;
  font: 600 12px system-ui, sans-serif;
}

.radar-scores {
  fill: #c93b2b29;
  stroke: #c93b2b;
  stroke-width: 2;
}

.radar-opponent {
  fill: #1d4ed81f;
  stroke: #1d4ed8;
  stroke-width: 2;
}

.radar-user-point { fill: #c93b2b; }
.radar-ai-point { fill: #1d4ed8; }

.radar-legend {
  fill: #57534e;
  font: 12px system-ui, sans-serif;
}

.legend-user { fill: #c93b2b; }
.legend-ai { fill: #1d4ed8; }
</style>
