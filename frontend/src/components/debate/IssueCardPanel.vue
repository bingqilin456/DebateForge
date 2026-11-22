<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { Alert, Button, Input, Modal } from '@arco-design/web-vue'
import { ApiError } from '../../api/client'
import type { IssueCardApi } from '../../api/issueCard'
import type { IssueCardItem, IssueCardList } from '../../types/issueCard'
import {
  ISSUE_CARD_DISPLAY_CAP,
  buildIssueCardViews,
  issueCardAnalysisMessage,
  issueCardMergePreview,
  issueCardRemovePreview,
  issueCardStateMessage,
  issueCardTitleAccessibleName,
  type IssueCardContents,
  type IssueCardCitationView,
} from '../../debate/issueCard'
import IssueCardCitationText from './IssueCardCitationText.vue'

/**
 * 争点卡面板（桌面右栏区块 / 窄屏主卡片下方独立区块）。
 *
 * 职责边界：
 * - 赛后集中整理：只有已完赛四轮场可读取与整理；未完赛与历史 quick_3 明确说明隔离原因。
 * - 默认最多展示 5 张，保持服务端顺序与稳定身份，不在前端重排或新建卡片。
 * - 每张卡展示标题、提出原文、相关回应、仍待澄清，以及回应是否切中的解释；
 *   双方引用各自回读校验，不可信时只显示“待核对”，不猜位置。
 * - 人工改名 / 合并 / 移除是唯一写入口；版本冲突只做弱提示并重新读取，
 *   绝不自动换版本重发，也不覆盖已有数据。
 * - 不自动 POST、不自动重试、不轮询分析；失败只影响提示，不影响已保存卡片。
 */

const props = defineProps<{
  api: IssueCardApi
  debateId: string
  /** message_id → 原文；只用于引用回读校验。 */
  contents: IssueCardContents
  /** 本场是否已完赛四轮；未完赛时不自动请求整理。 */
  completed?: boolean
  disabled?: boolean
  /** 二次确认的可注入边界：默认用 Arco 弹窗，测试注入桩函数以避免真实弹层。 */
  confirm?: (content: string, title: string) => Promise<boolean>
}>()

const emit = defineEmits<{
  locate: [key: string]
  'select-source': [source: { messageId: string; start: number; end: number; quote: string }]
  refreshRequested: []
}>()

const list = ref<IssueCardList | null>(null)
const loading = ref(false)
const loadError = ref('')
const notice = ref('')
const analyzing = ref(false)
const editingKey = ref<string | null>(null)
const renamingKey = ref<string | null>(null)
const renameDraft = ref('')
const mergeSource = ref<string | null>(null)
let active = true
let requestToken = 0

const views = computed(() => list.value ? buildIssueCardViews(list.value, props.contents) : [])
const state = computed(() => list.value?.state ?? 'not_ready')
const analysis = computed(() => list.value?.analysis ?? { status: 'not_started' as const, attempt_version: 0 })
const stateMessage = computed(() => issueCardStateMessage(state.value))
const analysisMessage = computed(() => list.value ? issueCardAnalysisMessage(list.value.analysis) : '')
const canRender = computed(() => state.value === 'ready')
const atCap = computed(() => views.value.length >= ISSUE_CARD_DISPLAY_CAP)
const busy = computed(() => Boolean(props.disabled) || analyzing.value || Boolean(editingKey.value))

/** 读取保存的卡片与人工编辑；无模型调用、无写操作。 */
async function load() {
  if (!active) return
  const token = ++requestToken
  loading.value = true
  loadError.value = ''
  try {
    const result = await props.api.list(props.debateId)
    if (!active || token !== requestToken) return
    list.value = result
    // 会话切换或刷新后不保留任何本地编辑态。
    renamingKey.value = null
    renameDraft.value = ''
    mergeSource.value = null
  } catch (error) {
    if (!active || token !== requestToken) return
    loadError.value = error instanceof ApiError ? error.message : '争点卡读取失败，请手动重试。'
  } finally {
    if (active && token === requestToken) loading.value = false
  }
}

/** 显式整理一次；重复触发只会读回既有结果，不做自动重试。 */
async function analyze() {
  if (busy.value) return
  analyzing.value = true
  notice.value = ''
  try {
    // 未开始是首次整理；已完成/已失败都必须显式 retry 并带版本，避免把重复触发当成重新分析。
    const request = analysis.value.status === 'not_started'
      ? {}
      : { retry: true, expected_attempt_version: analysis.value.attempt_version }
    const result = await props.api.analyze(props.debateId, request)
    if (!active) return
    list.value = result
    if (result.analysis.status === 'generating') notice.value = '争点卡整理中，可以继续查看已有卡片。'
    else if (result.analysis.status === 'failed') notice.value = '本次整理未完成，已有卡片与人工编辑不受影响。'
  } catch (error) {
    if (!active) return
    // 版本冲突或分析失败都只提示并重读，不自行换版本重发、不清空已保存卡片。
    notice.value = error instanceof ApiError ? error.message : '整理失败，请稍后手动重试。'
    await load()
  } finally {
    if (active) analyzing.value = false
  }
}

function beginRename(item: IssueCardItem) {
  if (busy.value) return
  renamingKey.value = item.id
  renameDraft.value = item.title
}

function cancelRename() {
  renamingKey.value = null
  renameDraft.value = ''
}

/** 提交改名：成功后用响应替换该卡，不重排其他卡片。 */
async function submitRename(item: IssueCardItem) {
  const title = renameDraft.value
  if (busy.value) return
  if (!title.trim()) {
    notice.value = '标题不能为空，请修改后再保存。'
    return
  }
  editingKey.value = item.id
  notice.value = ''
  try {
    const updated = await props.api.edit(props.debateId, item.id, {
      expected_version: item.version, action: 'rename', title,
    })
    if (!active) return
    replaceCard(updated)
    renamingKey.value = null
    renameDraft.value = ''
  } catch (error) {
    if (!active) return
    await handleEditError(error, '改名')
  } finally {
    if (active) editingKey.value = null
  }
}

/** 合并：先预览，再按当前版本提交；被合并卡不再单独显示。 */
async function submitMerge(source: IssueCardItem, target: IssueCardItem) {
  if (busy.value || source.id === target.id) return
  const confirmed = await confirmDialog(issueCardMergePreview(source, target), '确认合并卡片')
  if (!confirmed || !active) return
  editingKey.value = source.id
  notice.value = ''
  try {
    await props.api.edit(props.debateId, source.id, {
      expected_version: source.version, action: 'merge', target_card_id: target.id,
    })
    if (!active) return
    await load()
  } catch (error) {
    if (!active) return
    await handleEditError(error, '合并')
  } finally {
    if (active) editingKey.value = null
  }
}

/** 移除：二次确认后提交；移除的卡不因后续整理复活（由服务端保证）。 */
async function submitRemove(item: IssueCardItem) {
  if (busy.value) return
  const confirmed = await confirmDialog(issueCardRemovePreview(item), '确认移除卡片')
  if (!confirmed || !active) return
  editingKey.value = item.id
  notice.value = ''
  try {
    await props.api.edit(props.debateId, item.id, { expected_version: item.version, action: 'remove' })
    if (!active) return
    await load()
  } catch (error) {
    if (!active) return
    await handleEditError(error, '移除')
  } finally {
    if (active) editingKey.value = null
  }
}

/**
 * 编辑失败统一处理：版本冲突与越权都只做弱提示并重读，
 * 不自动换版本重发，也不在本地丢弃已有卡片。
 */
async function handleEditError(error: unknown, action: string) {
  if (error instanceof ApiError) {
    if (error.status === 409) notice.value = `${action}未完成：卡片版本已变化，已重新读取，请确认后再操作。`
    else if (error.status === 404) notice.value = `${action}未完成：卡片不存在或无权访问。`
    else if (error.status === 403) notice.value = `${action}未完成：安全校验失败，请刷新登录状态后重试。`
    else notice.value = `${action}未完成：${error.message}`
  } else {
    notice.value = `${action}未完成，请稍后手动重试。`
  }
  renamingKey.value = null
  renameDraft.value = ''
  await load()
}

function replaceCard(updated: IssueCardItem) {
  if (!list.value) return
  list.value = {
    ...list.value,
    items: list.value.items.map(item => item.id === updated.id ? updated : item),
  }
}

/**
 * 合并目标选择：直接在卡片内展开可选列表，不用嵌套下拉。
 * 只允许并入当前可见的其他活动卡片。
 */
function mergeCandidates(item: IssueCardItem) {
  return views.value.filter(view => view.item.id !== item.id).map(view => view.item)
}

function confirmDialog(content: string, title: string): Promise<boolean> {
  if (props.confirm) return props.confirm(content, title)
  return new Promise(resolve => {
    Modal.confirm({
      title,
      content,
      okText: '确认',
      cancelText: '取消',
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    })
  })
}

/** 点击引用标记：把定位键交给父组件，由它滚动并聚焦发言流中的对应原句。 */
function locate(key: string, view: IssueCardCitationView) {
  emit('locate', key)
  if (view.status !== 'verified' || view.start === null || view.end === null || view.citation.speaker !== 'user') return
  emit('select-source', {
    messageId: view.citation.anchor.message_id,
    start: view.start,
    end: view.end,
    quote: view.citation.anchor.quote,
  })
}

watch(() => [props.debateId, props.contents] as const, () => {
  list.value = null
  notice.value = ''
  void load()
}, { immediate: true })

onUnmounted(() => { active = false; requestToken++ })

defineExpose({ load, views })
</script>

<template>
  <aside class="issue-card-panel" aria-label="争点卡" data-issue-card-panel :aria-busy="loading">
    <header class="panel-heading">
      <h3>争点卡</h3>
      <span v-if="canRender" class="count" data-issue-card-count>{{ views.length }} / {{ ISSUE_CARD_DISPLAY_CAP }}</span>
    </header>

    <p v-if="loading && !list" role="status">正在读取争点卡…</p>
    <Alert v-else-if="loadError" type="error" :title="loadError" role="alert">
      <Button size="small" data-issue-card-reload :disabled="disabled" @click="load">重试读取</Button>
    </Alert>

    <template v-if="!loadError">
      <p v-if="notice" class="notice" role="status" data-issue-card-notice>{{ notice }}</p>

      <p v-if="stateMessage" class="state" role="status" data-issue-card-state>{{ stateMessage }}</p>

      <template v-if="canRender">
        <div class="analysis-row">
          <p v-if="analysisMessage" class="state" role="status" data-issue-card-analysis>{{ analysisMessage }}</p>
          <Button
            v-if="analysis.status === 'not_started' || analysis.status === 'failed'"
            size="small"
            data-issue-card-analyze
            :disabled="busy || completed === false"
            :loading="analyzing"
            @click="analyze"
          >
            {{ analysis.status === 'failed' ? '重试整理争点卡' : '整理争点卡' }}
          </Button>
          <Button
            v-else-if="analysis.status === 'completed'"
            size="small"
            data-issue-card-analyze
            :disabled="busy"
            :loading="analyzing"
            @click="analyze"
          >
            重新整理
          </Button>
        </div>

        <p v-if="atCap" class="state" data-issue-card-cap>默认只展示最相关的 {{ ISSUE_CARD_DISPLAY_CAP }} 张，其余争议不在本视图展开。</p>

        <p v-if="analysis.status === 'completed' && views.length === 0" class="state" role="status">
          本场没有选出主要争点卡，这不代表双方没有分歧。
        </p>

        <ol class="issue-card-list">
          <li v-for="view in views" :key="view.item.id" class="issue-card" :data-issue-card="view.item.id">
            <header class="card-heading">
              <h4 :aria-label="issueCardTitleAccessibleName(view.item)">{{ view.item.title }}</h4>
              <span class="card-version">v{{ view.item.version }}</span>
            </header>

            <p v-if="view.item.title_source === 'user'" class="muted">人工标题</p>

            <!-- 提出原文 / 相关回应 都来自双方已保存发言，各自回读校验。 -->
            <IssueCardCitationText
              :view="view.origin"
              label="提出原文"
              :disabled="busy"
              @locate="locate($event, view.origin)"
            />
            <div v-if="view.responses.length" class="responses">
              <IssueCardCitationText
                v-for="response in view.responses"
                :key="response.key"
                :view="response"
                label="相关回应"
                :disabled="busy"
                @locate="locate($event, response)"
              />
            </div>
            <p v-else class="muted">尚无相关回应记录。</p>

            <p class="explanation"><span class="field">回应是否切中问题</span>{{ view.item.explanation }}</p>

            <section v-if="view.pending.length" class="pending-block">
              <h5>仍待澄清</h5>
              <ul><li v-for="(question, index) in view.pending" :key="`pending-${view.item.id}-${index}`">{{ question }}</li></ul>
            </section>

            <ul v-if="view.notices.length" class="notices" role="status">
              <li v-for="(entry, index) in view.notices" :key="`notice-${view.item.id}-${index}`">{{ entry }}</li>
            </ul>

            <p class="disclaimer">卡片不判定“已驳倒”，引用存在不证明判断正确。</p>

            <div class="card-actions">
              <Button size="small" data-issue-card-rename :disabled="busy" @click="beginRename(view.item)">改名</Button>
              <Button size="small" data-issue-card-merge :disabled="busy || views.length < 2" @click="mergeSource = mergeSource === view.item.id ? null : view.item.id">合并到…</Button>
              <Button size="small" status="danger" data-issue-card-remove :disabled="busy" @click="submitRemove(view.item)">移除</Button>
            </div>

            <form v-if="renamingKey === view.item.id" class="inline-form" data-issue-card-rename-form @submit.prevent="submitRename(view.item)">
              <label :for="`issue-card-title-${view.item.id}`">新标题</label>
              <Input
                :id="`issue-card-title-${view.item.id}`"
                v-model="renameDraft"
                :max-length="80"
                show-word-limit
                :disabled="busy"
              />
              <div class="inline-actions">
                <Button size="small" html-type="submit" type="primary" data-issue-card-rename-save :disabled="busy">保存标题</Button>
                <Button size="small" data-issue-card-rename-cancel :disabled="busy" @click="cancelRename">取消</Button>
              </div>
            </form>

            <div v-if="mergeSource === view.item.id" class="inline-form" data-issue-card-merge-form>
              <p>选择要并入的卡片，合并后本卡不再单独显示，双方原句链接保留在目标卡。</p>
              <div class="inline-actions">
                <Button
                  v-for="target in mergeCandidates(view.item)"
                  :key="`merge-${view.item.id}-${target.id}`"
                  size="small"
                  :data-issue-card-merge-target="target.id"
                  :disabled="busy"
                  @click="submitMerge(view.item, target)"
                >并入「{{ target.title }}」</Button>
                <Button size="small" data-issue-card-merge-cancel :disabled="busy" @click="mergeSource = null">取消</Button>
              </div>
            </div>
          </li>
        </ol>
      </template>
    </template>
  </aside>
</template>

<style scoped>
.issue-card-panel {
  padding: 18px;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-panel);
  background: var(--app-surface);
}
.panel-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.panel-heading h3 { margin: 0 0 12px; font-size: 16px; }
.count { font-size: 12px; color: var(--app-text-muted); }
.state, .notice, .muted { color: var(--app-text-muted); font-size: 13px; }
.notice { color: var(--app-accent); }
.analysis-row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin: 8px 0 12px; }
.issue-card-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 12px; }
.issue-card { padding: 14px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); background: var(--app-background); }
.card-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; }
.card-heading h4 { margin: 0; font-size: 14px; overflow-wrap: anywhere; }
.card-version { font-size: 12px; color: var(--app-text-muted); }
.responses { border-left: 3px solid var(--app-border); padding-left: 10px; }
.explanation { margin: 10px 0; font-size: 13px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
.field { display: block; font-weight: 600; margin-bottom: 2px; }
.pending-block { margin: 10px 0; }
.pending-block h5 { margin: 0 0 4px; font-size: 13px; }
.pending-block ul { margin: 0; padding-left: 18px; font-size: 13px; line-height: 1.7; }
.notices { margin: 10px 0; padding-left: 18px; font-size: 12px; color: var(--app-warning-text, #a86b00); }
.disclaimer { margin: 8px 0; font-size: 12px; color: var(--app-text-muted); }
.card-actions, .inline-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.inline-form { margin-top: 10px; }
.inline-form label { display: block; font-size: 13px; font-weight: 600; margin-bottom: 6px; }
/* 窄屏：面板作为主卡片下的独立区块，不覆盖正文、不做抽屉。 */
@media (max-width: 720px) {
  .issue-card-panel { border-radius: var(--app-radius-panel) var(--app-radius-panel) 0 0; }
}
</style>
