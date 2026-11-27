<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { Button, Modal } from '@arco-design/web-vue'
import { IconBook, IconCompass, IconDashboard, IconFile, IconMenu, IconMute, IconSound, IconUser } from '@arco-design/web-vue/es/icon'
import { authState, type AuthState } from './auth/state'
import LoginForm from './components/auth/LoginForm.vue'
import RegisterForm from './components/auth/RegisterForm.vue'
import TrainingPanel from './components/debate/TrainingPanel.vue'
import FirstTurnPanel from './components/debate/FirstTurnPanel.vue'
import NeonShowcase from './components/debate/NeonShowcase.vue'
import ModelSettings from './views/ModelSettings.vue'
import { arenaSoundEnabled, playArenaSound, toggleArenaSound } from './debate/arenaSound'
import { getCupStatus, type CupStatus } from './api/cup'
import { feedback } from './debate/firstTurn'
import type { Debate } from './types/api'
import HistoryView from './views/HistoryView.vue'
import TopicsView from './views/TopicsView.vue'
import DojoView from './views/DojoView.vue'
import NeonProfileView from './views/NeonProfileView.vue'
import PublicProfileView from './views/PublicProfileView.vue'
import PublicReportView from './components/debate/PublicReportView.vue'
import ProfileAvatar from './components/ProfileAvatar.vue'
import { getPublicProfile, type ProfileIdentity } from './api/profile'

function isDebateRoute(hash: string) {
  return /^#(?:debate|training)=/.test(hash)
}

type View = 'debate' | 'topics' | 'review' | 'dojo' | 'history' | 'public-profile' | 'public-report'
const navigation = [
  { view: 'debate', label: '辩论竞技场', icon: IconDashboard },
  { view: 'topics', label: '辩题与杯赛', icon: IconCompass },
  { view: 'review', label: '深度复盘', icon: IconFile },
  { view: 'dojo', label: '思辨道场', icon: IconBook },
  { view: 'history', label: '个人中心', icon: IconUser },
] as const

function viewFromHash(hash: string): View {
  if (hash === '#topics') return 'topics'
  if (hash === '#history') return 'history'
  if (hash === '#review') return 'review'
  if (hash === '#dojo') return 'dojo'
  if (hash.startsWith('#profile=')) return 'public-profile'
  if (hash.startsWith('#report=')) return 'public-report'
  return hash ? 'debate' : 'topics'
}

const props = defineProps<{ auth?: AuthState }>()
const auth = props.auth ?? authState
const mode = ref<'login' | 'register'>('login')
const restoring = ref(false)
const message = ref('')
const route = ref(location.hash)
const currentView = ref<View>(viewFromHash(location.hash))
const lastDebateRoute = ref(isDebateRoute(location.hash) ? location.hash : '')
const panelVersion = ref(0)
const creating = ref(false)
const creationUncertain = ref(false)
const menuOpen = ref(false)
const focusReport = ref(false)
const settingsOpen = ref(false)
const cup = ref<CupStatus>()
const identity = ref<ProfileIdentity>()
let cupRequest = 0
let active = true

async function refreshCup() {
  const uid = auth.state.user?.id
  if (!uid || auth.state.status !== 'authenticated') return
  const request = ++cupRequest
  try {
    const data = await getCupStatus(auth.client)
    if (active && request === cupRequest && auth.state.user?.id === uid) cup.value = data
  } catch { /* 杯赛积分读取失败时，用户仍可开始普通练习。 */ }
}

watch(() => auth.state.user?.id, () => {
  cup.value = undefined
  identity.value = undefined
  void refreshCup()
  const uid = auth.state.user?.id
  if (uid) {
    void getPublicProfile(auth.client, uid).then(profile => {
      if (active && !identity.value && auth.state.user?.id === uid) identity.value = profile
    }).catch(() => { /* 资料不可用时保留登录名与导航。 */ })
  }
})

function routeChanged() {
  if (creating.value) {
    history.replaceState(null, '', `${location.pathname}${location.search}#topics`)
    return
  }
  const hash = location.hash
  if (isDebateRoute(hash)) lastDebateRoute.value = hash
  route.value = hash
  currentView.value = viewFromHash(hash)
  menuOpen.value = false
  focusReport.value = false
}

function switchView(view: View, showReport = false) {
  if (creating.value) return
  playArenaSound()
  if (isDebateRoute(location.hash)) {
    lastDebateRoute.value = location.hash
  }
  const nextHash = view === 'debate' ? lastDebateRoute.value || '#arena' : `#${view}`
  history.replaceState(null, '', `${location.pathname}${location.search}${nextHash}`)
  route.value = nextHash
  currentView.value = view
  menuOpen.value = false
  focusReport.value = showReport
}

function openDebate(debateOrId: string | Debate, training = false, showReport = false) {
  const debateId = typeof debateOrId === 'string' ? debateOrId : debateOrId.id
  const trainingRoute = typeof debateOrId === 'string' ? training : debateOrId.mode === 'training_v2'
  const nextHash = `#${trainingRoute ? 'training' : 'debate'}=${debateId}`
  lastDebateRoute.value = nextHash
  history.replaceState(null, '', `${location.pathname}${location.search}${nextHash}`)
  route.value = nextHash
  currentView.value = 'debate'
  focusReport.value = showReport
  panelVersion.value++
}

function newPractice() {
  switchView('topics')
}

watch(() => [auth.state.status, auth.state.user?.id] as const, ([status, id], [previous, previousId]) => {
  // 认证成功会先卸载表单，再轮到其完成事件；由共享状态清除旧失效提示。
  if (status === 'authenticated' && previous !== 'authenticated') message.value = ''
  if (previous === 'authenticated' && (status !== 'authenticated' || id !== previousId)) {
    creating.value = false; creationUncertain.value = false
    settingsOpen.value = false
    // 同一用户重新登录时仍可从地址恢复当前会话。
    lastDebateRoute.value = isDebateRoute(location.hash) ? location.hash : ''
    route.value = location.hash
    currentView.value = viewFromHash(location.hash)
    menuOpen.value = false
    focusReport.value = false
    mode.value = 'login'
    message.value = status === 'anonymous' ? '登录已结束，请重新登录。' : ''
  }
})
async function restore() {
  if (restoring.value || auth.state.pending) return
  // 创建会话或视图切换使用 replaceState，不触发 hashchange；重新挂载前同步当前地址。
  routeChanged()
  restoring.value = true
  message.value = ''
  try { await auth.restore() } catch (error) { if (active) message.value = feedback(error) }
  finally { if (active) restoring.value = false }
}
async function logout() {
  if (auth.state.pending || restoring.value) return
  message.value = ''
  try { await auth.logout() }
  catch (error) { if (active) message.value = `${feedback(error)} 退出结果尚未确认，可以恢复登录状态核实。` }
}
onMounted(() => { window.addEventListener('hashchange', routeChanged); void restore() })
onUnmounted(() => { active = false; window.removeEventListener('hashchange', routeChanged) })
</script>

<template>
  <div class="page">
    <header class="masthead" @keydown.esc="menuOpen = false">
      <div class="masthead-inner">
      <a class="brand" href="#topics" @click.prevent="switchView('topics')">
        <span class="brand-seal" aria-hidden="true">弈</span>
        <span class="brand-title">
          <span>思弈阁 <span class="brand-edition">DebateForge</span></span>
          <small>AI 辩论训练与逻辑复盘平台</small>
        </span>
      </a>
      <nav
        v-if="auth.state.status === 'authenticated'"
        id="neon-navigation"
        class="view-nav"
        :class="{ 'is-open': menuOpen }"
        aria-label="主视图导航"
        data-view-nav
      >
        <button
          v-for="item in navigation"
          :key="item.view"
          type="button"
          :data-view-tab="item.view"
          :aria-current="currentView === item.view ? 'page' : undefined"
          :disabled="auth.state.pending || restoring || creating"
          @click="switchView(item.view)"
        ><component :is="item.icon" aria-hidden="true" />{{ item.label }}</button>
      </nav>
      <div class="masthead-actions">
        <div v-if="auth.state.status === 'authenticated'" class="account">
          <a v-if="identity" class="account-identity" href="#history" @click.prevent="switchView('history')"><ProfileAvatar :profile="identity" size="small" /><span>{{ identity.nickname }}</span></a>
          <span v-else class="account-name">{{ auth.state.user?.username }}</span>
          <span v-if="cup" class="elo-badge" aria-label="当前 ELO">ELO {{ cup.elo }}</span>
          <Button :disabled="auth.state.pending || restoring || creating" @click="logout">退出登录</Button>
        </div>
        <Button class="sound-button" :aria-label="arenaSoundEnabled ? '音效开' : '音效关'" :aria-pressed="arenaSoundEnabled" @click="toggleArenaSound">
          <IconSound v-if="arenaSoundEnabled" aria-hidden="true" />
          <IconMute v-else aria-hidden="true" />
        </Button>
        <Button v-if="auth.state.status === 'authenticated'" class="model-button" :disabled="creating" @click="settingsOpen = true">接入模型</Button>
        <Button v-if="currentView !== 'review'" class="showcase-button" :disabled="creating" @click="switchView('review')">看完整复盘示例</Button>
        <Button
          v-if="auth.state.status === 'authenticated'"
          class="menu-toggle"
          aria-label="菜单"
          aria-controls="neon-navigation"
          :aria-expanded="menuOpen"
          :disabled="creating"
          @click="menuOpen = !menuOpen"
        ><IconMenu aria-hidden="true" /></Button>
      </div>
      </div>
    </header>
    <main :class="{
      'wide-main': auth.state.status === 'authenticated' || ['review', 'public-profile', 'public-report'].includes(currentView),
      'guest-main': auth.state.status === 'anonymous' && !['review', 'public-profile', 'public-report'].includes(currentView),
    }">
      <div v-if="currentView !== 'review' && currentView !== 'public-profile' && currentView !== 'public-report' && auth.state.status !== 'authenticated'" class="intro">
        <h1>辩论训练</h1>
        <p>选择一个辩题，从第一轮表达开始练习。</p>
      </div>
      <p v-if="message" class="page-feedback" role="alert">{{ message }}</p>
      <section v-if="currentView === 'review'" aria-label="示例复盘">
        <div class="showcase-actions">
          <Button v-if="auth.state.status === 'authenticated' && lastDebateRoute" @click="switchView('debate', true)">返回本场</Button>
          <Button v-else @click="switchView('topics')">{{ auth.state.status === 'authenticated' ? '选择辩题' : '返回登录' }}</Button>
        </div>
        <NeonShowcase />
      </section>
      <PublicProfileView v-else-if="currentView === 'public-profile'" :key="route" :user-id="route.slice('#profile='.length)" :client="auth.client" />
      <PublicReportView v-else-if="currentView === 'public-report'" :key="route" :share-id="route.slice('#report='.length)" @close="switchView('topics')" />
      <section v-else-if="restoring || auth.state.status === 'unknown'" class="card" :aria-busy="restoring" aria-live="polite">
        <h2>{{ restoring ? '正在恢复登录状态…' : '登录状态尚未确认' }}</h2>
        <p>请先确认登录状态，再继续练习。</p>
        <Button :disabled="restoring" @click="restore">恢复登录状态</Button>
      </section>
      <template v-else-if="auth.state.status === 'anonymous'">
        <LoginForm v-if="mode === 'login'" :auth="auth" @show-register="mode = 'register'; message = ''" @authenticated="message = ''" />
        <RegisterForm v-else :auth="auth" @show-login="mode = 'login'; message = ''" />
      </template>
      <template v-else>
        <Button v-if="message" class="restore-button" :disabled="auth.state.pending" @click="restore">恢复登录状态</Button>
        <DojoView v-if="currentView === 'dojo'" :key="`dojo:${auth.state.user?.id}`" :client="auth.client" @rating-changed="refreshCup" />
        <!-- 结果不明时保留创建面板，切页不能重置提交锁；成功后正常卸载。 -->
        <TopicsView
          v-if="currentView === 'topics' || creating || creationUncertain"
          v-show="currentView === 'topics'"
          :key="`topics:${auth.state.user?.id}`"
          :auth="auth"
          :cup="cup"
          :disabled="auth.state.pending || restoring || creating"
          @creating="creating = $event"
          @uncertain="creationUncertain = $event"
          @created="openDebate"
          @open-history="switchView('history')"
          @back-to-debate="switchView(lastDebateRoute ? 'debate' : 'topics')"
        />
        <NeonProfileView v-if="currentView === 'history'" :key="`profile:${auth.state.user?.id}`" :client="auth.client" @updated="identity = $event" />
        <HistoryView
          v-if="currentView === 'history'"
          :key="`history:${auth.state.user?.id}`"
          :auth="auth"
          :page-size="10"
          :disabled="auth.state.pending || restoring || creating"
          @select-debate="openDebate"
          @open-topics="switchView('topics')"
          @back-to-debate="switchView(lastDebateRoute ? 'debate' : 'topics')"
        />
        <TrainingPanel
          v-if="currentView === 'debate' && route.startsWith('#training=')"
          :key="`${auth.state.user?.id}:${route}:${panelVersion}`"
          :auth="auth"
          :route="route"
          @new-practice="newPractice"
          @open-history="switchView('history')"
        />
        <FirstTurnPanel
          v-else-if="currentView === 'debate'"
          :key="`${auth.state.user?.id}:${route}:${panelVersion}`"
          :auth="auth"
          :route="route === '#arena' ? '' : route"
          :focus-report="focusReport"
          @new-practice="newPractice"
          @open-history="switchView('history')"
          @cup-changed="refreshCup"
        />
      </template>
      <p class="scope-note">AI 生成内容仅供练习参考，请勿输入个人敏感信息。支持四轮标准赛制文字辩论与结构化复盘总结。</p>
    </main>
    <Modal v-if="settingsOpen" :visible="true" title="接入模型" modal-class="neon-model-dialog" :footer="false" width="min(640px, calc(100vw - 24px))" @cancel="settingsOpen = false">
      <ModelSettings :client="auth.client" />
    </Modal>
  </div>
</template>

<style>
:root { font-family: Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif; font-synthesis: none; color-scheme: light; }
* { box-sizing: border-box; }
body { margin: 0; min-width: 320px; color: var(--app-text); background: var(--app-background); }
.page { min-height: 100vh; }
.masthead { background: rgb(255 255 255 / 90%); backdrop-filter: blur(12px); border-bottom: 1px solid #e7e5e4; position: sticky; top: 0; z-index: 20; }
.masthead-inner { width: min(1280px, calc(100% - 32px)); min-height: 56px; margin: auto; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.masthead-actions, .account { display: flex; align-items: center; gap: 6px; min-width: 0; }
.masthead-actions { margin-inline-start: auto; }
.account-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 12ch; font-size: 12px; }
.account-identity { display: inline-flex; gap: 6px; align-items: center; color: var(--app-text); text-decoration: none; min-width: 0; }
.account-identity > span:last-child { max-width: 12ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.account > .elo-badge { color: #78350f; background: #fffbeb; border: 1px solid #fde68a; border-radius: 999px; padding: 4px 9px; white-space: nowrap; font-size: 12px; font-weight: 650; }
.brand { display: inline-flex; align-items: center; gap: 10px; color: var(--app-text); text-decoration: none; flex-shrink: 0; }
.brand-seal { display: grid; place-items: center; border-radius: 8px; width: 32px; height: 32px; background: #c9362b; color: white; font: 700 16px var(--app-serif); box-shadow: 0 1px 2px #18181b22; }
.brand-title { font: 700 16px/1.2 var(--app-serif); letter-spacing: .02em; }
.brand-edition { display: inline-block; padding: 2px 5px; border: 1px solid #e7e5e4; border-radius: 4px; color: #57534e; background: #f5f5f4; font: 500 10px system-ui, sans-serif; vertical-align: middle; }
.brand-title small { display: block; margin-top: 3px; color: #78716c; font: 11px system-ui, sans-serif; }
.masthead-actions .arco-btn { min-height: 32px; height: auto; padding: 5px 9px; border-radius: 8px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.masthead-actions .sound-button { color: #57534e; background: transparent; font-size: 16px; }
.masthead-actions .model-button { border: 1px solid #e7e5e4; background: #fff; color: #44403c; }
.masthead-actions .showcase-button { background: #18181b; color: white; }
.masthead-actions .arco-btn:not([disabled]):hover { filter: brightness(.94); }
main { width: min(880px, calc(100% - 32px)); margin: 28px auto 48px; }
main.wide-main { width: min(1280px, calc(100% - 32px)); margin-top: 24px; }
main.guest-main { width: min(440px, calc(100% - 32px)); margin-top: clamp(24px, 8vh, 80px); }
.guest-main .intro { text-align: center; margin-bottom: 22px; }
.neon-model-dialog { overflow: hidden; border-radius: var(--app-radius-panel); box-shadow: 0 16px 48px #29252430; }
/* Arco 锁滚动时写入打开瞬间的 body 像素宽度，保留随视口变化的宽度。 */
body:has(.neon-model-dialog) { width: auto !important; }
.showcase-actions { margin-bottom: 12px; }
.intro { margin-bottom: 32px; }
h1 { margin: 12px 0; font: 700 clamp(26px, 4vw, 36px)/1.4 var(--app-serif); }
h2 { margin-top: 0; font-size: 21px; }
p { line-height: 1.8; }
.intro > p:last-child, .scope-note { color: var(--app-text-muted); }
.card { padding: 28px; border: 1px solid #e5e1d9; border-radius: var(--app-radius-panel); background: #fff; box-shadow: 0 2px 10px rgba(55,45,33,.035); margin-bottom: 24px; }
.page-feedback { color: var(--app-error-text); padding: 12px 16px; background: var(--app-error-bg); border-radius: var(--app-radius-control); overflow-wrap: anywhere; }
.scope-note { font-size: 13px; margin-top: 24px; }
.restore-button { margin-bottom: 16px; }
.view-nav { display: flex; gap: 2px; padding: 4px; border: 1px solid #e5e1d9; border-radius: 12px; background: #f5f3ef; }
.view-nav button { display: flex; align-items: center; gap: 5px; padding: 6px 9px; min-height: 32px; border: 0; border-radius: 8px; color: #57534e; background: transparent; white-space: nowrap; font-size: 13px; }
.view-nav button:hover:not([disabled]) { color: var(--app-text); }
.view-nav button svg { font-size: 14px; }
.view-nav button[aria-current='page'] { color: var(--app-text); background: var(--app-surface); font-weight: 700; box-shadow: 0 1px 3px #18181b26; }
.masthead-actions .menu-toggle { display: none; }
button, select, textarea { font: inherit; }
.arco-btn, select { min-height: 44px; }
button, select { cursor: pointer; }
button:disabled, select:disabled, textarea:disabled { cursor: not-allowed; }
button:focus-visible, select:focus-visible, textarea:focus-visible, a:focus-visible { outline: 3px solid var(--app-accent); outline-offset: 3px; }
@media (max-width: 1199px) {
  .masthead-inner { flex-wrap: wrap; gap: 8px; padding-block: 7px; }
  .masthead-actions .menu-toggle { display: inline-flex; font-size: 18px; min-width: 40px; min-height: 40px; }
  .view-nav { display: none; order: 5; width: 100%; }
  .view-nav.is-open { display: grid; }
  .view-nav button { min-height: 44px; padding-inline: 12px; text-align: left; }
}
@media (max-width: 600px) {
  .masthead-inner { width: calc(100% - 24px); }
  .brand-title small, .brand-edition { display: none; }
  .masthead-actions { flex-wrap: wrap; justify-content: flex-end; }
  .masthead-actions .arco-btn { min-height: 38px; }
  .masthead-actions:has(.menu-toggle) .showcase-button { display: none; }
  .account-identity > span:last-child, .account-name { display: none; }
  .account { order: 1; }
  main { margin: 22px auto; }
  main.wide-main { width: calc(100% - 24px); margin-top: 16px; }
  .card { padding: 18px 15px; }
}
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; } }
</style>
