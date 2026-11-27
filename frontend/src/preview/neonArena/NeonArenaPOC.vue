<script setup lang="ts">
import { computed, ref } from 'vue'

type Rail = 'coach' | 'clash' | 'stream'

const rail = ref<Rail>('coach')
const timerRunning = ref(false)
const seconds = ref(120)
const draft = ref('')
const sent = ref(false)

const rounds = ['一辩立论', '二辩攻辩', '自由交锋', '四辩结辩']
const timerText = computed(() => `${String(Math.floor(seconds.value / 60)).padStart(2, '0')}:${String(seconds.value % 60).padStart(2, '0')}`)

function toggleTimer() {
  timerRunning.value = !timerRunning.value
}

function resetTimer() {
  timerRunning.value = false
  seconds.value = 120
}

function fillStrong() {
  draft.value = '我方认为，批判性思维的核心是提出问题、审视证据并作出独立判断。生成式 AI 可以承担信息初筛与结构整理，把时间还给更高阶的分析与判断。'
}

function submit() {
  if (!draft.value.trim()) return
  sent.value = true
}
</script>

<template>
  <div class="poc-page">
    <header class="poc-header">
      <div class="poc-header-inner">
        <div class="poc-brand">
          <div class="poc-seal">弈</div>
          <div class="poc-brand-copy">
            <div><strong>思弈阁</strong><span>DebateForge</span></div>
            <small>AI 辩论训练与逻辑复盘平台</small>
          </div>
        </div>
        <nav class="poc-nav" aria-label="Neon 页面导航">
          <button class="active">⌁ 竞技场</button>
          <button>◉ 辩题与杯赛</button>
          <button>▤ 深度复盘</button>
          <button>♢ 思辨道场</button>
          <button>◌ 个人中心</button>
        </nav>
        <div class="poc-actions">
          <span class="poc-login">♙ 登录 / 注册</span>
          <span class="poc-elo">♧ ELO 1420</span>
          <button class="poc-icon" aria-label="音效">◖</button>
          <button class="poc-model"><i></i>接入真实模型</button>
          <button class="poc-showcase">✦ 看完整复盘示例</button>
        </div>
      </div>
    </header>

    <main class="poc-main">
      <section class="poc-match-card">
        <div class="poc-match-top">
          <div class="poc-match-title">
            <div class="poc-tags">
              <span class="gold">思弈杯 · 复赛（校队）</span>
              <span>科技伦理</span>
              <span class="pro">我方：正方</span>
              <span class="con">AI 反方：苏格拉底·诘问型</span>
            </div>
            <h1>生成式 AI 普及对大学生批判性思维利大于弊</h1>
          </div>
          <div class="poc-match-actions">
            <div class="poc-timer-control"><span>◷</span><strong>{{ timerText }}</strong><button @click="toggleTimer">{{ timerRunning ? 'Ⅱ' : '▷' }}</button><button @click="resetTimer">↻</button></div>
            <button class="poc-plain-button">⌘ 换辩题</button>
            <button class="poc-plain-button">⟳ 重开本局</button>
          </div>
        </div>
        <div class="poc-step-row">
          <div class="poc-stepper">
            <div v-for="(round, index) in rounds" :key="round" class="poc-step" :class="{ current: index === 0 }"><b>{{ index + 1 }}</b>{{ round }}</div>
          </div>
          <div class="poc-momentum"><div><strong>正方气势 50%</strong><span>实时说服力博弈</span><strong>50% 反方气势</strong></div><div class="poc-momentum-bar"><i></i><i></i></div></div>
        </div>
      </section>

      <section class="poc-workspace">
        <article class="poc-stage">
          <div class="poc-stage-head"><div><span class="live-dot"></span>当前环节：第一轮 · 一辩立论（等待我方发言）</div><div class="poc-legend"><span><i class="jade"></i>我方亮点</span><span><i class="red"></i>我方漏洞</span><span><i class="gold-line"></i>论据补强</span><span><i class="violet"></i>敌方破绽</span><span><i class="blue"></i>敌方杀招</span></div></div>
          <div class="poc-feed">
            <div class="poc-chair"><b>⚑ 主席台 · 赛制引导</b>　欢迎进入思弈阁四轮模拟辩论赛！本场辩题为《生成式 AI 普及对大学生批判性思维利大于弊》。你代表「正方」，对手为「苏格拉底·诘问型」。</div>
            <div v-if="sent" class="poc-message user"><strong>正方</strong><p>{{ draft }}</p></div>
            <div v-else class="poc-empty"><span>准备好了吗？</span><p>从定义“批判性思维”开始，写下你的第一轮立论。</p></div>
          </div>
          <div class="poc-composer">
            <div class="poc-quick-row"><span>队友/辅助：</span><button class="blue-button">♧ AI 队友代打本轮 <em>AI</em></button><button>▤ 三段论框架 <em>AI</em></button><span class="separator"></span><button class="jade-button" @click="fillStrong">✦ 预设高分辩词</button><button class="red-button">♨ 预设漏洞辩词（测排雷）</button></div>
            <div class="poc-input-row"><div class="poc-textarea-wrap"><textarea v-model="draft" rows="3" placeholder="输入或语音口述你的本轮辩词（支持 Ctrl+Enter 发送），也可点击上方按钮一键填入示范论点..."></textarea><div class="poc-input-meta"><span>♩ 语音输入</span><span>{{ [...draft].length }} 字</span></div></div><button class="poc-send" @click="submit">⌁ <span>发送交锋</span></button></div>
          </div>
        </article>

        <aside class="poc-rail">
          <div class="poc-rail-tabs"><button :class="{ active: rail === 'coach' }" @click="rail = 'coach'">♙ AI 教练</button><button :class="{ active: rail === 'clash' }" @click="rail = 'clash'">⌘ 交锋图谱</button><button :class="{ active: rail === 'stream' }" @click="rail = 'stream'">⌁ AI 进度</button></div>
          <div v-if="rail === 'coach'" class="poc-rail-body"><section class="poc-annotation"><div><b>♧ 行内精准批注</b><span>0 条批注</span></div><p>发送第一轮发言后，系统会自动对双方辩词进行逐句逻辑扫描。点击辩词中带下划线的片段，可在此查看精准诊断与改写建议。</p></section><details class="poc-tactical"><summary>♧ 本轮攻防锦囊 <span>点击展开（防剧透）</span></summary><p>先明确评价标准，再把 AI 的帮助落到具体学习任务上。</p></details><div class="poc-rail-section"><h3>本场批注索引</h3><p>暂无批注记录</p></div></div>
          <div v-else-if="rail === 'clash'" class="poc-rail-body"><section class="poc-annotation"><b>交锋图谱</b><p>首轮发言后，双方的论点关系会在这里展开。</p></section></div>
          <div v-else class="poc-rail-body"><section class="poc-annotation"><b>AI 进度</b><p>浏览器 → 服务端 SSE → 陪练模型</p><p>本轮尚未收到回复片段。</p></section></div>
        </aside>
      </section>
      <p class="poc-note">Visual POC · 结构直接迁移自 Neon 交付包 view-arena，当前使用模拟数据验证首屏视觉。</p>
    </main>
  </div>
</template>

<style scoped>
:global(body) { margin: 0; background: #f7f6f2; color: #18181b; }
.poc-page { min-height: 100vh; font-family: Inter, -apple-system, BlinkMacSystemFont, "Microsoft YaHei", sans-serif; background: #f7f6f2; }
.poc-header { position: sticky; top: 0; z-index: 4; background: rgba(255,255,255,.92); border-bottom: 1px solid #e4e0da; backdrop-filter: blur(14px); }
.poc-header-inner { width: min(1280px, calc(100% - 32px)); min-height: 57px; margin: auto; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.poc-brand { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }
.poc-seal { display: grid; place-items: center; width: 32px; height: 32px; border-radius: 9px; color: #fff; background: #c9362b; font: 700 17px Georgia, serif; }
.poc-brand-copy strong { font: 700 16px Georgia, serif; letter-spacing: .04em; }
.poc-brand-copy span { margin-left: 6px; padding: 2px 5px; border-radius: 5px; color: #6a625b; background: #f1efec; border: 1px solid #e5e0da; font-size: 10px; }
.poc-brand-copy small { display: block; margin-top: 2px; color: #857a71; font-size: 11px; }
.poc-nav { display: flex; gap: 2px; padding: 4px; border: 1px solid #e5e0da; border-radius: 12px; background: #f4f2ee; }
.poc-nav button, .poc-plain-button, .poc-login, .poc-model, .poc-showcase { border: 0; border-radius: 8px; background: transparent; color: #655b53; font-size: 12px; white-space: nowrap; }
.poc-nav button { min-height: 34px; padding: 7px 10px; }
.poc-nav button.active { color: #18181b; background: #fff; font-weight: 700; box-shadow: 0 1px 3px #18181b22; }
.poc-actions { display: flex; align-items: center; gap: 7px; flex-shrink: 0; }
.poc-login, .poc-model, .poc-showcase { padding: 7px 10px; background: #f5f3f0; border: 1px solid #e5e0da; font-weight: 600; }
.poc-elo { padding: 6px 9px; border: 1px solid #ead6a5; border-radius: 9px; color: #795a1e; background: #fff8e8; font-size: 12px; font-weight: 700; }
.poc-icon { border: 0; background: transparent; color: #756b64; font-size: 17px; }
.poc-model { background: #fff; color: #655b53; }
.poc-model i { display: inline-block; width: 7px; height: 7px; margin-right: 5px; border-radius: 50%; background: #e5aa24; }
.poc-showcase { color: #fff; background: #18181b; border-color: #18181b; }
.poc-main { width: min(1280px, calc(100% - 32px)); margin: 18px auto 34px; }
.poc-match-card, .poc-stage, .poc-rail { border: 1px solid #e5e0d9; border-radius: 16px; background: #fff; box-shadow: 0 5px 18px rgba(55,45,33,.05); }
.poc-match-card { padding: 16px; }
.poc-match-top { display: flex; justify-content: space-between; align-items: flex-start; gap: 18px; }
.poc-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.poc-tags span { padding: 4px 8px; border: 1px solid #e5e0da; border-radius: 7px; color: #6a625b; background: #f7f5f2; font-size: 11px; font-weight: 600; }
.poc-tags span.gold { color: #82601f; border-color: #ead6a5; background: #fff8e8; }
.poc-tags span.pro { color: #a13a30; border-color: #efc8c2; background: #fff3f1; }
.poc-tags span.con { color: #275fa5; border-color: #c7d9f1; background: #f0f6ff; }
.poc-match-title h1 { margin: 0; font: 700 21px/1.35 Georgia, serif; }
.poc-match-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 7px; }
.poc-timer-control { display: flex; align-items: center; gap: 6px; padding: 6px 9px; border: 1px solid #e5e0da; border-radius: 10px; background: #faf9f7; }
.poc-timer-control strong { min-width: 42px; text-align: center; font: 700 13px ui-monospace, monospace; }
.poc-timer-control button { padding: 3px 5px; border: 0; border-radius: 6px; color: #756b64; background: transparent; }
.poc-timer-control button:hover, .poc-plain-button:hover { background: #eeeae5; }
.poc-plain-button { padding: 7px 10px; border: 1px solid #e5e0da; background: #fff; }
.poc-step-row { display: grid; grid-template-columns: minmax(0, 7fr) minmax(280px, 5fr); gap: 14px; align-items: center; margin-top: 15px; padding-top: 13px; border-top: 1px solid #eeeae5; }
.poc-stepper { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 5px; }
.poc-step { display: flex; align-items: center; justify-content: center; gap: 6px; min-height: 34px; padding: 6px; border: 1px solid #e5e0da; border-radius: 8px; color: #81776f; background: #faf8f5; font-size: 12px; white-space: nowrap; }
.poc-step b { display: grid; place-items: center; width: 17px; height: 17px; border-radius: 50%; background: #eeeae5; font-size: 10px; }
.poc-step.current { color: #fff; border-color: #18181b; background: #18181b; font-weight: 700; }
.poc-step.current b { color: #18181b; background: #fff; }
.poc-momentum > div:first-child { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 6px; font-size: 11px; }
.poc-momentum strong:first-child { color: #a13a30; }.poc-momentum strong:last-child { color: #275fa5; }.poc-momentum span { color: #9a9088; }
.poc-momentum-bar { display: flex; height: 9px; overflow: hidden; border-radius: 999px; background: #e5e0da; box-shadow: inset 0 1px 2px #18181b20; }.poc-momentum-bar i:first-child { width: 50%; background: #d24c43; }.poc-momentum-bar i:last-child { width: 50%; background: #5188cc; }
.poc-workspace { display: grid; grid-template-columns: minmax(0, 8fr) minmax(300px, 4fr); gap: 16px; margin-top: 16px; align-items: start; }
.poc-stage { min-height: 560px; overflow: hidden; display: flex; flex-direction: column; }
.poc-stage-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; border-bottom: 1px solid #eeeae5; background: #faf9f7; color: #736a63; font-size: 11px; }.live-dot { display: inline-block; width: 7px; height: 7px; margin-right: 6px; border-radius: 50%; background: #18a66f; }
.poc-legend { display: flex; flex-wrap: wrap; gap: 8px; }.poc-legend span { white-space: nowrap; }.poc-legend i { display: inline-block; width: 10px; height: 5px; margin-right: 3px; border-radius: 2px; }.poc-legend i.jade { background:#2d9e78; }.poc-legend i.red { background:#c9362b; }.poc-legend i.gold-line { background:#d5a32d; }.poc-legend i.violet { background:#7c4db2; }.poc-legend i.blue { background:#316eb8; }
.poc-feed { flex: 1; min-height: 390px; padding: 16px; }.poc-chair { padding: 13px 14px; border: 1px solid #eee5db; border-radius: 10px; color: #55483e; background: #faf7f3; line-height: 1.75; }.poc-chair b { color: #8d3e2c; }.poc-empty { display: grid; place-items: center; min-height: 240px; color: #9a9088; text-align: center; }.poc-empty span { color: #655b53; font: 700 18px Georgia, serif; }.poc-empty p { margin-top: 8px; font-size: 13px; }
.poc-message { max-width: 82%; margin-top: 16px; padding: 13px 15px; border: 1px solid #e3d9cf; border-radius: 10px; line-height: 1.8; }.poc-message.user { border-inline-start: 3px solid #c9362b; background: #fff4f2; }.poc-message strong { color: #a13a30; }.poc-message p { margin: 4px 0 0; }
.poc-composer { padding: 13px; border-top: 1px solid #e5e0da; background: #faf9f7; }.poc-quick-row { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; margin-bottom: 10px; color: #847a72; font-size: 11px; }.poc-quick-row button { padding: 5px 8px; border: 1px solid #e5e0da; border-radius: 7px; color: #655b53; background:#fff; font-size: 11px; }.poc-quick-row em { padding: 1px 3px; border-radius: 3px; background: #eef5ff; color: #316eb8; font-size: 9px; font-style: normal; }.poc-quick-row .blue-button { color:#275fa5; background:#eff6ff; border-color:#c7d9f1; }.poc-quick-row .jade-button { color:#287456; background:#eff9f3; border-color:#c7e6d3; }.poc-quick-row .red-button { color:#a13a30; background:#fff3f1; border-color:#efc8c2; }.separator { width:1px; height:18px; margin:0 2px; background:#d9d2cb; }
.poc-input-row { display: flex; align-items: stretch; gap: 10px; }.poc-textarea-wrap { position: relative; flex:1; }.poc-textarea-wrap textarea { display:block; width:100%; min-height:86px; padding:10px 12px 28px; border:1px solid #d2cbc4; border-radius:10px; resize:none; background:#fff; color:#3e3732; font:13px/1.7 inherit; }.poc-input-meta { position:absolute; bottom:7px; left:11px; right:11px; display:flex; justify-content:space-between; color:#9a9088; font-size:10px; pointer-events:none; }.poc-send { min-width:122px; border:1px solid #c9362b; border-radius:10px; color:#fff; background:#c9362b; font-weight:700; box-shadow:0 6px 14px #c9362b2e; }.poc-send:hover { background:#ad2d24; }
.poc-rail { overflow: hidden; }.poc-rail-tabs { display:grid; grid-template-columns:repeat(3,1fr); gap:4px; padding:5px; border-bottom:1px solid #e5e0da; background:#faf9f7; }.poc-rail-tabs button { min-height:35px; border:0; border-radius:8px; color:#746a62; background:transparent; font-size:11px; font-weight:600; }.poc-rail-tabs button.active { color:#4f3b2d; background:#fff; box-shadow:0 1px 3px #18181b18; }.poc-rail-body { display:grid; gap:14px; padding:15px; }.poc-annotation { padding:13px; border:1px solid #e5e0da; border-radius:10px; background:#fffdfa; }.poc-annotation > div { display:flex; justify-content:space-between; gap:8px; }.poc-annotation b { color:#4d4037; font-size:13px; }.poc-annotation span { color:#9a9088; font-size:10px; }.poc-annotation p { margin:8px 0 0; color:#7e7269; font-size:12px; line-height:1.7; }.poc-tactical { border:1px solid #ead6a5; border-radius:10px; overflow:hidden; background:#fff8e8; }.poc-tactical summary { padding:11px 12px; color:#514034; cursor:pointer; font-size:12px; font-weight:700; }.poc-tactical summary span { float:right; color:#917448; font-size:10px; font-weight:400; }.poc-tactical p { margin:0; padding:0 12px 12px; color:#806646; font-size:12px; line-height:1.7; }.poc-rail-section { padding-top:3px; border-top:1px solid #eeeae5; }.poc-rail-section h3 { margin:0; font:700 13px Georgia,serif; }.poc-rail-section p { color:#9a9088; font-size:12px; }
.poc-note { margin:14px 0 0; color:#958b83; font-size:11px; }
@media (max-width: 1120px) { .poc-nav button:nth-child(n+4), .poc-showcase { display:none; } }
@media (max-width: 980px) { .poc-workspace { grid-template-columns:1fr; }.poc-rail { position:static; }.poc-stage-head { align-items:flex-start; flex-direction:column; } }
@media (max-width: 720px) { .poc-header-inner { flex-wrap:wrap; padding:8px 0; }.poc-nav { order:3; width:100%; overflow-x:auto; }.poc-actions { margin-left:auto; }.poc-brand-copy small, .poc-model { display:none; }.poc-main { width:min(100% - 24px, 680px); margin-top:12px; }.poc-match-top { flex-direction:column; }.poc-match-actions { justify-content:flex-start; }.poc-step-row { grid-template-columns:1fr; }.poc-stepper { grid-template-columns:repeat(2,1fr); }.poc-legend { gap:6px; }.poc-input-row { flex-direction:column; }.poc-send { min-height:44px; }.poc-message { max-width:100%; } }
</style>
