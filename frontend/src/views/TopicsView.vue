<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { Alert, Button } from '@arco-design/web-vue'
import { authState, type AuthState } from '../auth/state'
import { createDebate, listTopics } from '../api/debate'
import { createTrainingApi } from '../api/training'
import { ApiError } from '../api/client'
import { AI_STYLE_LABELS, DEFAULT_AI_STYLE } from '../debate/styles'
import { CUSTOM_TOPIC_BACKGROUND_MAX_CHARS, CUSTOM_TOPIC_TITLE_MAX_CHARS } from '../shared/limits'
import type { AIStyle, Debate, DebateTopic, Stance, Topic } from '../types/api'

export interface TopicsViewProps {
  auth?: AuthState
  disabled?: boolean
}
const props = withDefaults(defineProps<TopicsViewProps>(), { disabled: false })
const emit = defineEmits<{
  /** 创建辩论会话成功后触发，交由顶层视图切换至辩论主视图并加载该会话 */
  created: [debate: Debate]
  creating: [value: boolean]
  uncertain: [value: boolean]
  /** 切换至本人历史会话视图（T11） */
  'open-history': []
  /** 返回当前辩论主视图 */
  'back-to-debate': []
}>()

const currentAuth = computed(() => props.auth ?? authState)
const authenticated = computed(() => currentAuth.value.state.status === 'authenticated' && !!currentAuth.value.state.user)
const blocked = computed(() => props.disabled || currentAuth.value.state.pending || !authenticated.value)
const topics = ref<Topic[]>([])
const selectedTopicId = ref('')
// 选题来源：预设与自定义严格二选一，与后端请求形状一致。
const source = ref<'preset' | 'custom'>('preset')
const customTitle = ref('')
const customBackground = ref('')
const stance = ref<Stance>('pro')
// 练习模式：轻量练习沿用 V1 四轮会话，完整训练赛使用 V2 权威阶段模型。
// 两者都明确展示，默认轻量练习；训练赛没有解锁条件。
const practiceMode = ref<'standard_4' | 'training_v2'>('standard_4')
const MODE_LABELS: Record<'standard_4' | 'training_v2', { title: string; detail: string }> = {
  standard_4: { title: '四轮轻量练习', detail: '4 轮 · 8 条正式发言 · 批注/争点/重练' },
  training_v2: { title: '完整训练赛', detail: '四阶段 · 自由辩 1–10 回合可主动结束' },
}
const aiStyle = ref<AIStyle>(DEFAULT_AI_STYLE)
const loading = ref(false)
const loaded = ref(false)
const creating = ref(false)
const loadError = ref('')
const createError = ref('')
const createdDebate = ref<Debate | null>(null)
const creationUncertain = ref(false)
const id = useId()
const formDisabled = computed(() => blocked.value || loading.value || creating.value || !!createdDebate.value || creationUncertain.value)
const navigationDisabled = computed(() => blocked.value || creating.value || !!createdDebate.value)

// 计数按解码后的 Unicode 码点，与后端一致：补充平面字符算一个，组合字符算多个。
// 不使用 .length（UTF-16 码元），也不使用会截断输入的 maxlength。
const titleCount = computed(() => [...customTitle.value].length)
const backgroundCount = computed(() => [...customBackground.value].length)
const titleBlank = computed(() => !/[^\p{White_Space}\u{FEFF}]/u.test(customTitle.value))
const titleInvalid = computed(() => titleBlank.value || titleCount.value > CUSTOM_TOPIC_TITLE_MAX_CHARS)
const backgroundInvalid = computed(() => backgroundCount.value > CUSTOM_TOPIC_BACKGROUND_MAX_CHARS)
const customInvalid = computed(() => titleInvalid.value || backgroundInvalid.value)
const canSubmit = computed(() => {
  if (formDisabled.value) return false
  if (source.value === 'custom') return practiceMode.value === 'standard_4' && !customInvalid.value
  return !!selectedTopicId.value && topics.value.some(topic => topic.id === selectedTopicId.value)
})
watch(creating, value => emit('creating', value), { flush: 'sync' })
watch(creationUncertain, value => emit('uncertain', value), { flush: 'sync' })
let version = 0
let disposed = false
function current(request: number) { return !disposed && request === version }

async function loadTopics() {
  if (formDisabled.value) return
  const request = ++version
  loading.value = true
  loadError.value = ''
  createError.value = ''
  try {
    const items = await listTopics(currentAuth.value.client)
    if (!current(request)) return
    topics.value = items
    if (!items.some(topic => topic.id === selectedTopicId.value)) selectedTopicId.value = ''
  } catch (error) {
    if (!current(request)) return
    // 题库加载失败不清空草稿：自定义题仍可填写并提交。
    topics.value = []
    selectedTopicId.value = ''
    loadError.value = error instanceof ApiError ? error.message : '辩题加载失败，请手动重试。'
  } finally {
    if (current(request)) { loading.value = false; loaded.value = true }
  }
}

function selectSource(value: 'preset' | 'custom') {
  if (formDisabled.value) return
  source.value = value
  if (value === 'custom') practiceMode.value = 'standard_4'
}

async function create() {
  if (!canSubmit.value) return
  // 同步置位：DOM 更新 disabled 前连续提交也不能创建两个不同会话。
  creating.value = true
  createError.value = ''
  const request = ++version
  try {
    const debate = practiceMode.value === 'training_v2'
      ? await createTrainingApi(currentAuth.value.client).create({ topic_id: selectedTopicId.value, user_stance: stance.value, ai_style: aiStyle.value })
      : source.value === 'custom'
        ? await createDebate({ custom_topic: { title: customTitle.value, background: customBackground.value }, user_stance: stance.value, ai_style: aiStyle.value }, currentAuth.value.client)
      : await createDebate({ topic_id: selectedTopicId.value, user_stance: stance.value, ai_style: aiStyle.value }, currentAuth.value.client)
    if (!current(request)) return
    createdDebate.value = debate
    emit('created', debate)
  } catch (error) {
    if (!current(request)) return
    // 草稿保留在表单内：超长、服务端错误与结果不明都不清空用户输入。
    createError.value = error instanceof ApiError ? error.message : '创建会话未获确认。'
    // 创建不是幂等操作；仅明确在创建前拒绝的状态允许修正/手动重提，其余先查历史。
    creationUncertain.value = !(error instanceof ApiError) || error.kind !== 'http'
      || ![400, 401, 403, 404, 429].includes(error.status ?? 0)
    if (creationUncertain.value) createError.value = '未能确认创建结果，请先查看历史会话，避免重复创建。'
  } finally { if (current(request)) creating.value = false }
}
function openHistory() { if (!navigationDisabled.value) emit('open-history') }
function back() { if (!navigationDisabled.value) emit('back-to-debate') }

// 当前来源下用于展示的题目预览：预设题与自定义题共用同一套会话快照文本。
const previewTopic = computed<DebateTopic | null>(() => {
  if (source.value === 'custom') {
    return customTitle.value || customBackground.value
      ? { id: null, title: customTitle.value, description: customBackground.value }
      : null
  }
  const selected = topics.value.find(topic => topic.id === selectedTopicId.value)
  return selected ? { id: selected.id, title: selected.title, description: selected.description } : null
})

watch([() => currentAuth.value, () => currentAuth.value.state.status, () => currentAuth.value.state.user?.id], () => {
  // 账号变化立即作废在途响应，旧用户的结果不能回填到新视图；草稿随账号清理。
  version++
  topics.value = []; selectedTopicId.value = ''; stance.value = 'pro'; aiStyle.value = DEFAULT_AI_STYLE
  source.value = 'preset'; customTitle.value = ''; customBackground.value = ''
  loading.value = false; loaded.value = false; creating.value = false
  loadError.value = ''; createError.value = ''; createdDebate.value = null; creationUncertain.value = false
  void loadTopics()
}, { immediate: true, flush: 'sync' })
watch(blocked, value => { if (!value && !loaded.value && !loading.value) void loadTopics() })
onUnmounted(() => { disposed = true; version++ })
</script>

<template>
  <section class="card topics-view" aria-label="辩题列表视图" data-view="topics" :aria-busy="loading || creating">
    <div class="view-header">
      <h2>辩题列表与立场选择</h2>
      <Button :disabled="formDisabled" @click="loadTopics">{{ loading ? '加载中…' : loadError ? '重试加载辩题' : '刷新辩题' }}</Button>
    </div>
    <p class="muted">当前用户：{{ currentAuth.state.user?.username ?? '未确认' }}。选定辩题和立场后创建会话，创建时不会调用模型。</p>
    <p v-if="!authenticated" role="status">请先登录，再选择辩题。</p>
    <p v-else-if="loading" role="status">正在加载辩题…</p>
    <Alert v-if="loadError" type="error" :title="loadError" role="alert" />
    <!-- 题库为空或加载失败时不清空表单：自定义辩题仍可填写并提交。
         保留原提示文案，避免改动既有页面契约与浏览器回归断言。 -->
    <p v-if="authenticated && loaded && !loading && !loadError && !topics.length" role="status">暂无可用辩题，请稍后刷新。</p>
    <form v-if="authenticated" @submit.prevent="create">
      <fieldset :disabled="formDisabled" class="source-options" data-source-options>
        <legend>辩题来源</legend>
        <label><input :checked="source === 'preset'" type="radio" :name="`${id}-source`" value="preset" data-source="preset" @change="selectSource('preset')">从公共题库选择</label>
        <label><input :checked="source === 'custom'" type="radio" :name="`${id}-source`" value="custom" data-source="custom" @change="selectSource('custom')">自定义辩题（仅本人可见）</label>
      </fieldset>
      <fieldset v-if="source === 'preset'" :disabled="formDisabled" class="topic-options">
        <legend>选择辩题</legend>
        <label v-for="topic in topics" :key="topic.id" class="topic-option" :class="{ selected: selectedTopicId === topic.id }" :data-topic-id="topic.id">
          <input v-model="selectedTopicId" type="radio" :name="`${id}-topic`" :value="topic.id">
          <span><strong>{{ topic.title }}</strong><span class="description">{{ topic.description }}</span></span>
        </label>
      </fieldset>
      <!-- 未选自定义时不渲染输入框：既避免隐藏控件参与表单查询，
           也保证草稿仍保存在组件状态里，切换来源不会丢输入。 -->
      <div v-if="source === 'custom'" class="custom-topic" data-custom-topic>
        <p class="muted">自定义辩题仅你可在本站查看，不会进入公共题库；创建后题目与立场固定，不可修改。</p>
        <div class="custom-field">
          <label :for="`${id}-custom-title`">辩题（必填，1–200 字）</label>
          <textarea
            :id="`${id}-custom-title`"
            v-model="customTitle"
            name="custom_title"
            rows="2"
            :disabled="formDisabled"
            data-custom-title
            :aria-describedby="`${id}-custom-title-count`"
          ></textarea>
          <p :id="`${id}-custom-title-count`" class="counter" :class="{ invalid: titleInvalid }" data-custom-title-count>
            {{ titleCount }} / {{ CUSTOM_TOPIC_TITLE_MAX_CHARS }}
            <span v-if="titleBlank && customTitle.length > 0">题目不能为纯空白</span>
            <span v-else-if="titleCount > CUSTOM_TOPIC_TITLE_MAX_CHARS">超出上限，请删减后再提交</span>
          </p>
        </div>
        <div class="custom-field">
          <label :for="`${id}-custom-background`">背景（选填，0–{{ CUSTOM_TOPIC_BACKGROUND_MAX_CHARS }} 字）</label>
          <textarea
            :id="`${id}-custom-background`"
            v-model="customBackground"
            name="custom_background"
            rows="4"
            :disabled="formDisabled"
            data-custom-background
            :aria-describedby="`${id}-custom-background-count`"
          ></textarea>
          <p :id="`${id}-custom-background-count`" class="counter" :class="{ invalid: backgroundInvalid }" data-custom-background-count>
            {{ backgroundCount }} / {{ CUSTOM_TOPIC_BACKGROUND_MAX_CHARS }}
            <span v-if="backgroundInvalid">超出上限，请删减后再提交</span>
          </p>
        </div>
      </div>
      <fieldset :disabled="formDisabled" class="mode-options">
        <legend>选择练习模式</legend>
        <label v-for="(label, value) in MODE_LABELS" :key="value" class="mode-option" :class="{ selected: practiceMode === value }" :data-mode="value">
          <input v-model="practiceMode" type="radio" :name="`${id}-mode`" :value="value" :disabled="value === 'training_v2' && source === 'custom'">
          <span><strong>{{ label.title }}</strong><span class="description">{{ label.detail }}</span></span>
        </label>
        <p class="muted">两者都提供赛后复盘入口；完整训练赛没有解锁条件，也不会自动切换模式。</p>
      </fieldset>
      <fieldset :disabled="formDisabled" class="stance-options">
        <legend>选择你的立场</legend>
        <label><input v-model="stance" type="radio" :name="`${id}-stance`" value="pro">正方（支持）</label>
        <label><input v-model="stance" type="radio" :name="`${id}-stance`" value="con">反方（反对）</label>
      </fieldset>
      <div class="style-field">
        <label :for="`${id}-ai-style`">AI 陪练流派</label>
        <select :id="`${id}-ai-style`" v-model="aiStyle" name="ai_style" :disabled="formDisabled">
          <option v-for="(label, value) in AI_STYLE_LABELS" :key="value" :value="value">{{ label }}</option>
        </select>
        <p class="muted">默认逻辑型。创建后流派固定，历史和策略均以服务端保存值为准。</p>
      </div>
      <div v-if="previewTopic" class="topic-preview" data-topic-preview>
        <p class="muted">本场题目预览（按纯文本展示，创建后保存为快照）</p>
        <h3>{{ previewTopic.title }}</h3>
        <p v-if="previewTopic.description" class="description">{{ previewTopic.description }}</p>
      </div>
      <p class="muted">你将持{{ stance === 'pro' ? '正方' : '反方' }}，AI 持{{ stance === 'pro' ? '反方' : '正方' }}。{{ source === 'custom' ? '请填写辩题后创建会话。' : '请选择一个辩题后创建会话。' }}</p>
      <Alert v-if="createError" type="error" :title="createError" role="alert" />
      <p v-if="creationUncertain" role="status">创建结果尚未确认。请先查看历史会话，核实是否已创建，避免重复创建。</p>
      <p v-if="createdDebate" role="status">会话已创建。</p>
      <Button type="primary" html-type="submit" :loading="creating" :disabled="!canSubmit">{{ creating ? '创建中…' : '创建会话' }}</Button>
    </form>
    <div class="view-actions">
      <Button type="outline" data-nav-debate :disabled="navigationDisabled" @click="back">返回辩论主视图</Button>
      <Button type="outline" data-nav-history :disabled="navigationDisabled" @click="openHistory">查看历史会话</Button>
    </div>
  </section>
</template>

<style scoped>
.view-header, .view-actions, .stance-options, .source-options { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.view-header { justify-content: space-between; }
.view-header h2 { margin-bottom: 0; }
.muted, .description { color: var(--app-text-muted); font-size: 13px; line-height: 1.8; }
fieldset { min-width: 0; padding: 0; margin: 20px 0; border: 0; }
legend { margin-bottom: 12px; font-weight: 600; }
.source-options label { display: inline-flex; gap: 8px; align-items: center; }
.topic-options { display: grid; gap: 10px; }
.topic-option { display: flex; align-items: flex-start; gap: 12px; padding: 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); overflow-wrap: anywhere; }
.topic-option.selected { border-color: var(--app-selected-border); background: var(--app-selected-bg); }
.topic-option input { flex: none; margin-top: 4px; }
.description { display: block; white-space: pre-wrap; }
.stance-options label { display: inline-flex; gap: 8px; align-items: center; }
.mode-options { display: grid; gap: 10px; }
.mode-option { display: flex; align-items: flex-start; gap: 12px; padding: 14px 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); overflow-wrap: anywhere; }
.mode-option.selected { border-color: var(--app-selected-border); background: var(--app-selected-bg); }
.mode-option input { flex: none; margin-top: 4px; }
.view-actions { margin-top: 20px; }
[role="alert"], .custom-topic { margin-bottom: 16px; }
.custom-field { margin: 16px 0; }
.custom-field label { display: block; margin-bottom: 8px; font-weight: 600; }
.custom-field textarea { width: 100%; padding: 10px; border: 1px solid var(--app-input-border); border-radius: var(--app-radius-control); background: var(--app-surface); color: var(--app-text); font: inherit; resize: vertical; }
.counter { margin: 6px 0 0; font-size: 12px; color: var(--app-text-muted); }
.counter.invalid { color: var(--app-danger-text, #cb2634); }
.style-field label { display: block; margin-bottom: 8px; font-weight: 600; }
.style-field select { width: 100%; padding: 10px; border: 1px solid var(--app-input-border); border-radius: var(--app-radius-control); background: var(--app-surface); color: var(--app-text); }
.topic-preview { padding: 16px; border: 1px dashed var(--app-border); border-radius: var(--app-radius-control); overflow-wrap: anywhere; }
.topic-preview h3 { margin: 8px 0; white-space: pre-wrap; }
</style>
