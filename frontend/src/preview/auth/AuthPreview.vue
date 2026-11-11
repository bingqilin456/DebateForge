<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import LoginForm from '../../components/auth/LoginForm.vue'
import RegisterForm from '../../components/auth/RegisterForm.vue'
import { createAuthState } from '../../auth/state'
import { ApiError } from '../../api/client'
import type { PreviewAuthMock } from './mockAuth'

const props = defineProps<{ mock: PreviewAuthMock }>()
const auth = createAuthState()
const mode = ref<'login' | 'register'>('login')
const ready = ref(false)
const notice = ref('')
const formVersion = ref(0)
const busy = computed(() => props.mock.state.pending > 0 || auth.state.pending)
const statusLabel = computed(() => auth.state.status === 'authenticated'
  ? `已登录（模拟）：${auth.state.user?.username}` : auth.state.status === 'anonymous' ? '未登录（模拟）' : '恢复中（模拟）')
function switchMode(next: 'login' | 'register') { mode.value = next; notice.value = '' }
function reload() { window.location.reload() }
async function logout() {
  try {
    await auth.logout()
    formVersion.value++
    notice.value = '模拟会话已退出；没有操作真实 Session Cookie。'
  } catch (error) { notice.value = error instanceof ApiError ? error.message : '模拟退出失败。' }
}
onMounted(async () => {
  try { await auth.restore() }
  catch (error) { notice.value = error instanceof ApiError ? error.message : '模拟状态恢复失败。' }
  finally { ready.value = true }
})
</script>

<template>
  <main class="preview-page">
    <header>
      <p class="eyebrow">T06-B · LOCAL PREVIEW</p>
      <h1>认证表单交互预览</h1>
      <p class="mock-warning" role="note"><strong>仅本地模拟，不是真实注册登录，也不是真实后端联调。</strong><br>所有认证响应由本页面内存模拟；刷新会清空模拟会话。请勿输入真实密码。</p>
    </header>
    <div class="preview-grid">
      <section aria-label="复用现有认证表单" class="form-area">
        <p v-if="!ready" role="status">正在初始化模拟预览…</p>
        <LoginForm v-else-if="mode === 'login'" :key="`login-${formVersion}`" :auth="auth"
          @show-register="switchMode('register')" @authenticated="notice = '收到 authenticated 事件（模拟），现有内存认证状态已更新。'" />
        <RegisterForm v-else :key="`register-${formVersion}`" :auth="auth"
          @show-login="switchMode('login')" @registered="notice = '只收到 registered 事件（模拟）；没有自动调用登录接口。请点击去登录。'" />
        <p v-if="notice" class="preview-notice" aria-live="polite">{{ notice }}</p>
      </section>
      <aside class="preview-tools" aria-label="模拟场景与诊断">
        <h2>模拟控制台</h2>
        <p class="sample">示例输入（非真实账号）：<br><code>preview_user</code> / <code>preview-pass-123</code><br>“模拟成功”接受任意通过前端校验的输入，不校验真实口令。</p>
        <div class="tool-field">
          <label for="preview-scenario">模拟响应</label>
          <select id="preview-scenario" v-model="mock.controls.scenario" :disabled="busy">
            <option value="success">模拟成功</option>
            <option value="rejected">登录 401 / 注册 409</option>
            <option value="rate_limited">429 频率限制</option>
            <option value="unavailable">503 服务不可用</option>
            <option value="network">模拟网络失败</option>
          </select>
        </div>
        <div class="tool-field">
          <label for="preview-delay">模拟延迟</label>
          <select id="preview-delay" v-model.number="mock.controls.delayMs" :disabled="busy">
            <option :value="0">立即响应</option>
            <option :value="1500">1.5 秒（观察等待与防重复）</option>
            <option :value="6000">6 秒（触发 client 的 5 秒超时）</option>
          </select>
        </div>
        <p><strong>认证状态：</strong><output data-testid="auth-status">{{ statusLabel }}</output></p>
        <dl class="counts">
          <div><dt>注册次数</dt><dd data-testid="count-register">{{ mock.state.counts.register }}</dd></div>
          <div><dt>登录次数</dt><dd data-testid="count-login">{{ mock.state.counts.login }}</dd></div>
          <div><dt>状态查询次数</dt><dd data-testid="count-me">{{ mock.state.counts.me }}</dd></div>
          <div><dt>退出次数</dt><dd data-testid="count-logout">{{ mock.state.counts.logout }}</dd></div>
        </dl>
        <p class="request-status"><code>{{ mock.state.lastRequest }}</code><br>结果：{{ mock.state.pending ? '等待中（模拟）' : mock.state.lastStatus ?? '尚无响应' }}</p>
        <div class="preview-actions">
          <button class="preview-action" type="button" :disabled="busy || !auth.state.user" @click="logout">退出模拟会话</button>
          <button class="preview-action" type="button" :disabled="busy" @click="reload">重置预览</button>
        </div>
        <p class="privacy-note">诊断只显示方法、路径、结果和次数，不显示密码、请求正文或 token。本页不发送真实认证请求，不写 Cookie 或浏览器持久化存储。</p>
      </aside>
    </div>
  </main>
</template>

<style>
body { margin: 0; background: #f6f8fa; color: #172b3a; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif; }
* { box-sizing: border-box; }
</style>
<style scoped>
.preview-page { max-width: 1040px; margin: 0 auto; padding: 32px 24px 48px; }
.eyebrow { color: #235b81; letter-spacing: 2px; font-weight: 700; font-size: 12px; }
h1 { margin: 12px 0 20px; font-size: 30px; }
.mock-warning { padding: 16px 20px; border: 1px solid #e5b54b; border-radius: 10px; background: #fff8e6; color: #6b4a00; line-height: 1.9; }
.preview-grid { display: grid; grid-template-columns: minmax(0, 440px) minmax(0, 1fr); gap: 28px; align-items: start; margin-top: 28px; }
.preview-tools { padding: 24px; border: 1px solid #e3e8ee; border-radius: 16px; background: #fff; }
h2 { margin: 0 0 16px; font-size: 18px; }
.sample, .privacy-note, .request-status, .preview-notice { font-size: 13px; line-height: 1.8; overflow-wrap: anywhere; }
.sample, .privacy-note { color: #526578; }
.tool-field { display: grid; gap: 8px; margin: 18px 0; }
.tool-field label { font-weight: 600; }
select, .preview-action { min-height: 40px; font: inherit; border: 1px solid #ccd5df; border-radius: 6px; padding: 8px; background: #fff; color: #172b3a; }
select { width: 100%; }
.counts { padding: 8px 0; border-block: 1px solid #e3e8ee; }
.counts div { display: flex; justify-content: space-between; margin: 10px 0; }
.counts dd { margin: 0; font-variant-numeric: tabular-nums; }
.preview-actions { display: flex; flex-wrap: wrap; gap: 10px; }
.preview-action:not(:disabled) { cursor: pointer; }
.preview-action:disabled { opacity: 0.55; }
.privacy-note { margin-bottom: 0; }
@media (max-width: 720px) { .preview-page { padding: 20px 16px 32px; } .preview-grid { grid-template-columns: minmax(0, 1fr); gap: 20px; } h1 { font-size: 25px; } }
</style>
