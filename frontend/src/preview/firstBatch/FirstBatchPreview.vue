<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { Button } from '@arco-design/web-vue'
import type { ReviewQuote, ReviewStatus } from '../../types/api'
import ReviewPanel from '../../components/debate/ReviewPanel.vue'
import IssueCardPreview from '../issueCard/IssueCardPreview.vue'
import type { IssueCardPreviewMock } from '../issueCard/mockIssueCard'
import { demoReview } from './demoReview'

type Source = { messageId: string; start: number; end: number; quote: string }
type Version = { id: number; source: Source; rewrite: string; goal: string; status: 'failed' | 'completed' }
const props = defineProps<{ mock: IssueCardPreviewMock }>()
const preview = ref<InstanceType<typeof IssueCardPreview> | null>(null)
const editor = ref<HTMLTextAreaElement | null>(null)
const practice = ref<HTMLElement | null>(null)
const source = ref<Source | null>(null)
const rewrite = ref('')
const goal = ref('')
const failure = ref(false)
const reviewFailure = ref(false)
const reviewKey = 'siyige:first-batch:review'
const versionsKey = 'siyige:first-batch:versions'
const initialReview: ReviewStatus = { status: 'not_started', attempt_version: null }
function load<T>(key: string, fallback: T): T {
  try { return JSON.parse(sessionStorage.getItem(key) ?? 'null') as T ?? fallback }
  catch { return fallback }
}
const versions = ref(load<Version[]>(versionsKey, []))
const review = ref(load<ReviewStatus>(reviewKey, initialReview))
const canSubmit = computed(() => !!source.value && /[^\p{White_Space}\ufeff]/u.test(rewrite.value) && [...rewrite.value].length <= 2000 && [...goal.value].length <= 2000)

async function selectSource(value: Source) {
  source.value = value
  rewrite.value = ''
  await nextTick()
  editor.value?.focus()
}
function submit() {
  if (!canSubmit.value || !source.value) return
  versions.value.push({ id: versions.value.length + 1, source: source.value, rewrite: rewrite.value, goal: goal.value, status: failure.value ? 'failed' : 'completed' })
  failure.value = false
  sessionStorage.setItem(versionsKey, JSON.stringify(versions.value))
  rewrite.value = ''
}
function retry(version: Version) {
  if (version.status !== 'failed') return
  if (!failure.value) version.status = 'completed'
  failure.value = false
  sessionStorage.setItem(versionsKey, JSON.stringify(versions.value))
}
function saveReview() {
  if (review.value.status === 'completed') return
  const attempt_version = (review.value.attempt_version ?? 0) + 1
  review.value = reviewFailure.value
    ? { status: 'failed', attempt_version, error_code: 'model_timeout' }
    : { status: 'completed', attempt_version, schema_version: 1, content: demoReview(props.mock.contents) }
  reviewFailure.value = false
  sessionStorage.setItem(reviewKey, JSON.stringify(review.value))
}
function prepareGoal(improvement: string, citation: ReviewQuote | null) {
  goal.value = improvement
  const match = citation?.resolved && citation.start_cp !== null && citation.end_cp !== null
    && props.mock.messages.some(message => message.id === citation.message_id && message.role === 'user')
    && Array.from(props.mock.contents[citation.message_id] ?? '').slice(citation.start_cp, citation.end_cp).join('') === citation.quote
  if (match && citation?.start_cp != null && citation.end_cp != null) {
    void selectSource({ messageId: citation.message_id, start: citation.start_cp, end: citation.end_cp, quote: citation.quote })
  } else if (source.value) editor.value?.focus()
  else practice.value?.focus()
}
function locate(messageId: string, start: number, end: number) {
  preview.value?.locate(`${messageId}|${start}|${end}`)
}
</script>

<template>
  <IssueCardPreview ref="preview" :mock="mock" integrated @select-source="selectSource">
    <template #after>
      <section ref="practice" class="practice" tabindex="-1" aria-labelledby="practice-heading">
        <h2 id="practice-heading">局部重练</h2>
        <p>在上方选中自己发言的一句话，再点「选句重练」。练习不会修改本场原文。</p>
        <form v-if="source" class="editor" @submit.prevent="submit">
          <div><strong>你选的原文</strong><blockquote data-practice-quote>{{ source.quote }}</blockquote></div>
          <label>这次想改进什么（选填） <input v-model="goal" /></label>
          <label>你的改写 <textarea ref="editor" v-model="rewrite" rows="4" aria-describedby="rewrite-count" /></label>
          <small id="rewrite-count">{{ [...rewrite].length }} / 2000 字</small>
          <Button html-type="submit" type="primary" :disabled="!canSubmit">提交改写，查看反馈</Button>
        </form>
        <label class="demo-option"><input v-model="failure" type="checkbox" /> 演示下一次重练反馈失败</label>
        <article v-for="version in [...versions].reverse()" :key="version.id" class="version" data-practice-version>
          <h3>练习版本 {{ version.id }}</h3>
          <p><strong>原文：</strong>{{ version.source.quote }}</p>
          <p v-if="version.goal"><strong>改进目标：</strong>{{ version.goal }}</p>
          <p><strong>改写：</strong><span>{{ version.rewrite }}</span></p>
          <template v-if="version.status === 'failed'">
            <p role="status">反馈未生成，改写已保留。</p>
            <Button @click="retry(version)">重试这次反馈</Button>
          </template>
          <template v-else>
            <p><strong>是否改善：</strong>示例反馈：这版可以检查主张是否比原文更具体。</p>
            <p><strong>理由：</strong>写出双方如何判断，读者才知道标准如何使用。</p>
            <p><strong>留意：</strong>举例不要替代标准本身。</p>
          </template>
        </article>
      </section>
      <section aria-label="整场复盘">
        <label class="demo-option" v-if="review.status !== 'completed'"><input v-model="reviewFailure" type="checkbox" /> 演示下一次复盘失败</label>
        <ReviewPanel :review="review" :can-generate="review.status === 'not_started'" :can-retry="review.status === 'failed'"
          :locate="locate" :repractice="prepareGoal" @generate="saveReview" @retry="saveReview" @refresh="review = load(reviewKey, initialReview)" />
      </section>
    </template>
  </IssueCardPreview>
</template>

<style scoped>
.practice { padding: 20px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-surface); }
.practice h2 { margin: 0; }
.editor { display: grid; gap: 10px; max-width: 760px; margin-block: 16px; }
.editor label { display: grid; gap: 6px; }
.editor input, .editor textarea { width: 100%; box-sizing: border-box; padding: 10px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); background: var(--app-background); color: inherit; font: inherit; }
blockquote { margin: 8px 0; border-inline-start: 3px solid var(--app-accent); padding-inline-start: 12px; }
.demo-option { display: inline-flex; align-items: center; gap: 8px; min-height: 44px; }
.version { margin-top: 12px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); padding: 12px; overflow-wrap: anywhere; }
.version h3 { margin: 0; }
.version span, blockquote { white-space: pre-wrap; }
</style>
