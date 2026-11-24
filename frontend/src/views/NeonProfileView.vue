<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { ApiClient } from '../api/client'
import { getLeaderboard, getProfile, saveProfile, type ProfileIdentity } from '../api/profile'
import ProfileAvatar from '../components/ProfileAvatar.vue'
import ProfileStats from '../components/ProfileStats.vue'

const props = defineProps<{ client: ApiClient }>()
const emit = defineEmits<{ updated: [profile: ProfileIdentity] }>()
const profile = ref<Awaited<ReturnType<typeof getProfile>>>()
const leaderboard = ref<Awaited<ReturnType<typeof getLeaderboard>>>()
const draft = ref<ProfileIdentity>()
const loadError = ref(false)
const rankError = ref(false)
const leaderboardOpen = ref(false)
const rankPage = ref(1)
const loadingRanks = ref(false)
const saving = ref(false)
const readingImage = ref(false)
const editError = ref('')
const notice = ref('')
let imageRequest = 0

const points = computed(() => {
  const history = profile.value?.elo_history ?? []
  const lowest = Math.min(...history.map(event => event.elo))
  const highest = Math.max(...history.map(event => event.elo))
  const spread = Math.max(highest - lowest, 1)
  return history.map((event, i) => ({
    x: 12 + i / Math.max(history.length - 1, 1) * 456,
    y: highest === lowest ? 48 : 84 - (event.elo - lowest) / spread * 72,
    elo: event.elo,
    timestamp: event.created_at,
  }))
})
const trend = computed(() => points.value.map(point => `${point.x},${point.y}`).join(' '))
const rankPages = computed(() => Math.max(1, Math.ceil((leaderboard.value?.total ?? 0) / 10)))

async function load() {
  loadError.value = false
  try { profile.value = await getProfile(props.client) } catch { loadError.value = true }
}
async function loadRanks(page: number) {
  if (loadingRanks.value) return
  loadingRanks.value = true
  rankError.value = false
  rankPage.value = page
  try { leaderboard.value = await getLeaderboard(props.client, page) }
  catch { rankError.value = true }
  finally { loadingRanks.value = false }
}
function toggleRankings() {
  leaderboardOpen.value = !leaderboardOpen.value
  if (leaderboardOpen.value) void loadRanks(rankPage.value)
}
function beginEdit() {
  if (!profile.value) return
  const { nickname, avatar_kind, avatar_seal, avatar_image } = profile.value
  draft.value = { nickname, avatar_kind, avatar_seal, avatar_image }
  editError.value = notice.value = ''
}
function cancelEdit() {
  imageRequest++
  readingImage.value = false
  draft.value = undefined
  editError.value = ''
}
function selectKind(kind: ProfileIdentity['avatar_kind']) {
  if (!draft.value || draft.value.avatar_kind === kind) return
  imageRequest++
  readingImage.value = false
  editError.value = ''
  draft.value = { nickname: draft.value.nickname, avatar_kind: kind, avatar_seal: kind === 'seal' ? '辩' : '', avatar_image: '' }
}
async function chooseImage(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !draft.value) return
  input.value = ''
  editError.value = ''
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type) || file.size > 5 * 1024 * 1024) {
    editError.value = '请选择不超过 5 MB 的 JPG、PNG 或 WebP 图片。'
    return
  }
  const version = ++imageRequest
  readingImage.value = true
  try {
    const bitmap = await createImageBitmap(file)
    try {
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 128
      const context = canvas.getContext('2d')
      if (!context) throw new Error('canvas unavailable')
      const side = Math.min(bitmap.width, bitmap.height)
      context.fillStyle = '#fff'
      context.fillRect(0, 0, 128, 128)
      context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, 128, 128)
      const image = canvas.toDataURL('image/jpeg', 0.75)
      if (draft.value && imageRequest === version) draft.value.avatar_image = image
    } finally { bitmap.close() }
  } catch {
    if (imageRequest === version) editError.value = '图片读取失败，请换一张再试。'
  } finally {
    if (imageRequest === version) readingImage.value = false
  }
}
async function submit() {
  if (!draft.value || !profile.value || saving.value || readingImage.value) return
  if (draft.value.avatar_kind === 'image' && !draft.value.avatar_image) {
    editError.value = '请先选择一张头像图片。'
    return
  }
  saving.value = true
  editError.value = ''
  try {
    const identity = await saveProfile(props.client, draft.value)
    profile.value = { ...profile.value, ...identity }
    emit('updated', identity)
    cancelEdit()
    notice.value = '资料已保存。'
    if (leaderboardOpen.value) void loadRanks(rankPage.value)
  } catch { editError.value = '资料未保存，请检查昵称和头像后重试。' }
  finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <section class="card profile-card" aria-label="个人档案">
    <h1>个人中心</h1>
    <template v-if="profile">
      <div class="profile-heading">
        <ProfileAvatar :profile="profile" size="large" />
        <div><h2>{{ profile.nickname }}</h2><small>正式战绩 {{ profile.games }} 局 · {{ profile.wins }} 胜</small></div>
        <button v-if="!draft" class="profile-button" type="button" @click="beginEdit">编辑资料</button>
      </div>
      <ProfileStats :profile="profile" />
      <p v-if="notice" role="status">{{ notice }}</p>
      <form v-if="draft" class="profile-form" @submit.prevent="submit">
        <label>昵称 <input v-model.trim="draft.nickname" maxlength="24" required :disabled="saving" /></label>
        <fieldset :disabled="saving">
          <legend>头像</legend>
          <label><input type="radio" name="avatar-kind" :checked="draft.avatar_kind === 'seal'" @change="selectKind('seal')" /> 印章</label>
          <label><input type="radio" name="avatar-kind" :checked="draft.avatar_kind === 'image'" @change="selectKind('image')" /> 图片</label>
        </fieldset>
        <label v-if="draft.avatar_kind === 'seal'">印章文字 <input v-model.trim="draft.avatar_seal" required maxlength="2" :disabled="saving" /></label>
        <label v-else>头像图片 <input type="file" accept="image/jpeg,image/png,image/webp" :disabled="saving" @change="chooseImage" /></label>
        <ProfileAvatar v-if="draft.avatar_kind === 'seal' || draft.avatar_image" :profile="draft" />
        <p v-if="editError" role="alert">{{ editError }}</p>
        <div class="profile-controls">
          <button class="profile-button primary" type="submit" :disabled="saving || readingImage">保存资料</button>
          <button class="profile-button" type="button" :disabled="saving" @click="cancelEdit">取消</button>
        </div>
      </form>
      <div class="profile-insights">
        <section>
          <h3>ELO 成长</h3>
          <figure v-if="points.length">
            <svg viewBox="0 0 480 96" role="img" :aria-label="`ELO 从 ${points[0]?.elo} 变为 ${points.at(-1)?.elo}`">
              <path d="M12 48H468 M12 84H468" class="chart-lines" />
              <polyline :points="trend" class="chart-trend" />
              <circle v-for="point in points" :key="point.x" :cx="point.x" :cy="point.y" r="3" class="chart-point">
                <title>{{ new Date(point.timestamp).toLocaleDateString('zh-CN') }} · ELO {{ point.elo }}</title>
              </circle>
            </svg>
            <figcaption>{{ points[0]?.elo }} → {{ points.at(-1)?.elo }}</figcaption>
          </figure>
          <p v-else class="muted">完成积分结算或答对道场题目后，这里会显示积分变化。</p>
        </section>
        <section>
          <h3>常见漏洞</h3>
          <ul v-if="profile.vulnerabilities.length">
            <li v-for="item in profile.vulnerabilities" :key="item.label">{{ item.label }} <span>{{ item.count }} 次</span></li>
          </ul>
          <p v-else class="muted">暂未找到可统计的个人批注。</p>
        </section>
      </div>
    </template>
    <div v-else-if="loadError"><p role="alert">个人档案读取失败。</p><button class="profile-button" @click="load">重试</button></div>
    <p v-else role="status">正在读取个人档案…</p>
  </section>

  <section class="card profile-card" aria-labelledby="elo-leaderboard">
    <div class="profile-heading">
      <h2 id="elo-leaderboard">ELO 思辨排行榜</h2>
      <button class="profile-button" type="button" aria-controls="rankings-content" :aria-expanded="leaderboardOpen" @click="toggleRankings">{{ leaderboardOpen ? '收起排行榜' : '展开排行榜' }}</button>
    </div>
    <div v-if="leaderboardOpen" id="rankings-content">
      <p v-if="loadingRanks" role="status">正在读取排行榜…</p>
      <p v-else-if="rankError" role="alert">排行榜读取失败。 <button class="profile-button" type="button" @click="loadRanks(rankPage)">重试</button></p>
      <template v-else-if="leaderboard">
        <ol v-if="leaderboard.items.length" class="ranks">
          <li v-for="ranked in leaderboard.items" :key="ranked.user_id" :data-ranked-user="ranked.user_id">
            <span class="rank-no">{{ ranked.rank }}</span>
            <ProfileAvatar :profile="ranked" size="small" />
            <a :href="`#profile=${ranked.user_id}`">{{ ranked.nickname }}</a>
            <strong>{{ ranked.elo }}</strong>
            <span class="muted">胜率 {{ ranked.win_rate.toFixed(1) }}%</span>
            <a :href="`#profile=${ranked.user_id}`" :aria-label="`查看主页 · ${ranked.nickname}`">查看主页</a>
          </li>
        </ol>
        <p v-else>本页暂无辩手。</p>
      </template>
      <nav class="profile-controls" aria-label="排行榜分页">
        <button class="profile-button" type="button" :disabled="rankPage <= 1 || loadingRanks" @click="loadRanks(rankPage - 1)">上一页</button>
        <span>第 {{ rankPage }} / {{ rankPages }} 页</span>
        <button class="profile-button" type="button" :disabled="rankPage >= rankPages || loadingRanks || rankError" @click="loadRanks(rankPage + 1)">下一页</button>
      </nav>
    </div>
  </section>
</template>

<style scoped>
.profile-heading { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.profile-heading h2, .profile-heading small { display: block; margin: 0; overflow-wrap: anywhere; }
.profile-heading > button { margin-left: auto; }
.profile-button { min-height: 42px; padding: 8px 12px; border: 1px solid var(--app-border); border-radius: 7px; background: var(--app-surface); color: var(--app-text); }
.profile-button.primary { background: var(--app-pro); color: #fff; border-color: var(--app-pro); }
.profile-form { display: grid; gap: 12px; margin-top: 18px; padding: 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); }
.profile-form input:not([type=radio]) { display: block; max-width: 100%; margin-top: 4px; padding: 8px; border: 1px solid var(--app-border); border-radius: 6px; }
.profile-form fieldset { border: 0; padding: 0; }
.profile-form fieldset label { margin-right: 14px; }
.profile-insights { display: grid; grid-template-columns: 1.25fr 1fr; gap: 18px; margin-top: 14px; }
.profile-insights h3 { font: 700 17px var(--app-serif); }
.profile-insights ul, .ranks { list-style: none; padding: 0; }
.profile-insights li { display: flex; gap: 6px; justify-content: space-between; padding: 8px 4px; border-bottom: 1px solid var(--app-border); }
.profile-insights li span { white-space: nowrap; color: var(--app-text-muted); }
figure { margin: 0; }
figure svg { display: block; width: 100%; background: var(--app-background); border-radius: 7px; }
.chart-lines { stroke: var(--app-border); }
.chart-trend { stroke: var(--app-pro); stroke-width: 2.5; fill: none; }
.chart-point { fill: var(--app-pro); }
figcaption, .muted { color: var(--app-text-muted); font-size: 13px; }
.ranks li { display: grid; grid-template-columns: 2em 30px minmax(0,1fr) minmax(35px,auto) auto auto; gap: 10px; align-items: center; min-height: 48px; border-bottom: 1px solid var(--app-border); }
.rank-no { font-variant-numeric: tabular-nums; color: var(--app-text-muted); }
.ranks li a { overflow-wrap: anywhere; }
.profile-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
@media (max-width: 550px) { .profile-insights { grid-template-columns: 1fr; } .ranks li { gap: 6px; grid-template-columns: 18px 30px minmax(0,1fr) 42px auto; font-size: 13px; } .ranks li .muted { display: none; } }
</style>
