<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getPublicProfile, type PublicProfile } from '../api/profile'
import type { ApiClient } from '../api/client'
import { listPublicReports } from '../api/publicReports'
import ProfileAvatar from '../components/ProfileAvatar.vue'
import ProfileStats from '../components/ProfileStats.vue'

const props = defineProps<{ userId: string; client: ApiClient }>()
const profile = ref<PublicProfile>()
const reports = ref<Awaited<ReturnType<typeof listPublicReports>>>()
const failed = ref(false)
const reportsFailed = ref(false)
const loadingReports = ref(false)

async function loadReports(page: number) {
  loadingReports.value = true
  reportsFailed.value = false
  try { reports.value = await listPublicReports({ ownerId: props.userId, page }) }
  catch { reportsFailed.value = true }
  finally { loadingReports.value = false }
}
onMounted(async () => {
  try { profile.value = await getPublicProfile(props.client, props.userId) }
  catch { failed.value = true; return }
  void loadReports(1)
})
</script>

<template>
  <section class="card" aria-label="公开主页">
    <a href="#history">返回个人中心</a>
    <template v-if="profile">
      <div class="public-owner"><ProfileAvatar :profile="profile" size="large" /><h1>{{ profile.nickname }}</h1></div>
      <ProfileStats :profile="profile" />
      <div class="public-reports">
        <h2>已公开复盘</h2>
        <p v-if="loadingReports" role="status">正在读取公开复盘…</p>
        <p v-else-if="reportsFailed" role="alert">公开复盘读取失败。 <button type="button" @click="loadReports(reports?.page ?? 1)">重试</button></p>
        <template v-else-if="reports">
          <ul v-if="reports.items.length">
            <li v-for="report in reports.items" :key="report.id"><a :href="`#report=${report.id}`">{{ report.title }}</a></li>
          </ul>
          <p v-else>暂无公开复盘。</p>
          <nav v-if="reports.total > reports.page_size" aria-label="公开复盘分页">
            <button type="button" :disabled="reports.page <= 1" @click="loadReports(reports.page - 1)">上一页</button>
            <span>第 {{ reports.page }} 页</span>
            <button type="button" :disabled="reports.page * reports.page_size >= reports.total" @click="loadReports(reports.page + 1)">下一页</button>
          </nav>
        </template>
      </div>
    </template>
    <p v-else-if="failed" role="alert">该公开主页无法访问。</p>
    <p v-else role="status">正在读取公开主页…</p>
  </section>
</template>

<style scoped>
.public-owner { display: flex; align-items: center; gap: 14px; margin-top: 14px; overflow-wrap: anywhere; }
.public-reports { margin-top: 24px; }
.public-reports li { padding: 5px 0; overflow-wrap: anywhere; }
.public-reports nav { display: flex; align-items: center; gap: 10px; }
.public-reports button { min-height: 36px; }
.card[aria-label="公开主页"] { border-color: #e5e0da; background: #fff; box-shadow: 0 5px 18px rgba(55,45,33,.05); }
.public-owner h1 { font-family: Georgia, var(--app-serif); }
</style>
