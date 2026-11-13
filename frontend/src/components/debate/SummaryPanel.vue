<script setup lang="ts">
import { Button } from '@arco-design/web-vue'
import type { SummaryStatus } from '../../api/debate'

defineProps<{
  summary: SummaryStatus
  busy?: boolean
  canRetry?: boolean
  canStart?: boolean
}>()

const emit = defineEmits<{
  retry: []
  start: []
}>()
</script>

<template>
  <section class="summary-panel" aria-label="结构化总结面板" data-summary-panel>
    <div class="summary-header">
      <h3>训练复盘总结</h3>
      <span class="muted">AI 生成反馈仅供练习参考，不做胜负裁定</span>
    </div>

    <div v-if="summary.status === 'not_started'" class="summary-state" role="status">
      <p>辩论已完成，正在准备生成结构化总结…</p>
      <Button v-if="canStart" type="outline" data-start-summary :disabled="busy" @click="emit('start')">
        生成结构化总结
      </Button>
    </div>

    <div v-else-if="summary.status === 'generating'" class="summary-state" role="status">
      <p>正在生成结构化总结（当前尝试 v{{ summary.attempt_version }}），请稍候…</p>
    </div>

    <div v-else-if="summary.status === 'failed'" class="summary-failed" role="alert">
      <p>
        总结生成失败（错误码：{{ summary.error_code }} · 当前版本 v{{ summary.attempt_version }}）。本场辩论记录已完整保存，你可以手动重试总结。
      </p>
      <Button
        type="primary"
        data-retry-summary
        :disabled="busy || !canRetry"
        :loading="busy"
        @click="emit('retry')"
      >
        重试生成总结（v{{ summary.attempt_version }}）
      </Button>
    </div>

    <div v-else-if="summary.status === 'completed'" class="summary-grid" data-summary-completed>
      <section class="summary-group" data-summary-group="strengths">
        <h4>论证优点</h4>
        <ul>
          <li v-for="(item, index) in summary.content.strengths" :key="`strength-${index}`">
            <span class="round-badge">第 {{ item.round_no }} 轮</span>
            <strong class="summary-point">{{ item.point }}</strong>
            <p class="summary-reason">{{ item.reason }}</p>
          </li>
        </ul>
      </section>

      <section class="summary-group" data-summary-group="issues">
        <h4>主要问题</h4>
        <ul>
          <li v-for="(item, index) in summary.content.issues" :key="`issue-${index}`">
            <span class="round-badge">第 {{ item.round_no }} 轮</span>
            <strong class="summary-point">{{ item.point }}</strong>
            <p class="summary-reason">{{ item.reason }}</p>
          </li>
        </ul>
      </section>

      <section class="summary-group" data-summary-group="suggestions">
        <h4>改进建议</h4>
        <ul>
          <li v-for="(item, index) in summary.content.suggestions" :key="`suggestion-${index}`">
            <span class="round-badge">第 {{ item.round_no }} 轮</span>
            <strong class="summary-point">{{ item.action }}</strong>
            <p class="summary-reason">{{ item.example }}</p>
          </li>
        </ul>
      </section>
    </div>
  </section>
</template>

<style scoped>
.summary-panel {
  margin-top: 28px;
  padding: 22px;
  border: 1px solid #c9d8e6;
  border-radius: 14px;
  background: #f9fbfd;
}
.summary-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}
.summary-header h3 {
  margin: 0;
  font-size: 18px;
}
.muted {
  color: #526578;
  font-size: 13px;
}
.summary-state p,
.summary-failed p {
  margin: 0 0 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.summary-failed {
  padding: 16px;
  border: 1px solid #e6b8be;
  border-radius: 10px;
  background: #fff5f6;
  color: #8a2330;
}
.summary-grid {
  display: grid;
  gap: 16px;
}
.summary-group {
  padding: 16px;
  border: 1px solid #dce5ee;
  border-radius: 10px;
  background: #fff;
}
.summary-group h4 {
  margin: 0 0 12px;
  font-size: 15px;
  color: #1f4d70;
}
.summary-group ul {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 12px;
}
.summary-group li {
  padding-top: 8px;
  border-top: 1px dashed #e3ebf2;
}
.summary-group li:first-child {
  padding-top: 0;
  border-top: none;
}
.round-badge {
  display: inline-block;
  font-size: 12px;
  font-weight: 600;
  color: #235b81;
  background: #e7f1f8;
  padding: 2px 8px;
  border-radius: 999px;
  margin-right: 8px;
}
.summary-point {
  font-weight: 600;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.summary-reason {
  margin: 6px 0 0;
  color: #34495e;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
