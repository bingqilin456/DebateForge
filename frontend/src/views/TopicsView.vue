<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { Alert, Button } from '@arco-design/web-vue'
import { authState, type AuthState } from '../auth/state'
import { createDebate, listTopics } from '../api/debate'
import { ApiError } from '../api/client'
import type { Debate, Stance, Topic } from '../types/api'

export interface TopicsViewProps {
  auth?: AuthState
  disabled?: boolean
}
const props = withDefaults(defineProps<TopicsViewProps>(), { disabled: false })
const emit = defineEmits<{
  /** 创建辩论会话成功后触发，交由顶层视图切换至辩论主视图并加载该会话 */
  created: [debate: Debate]
  /** 按会话 ID 切换至辩论主视图并打开对应会话 */
  'open-debate': [debateId: string]
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
const stance = ref<Stance>('pro')
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
    topics.value = []
    selectedTopicId.value = ''
    loadError.value = error instanceof ApiError ? error.message : '辩题加载失败，请手动重试。'
  } finally {
    if (current(request)) { loading.value = false; loaded.value = true }
  }
}

async function create() {
  if (formDisabled.value || !topics.value.some(topic => topic.id === selectedTopicId.value)) return
  // 同步置位：DOM 更新 disabled 前连续提交也不能创建两个不同会话。
  creating.value = true
  createError.value = ''
  const request = ++version
  try {
    const debate = await createDebate(selectedTopicId.value, stance.value, currentAuth.value.client)
    if (!current(request)) return
    createdDebate.value = debate
    // 不再同时发 open-debate，否则顶层会对同一结果打开两次。
    emit('created', debate)
  } catch (error) {
    if (!current(request)) return
    createError.value = error instanceof ApiError ? error.message : '创建会话失败，请稍后再试。'
    // 创建不是幂等操作。丢失/损坏响应或 5xx 时先去历史核实，不能盲目重发。
    creationUncertain.value = !(error instanceof ApiError) || error.kind !== 'http' || (error.status ?? 500) >= 500
  } finally { if (current(request)) creating.value = false }
}
function openHistory() { if (!navigationDisabled.value) emit('open-history') }
function back() { if (!navigationDisabled.value) emit('back-to-debate') }

watch([() => currentAuth.value, () => currentAuth.value.state.status, () => currentAuth.value.state.user?.id], () => {
  // 账号变化立即作废在途响应，旧用户的结果不能回填到新视图。
  version++
  topics.value = []; selectedTopicId.value = ''; stance.value = 'pro'
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
    <p v-if="authenticated && loaded && !loading && !loadError && !topics.length" role="status">暂无可用辩题，请稍后刷新。</p>
    <form v-if="authenticated" @submit.prevent="create">
      <fieldset :disabled="formDisabled" class="topic-options">
        <legend>选择辩题</legend>
        <label v-for="topic in topics" :key="topic.id" class="topic-option" :class="{ selected: selectedTopicId === topic.id }" :data-topic-id="topic.id">
          <input v-model="selectedTopicId" type="radio" :name="`${id}-topic`" :value="topic.id">
          <span><strong>{{ topic.title }}</strong><span class="description">{{ topic.description }}</span></span>
        </label>
      </fieldset>
      <fieldset :disabled="formDisabled" class="stance-options">
        <legend>选择你的立场</legend>
        <label><input v-model="stance" type="radio" :name="`${id}-stance`" value="pro">正方（支持）</label>
        <label><input v-model="stance" type="radio" :name="`${id}-stance`" value="con">反方（反对）</label>
      </fieldset>
      <p class="muted">你将持{{ stance === 'pro' ? '正方' : '反方' }}，AI 持{{ stance === 'pro' ? '反方' : '正方' }}。请选择一个辩题后创建会话。</p>
      <Alert v-if="createError" type="error" :title="createError" role="alert" />
      <p v-if="creationUncertain" role="status">创建结果尚未确认。请先查看历史会话，核实是否已创建，避免重复创建。</p>
      <p v-if="createdDebate" role="status">会话已创建。</p>
      <Button type="primary" html-type="submit" :loading="creating" :disabled="formDisabled || !selectedTopicId">{{ creating ? '创建中…' : '创建会话' }}</Button>
    </form>
    <div class="view-actions">
      <Button type="outline" data-nav-debate :disabled="navigationDisabled" @click="back">返回辩论主视图</Button>
      <Button type="outline" data-nav-history :disabled="navigationDisabled" @click="openHistory">查看历史会话</Button>
    </div>
  </section>
</template>

<style scoped>
.view-header, .view-actions, .stance-options { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.view-header { justify-content: space-between; }
.view-header h2 { margin-bottom: 0; }
.muted, .description { color: #526578; font-size: 13px; line-height: 1.8; }
fieldset { min-width: 0; padding: 0; margin: 20px 0; border: 0; }
legend { margin-bottom: 12px; font-weight: 600; }
.topic-options { display: grid; gap: 10px; }
.topic-option { display: flex; align-items: flex-start; gap: 12px; padding: 16px; border: 1px solid #e3e8ee; border-radius: 8px; overflow-wrap: anywhere; }
.topic-option.selected { border-color: #165dff; background: #f3f7ff; }
.topic-option input { flex: none; margin-top: 4px; }
.description { display: block; white-space: pre-wrap; }
.stance-options label { display: inline-flex; gap: 8px; align-items: center; }
.view-actions { margin-top: 20px; }
[role="alert"] { margin-bottom: 16px; }
</style>
