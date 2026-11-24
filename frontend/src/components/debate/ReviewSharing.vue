<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Button } from '@arco-design/web-vue'
import type { ApiClient } from '../../api/client'
import { changeReviewSharing, getReviewSharing, ownReportMarkdownURL, publicReportLink } from '../../api/publicReports'

const props = defineProps<{ client: ApiClient; debateId: string }>()
const shareId = ref<string | null>(null)
const busy = ref(false)
const ready = ref(false)
const note = ref('')
const dialogError = ref('')
const confirmDialog = ref<HTMLDialogElement | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let active = true

function ask() {
  confirmDialog.value?.showModal()
}

async function change(action: 'prompt' | 'publish' | 'withdraw', manual = false) {
  if (busy.value) return
  busy.value = true
  note.value = ''
  dialogError.value = ''
  try {
    const state = await changeReviewSharing(props.client, props.debateId, action)
    if (!active) return
    shareId.value = state.share_id
    if (state.show_prompt || (manual && !state.share_id)) ask()
    if (action !== 'prompt') {
      clearTimeout(timer)
      confirmDialog.value?.close()
      note.value = action === 'publish' ? '复盘已公开。' : '已撤回公开，原链接已失效。'
    }
  } catch {
    if (active) {
      const message = action === 'prompt' ? '暂时无法读取公开状态，可稍后手动重试。' : '操作未确认，请重新打开复盘查看公开状态。'
      if (confirmDialog.value?.open) dialogError.value = message
      else note.value = message
    }
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  try {
    const state = await getReviewSharing(props.client, props.debateId)
    if (!active) return
    shareId.value = state.share_id
    ready.value = true
    if (!state.prompt_handled) timer = setTimeout(() => { void change('prompt') }, 12_000)
  } catch {
    if (active) note.value = '暂时无法读取公开状态。'
  }
})
onBeforeUnmount(() => { active = false; clearTimeout(timer) })
</script>

<template>
  <section class="sharing" aria-label="复盘分享">
    <div class="sharing-actions">
      <strong>复盘分享</strong>
      <template v-if="ready">
        <span v-if="shareId">已公开</span>
        <span v-else>仅自己可见</span>
        <a v-if="shareId" :href="publicReportLink(shareId)" target="_blank" rel="noopener">查看公开复盘</a>
        <Button v-if="shareId" size="small" :disabled="busy" @click="change('withdraw')">撤回公开</Button>
        <Button v-else size="small" :disabled="busy" @click="change('prompt', true)">公开复盘</Button>
      </template>
      <a :href="ownReportMarkdownURL(debateId)" download="siyige-review.md">导出 Markdown</a>
    </div>
    <p v-if="note" class="muted" role="status">{{ note }}</p>
    <dialog ref="confirmDialog" aria-labelledby="share-confirm-title">
      <h3 id="share-confirm-title">公开这场复盘？</h3>
      <p>公开后，其他人可以阅读并导出这场的辩题、辩词、已保存的裁决、评语和批注。你随时可以撤回公开。</p>
      <p v-if="dialogError" class="muted" role="alert">{{ dialogError }}</p>
      <div class="sharing-actions">
        <Button autofocus :disabled="busy" @click="confirmDialog?.close()">保留私密</Button>
        <Button type="primary" :loading="busy" @click="change('publish')">确认公开</Button>
      </div>
    </dialog>
  </section>
</template>

<style scoped>
.sharing { margin: 16px 0; padding: 14px 16px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); background: var(--app-surface); }
.sharing-actions { display: flex; flex-wrap: wrap; gap: 10px 16px; align-items: center; }
dialog { width: min(430px, calc(100vw - 36px)); color: var(--app-text); background: var(--app-surface); border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); padding: 22px; }
dialog::backdrop { background: rgb(13 19 26 / 48%); }
dialog h3 { margin-top: 0; }
</style>
