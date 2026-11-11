<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { Button } from '@arco-design/web-vue'
import { authState, type AuthState } from './auth/state'
import LoginForm from './components/auth/LoginForm.vue'
import RegisterForm from './components/auth/RegisterForm.vue'
import FirstTurnPanel from './components/debate/FirstTurnPanel.vue'
import { feedback } from './debate/firstTurn'

const props = defineProps<{ auth?: AuthState }>()
const auth = props.auth ?? authState
const mode = ref<'login' | 'register'>('login')
const restoring = ref(false)
const message = ref('')
const route = ref(location.hash)
const panelVersion = ref(0)
let active = true
function routeChanged() { route.value = location.hash }
function newPractice() {
  history.replaceState(null, '', location.pathname + location.search)
  route.value = ''
  panelVersion.value++
}
watch(() => [auth.state.status, auth.state.user?.id] as const, ([status, id], [previous, previousId]) => {
  // 认证成功会先卸载表单，再轮到其完成事件；由共享状态清除旧失效提示。
  if (status === 'authenticated' && previous !== 'authenticated') message.value = ''
  if (previous === 'authenticated' && (status !== 'authenticated' || id !== previousId)) {
    history.replaceState(null, '', location.pathname + location.search)
    route.value = ''
    mode.value = 'login'
    message.value = status === 'anonymous' ? '登录已结束，请重新登录。' : ''
  }
})
async function restore() {
  if (restoring.value || auth.state.pending) return
  // 创建会话使用 replaceState，不触发 hashchange；重新挂载前读取当前地址。
  route.value = location.hash
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
    <header class="masthead">
      <a class="brand" href="/">思弈阁 <span>Lite</span></a>
      <div v-if="auth.state.status === 'authenticated'" class="account">
        <span>{{ auth.state.user?.username }}</span>
        <Button :disabled="auth.state.pending || restoring" @click="logout">退出登录</Button>
      </div>
      <span v-else class="muted">文字辩论练习</span>
    </header>
    <main>
      <div class="intro"><p class="eyebrow">思考 · 表达 · 回应</p><h1>让观点经得起交锋。</h1>
        <p>选择一个辩题，从第一轮表达开始练习。</p>
      </div>
      <p v-if="message" class="page-feedback" role="alert">{{ message }}</p>
      <section v-if="restoring || auth.state.status === 'unknown'" class="card" :aria-busy="restoring" aria-live="polite">
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
        <FirstTurnPanel :key="`${auth.state.user?.id}:${route}:${panelVersion}`" :auth="auth" :route="route" @new-practice="newPractice" />
      </template>
      <p class="scope-note">AI 生成内容仅供练习参考，请勿输入个人敏感信息。支持三轮文字辩论与结构化复盘总结。</p>
    </main>
    <footer>思弈阁 Lite · 让每一次表达，都有进步。</footer>
  </div>
</template>

<style>
:root { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif; color: #172b3a; background: #f6f8fa; font-synthesis: none; }
* { box-sizing: border-box; }
body { margin: 0; min-width: 320px; }
.page { max-width: 1040px; min-height: 100vh; margin: auto; padding: 32px; display: flex; flex-direction: column; }
.masthead, .account { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.brand { color: #172b3a; text-decoration: none; font-size: 22px; font-weight: 700; letter-spacing: 1px; }
.brand span, .muted { color: #526578; font-size: 14px; font-weight: 400; }
main { width: 100%; max-width: 760px; margin: 48px auto; }
.intro { margin-bottom: 32px; }
.eyebrow { color: #235b81; font-size: 13px; font-weight: 600; letter-spacing: 3px; }
h1 { margin: 12px 0; font-size: clamp(28px, 5vw, 38px); line-height: 1.4; }
h2 { margin-top: 0; font-size: 21px; }
p { line-height: 1.8; }
.intro > p:last-child, .scope-note { color: #526578; }
.card { padding: 28px; border: 1px solid #d8e0e8; border-radius: 16px; background: white; margin-bottom: 24px; }
.page-feedback { color: #9f2936; padding: 12px 16px; background: #fff0f0; border-radius: 8px; overflow-wrap: anywhere; }
.scope-note { font-size: 13px; margin-top: 24px; }
.restore-button { margin-bottom: 16px; }
button, select, textarea { font: inherit; }
.arco-btn, select { min-height: 44px; }
button, select { cursor: pointer; }
button:disabled, select:disabled, textarea:disabled { cursor: not-allowed; }
button:focus-visible, select:focus-visible, textarea:focus-visible, a:focus-visible { outline: 3px solid #235b81; outline-offset: 3px; }
footer { margin-top: auto; color: #526578; font-size: 12px; text-align: center; }
@media (max-width: 600px) { .page { padding: 24px 16px; } main { margin: 32px auto; } .card { padding: 22px 18px; } .account { gap: 8px; } }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; } }
</style>
