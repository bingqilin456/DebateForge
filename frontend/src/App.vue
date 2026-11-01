<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Alert, Button, Tag } from '@arco-design/web-vue'
import { ApiError } from './api/client'
import { getLiveHealth } from './api/health'

type CheckStatus = 'checking' | 'success' | 'failure'
const status = ref<CheckStatus>('checking')
const message = ref('正在连接本地服务，请稍候。')
const checkedAt = ref<string | null>(null)
let inFlight = false
const title = computed(() => ({ checking: '正在检查连接', success: '本地服务已连通', failure: '连接检查失败' })[status.value])

async function checkHealth() {
  // 请求完成前不允许重复检查；失败后只由用户明确触发重试。
  if (inFlight) return
  inFlight = true
  status.value = 'checking'
  message.value = '正在连接本地服务，请稍候。'
  try {
    await getLiveHealth()
    status.value = 'success'
    message.value = '浏览器已通过本地代理收到 Go 服务的存活响应。'
  } catch (error) {
    status.value = 'failure'
    message.value = error instanceof ApiError ? error.message : '检查失败，请稍后手动重试。'
  } finally {
    checkedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
    inFlight = false
  }
}

onMounted(checkHealth)
</script>

<template>
  <div class="page">
    <header class="masthead">
      <span class="brand">思弈阁 <span class="brand-lite">Lite</span></span>
      <Tag color="gray">工程初始化</Tag>
    </header>
    <main>
      <p class="eyebrow">PROJECT BOOTSTRAP</p>
      <h1>从一次连接开始。</h1>
      <p class="intro">确认前端与后端在本机正常通信，为后续开发做好准备。</p>

      <section class="status-card" aria-labelledby="check-heading" :aria-busy="status === 'checking'">
        <div class="card-heading">
          <h2 id="check-heading">本地联通验证</h2>
          <span class="local-label">LOCAL</span>
        </div>
        <div class="feedback" aria-live="polite" aria-atomic="true">
          <Alert :type="status === 'success' ? 'success' : status === 'failure' ? 'error' : 'info'" :title="title">
            {{ message }}
          </Alert>
        </div>
        <dl class="details">
          <div><dt>检查接口</dt><dd><code>GET /health/live</code></dd></div>
          <div><dt>最近检查</dt><dd>{{ checkedAt ?? '等待首次检查' }}</dd></div>
        </dl>
        <Button type="primary" size="large" long :loading="status === 'checking'" :disabled="status === 'checking'" @click="checkHealth">
          {{ status === 'checking' ? '检查中…' : status === 'failure' ? '重试连接' : '重新检查' }}
        </Button>
      </section>

      <p class="scope-note">本次仅验证本地服务存活。数据库、登录和 AI 辩论将在后续阶段逐步接入。</p>
    </main>
    <footer>思弈阁 Lite · 让每一次表达，都有进步。</footer>
  </div>
</template>

<style>
:root {
  --page-bg: #f6f8fa;
  --surface: #ffffff;
  --text: #172b3a;
  --muted: #526578;
  --border: #e3e8ee;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
  color: var(--text);
  background: var(--page-bg);
  font-synthesis: none;
}
* { box-sizing: border-box; }
body { margin: 0; min-width: 320px; }
.page { max-width: 1080px; min-height: 100vh; margin: 0 auto; padding: 32px; display: flex; flex-direction: column; }
.masthead { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.brand { font-size: 22px; font-weight: 700; letter-spacing: 1px; }
.brand-lite { color: var(--muted); font-size: 15px; font-weight: 500; letter-spacing: 0; }
main { width: 100%; max-width: 560px; margin: 80px auto 48px; }
.eyebrow { color: #235b81; font-size: 12px; font-weight: 700; letter-spacing: 2px; margin: 0 0 16px; }
h1 { margin: 0 0 16px; font-size: clamp(28px, 5vw, 40px); line-height: 1.35; letter-spacing: -1px; }
.intro { margin: 0 0 32px; color: var(--muted); font-size: 16px; line-height: 1.8; }
.status-card { background: var(--surface); border: 1px solid var(--border); border-radius: 16px; padding: 28px; box-shadow: 0 8px 30px #172b3a06; }
.card-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 24px; }
h2 { margin: 0; font-size: 18px; line-height: 1.5; }
.local-label { color: var(--muted); font-size: 11px; letter-spacing: 1px; }
.feedback { min-height: 90px; }
.details { margin: 24px 0; color: var(--muted); font-size: 14px; }
.details > div { display: flex; justify-content: space-between; gap: 12px; padding: 10px 0; }
.details dd { margin: 0; color: var(--text); text-align: right; }
code { font-size: 13px; overflow-wrap: anywhere; }
.status-card .arco-btn { min-height: 44px; border-radius: 8px; font-weight: 500; cursor: pointer; }
.status-card .arco-btn:disabled { cursor: wait; }
.arco-btn:focus-visible { outline: 3px solid #235b81; outline-offset: 3px; }
.scope-note { margin: 20px 8px 0; color: var(--muted); font-size: 13px; line-height: 1.8; }
footer { margin-top: auto; padding-top: 24px; text-align: center; color: var(--muted); font-size: 12px; line-height: 1.8; }
@media (max-width: 600px) {
  .page { padding: 24px 20px; }
  main { margin-top: 56px; }
  .status-card { padding: 22px 18px; }
}
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; }
}
</style>
