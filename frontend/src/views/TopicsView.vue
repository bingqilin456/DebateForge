<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { Alert, Button } from '@arco-design/web-vue'
import { authState, type AuthState } from '../auth/state'
import { createDebate, listTopics } from '../api/debate'
import { cupStageIds, type CupStageId, type CupStatus } from '../api/cup'
import { ApiError } from '../api/client'
import { createNeonCatalogApi, neonCategoryLabels, neonTopicKey } from '../api/neonCatalog'
import NeonCatalogEditor from '../components/debate/NeonCatalogEditor.vue'
import TopicPublicReports from '../components/debate/TopicPublicReports.vue'
import { AI_STYLE_LABELS, DEFAULT_AI_STYLE } from '../debate/styles'
import { CUSTOM_TOPIC_BACKGROUND_MAX_CHARS, CUSTOM_TOPIC_TITLE_MAX_CHARS } from '../shared/limits'
import type { AIStyle, Debate, DebateDifficulty, DebateTopic, NeonOpponent, NeonTopic, NeonTopicCategory, Stance, Topic } from '../types/api'

export interface TopicsViewProps {
  auth?: AuthState
  disabled?: boolean
  cup?: CupStatus
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
const catalogTopics = ref<NeonTopic[] | null>(null)
const opponents = ref<NeonOpponent[]>([])
const catalogLoading = ref(false)
const topicFilter = ref<NeonTopicCategory | 'all' | 'hot'>('all')
const opponentId = ref('')
const selectedOpponent = computed(() => opponents.value.find(item => !item.official && item.id === opponentId.value))
const visibleTopics = computed<(Topic & { item?: NeonTopic })[]>(() => {
  const items = catalogTopics.value
  if (!items) return topics.value
  const filtered = topicFilter.value === 'hot' ? items.filter(item => item.public).sort((a, b) => b.heat - a.heat)
    : items.filter(item => topicFilter.value === 'all' || item.category === topicFilter.value)
  return filtered.map(item => ({ id: neonTopicKey(item), title: item.title, description: item.background, item }))
})
const selectedCatalogTopic = computed(() => catalogTopics.value?.find(item => neonTopicKey(item) === selectedTopicId.value))
const availableOpponents = computed(() => opponents.value.filter(item => !item.official).sort((a, b) => b.heat - a.heat))
const opponentCards = computed(() => {
  const official = Object.entries(AI_STYLE_LABELS).map(([value, label]) => {
    const item = opponents.value.find(candidate => candidate.official && candidate.style === value)
    return { value, name: item?.name ?? label, item }
  })
  return [...official, ...availableOpponents.value.map(item => ({ value: `custom-${item.id}`, name: item.name, item }))]
    .sort((a, b) => (b.item?.heat ?? 0) - (a.item?.heat ?? 0))
})
const opponentChoice = computed({
  get: () => opponentId.value ? `custom-${opponentId.value}` : aiStyle.value,
  set(value: string) {
    if (value.startsWith('custom-')) {
      opponentId.value = value.slice(7)
      aiStyle.value = DEFAULT_AI_STYLE
      if (selectedOpponent.value) difficulty.value = selectedOpponent.value.difficulty
      return
    }
    opponentId.value = ''
    aiStyle.value = value as AIStyle
    const official = opponents.value.find(item => item.official && item.style === value)
    if (official) difficulty.value = official.difficulty
  },
})
// 选题来源：预设与自定义严格二选一，与后端请求形状一致。
const source = ref<'preset' | 'custom'>('preset')
const customTitle = ref('')
const customBackground = ref('')
const stance = ref<Stance>('pro')
const aiStyle = ref<AIStyle>(DEFAULT_AI_STYLE)
const difficulty = ref<DebateDifficulty>('beginner')
const practice = ref(false)
const cupStage = ref<CupStageId>()
const difficultyLabels: Record<DebateDifficulty, string> = {
  beginner: '入门 · 600–800 字',
  varsity: '校队 · 800–1100 字',
  national: '国赛级 · 1000–1400 字',
}
const cupDifficulty: Record<DebateDifficulty, string> = { beginner: '入门', varsity: '校队', national: '国赛级' }
const selectedCupStage = computed(() => props.cup?.stages.find(stage => stage.id === cupStage.value))
function cupStageLocked(stage: CupStageId) {
  return !!props.cup && cupStageIds.indexOf(stage) > cupStageIds.indexOf(props.cup.unlocked_stage)
}
function selectCupStage(id?: CupStageId) {
  const stage = props.cup?.stages.find(item => item.id === id)
  cupStage.value = stage?.id
  if (stage) {
    opponentId.value = ''
    difficulty.value = stage.difficulty
    aiStyle.value = stage.ai_style
  }
}
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
  if (source.value === 'custom') return !customInvalid.value
  return !!selectedTopicId.value && (catalogTopics.value
    ? !!selectedCatalogTopic.value : topics.value.some(topic => topic.id === selectedTopicId.value))
})
watch(creating, value => emit('creating', value), { flush: 'sync' })
watch(creationUncertain, value => emit('uncertain', value), { flush: 'sync' })
let version = 0
let catalogVersion = 0
let catalogAccount = 0
let disposed = false
function current(request: number) { return !disposed && request === version }

async function loadCatalog() {
  const latest = ++catalogVersion
  const account = catalogAccount
  catalogLoading.value = true
  const catalog = createNeonCatalogApi(currentAuth.value.client)
  try {
    const [newTopics, newOpponents] = await Promise.all([catalog.topics(), catalog.opponents()])
    if (disposed || latest !== catalogVersion || account !== catalogAccount) return
    catalogTopics.value = newTopics.items
    opponents.value = newOpponents.items
    if (selectedTopicId.value && !selectedCatalogTopic.value) selectedTopicId.value = ''
    if (opponentId.value && !selectedOpponent.value) opponentId.value = ''
  } catch {
    // 保留已读取的目录和临时题表单；下次刷新可重试。
  } finally {
    if (!disposed && latest === catalogVersion && account === catalogAccount) catalogLoading.value = false
  }
}

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
    if (!catalogTopics.value && !items.some(topic => topic.id === selectedTopicId.value)) selectedTopicId.value = ''
  } catch (error) {
    if (!current(request)) return
    // 题库加载失败不清空草稿：自定义题仍可填写并提交。
    topics.value = []
    selectedTopicId.value = ''
    loadError.value = error instanceof ApiError ? error.message : '辩题加载失败，请手动重试。'
  } finally {
    if (current(request)) { loading.value = false; loaded.value = true; void loadCatalog() }
  }
}

function selectSource(value: 'preset' | 'custom') {
  if (formDisabled.value) return
  source.value = value
}

function startTopic(topicId: string) {
  if (formDisabled.value) return
  selectedTopicId.value = topicId
  void create()
}

async function create() {
  if (!canSubmit.value) return
  // 同步置位：DOM 更新 disabled 前连续提交也不能创建两个不同会话。
  creating.value = true
  createError.value = ''
  const request = ++version
  try {
    const topic = source.value === 'custom'
      ? { custom_topic: { title: customTitle.value, background: customBackground.value } }
      : selectedCatalogTopic.value && !selectedCatalogTopic.value.official
        ? { neon_topic_id: selectedCatalogTopic.value.id }
        : { topic_id: selectedTopicId.value }
    const debate = await createDebate({ ...topic, user_stance: stance.value, ai_style: aiStyle.value,
      ...(difficulty.value === 'beginner' ? {} : { difficulty: difficulty.value }),
      ...(practice.value ? { practice: true } : {}),
      ...(cupStage.value ? { cup_stage: cupStage.value } : {}),
      ...(selectedOpponent.value ? { neon_opponent_id: selectedOpponent.value.id } : {}) }, currentAuth.value.client)
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
  const selected = selectedCatalogTopic.value
  if (selected) return { id: selectedTopicId.value, title: selected.title, description: selected.background }
  const preset = topics.value.find(topic => topic.id === selectedTopicId.value)
  return preset ?? null
})

watch([() => currentAuth.value, () => currentAuth.value.state.status, () => currentAuth.value.state.user?.id], () => {
  // 账号变化立即作废在途响应，旧用户的结果不能回填到新视图；草稿随账号清理。
  version++
  catalogAccount++
  catalogTopics.value = null; opponents.value = []; opponentId.value = ''; catalogLoading.value = false; topicFilter.value = 'all'
  topics.value = []; selectedTopicId.value = ''; stance.value = 'pro'; aiStyle.value = DEFAULT_AI_STYLE
  difficulty.value = 'beginner'
  practice.value = false
  cupStage.value = undefined
  source.value = 'preset'; customTitle.value = ''; customBackground.value = ''
  loading.value = false; loaded.value = false; creating.value = false
  loadError.value = ''; createError.value = ''; createdDebate.value = null; creationUncertain.value = false
  void loadTopics()
}, { immediate: true, flush: 'sync' })
watch(blocked, value => { if (!value && !loaded.value && !loading.value) void loadTopics() })
onUnmounted(() => { disposed = true; version++ })
</script>

<template>
  <section class="topics-view" aria-label="辩题列表视图" data-view="topics" :aria-busy="loading || creating">
    <div class="view-header">
      <div>
        <h2>辩题列表与立场选择</h2>
        <p class="muted">单场四轮 · 立论 / 攻辩 / 交锋 / 结辩</p>
      </div>
      <Button :disabled="formDisabled" @click="loadTopics">{{ loading ? '加载中…' : loadError ? '重试加载辩题' : '刷新辩题' }}</Button>
    </div>
    <section v-if="cup" class="lobby-panel cup-preview" aria-label="思弈杯">
      <div class="cup-heading">
        <div>
          <h3>思弈杯 · 四阶晋级赛制</h3>
          <p class="muted">当前 ELO {{ cup.elo }}<template v-if="cup.rank"> · 第 {{ cup.rank }} 名</template></p>
        </div>
        <Button :disabled="formDisabled" @click="selectCupStage(cup.recommended_stage)">按我的 ELO 智能匹配难度</Button>
      </div>
      <div class="cup-stages">
        <button v-for="stage in cup.stages" :key="stage.id" type="button" :aria-pressed="cupStage === stage.id"
          :disabled="formDisabled" :data-cup-stage="stage.id" @click="selectCupStage(stage.id)">
          <strong>{{ stage.name }}</strong>
          <small>{{ cupDifficulty[stage.difficulty] }} · {{ cupStageLocked(stage.id) ? '可练手' : '已解锁' }}</small>
        </button>
      </div>
      <div v-if="selectedCupStage" class="cup-heading">
        <p>{{ selectedCupStage.name }} · {{ cupDifficulty[selectedCupStage.difficulty] }} · {{ AI_STYLE_LABELS[selectedCupStage.ai_style] }}。{{ selectedCupStage.explanation }}
          <strong v-if="cupStageLocked(selectedCupStage.id)">该赛段当前只用于练手。</strong></p>
        <Button type="text" :disabled="formDisabled" @click="selectCupStage()">切回普通练习</Button>
      </div>
    </section>
    <p v-if="!authenticated" role="status">请先登录，再选择辩题。</p>
    <p v-else-if="loading" role="status">正在加载辩题…</p>
    <Alert v-if="loadError" type="error" :title="loadError" role="alert" />
    <!-- 题库为空或加载失败时不清空表单：自定义辩题仍可填写并提交。
         保留原提示文案，避免改动既有页面契约与浏览器回归断言。 -->
    <p v-if="authenticated && loaded && !loading && !loadError && !topics.length" role="status">暂无可用辩题，请稍后刷新。</p>
    <NeonCatalogEditor v-if="authenticated && catalogTopics" :key="currentAuth.state.user?.id"
      :client="currentAuth.client" :topics="catalogTopics" :opponents="opponents"
      :disabled="formDisabled || catalogLoading" @changed="loadCatalog" />
    <form v-if="authenticated" @submit.prevent="create">
      <section class="lobby-panel" :aria-labelledby="`${id}-opponents-title`">
        <div class="section-heading">
          <h3 :id="`${id}-opponents-title`">第一步：选择 AI 辩手与持方</h3>
          <fieldset :disabled="formDisabled" class="stance-options">
            <legend>选择你的立场</legend>
            <label><input v-model="stance" type="radio" :name="`${id}-stance`" value="pro">正方（支持）</label>
            <label><input v-model="stance" type="radio" :name="`${id}-stance`" value="con">反方（反对）</label>
          </fieldset>
        </div>
        <fieldset class="persona-grid" :disabled="formDisabled || !!selectedCupStage" aria-label="AI 辩手卡片">
          <button
            v-for="opponent in opponentCards" :key="opponent.value" type="button" class="persona-card"
            :aria-pressed="opponentChoice === opponent.value" @click="opponentChoice = opponent.value"
          >
            <span class="persona-heading">
              <strong>{{ opponent.name }}</strong>
              <span v-if="opponent.item" class="pill">{{ cupDifficulty[opponent.item.difficulty] }}</span>
            </span>
            <span v-if="opponent.item" class="persona-meta">
              <span>{{ opponent.item.tag }}</span>
              <span v-if="!opponent.item.official" class="heat">{{ opponent.item.public ? `热度 ${opponent.item.heat}` : '仅本人可见' }}</span>
            </span>
            <span v-if="opponent.item?.persona" class="muted">{{ opponent.item.persona }}</span>
          </button>
        </fieldset>
        <div class="opponent-settings">
          <div class="style-field">
            <label :for="`${id}-ai-style`">AI 陪练流派</label>
            <select :id="`${id}-ai-style`" v-model="opponentChoice" name="ai_style" :disabled="formDisabled || !!selectedCupStage">
              <option v-for="(label, value) in AI_STYLE_LABELS" :key="value" :value="value">{{ opponents.find(item => item.official && item.style === value)?.name ?? label }}</option>
              <option v-for="opponent in availableOpponents" :key="opponent.id" :value="`custom-${opponent.id}`">{{ opponent.name }} · {{ opponent.tag }}</option>
            </select>
            <p v-if="selectedOpponent" class="muted">{{ selectedOpponent.persona }}</p>
            <p v-else class="muted">默认逻辑型。开赛后保存当时的辩手配置。</p>
          </div>
          <div class="style-field">
            <label :for="`${id}-difficulty`">交锋难度</label>
            <select :id="`${id}-difficulty`" v-model="difficulty" name="difficulty" :disabled="formDisabled || !!selectedCupStage || !!selectedOpponent">
              <option v-for="(label, value) in difficultyLabels" :key="value" :value="value">{{ label }}</option>
            </select>
          </div>
        </div>
        <label class="practice-option" :for="`${id}-practice`">
          <input :id="`${id}-practice`" v-model="practice" type="checkbox" :disabled="formDisabled" />
          使用本地演示开练习局（不计正式积分）
        </label>
      </section>
      <div class="section-heading"><h3>第二步：选择辩题并进入竞技场</h3></div>
      <fieldset :disabled="formDisabled" class="source-options" data-source-options>
        <legend>辩题来源</legend>
        <label><input :checked="source === 'preset'" type="radio" :name="`${id}-source`" value="preset" data-source="preset" @change="selectSource('preset')">从公共题库选择</label>
        <label><input :checked="source === 'custom'" type="radio" :name="`${id}-source`" value="custom" data-source="custom" @change="selectSource('custom')">自定义辩题（仅本人可见）</label>
      </fieldset>
      <fieldset v-if="source === 'preset'" :disabled="formDisabled" class="topic-options">
        <legend>选择辩题</legend>
        <div v-if="catalogTopics" class="topic-filters" aria-label="辩题主题">
          <button type="button" :aria-pressed="topicFilter === 'hot'" @click="topicFilter = 'hot'">热门榜</button>
          <button type="button" :aria-pressed="topicFilter === 'all'" @click="topicFilter = 'all'">全部</button>
          <button v-for="(label, category) in neonCategoryLabels" :key="category" type="button"
            :aria-pressed="topicFilter === category" @click="topicFilter = category">{{ label }}</button>
        </div>
        <p v-if="catalogTopics && !visibleTopics.length">这个主题暂无辩题。</p>
        <div class="topic-grid">
          <article v-for="(topic, index) in visibleTopics" :key="topic.id" class="topic-option" :class="{ selected: selectedTopicId === topic.id }" :data-topic-id="topic.id">
            <label>
              <input v-model="selectedTopicId" type="radio" :name="`${id}-topic`" :value="topic.id">
              <span class="topic-content">
                <span v-if="topic.item" class="topic-meta">
                  <span v-if="topicFilter === 'hot'" class="pill topic-rank">TOP {{ index + 1 }}</span>
                  <span class="pill">{{ neonCategoryLabels[topic.item.category] }}</span>
                  <span class="heat">{{ topic.item.public ? `${topic.item.heat} 人交锋` : '仅本人可见' }}</span>
                </span>
                <strong>{{ topic.title }}</strong>
                <span class="description">{{ topic.description }}</span>
              </span>
            </label>
            <div class="topic-actions">
              <Button type="secondary" html-type="button" :disabled="formDisabled" @click="startTopic(topic.id)">立即开辩</Button>
              <TopicPublicReports v-if="topic.item?.public" :topic-id="topic.id" />
            </div>
          </article>
        </div>
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
.topics-view { display: grid; gap: 24px; min-width: 0; }
.topics-view form { display: grid; gap: 14px; }
.view-header, .view-actions, .section-heading, .cup-heading { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; justify-content: space-between; }
.view-header { align-items: flex-end; }
.view-header h2 { margin: 0; color: var(--app-text); font: 700 24px/1.3 var(--app-serif); }
.view-header p, .cup-heading p { margin: 4px 0 0; }
.section-heading h3, .cup-heading h3 { margin: 0; font-size: 14px; font-weight: 700; }
.lobby-panel, .topic-option {
  border: 1px solid var(--app-border);
  border-radius: 16px;
  background: var(--app-surface);
  box-shadow: 0 1px 3px rgba(28, 25, 23, .06);
}
.lobby-panel { display: grid; gap: 14px; padding: 16px; min-width: 0; }
.cup-heading > .arco-btn { color: #78350f; border: 1px solid #fde68a; border-radius: 12px; background: #fffbeb; }
.cup-stages { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.cup-stages button { display: grid; gap: 4px; padding: 12px; border: 1px solid var(--app-border); border-radius: 12px; color: var(--app-text); background: var(--app-surface); text-align: left; }
.cup-stages button[aria-pressed='true'] { border-color: #d6a858; background: #fffbeb; }
.cup-stages small { color: var(--app-text-muted); }
fieldset { min-width: 0; padding: 0; margin: 0; border: 0; }
legend { margin-bottom: 8px; font-size: 13px; font-weight: 600; }
.source-options, .stance-options { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.source-options label, .stance-options label, .practice-option { display: inline-flex; gap: 6px; align-items: center; }
.stance-options { padding: 4px; border-radius: 12px; background: var(--app-background); }
.stance-options legend { float: left; margin: 0 4px; align-self: center; }
.stance-options label { padding: 4px 8px; border-radius: 8px; font-size: 12px; }
.stance-options label:has(:checked) { background: var(--app-surface); box-shadow: 0 1px 2px rgba(28, 25, 23, .12); }
.persona-grid, .topic-grid, .opponent-settings { display: grid; gap: 12px; }
.persona-card { display: grid; align-content: start; gap: 8px; min-width: 0; padding: 14px; border: 1px solid var(--app-border); border-radius: 12px; color: var(--app-text); background: var(--app-surface); text-align: left; overflow-wrap: anywhere; }
.persona-card[aria-pressed='true'], .topic-option.selected { border-color: var(--app-pro); box-shadow: 0 0 0 2px var(--app-pro-pale); }
.persona-card[aria-pressed='true'] { background: var(--app-pro-pale); }
.persona-heading, .persona-meta, .topic-meta { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 5px; }
.persona-meta { color: var(--app-con); font-size: 11px; }
.pill, .heat { font-size: 11px; }
.pill { padding: 2px 8px; border-radius: 999px; background: var(--app-background); color: var(--app-text-muted); }
.topic-rank { background: var(--app-pro-pale); color: var(--app-pro); }
.heat { color: #92400e; }
.muted, .description { color: var(--app-text-muted); font-size: 12px; line-height: 1.7; }
.topic-options { display: grid; gap: 12px; }
.topic-filters, .topic-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.topic-filters button { padding: 5px 12px; border: 1px solid var(--app-border); border-radius: 8px; color: var(--app-text); background: var(--app-surface); font-size: 12px; }
.topic-filters button[aria-pressed='true'] { border-color: #1c1917; color: #fff; background: #1c1917; }
.topic-option { display: flex; flex-direction: column; justify-content: space-between; gap: 16px; padding: 16px; overflow-wrap: anywhere; }
.topic-option label { display: flex; align-items: flex-start; gap: 8px; cursor: pointer; }
.topic-option input { flex: none; margin-top: 4px; }
.topic-content { display: grid; gap: 8px; flex: 1; min-width: 0; }
.topic-content strong { font: 700 16px/1.4 var(--app-serif); }
.topic-actions > .arco-btn { flex: 1; }
.topic-actions > .arco-btn,
.topics-view form > .arco-btn-primary { border-radius: var(--app-radius-sm); font-weight: 650; }
.topic-actions > .arco-btn { color: var(--app-pro); background: var(--app-pro-pale); }
.topics-view form > .arco-btn-primary:not([disabled]) { color: #fff; background: #c9362b; }
.topics-view form > .arco-btn-primary:not([disabled]):hover { background: var(--app-pro); }
.description { display: block; white-space: pre-wrap; }
.view-actions { justify-content: flex-start; }
.custom-field { margin: 12px 0; }
.custom-field label, .style-field label { display: block; margin-bottom: 8px; font-weight: 600; }
.custom-field textarea, .style-field select {
  width: 100%;
  padding: 10px;
  border: 1px solid var(--app-input-border);
  border-radius: var(--app-radius-control);
  background: var(--app-surface);
  color: var(--app-text);
  font: inherit;
}
.custom-field textarea { resize: vertical; }
.counter { margin: 6px 0 0; font-size: 12px; color: var(--app-text-muted); }
.counter.invalid { color: var(--app-danger-text, #cb2634); }
.topic-preview { padding: 16px; border: 1px dashed var(--app-border); border-radius: var(--app-radius-control); overflow-wrap: anywhere; }
.topic-preview h3 { margin: 8px 0; white-space: pre-wrap; }
@media (min-width: 640px) {
  .lobby-panel { padding: 20px; }
  .opponent-settings { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (min-width: 768px) {
  .persona-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .topic-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (min-width: 1024px) {
  .cup-stages { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .topic-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
</style>
