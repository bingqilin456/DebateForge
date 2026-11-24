<script setup lang="ts">
import { ref, watch } from 'vue'
import { listPublicReports, publicReportLink, type PublicReportSummary } from '../../api/publicReports'

const props = defineProps<{ topicId: string }>()
const expanded = ref(false)
const items = ref<PublicReportSummary[]>([])
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const error = ref(false)
const pageSize = 10
const viewVersion = ref(0)

watch(() => props.topicId, () => { page.value = 1; viewVersion.value++ })
watch([expanded, page, viewVersion], async ([open, selectedPage], _, onCleanup) => {
  if (!open) return
  let active = true
  onCleanup(() => { active = false })
  items.value = []
  loading.value = true
  error.value = false
  try {
    const data = await listPublicReports({ topicId: props.topicId, page: selectedPage, pageSize })
    if (active) {
      items.value = data.items
      total.value = data.total
    }
  } catch {
    if (active) error.value = true
  } finally {
    if (active) loading.value = false
  }
})
</script>

<template>
  <details class="past-reports" @toggle="expanded = ($event.target as HTMLDetailsElement).open">
    <summary>往期复盘</summary>
    <p v-if="loading" role="status">正在读取公开复盘…</p>
    <p v-else-if="error" role="status">暂时无法读取。<button type="button" @click="viewVersion++">重试</button></p>
    <p v-else-if="!items.length">暂无公开复盘。</p>
    <ul v-else>
      <li v-for="item in items" :key="item.id">
        <a :href="publicReportLink(item.id)">{{ item.title }}</a>
        <small>{{ new Date(item.published_at).toLocaleString() }}</small>
      </li>
    </ul>
    <div v-if="(page > 1 || total > pageSize) && !error">
      <button type="button" :disabled="page <= 1 || loading" @click="page--">上一页</button>
      <span>第 {{ page }} 页</span>
      <button type="button" :disabled="page * pageSize >= total || loading" @click="page++">下一页</button>
    </div>
  </details>
</template>

<style scoped>
.past-reports { margin-top: 8px; }
.past-reports summary { cursor: pointer; }
.past-reports ul { margin: 8px 0; padding-left: 18px; }
.past-reports li, .past-reports div { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; margin-top: 4px; }
</style>
