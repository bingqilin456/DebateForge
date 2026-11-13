<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { Alert, Button } from '@arco-design/web-vue'
import { authState, type AuthState } from '../auth/state'
import { listDebates } from '../api/debate'
import { ApiError } from '../api/client'
import type { DebateListResult } from '../types/api'

export interface HistoryViewProps {
  auth?: AuthState
  pageSize?: number
  disabled?: boolean
}
const props = withDefaults(defineProps<HistoryViewProps>(), { pageSize: 20, disabled: false })
const emit = defineEmits<{
  /** 选中某条历史会话后触发，交由顶层切换至辩论主视图重读详情或继续未完成会话 */
  'select-debate': [debateId: string]
  /** 按会话 ID 打开辩论主视图（与 select-debate 等价） */
  'open-debate': [debateId: string]
  /** 切换至辩题列表视图（T08） */
  'open-topics': []
  /** 返回当前辩论主视图 */
  'back-to-debate': []
}>()

const currentAuth = computed(() => props.auth ?? authState)
const authenticated = computed(() => currentAuth.value.state.status === 'authenticated' && !!currentAuth.value.state.user)
const blocked = computed(() => props.disabled || currentAuth.value.state.pending || !authenticated.value)
function emptyHistory(): DebateListResult { return { items: [], total: 0, page: 1, page_size: 20 } }
const history = ref<DebateListResult>(emptyHistory())
const page = ref(1)
const loading = ref(false)
const loaded = ref(false)
const loadError = ref('')
const selectedDebateId = ref('')
const pageCount = computed(() => Math.max(1, Math.ceil(history.value.total / history.value.page_size)))
const controlsDisabled = computed(() => blocked.value || loading.value || !!selectedDebateId.value)
const navigationDisabled = computed(() => blocked.value || !!selectedDebateId.value)
let version = 0
let disposed = false
function current(request: number) { return !disposed && request === version }
function localTime(utc: string) { return new Date(utc).toLocaleString('zh-CN', { hour12: false }) }

async function loadPage(target = page.value) {
  if (controlsDisabled.value) return
  const request = ++version
  const client = currentAuth.value.client
  loading.value = true
  loaded.value = false
  loadError.value = ''
  page.value = target
  // 保留已知分页信息，但不把上一页/上一次刷新的记录当作本次成功结果。
  history.value = { ...history.value, items: [] }
  try {
    let data = await listDebates(target, props.pageSize, client)
    if (!current(request)) return
    const lastPage = Math.max(1, Math.ceil(data.total / data.page_size))
    if (target > lastPage) {
      // 并发数据变化可能让末页消失，只补查一次新的末页，不无限轮询。
      page.value = lastPage
      data = await listDebates(lastPage, props.pageSize, client)
      if (!current(request)) return
    }
    history.value = data
    page.value = data.page
  } catch (error) {
    if (!current(request)) return
    loadError.value = error instanceof ApiError ? error.message : '历史加载失败，请手动重试。'
  } finally {
    if (current(request)) { loading.value = false; loaded.value = true }
  }
}
function previousPage() { if (page.value > 1) void loadPage(page.value - 1) }
function nextPage() { if (page.value < pageCount.value && !loadError.value) void loadPage(page.value + 1) }
function selectDebate(id: string) {
  if (controlsDisabled.value) return
  // 交接前同步锁定，只发一种事件；详情重读和展示完全由已有主界面处理。
  selectedDebateId.value = id
  emit('select-debate', id)
}
function openTopics() { if (!navigationDisabled.value) emit('open-topics') }
function back() { if (!navigationDisabled.value) emit('back-to-debate') }

watch([() => currentAuth.value, () => currentAuth.value.state.status, () => currentAuth.value.state.user?.id, () => props.pageSize], () => {
  version++
  history.value = emptyHistory(); page.value = 1
  loading.value = false; loaded.value = false; loadError.value = ''; selectedDebateId.value = ''
  void loadPage(1)
}, { immediate: true, flush: 'sync' })
watch(blocked, value => { if (!value && !loaded.value && !loading.value) void loadPage(1) })
onUnmounted(() => { disposed = true; version++ })
</script>

<template>
  <section class="card history-view" aria-label="历史会话视图" data-view="history" :aria-busy="loading">
    <div class="view-header">
      <h2>本人历史会话</h2>
      <Button :disabled="controlsDisabled" @click="loadPage()">{{ loading ? '加载中…' : loadError ? '重试加载历史' : '刷新历史' }}</Button>
    </div>
    <p class="muted">当前用户：{{ currentAuth.state.user?.username ?? '未确认' }}。仅展示本人的会话，时间按当前设备本地时区显示。</p>
    <p v-if="!authenticated" role="status">请先登录，再查看本人历史会话。</p>
    <p v-else-if="loading" role="status">正在加载历史，第 {{ page }} 页…</p>
    <Alert v-if="loadError" type="error" :title="loadError" role="alert" />
    <template v-if="authenticated && loaded && !loadError && !loading">
      <p v-if="!history.items.length" role="status">{{ history.total ? '本页暂无会话，请刷新或返回上一页。' : '暂无历史会话，前往辩题列表开始一次练习吧。' }}</p>
      <ul v-else class="history-list">
        <li v-for="debate in history.items" :key="debate.id" :data-history-id="debate.id">
          <div class="record-header">
            <h3>{{ debate.topic.title }}</h3>
            <span class="status" :class="{ completed: debate.status === 'completed' }">{{ debate.status === 'completed' ? '已完成' : '进行中' }}</span>
          </div>
          <dl class="record-details">
            <div><dt>你的立场</dt><dd>{{ debate.user_stance === 'pro' ? '正方' : '反方' }}</dd></div>
            <div><dt>AI 立场</dt><dd>{{ debate.ai_stance === 'pro' ? '正方' : '反方' }}</dd></div>
            <div><dt>完成轮数</dt><dd>{{ debate.completed_rounds }} / 3 轮</dd></div>
            <div><dt>创建时间</dt><dd><time :datetime="debate.created_at">{{ localTime(debate.created_at) }}</time></dd></div>
            <div><dt>更新时间</dt><dd><time :datetime="debate.updated_at">{{ localTime(debate.updated_at) }}</time></dd></div>
          </dl>
          <Button type="outline" :data-debate-id="debate.id" :aria-label="`查看会话：${debate.topic.title}`" :disabled="controlsDisabled" @click="selectDebate(debate.id)">
            {{ selectedDebateId === debate.id ? '正在打开…' : '查看会话' }}
          </Button>
        </li>
      </ul>
    </template>
    <nav v-if="authenticated" class="pagination" aria-label="历史分页">
      <Button :disabled="controlsDisabled || page <= 1" @click="previousPage">上一页</Button>
      <span aria-live="polite">第 {{ page }} 页<span v-if="!loading && !loadError"> / 共 {{ pageCount }} 页，共 {{ history.total }} 条</span></span>
      <Button :disabled="controlsDisabled || !!loadError || page >= pageCount" @click="nextPage">下一页</Button>
    </nav>
    <div class="view-actions">
      <Button type="outline" data-nav-debate :disabled="navigationDisabled" @click="back">返回辩论主视图</Button>
      <Button type="outline" data-nav-topics :disabled="navigationDisabled" @click="openTopics">前往辩题列表</Button>
    </div>
  </section>
</template>

<style scoped>
.view-header, .view-actions, .pagination, .record-header { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.view-header, .record-header { justify-content: space-between; }
.view-header h2, .record-header h3 { margin: 0; }
.muted, .record-details { color: #526578; font-size: 13px; line-height: 1.8; }
.history-list { display: grid; gap: 16px; padding: 0; list-style: none; }
.history-list li { padding: 18px; border: 1px solid #e3e8ee; border-radius: 8px; overflow-wrap: anywhere; }
.record-header h3 { font-size: 16px; }
.status { padding: 3px 8px; border-radius: 4px; background: #eef5ff; color: #235b81; }
.status.completed { background: #e8f7ed; color: #24633b; }
.record-details { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 8px 20px; }
.record-details div { min-width: 0; }
.record-details dt { font-weight: 600; }
.record-details dd { margin: 0; }
.view-actions, .pagination { margin-top: 20px; }
</style>
