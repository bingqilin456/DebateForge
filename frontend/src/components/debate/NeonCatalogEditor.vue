<script setup lang="ts">
import { computed, ref } from 'vue'
import { createNeonCatalogApi, neonCategoryLabels } from '../../api/neonCatalog'
import type { ApiClient } from '../../api/client'
import type { NeonOpponent, NeonOpponentDraft, NeonTopic, NeonTopicDraft } from '../../types/api'
import { CUSTOM_TOPIC_BACKGROUND_MAX_CHARS, CUSTOM_TOPIC_TITLE_MAX_CHARS } from '../../shared/limits'

const props = defineProps<{ client: ApiClient; topics: NeonTopic[]; opponents: NeonOpponent[]; disabled: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const kind = ref<'topics' | 'opponents'>('topics')
const pending = ref(false)
const error = ref('')
const message = ref('')
const editTopicId = ref<string>()
const editOpponentId = ref<string>()
const topicDraft = ref<NeonTopicDraft>(emptyTopic())
const opponentDraft = ref<NeonOpponentDraft>(emptyOpponent())
const ownTopics = computed(() => props.topics.filter(item => item.owned))
const ownOpponents = computed(() => props.opponents.filter(item => item.owned))
const visibleOpponents = computed(() => props.opponents.filter(item => item.public).sort((a, b) => b.heat - a.heat))
const canSaveTopic = computed(() => !!topicDraft.value.title.trim() && [...topicDraft.value.title].length <= CUSTOM_TOPIC_TITLE_MAX_CHARS
  && [...topicDraft.value.background].length <= CUSTOM_TOPIC_BACKGROUND_MAX_CHARS)
function validField(value: string, max: number) { return !!value.trim() && [...value].length <= max }
const canSaveOpponent = computed(() => validField(opponentDraft.value.name, 40)
  && validField(opponentDraft.value.tag, 30) && validField(opponentDraft.value.persona, 1000))

function emptyTopic(): NeonTopicDraft { return { title: '', background: '', category: 'technology' } }
function emptyOpponent(): NeonOpponentDraft { return { name: '', tag: '', difficulty: 'beginner', persona: '' } }
function editTopic(item?: NeonTopic) {
  editTopicId.value = item?.id
  topicDraft.value = item ? { title: item.title, background: item.background, category: item.category } : emptyTopic()
}
function editOpponent(item?: NeonOpponent) {
  editOpponentId.value = item?.id
  opponentDraft.value = item ? { name: item.name, tag: item.tag, difficulty: item.difficulty, persona: item.persona } : emptyOpponent()
}

async function mutate(action: () => Promise<unknown>, success: string) {
  if (pending.value || props.disabled) return
  pending.value = true
  message.value = ''
  error.value = ''
  try {
    await action()
    message.value = success
    emit('changed')
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : '保存失败，请重试。'
  } finally {
    pending.value = false
  }
}

function saveTopic() {
  if (!canSaveTopic.value) return
  const id = editTopicId.value
  void mutate(async () => {
    await createNeonCatalogApi(props.client).saveTopic({ ...topicDraft.value }, id)
    editTopic()
  }, id ? '辩题已更新。' : '已保存为私密辩题。')
}

function saveOpponent() {
  if (!canSaveOpponent.value) return
  const id = editOpponentId.value
  void mutate(async () => {
    await createNeonCatalogApi(props.client).saveOpponent({ ...opponentDraft.value }, id)
    editOpponent()
  }, id ? 'AI 辩手已更新。' : '已保存为私密 AI 辩手。')
}

function toggleVisibility(item: NeonTopic | NeonOpponent) {
  const isTopic = 'title' in item
  const name = isTopic ? item.title : item.name
  if (!item.public && !window.confirm(`公开「${name}」后，其他账号可以查看和选用。确认公开？`)) return
  const catalog = createNeonCatalogApi(props.client)
  void mutate(() => isTopic ? catalog.topicVisibility(item.id, !item.public) : catalog.opponentVisibility(item.id, !item.public),
    item.public ? '已撤回公开。' : '已公开。')
}
</script>

<template>
  <details class="catalog-editor">
    <summary>管理自定义辩题与 AI 辩手</summary>
    <div class="editor-tabs">
      <button type="button" :aria-pressed="kind === 'topics'" @click="kind = 'topics'">我的辩题</button>
      <button type="button" :aria-pressed="kind === 'opponents'" @click="kind = 'opponents'">AI 辩手榜与管理</button>
    </div>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="message" role="status">{{ message }}</p>
    <template v-if="kind === 'topics'">
      <ul v-if="ownTopics.length" class="catalog-list">
        <li v-for="topic in ownTopics" :key="topic.id">{{ topic.title }} · {{ topic.public ? '公开' : '私密' }}
          <button type="button" :disabled="pending || disabled" @click="editTopic(topic)">编辑</button>
          <button type="button" :disabled="pending || disabled" @click="toggleVisibility(topic)">{{ topic.public ? '撤回' : '公开' }}</button>
        </li>
      </ul>
      <form @submit.prevent="saveTopic">
        <fieldset :disabled="pending || disabled">
          <legend>{{ editTopicId ? '编辑辩题' : '新增辩题' }}</legend>
          <label>题目 <input v-model="topicDraft.title" required /></label>
          <label>主题 <select v-model="topicDraft.category"><option v-for="(label, category) in neonCategoryLabels" :key="category" :value="category">{{ label }}</option></select></label>
          <label>背景 <textarea v-model="topicDraft.background" rows="3"></textarea></label>
          <button type="submit" :disabled="!canSaveTopic">保存辩题</button>
          <button v-if="editTopicId" type="button" @click="editTopic()">取消编辑</button>
        </fieldset>
      </form>
    </template>
    <template v-else>
      <ol class="catalog-list" aria-label="热门 AI 辩手">
        <li v-for="opponent in visibleOpponents" :key="opponent.id">
          <strong>{{ opponent.name }}</strong> · {{ opponent.tag }} · {{ opponent.heat }} 人交锋
          <small>{{ opponent.persona }}</small>
        </li>
      </ol>
      <ul v-if="ownOpponents.length" class="catalog-list">
        <li v-for="opponent in ownOpponents" :key="opponent.id">{{ opponent.name }} · {{ opponent.public ? '公开' : '私密' }}
          <button type="button" :disabled="pending || disabled" @click="editOpponent(opponent)">编辑</button>
          <button type="button" :disabled="pending || disabled" @click="toggleVisibility(opponent)">{{ opponent.public ? '撤回' : '公开' }}</button>
        </li>
      </ul>
      <form @submit.prevent="saveOpponent">
        <fieldset :disabled="pending || disabled">
          <legend>{{ editOpponentId ? '编辑 AI 辩手' : '新增 AI 辩手' }}</legend>
          <label>名称 <input v-model="opponentDraft.name" required /></label>
          <label>标签 <input v-model="opponentDraft.tag" required /></label>
          <label>难度 <select v-model="opponentDraft.difficulty"><option value="beginner">入门</option><option value="varsity">校队</option><option value="national">国赛级</option></select></label>
          <label>辩手人设 <textarea v-model="opponentDraft.persona" rows="3" required></textarea></label>
          <button type="submit" :disabled="!canSaveOpponent">保存 AI 辩手</button>
          <button v-if="editOpponentId" type="button" @click="editOpponent()">取消编辑</button>
        </fieldset>
      </form>
    </template>
  </details>
</template>

<style scoped>
.catalog-editor { margin: 1rem 0; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); padding: 12px; }
summary { cursor: pointer; font-weight: 600; }
.editor-tabs { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0; }
button { padding: 6px 10px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); background: var(--app-surface); color: var(--app-text); cursor: pointer; }
button[aria-pressed="true"] { border-color: var(--app-selected-border); background: var(--app-selected-bg); }
button:disabled { opacity: 0.55; cursor: default; }
.catalog-list { display: grid; gap: 6px; padding-inline-start: 22px; overflow-wrap: anywhere; }
.catalog-list small { display: block; }
fieldset { display: grid; gap: 8px; border: 0; min-width: 0; padding: 0; }
label { display: grid; gap: 4px; }
input, select, textarea { border: 1px solid var(--app-input-border); border-radius: var(--app-radius-control); background: var(--app-surface); color: var(--app-text); padding: 6px; }
</style>
