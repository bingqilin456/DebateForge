<script setup lang="ts">
import { computed } from 'vue'
import type { IssueCardCitationView } from '../../debate/issueCard'

/**
 * 单条双方原句引用：可定位时渲染为可点击标记，否则只显示待核对的纯文本引用。
 *
 * - 只接受已完成回读校验的投影视图，不在组件内重新计算坐标。
 * - 卡面只放这一处引用，不复制整条发言；点击“定位原文”到发言流中精确查找。
 * - 用户与模型文本一律按纯文本渲染，不使用 v-html。
 */

const props = defineProps<{
  view: IssueCardCitationView
  /** 引用出现在卡片中的位置说明，如“提出原文”“相关回应”。 */
  label: string
  disabled?: boolean
}>()

const emit = defineEmits<{ locate: [key: string] }>()

const verified = computed(() => props.view.status === 'verified')
const accessibleName = computed(() => props.label + '：' + props.view.citation.anchor.quote +
  (verified.value ? '（可定位）' : '（原文待核对）'))
</script>

<template>
  <div class="issue-card-citation" :data-issue-card-citation-status="verified ? 'verified' : 'pending'">
    <p class="citation-head">
      <span class="citation-label">{{ label }}</span>
      <span class="citation-turn">{{ view.citation.speaker === 'user' ? '你' : 'AI' }} · 第 {{ view.citation.turn_no }} 轮</span>
    </p>
    <p class="citation-text">
      <button
        v-if="verified"
        type="button"
        class="citation-mark"
        :data-issue-card-citation="view.key"
        :aria-label="accessibleName"
        :disabled="disabled"
        @click="emit('locate', view.key)"
      >{{ view.citation.anchor.quote }}</button>
      <span v-else class="citation-pending">{{ view.citation.anchor.quote }}</span>
    </p>
    <p class="citation-note">{{ view.citation.note }}</p>
    <p v-if="!verified" class="pending" role="status">
      原文待核对：该引用不唯一、坐标缺失或与保存坐标不一致，因此不高亮，也不猜测位置。
    </p>
  </div>
</template>

<style scoped>
.issue-card-citation { margin: 10px 0; }
.citation-head { margin: 0; display: flex; align-items: baseline; gap: 8px; }
.citation-label { font-size: 12px; font-weight: 600; }
.citation-turn { font-size: 12px; color: var(--app-text-muted); }
.citation-text { margin: 4px 0; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 13px; line-height: 1.7; }
.citation-mark {
  padding: 0 2px; border: 0; background: #eef8ee; color: inherit; font: inherit; cursor: pointer;
  text-decoration: underline; text-underline-offset: 0.2em; text-decoration-thickness: 2px;
}
.citation-mark:focus { outline: 3px solid var(--app-accent); outline-offset: 2px; }
.citation-mark:disabled { cursor: default; }
.citation-pending { border-bottom: 2px dotted var(--app-warning-text, #a86b00); }
.citation-note { margin: 0; font-size: 12px; color: var(--app-text-muted); white-space: pre-wrap; overflow-wrap: anywhere; }
.pending { margin: 4px 0 0; font-size: 12px; color: var(--app-warning-text, #a86b00); }
</style>
