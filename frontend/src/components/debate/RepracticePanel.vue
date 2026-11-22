<script setup lang="ts">
import { computed } from 'vue'
import { Button, Input, Textarea } from '@arco-design/web-vue'
import type { RepracticeSource, RepracticeVersion } from '../../types/api'
import { REPRACTICE_GOAL_MAX_CHARS, REPRACTICE_REWRITE_MAX_CHARS } from '../../shared/limits'

/**
 * V2-08 局部重练面板。
 *
 * 边界（PRD §5.7、AC-V2-17、AC-V2-26）：
 * - 只在完赛后出现；选段必须是本人已保存发言的一段原文，AI 原文与未完成训练不开放。
 * - 打开面板不触发生成；只有用户显式提交才调用模型。
 * - 反馈只展示“是否改善 / 理由 / 可能的新问题”，不评分、不返回整段范文。
 * - 每次改写都是独立版本；历史版本可回看，也可恢复到编辑区继续改写，
 *   恢复本身不覆盖既有版本（再次提交才产生新版本）。
 * - 失败保留改写与选段，可按当前版本显式重试；能力关闭时整个面板隐藏。
 */

const props = defineProps<{
  /** 服务端是否开启该能力；false 时整块不渲染。 */
  available: boolean
  versions: RepracticeVersion[]
  selection: RepracticeSource | null
  goal: string
  rewrite: string
  busy?: boolean
  loading?: boolean
  notice?: string
  failure?: string
  /** 由父组件计算的码点数，避免两侧各算一遍产生口径漂移。 */
  rewriteCount: number
  goalCount: number
  canSubmit: boolean
  /** 恢复来源：仅作为提示，不参与提交内容展示之外的判断。 */
  restoredFrom?: string | null
}>()

const emit = defineEmits<{
  submit: []
  retry: [version: RepracticeVersion]
  restore: [version: RepracticeVersion]
  'clear-selection': []
  locate: [messageId: string, startCp: number, endCp: number]
  'update:goal': [value: string]
  'update:rewrite': [value: string]
}>()

// 历史版本按写入顺序由服务端返回；界面把最新一版放最前，便于先看到最近一次改写。
const ordered = computed(() => [...props.versions].reverse())

function sourceLabel(version: RepracticeVersion): string {
  return `第 ${version.attempt_version} 次尝试`
}

function statusText(version: RepracticeVersion): string {
  switch (version.status) {
    case 'generating': return '反馈生成中'
    case 'failed': return '反馈未生成'
    default: return '反馈已生成'
  }
}

function onLocate(source: RepracticeSource) {
  emit('locate', source.message_id, source.start_cp, source.end_cp)
}
</script>

<template>
  <section v-if="available" class="repractice" aria-label="局部重练" data-repractice-panel>
    <header class="repractice-header">
      <h3>局部重练 · 只改写本人原句</h3>
      <span class="muted">练习不会修改本场原文、比赛状态与复盘；每次改写都是独立版本。</span>
    </header>

    <div class="repractice-source" data-repractice-source>
      <template v-if="selection">
        <p class="source-label">原句</p>
        <p class="source-quote" data-repractice-quote>{{ selection.quote }}</p>
        <p class="muted">第 {{ selection.start_cp }}–{{ selection.end_cp }} 个字符（Unicode 码点）</p>
        <div class="source-actions">
          <Button size="mini" type="text" data-repractice-locate @click="onLocate(selection)">定位原句</Button>
          <Button size="mini" type="text" data-repractice-clear @click="emit('clear-selection')">重新选段</Button>
        </div>
      </template>
      <p v-else class="muted" data-repractice-hint>
        请先在上方本人发言中选中一段原文，再进入重练；也可以从复盘或争点的原文入口直接带入原句。
      </p>
    </div>

    <form class="repractice-form" data-repractice-form @submit.prevent="emit('submit')">
      <label for="repractice-goal">这次想改进什么（选填）</label>
      <Input
        id="repractice-goal"
        :model-value="goal"
        data-repractice-goal
        placeholder="例如：补一句判断依据"
        :disabled="busy"
        :max-length="REPRACTICE_GOAL_MAX_CHARS"
        @update:model-value="(value: string) => emit('update:goal', value)"
      />
      <p class="muted">{{ goalCount }} / {{ REPRACTICE_GOAL_MAX_CHARS }} 字</p>

      <label for="repractice-rewrite">你的改写</label>
      <Textarea
        id="repractice-rewrite"
        :model-value="rewrite"
        data-repractice-rewrite
        :rows="5"
        placeholder="先自己改写这一段，再让系统对照原句给出是否改善的判断。"
        :disabled="busy"
        :max-length="REPRACTICE_REWRITE_MAX_CHARS"
        @update:model-value="(value: string) => emit('update:rewrite', value)"
      />
      <p class="muted">{{ rewriteCount }} / {{ REPRACTICE_REWRITE_MAX_CHARS }} 字</p>
      <p v-if="restoredFrom" class="muted" data-repractice-restored>本版由历史版本 v{{ restoredFrom }} 恢复后继续改写，提交会新建版本。</p>

      <Button
        type="primary"
        html-type="submit"
        data-repractice-submit
        :disabled="!canSubmit"
        :loading="busy"
      >
        {{ busy ? '正在生成反馈…' : '提交改写，查看反馈' }}
      </Button>
    </form>

    <p v-if="notice" class="panel-notice" role="status" aria-live="polite" data-repractice-notice>{{ notice }}</p>
    <p v-if="failure" class="panel-error" role="alert" data-repractice-failure>{{ failure }}</p>

    <section class="repractice-history" aria-label="重练历史版本">
      <h4>历史版本</h4>
      <p v-if="loading" class="muted" data-repractice-loading>正在读取历史版本…</p>
      <p v-else-if="ordered.length === 0" class="muted" data-repractice-empty>还没有重练版本；提交后会在这里保留每一版改写与反馈。</p>
      <article
        v-for="version in ordered"
        :key="version.id"
        class="version"
        :class="`is-${version.status}`"
        :data-repractice-version="version.id"
      >
        <header class="version-head">
          <strong>版本 v{{ version.id }}</strong>
          <span class="muted">{{ sourceLabel(version) }} · {{ statusText(version) }}</span>
          <span v-if="version.restored_from" class="muted" :data-repractice-from="version.restored_from">由 v{{ version.restored_from }} 恢复</span>
        </header>
        <p class="version-quote muted">原句：“{{ version.source.quote }}”</p>
        <p v-if="version.goal" class="version-line">想改善：{{ version.goal }}</p>
        <p class="version-line" :data-repractice-rewrite-text="version.id">改写：{{ version.rewrite }}</p>
        <div v-if="version.status === 'completed'" class="version-feedback" :data-repractice-feedback="version.id">
          <p><span class="field-label">是否改善：</span>{{ version.feedback.improved ? '有改善' : '尚未改善' }}</p>
          <p><span class="field-label">理由：</span>{{ version.feedback.reason }}</p>
          <p v-if="version.feedback.change"><span class="field-label">改变：</span>{{ version.feedback.change }}</p>
          <p v-if="version.feedback.new_problem"><span class="field-label">可能的新问题：</span>{{ version.feedback.new_problem }}</p>
        </div>
        <p v-else-if="version.status === 'failed'" class="version-failed">
          反馈未生成，改写已保留（错误码：{{ version.error_code }}）。
        </p>
        <p v-else class="muted">反馈仍在生成；刷新页面后可按已保存状态恢复。</p>
        <div class="version-actions">
          <Button
            v-if="version.status === 'failed'"
            size="mini"
            type="primary"
            :data-repractice-retry="version.id"
            :disabled="busy"
            :loading="busy"
            @click="emit('retry', version)"
          >
            重试这次反馈（v{{ version.attempt_version }}）
          </Button>
          <Button
            size="mini"
            type="text"
            :data-repractice-restore="version.id"
            :disabled="busy"
            @click="emit('restore', version)"
          >
            恢复到编辑区
          </Button>
          <Button size="mini" type="text" :disabled="busy" @click="onLocate(version.source)">定位原句</Button>
        </div>
      </article>
    </section>
  </section>
</template>

<style scoped>
.repractice { margin-top: 28px; padding: 22px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-surface); }
.repractice-header { display: flex; justify-content: space-between; align-items: baseline; flex-wrap: wrap; gap: 8px; margin-bottom: 14px; }
.repractice-header h3 { margin: 0; font-size: 18px; }
.muted { color: var(--app-text-muted); font-size: 13px; }
.repractice-source { padding: 14px 16px; border: 1px dashed var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); }
.source-label { margin: 0; font-size: 12px; font-weight: 600; color: var(--app-text-secondary); }
.source-quote { margin: 6px 0; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 15px; }
.source-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 6px; }
.repractice-form { margin-top: 18px; display: grid; gap: 8px; }
.repractice-form label { font-weight: 600; margin-top: 6px; }
.repractice-form .muted { margin: 0; }
.panel-notice { color: var(--app-accent); background: var(--app-info-bg); padding: 12px 16px; border-radius: var(--app-radius-control); white-space: pre-wrap; overflow-wrap: anywhere; }
.panel-error { color: var(--app-error-text); background: var(--app-error-bg); padding: 12px 16px; border-radius: var(--app-radius-control); white-space: pre-wrap; overflow-wrap: anywhere; }
.repractice-history { margin-top: 22px; }
.repractice-history h4 { margin: 0 0 10px; font-size: 15px; color: var(--app-accent); }
.version { padding: 14px 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-background); margin-bottom: 12px; }
.version.is-failed { border-color: var(--app-error-border); background: var(--app-error-bg); }
.version-head { display: flex; flex-wrap: wrap; align-items: baseline; gap: 10px; }
.version-quote, .version-line { margin: 8px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 14px; line-height: 1.7; }
.version-feedback { margin-top: 8px; display: grid; gap: 4px; font-size: 14px; line-height: 1.7; overflow-wrap: anywhere; white-space: pre-wrap; }
.version-feedback p { margin: 0; }
.field-label { font-weight: 600; color: var(--app-text-secondary); }
.version-failed { margin: 8px 0 0; font-size: 14px; color: var(--app-error-text); }
.version-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px; }
</style>
