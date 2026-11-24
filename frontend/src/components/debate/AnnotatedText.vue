<script setup lang="ts">
import { computed } from 'vue'
import type { AnnotationItem } from '../../types/api'
import { ANNOTATION_STYLES, annotationDisplayLabel, annotationEntryKey, buildAnnotationView, type AnnotatedSegment } from '../../debate/annotations'

/**
 * 在正式消息原文上渲染批注标记。
 *
 * - 先按码点回读校验坐标，再判定字素端点；不可信的条目只进入"原文待核对"。
 * - 重叠区间由纯函数切分成无重叠文本段，同段多条批注共用一个入口。
 * - 颜色与线型总是与文字标签同时出现，颜色不单独承载语义。
 * - 这里只渲染这一条消息自己的原文，绝不复制第二份内容到侧栏。
 */

const props = defineProps<{
  content: string
  items: AnnotationItem[]
  /** 关闭场中展示时仍渲染完整原文，只是不产生标记入口。 */
  showMarkers: boolean
}>()

const emit = defineEmits<{
  /** 回传该段全部条目键与元素自身，供父组件做精确定位（不拼 CSS 选择器）。 */
  select: [keys: string[], element: HTMLElement]
}>()

const view = computed(() => buildAnnotationView(props.content, props.items))

function styleOf(item: AnnotationItem) {
  return ANNOTATION_STYLES[item.category]
}

function classes(segment: AnnotatedSegment) {
  return segment.items.map(item => `marker-${styleOf(item).className}`)
}

/** 可访问名称：类别标签是语义载体，颜色与线型只是辅助。 */
function entryLabel(segment: AnnotatedSegment) {
  return `批注：${segment.items.map(annotationDisplayLabel).join('、')}`
}

/** 该段全部条目键；仅用于比较与查找，不进入选择器。 */
function entryKeys(segment: AnnotatedSegment) {
  return segment.items.map(item => annotationEntryKey(item))
}

function select(segment: AnnotatedSegment, event: Event) {
  const element = event.currentTarget as HTMLElement | null
  if (!element) return
  emit('select', entryKeys(segment), element)
}
</script>

<template>
  <span class="annotated-text" :data-annotation-markers="showMarkers ? 'on' : 'off'">
    <template v-for="segment in view.segments" :key="segment.key">
      <span
        v-if="showMarkers && segment.items.length"
        role="button" tabindex="0"
        class="marker"
        :class="classes(segment)"
        :aria-label="entryLabel(segment)"
        :data-annotation-markers-entry="segment.key"
        :data-annotation-keys="entryKeys(segment).join('\u0001')"
        @click="select(segment, $event)"
        @keydown.enter.prevent="select(segment, $event)"
        @keydown.space.prevent
        @keyup.space.prevent="select(segment, $event)"
      >{{ segment.text }}</span>
      <span v-else>{{ segment.text }}</span>
    </template>
  </span>
  <span v-if="showMarkers && view.pending.length" class="pending" role="status">
    原文待核对：{{ view.pending.map(item => item.anchor.quote).join('、') }}（位置不唯一或与保存坐标不一致，因此不高亮）
  </span>
</template>

<style scoped>
.annotated-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.marker {
  cursor: pointer;
  text-decoration-line: underline;
  text-decoration-thickness: 2px;
  text-underline-offset: 0.2em;
  text-decoration-skip-ink: none;
}
.marker:focus { outline: 3px solid var(--app-accent); outline-offset: 2px; }
/* 用文字装饰逐行标注，引用换行时线条仍跟随文字。 */
.marker-strength { background: #eef8ee; text-decoration-color: #4a8f4a; }
.marker-logic-gap { text-decoration-style: wavy; text-decoration-color: #aa5415; }
.marker-support { background: #edf4fb; text-decoration-style: dashed; text-decoration-color: #4a76a8; }
.marker-scope { text-decoration-style: solid; text-decoration-color: #6f4a9c; }
.marker-opponent-weakness { text-decoration-style: wavy; text-decoration-color: #a12736; }
.marker-opponent-strength { text-decoration-style: double; text-decoration-color: #2e4e9e; }
.pending { display: block; margin: 6px 0 0; font-size: 13px; color: var(--app-warning-text, #a86b00); }
</style>
