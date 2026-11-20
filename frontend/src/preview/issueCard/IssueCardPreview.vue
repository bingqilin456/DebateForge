<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { Button } from '@arco-design/web-vue'
import IssueCardPanel from '../../components/debate/IssueCardPanel.vue'
import { locateQuoteText } from '../../shared/locateQuote'
import type { IssueCardPreviewMock } from './mockIssueCard'
import type { IssueCardContents } from '../../debate/issueCard'

/**
 * 争点卡隔离预览页：只在本机回环的独立 Vite 端口使用。
 *
 * 用途：在共享工作台/历史路由与后端装配尚未整合前，完整走一遍赛后行为——
 * 5 卡上限、双方引用点击定位、待核对降级、改名/合并/移除、刷新后读取、
 * 整理失败与版本冲突弱提示、越权 404。
 *
 * 这不是产品页面，也不代表生产持久化或真实模型已经接通。
 */

const props = defineProps<{ mock: IssueCardPreviewMock; integrated?: boolean }>()

const emit = defineEmits<{ selectSource: [source: { messageId: string; start: number; end: number; quote: string }] }>()
const scenario = ref('正常')
const transcript = ref<HTMLElement | null>(null)
const panel = ref<InstanceType<typeof IssueCardPanel> | null>(null)
const notice = ref('')
const located = ref<{ messageId: string; start: number; end: number } | null>(null)
// 可注入的二次确认边界：预览页用页面内弹层，避免依赖框架级弹窗与真实点击拦截。
const pendingConfirm = ref<{ title: string; content: string; resolve: (value: boolean) => void } | null>(null)

function confirmDialog(content: string, title: string): Promise<boolean> {
  return new Promise(resolve => { pendingConfirm.value = { title, content, resolve } })
}

function answerConfirm(value: boolean) {
  pendingConfirm.value?.resolve(value)
  pendingConfirm.value = null
}

const contents = computed<IssueCardContents>(() => props.mock.contents)
const messages = computed(() => props.mock.messages.map((message, index) => ({
  ...message,
  turn: Math.floor(index / 2) + 1,
})))

const scenarios = [
  { label: '正常', apply: () => { props.mock.behaviors.forbidden = false; props.mock.behaviors.failAnalyzeOnce = false; props.mock.behaviors.conflictEditOnce = false } },
  { label: '越权 404', apply: () => { props.mock.behaviors.forbidden = true } },
  { label: '整理失败一次', apply: () => { props.mock.behaviors.forbidden = false; props.mock.behaviors.failAnalyzeOnce = true } },
  { label: '编辑版本冲突一次', apply: () => { props.mock.behaviors.forbidden = false; props.mock.behaviors.conflictEditOnce = true } },
]

async function applyScenario(label: string, apply: () => void) {
  apply()
  scenario.value = label
  notice.value = ''
  await panel.value?.load()
}

/**
 * 引用点击 → 在发言流中定位已复核的码点区间。
 *
 * 定位/滚动/聚焦复用正式工作台的共享实现（shared/locateQuote）：整段原文承载
 * `data-located-quote`，语义与正式页一致；预览页只在 scoped CSS 里取消整段的可见外框，
 * 改由精确片段 `.located-fragment` 自己画一个 inline 外框——不框整条发言，也不逐字描边。
 * 定位失败时保持上一次成功的落点，只提示待核对；不猜位置，也不留一半亮一半灭的状态。
 */
const LOCATE_PENDING = '该引用在当前视图无法唯一定位，已按“原文待核对”处理。'

/**
 * 解析共用定位键 `message_id|start_cp|end_cp`。
 *
 * 只接受完整三段与规范十进制非负整数：`Number('')` 会得到 0，
 * 空段、缺段、带符号或前导零都不是后端/面板的规范写法，一律按“无法定位”处理。
 */
function parseLocateKey(key: string): { messageId: string; start: number; end: number } | null {
  const parts = key.split('|')
  if (parts.length !== 3) return null
  const [messageId, startRaw, endRaw] = parts
  if (!messageId) return null
  if (!/^(0|[1-9]\d*)$/.test(startRaw) || !/^(0|[1-9]\d*)$/.test(endRaw)) return null
  const start = Number(startRaw)
  const end = Number(endRaw)
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end)) return null
  return { messageId, start, end }
}

async function locate(key: string) {
  notice.value = ''
  const target = parseLocateKey(key)
  const hit = target ? locateQuoteText(transcript.value, target.messageId, target.start, target.end) : null
  if (!target || !hit) {
    // 共享定位失败时不会清除旧落点；这里只更新提示，避免“淡底没了、旧框还在”的半截状态。
    notice.value = LOCATE_PENDING
    return
  }
  located.value = target
  await nextTick()
}

function selectText(event: MouseEvent, message: typeof messages.value[number]) {
  if (message.role !== 'user') return
  const paragraph = (event.currentTarget as HTMLElement).closest('article')?.querySelector('.message-text')
  const selection = window.getSelection()
  const range = selection?.rangeCount === 1 ? selection.getRangeAt(0) : null
  if (!paragraph || !range || range.collapsed || !paragraph.contains(range.startContainer) || !paragraph.contains(range.endContainer)) {
    notice.value = '请先在这条发言中选一段原文。'
    return
  }
  const prefix = document.createRange()
  prefix.selectNodeContents(paragraph)
  prefix.setEnd(range.startContainer, range.startOffset)
  const start = Array.from(prefix.toString()).length
  const quote = range.toString()
  const end = start + Array.from(quote).length
  if (!/[^\p{White_Space}\ufeff]/u.test(quote) || Array.from(message.content).slice(start, end).join('') !== quote) {
    notice.value = '选中的内容未能与原文核对，请重新选一句。'
    return
  }
  notice.value = ''
  emit('selectSource', { messageId: message.id, start, end, quote })
}

defineExpose({ locate })

type CharNode = { index: number; char: string }
type MessageRun = { key: string; located: boolean; chars: CharNode[] }

/**
 * 把一条发言拆成“普通片段 / 已核验片段”。
 *
 * 只有已核验的码点区间才组成一个独立的外层片段，整条发言不会被包进定位框；
 * 片段内部仍保留逐字节点，坐标、选段与回读校验都沿用原来的码点口径。
 */
function runsOf(message: { id: string; content: string }): MessageRun[] {
  const points = Array.from(message.content)
  const current = located.value
  if (!current || current.messageId !== message.id) {
    return [{ key: `${message.id}-all`, located: false, chars: points.map((char, index) => ({ index, char })) }]
  }
  const runs: MessageRun[] = []
  if (current.start > 0) {
    runs.push({
      key: `${message.id}-before`, located: false,
      chars: points.slice(0, current.start).map((char, offset) => ({ index: offset, char })),
    })
  }
  runs.push({
    key: `${message.id}-located`, located: true,
    chars: points.slice(current.start, current.end).map((char, offset) => ({ index: current.start + offset, char })),
  })
  if (current.end < points.length) {
    runs.push({
      key: `${message.id}-after`, located: false,
      chars: points.slice(current.end).map((char, offset) => ({ index: current.end + offset, char })),
    })
  }
  return runs
}
</script>

<template>
  <div class="preview">
    <header class="preview-head">
      <h1>{{ integrated ? '四轮练习 · 赛后回看' : '争点卡隔离 Mock 预览' }}</h1>
      <p v-if="integrated" class="muted">已完赛 · 四轮 · 本地示例。反馈为演示内容，不会发送给 AI。</p>
      <p v-else class="muted">
        使用本地示例，不访问后端或真实模型；本标签页刷新后保留修改。
        仅供 V2-06/V2-07 赛后行为的界面验证，不代表共享路由或生产持久化已接通。
      </p>
      <div v-if="!integrated" class="scenarios">
        <Button
          v-for="entry in scenarios"
          :key="entry.label"
          size="small"
          :type="scenario === entry.label ? 'primary' : 'secondary'"
          :data-preview-scenario="entry.label"
          @click="applyScenario(entry.label, entry.apply)"
        >{{ entry.label }}</Button>
        <Button size="small" data-preview-reset @click="applyScenario('正常', () => mock.reset())">重置夹具</Button>
      </div>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    </header>

    <div class="preview-body">
      <section ref="transcript" class="card transcript" aria-label="已完赛四轮发言（夹具）">
        <h2>已保存的双方发言</h2>
        <p class="muted">{{ integrated ? '可以选中自己的发言，改写其中一句。争点卡和复盘都能带你回到相应的原文。' : '本场为已完赛的四轮基础练习（standard_4）夹具；争点卡只读取这些已保存原文。' }}</p>
        <article v-for="message in messages" :key="message.id" :class="['message', message.role]" :data-message-id="message.id">
          <h3>第 {{ message.turn }} 轮 · {{ message.role === 'user' ? '你' : 'AI' }}</h3>
          <p class="message-text" data-message-content tabindex="-1">
            <!-- 整段原文保留 data-located-quote 语义（共享定位据此滚动/聚焦），
                 但预览页会取消它的可见外框；只有下面的精确片段画一个 inline 外框。
                 内部逐字节点仅用于码点坐标，不描边、不加定位 class。 -->
            <template v-for="run in runsOf(message)" :key="run.key">
              <span
                v-if="run.located"
                class="message-run located-fragment"
                :data-located-run="run.chars[0]?.index"
                :data-located-message="message.id"
              >
                <span
                  v-for="point in run.chars"
                  :key="`${message.id}-${point.index}`"
                  :data-preview-message="message.id"
                  :data-preview-index="point.index"
                >{{ point.char }}</span>
              </span>
              <template v-else>
                <span
                  v-for="point in run.chars"
                  :key="`${message.id}-${point.index}`"
                  :data-preview-message="message.id"
                  :data-preview-index="point.index"
                >{{ point.char }}</span>
              </template>
            </template>
          </p>
          <div v-if="integrated && message.role === 'user'" class="practice-actions">
            <Button size="small" @mousedown.prevent @click="selectText($event, message)">选句重练</Button>
            <Button size="small" type="text" @click="emit('selectSource', { messageId: message.id, start: 0, end: Array.from(message.content).length, quote: message.content })">重练整条</Button>
          </div>
        </article>
      </section>

      <IssueCardPanel
        ref="panel"
        class="card panel"
        :api="mock.api"
        :debate-id="'9001'"
        :contents="contents"
        :completed="true"
        :confirm="confirmDialog"
        @locate="locate"
      />
    </div>

    <slot name="after" />

    <!-- 页面内二次确认：预览页不依赖框架弹窗，行为与正式面板一致。 -->
    <section v-if="pendingConfirm" class="confirm-backdrop" role="dialog" aria-modal="true" aria-label="确认操作" data-preview-confirm>
      <div class="confirm-box">
        <h2>{{ pendingConfirm.title }}</h2>
        <p>{{ pendingConfirm.content }}</p>
        <div class="scenarios">
          <Button size="small" type="primary" data-preview-confirm-ok @click="answerConfirm(true)">确认</Button>
          <Button size="small" data-preview-confirm-cancel @click="answerConfirm(false)">取消</Button>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.preview { display: grid; gap: 18px; }
.preview-head h1 { margin: 0 0 8px; font-size: 20px; }
.muted { color: var(--app-text-muted); font-size: 13px; }
.scenarios { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0; }
.notice { color: var(--app-warning-text, #a86b00); font-size: 13px; }
.preview-body { display: grid; gap: 18px; }
.card { padding: 18px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-surface); }
.message { margin: 14px 0; padding: 14px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); background: var(--app-background); }
.message.assistant { background: var(--app-info-bg); }
.message h3 { margin: 0 0 6px; font-size: 13px; }
.message-text { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 14px; line-height: 1.8; }
.practice-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
/* 预览页取消整段原文的可见定位外框：`data-located-quote` 只保留语义，
   用于共享定位的滚动/聚焦与“当前落点”判定；视觉只落在精确片段上。
   选择器带 `.message-text` 与 scoped 属性，稳定压过 styles/theme.css 的全局外框定义。 */
.message-text[data-located-quote] { outline: none; }
/* 已核验引用片段：只在这段外层画一个 inline 外框。跨行时浏览器按行分段，
   每行各自成框（box-decoration-break: clone），不会框住整条发言，也不给单个字描边。 */
.located-fragment {
  background: #fff3bf;
  border: 2px solid var(--app-accent);
  border-radius: 3px;
  box-decoration-break: clone;
  -webkit-box-decoration-break: clone;
}
.confirm-backdrop {
  position: fixed; inset: 0; display: grid; place-items: center;
  background: rgba(20, 24, 32, 0.45); z-index: 10;
}
.confirm-box {
  max-width: 480px; padding: 20px; border-radius: var(--app-radius-panel);
  background: var(--app-surface); border: 1px solid var(--app-border);
}
.confirm-box h2 { margin: 0 0 8px; font-size: 16px; }
.confirm-box p { margin: 0 0 12px; font-size: 13px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
@media (min-width: 1000px) {
  .preview-body { grid-template-columns: minmax(0, 1fr) minmax(320px, 34%); align-items: start; }
}
</style>
