<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { Button } from '@arco-design/web-vue'
import { createRepracticeApi } from '../../api/repractice'
import { ApiError, type ApiClient } from '../../api/client'
import type { RepracticeRequest, RepracticeVersion } from '../../types/api'
import { USER_CONTENT_MAX_CHARS } from '../../shared/limits'

const props = defineProps<{ client: ApiClient; debateId: string }>()
const api = createRepracticeApi(props.client, props.debateId)
const items = ref<RepracticeVersion[]>([])
type Target = Omit<RepracticeRequest, 'client_request_id' | 'rewrite'>
const target = ref<Target | null>(null)
const pending = ref<RepracticeRequest | null>(null)
const draft = ref('')
const input = ref<HTMLTextAreaElement | null>(null)
const busy = ref(false)
const notice = ref('')
const canSubmit = computed(() => draft.value.trim() && [...draft.value].length <= USER_CONTENT_MAX_CHARS)

async function prepare(value: Target) {
  if (busy.value || pending.value) return
  target.value = value
  notice.value = ''
  await nextTick()
  input.value?.focus()
}

async function load() {
  try {
    items.value = await api.list()
  } catch (error) {
    notice.value = error instanceof ApiError ? error.message : '重练记录读取失败。'
  }
}

async function save(previous?: RepracticeVersion) {
  if (busy.value || (!previous && (!target.value || !canSubmit.value))) return
  busy.value = true
  notice.value = ''
  try {
    if (previous) {
      await api.retry(previous)
    } else if (target.value) {
      pending.value ??= { ...target.value, client_request_id: crypto.randomUUID(), rewrite: draft.value }
      await api.submit(pending.value)
      pending.value = null
      draft.value = ''
      target.value = null
    }
  } catch (error) {
    if (error instanceof ApiError && error.status && error.status < 500) pending.value = null
    notice.value = error instanceof ApiError ? error.message : '操作未确认，请查询记录或重新发送原请求。'
  } finally {
    await load()
    busy.value = false
  }
}

onMounted(load)
defineExpose({ prepare })
</script>

<template>
  <section class="training-repractice" aria-label="局部重练">
    <h3>局部重练</h3>
    <p v-if="notice" role="status">{{ notice }}</p>
    <form v-if="target" @submit.prevent="save()">
      <blockquote>{{ target.quote }}</blockquote>
      <p>{{ target.goal }}</p>
      <label>改写原句<textarea ref="input" v-model="draft" :readonly="busy || Boolean(pending)" rows="4" /></label>
      <Button html-type="submit" :loading="busy" :disabled="!canSubmit || busy">{{ pending ? '重发原请求' : '提交改写' }}</Button>
    </form>
    <p v-else-if="!items.length">从复盘中选择一条可定位的本人原文开始重练。</p>
    <article v-for="item in items" :key="item.id" data-training-repractice-item>
      <h4>改写版本 {{ item.id }}</h4>
      <p>{{ item.rewrite }}</p>
      <template v-if="item.status === 'completed'">
        <p>{{ item.feedback.change }}</p>
        <p>{{ item.feedback.reason }}</p>
        <p>{{ item.feedback.new_problem }}</p>
      </template>
      <template v-else-if="item.status === 'failed'">
        <p>反馈生成失败，改写已保存。</p>
        <Button :disabled="busy" @click="save(item)">重试反馈</Button>
      </template>
      <Button v-else :disabled="busy" @click="load">查询反馈</Button>
    </article>
  </section>
</template>

<style scoped>
.training-repractice { margin-top: 24px; }
label, textarea { display: block; width: 100%; }
textarea { margin: 8px 0; color: var(--app-text); background: var(--app-surface); }
blockquote, article { overflow-wrap: anywhere; }
</style>
